package powerlockprometheus

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/donomii/powerlock"
	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type pausingGauge struct {
	promclient.Gauge
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (g *pausingGauge) Set(value float64) {
	g.once.Do(func() {
		close(g.entered)
		<-g.release
	})
	g.Gauge.Set(value)
}

func TestObserver_RecordsAcquisitionAndState(t *testing.T) {
	registry := promclient.NewRegistry()
	observer, err := New(registry)
	if err != nil {
		t.Fatalf("expected observer registration, got %v", err)
	}
	m := observer.NewObservedRWMutex("cache")
	m.Lock()
	if got := testutil.ToFloat64(observer.held.WithLabelValues("cache", "write")); got != 1 {
		t.Fatalf("expected one held writer, got %v", got)
	}
	m.Unlock()
	if got := testutil.ToFloat64(observer.held.WithLabelValues("cache", "write")); got != 0 {
		t.Fatalf("expected no held writer, got %v", got)
	}
	if got := testutil.ToFloat64(observer.acquisitions.WithLabelValues("cache", "write", "acquired")); got != 1 {
		t.Fatalf("expected one acquired write, got %v", got)
	}
}

func TestObserver_RecordsContention(t *testing.T) {
	registry := promclient.NewRegistry()
	observer := MustNew(registry)
	m := observer.NewObservedRWMutex("cache")
	m.Lock()
	result := make(chan error, 1)
	go func() {
		err := m.RLockContext(context.Background())
		result <- err
		if err == nil {
			m.RUnlock()
		}
	}()
	waitForPrometheusState(t, m, func(state powerlock.LockState) bool {
		return state.WaitingReaders == 1
	})
	waitForMetricValue(t, func() float64 {
		return testutil.ToFloat64(observer.waiting.WithLabelValues("cache", "read"))
	}, 1)
	m.Unlock()
	if err := <-result; err != nil {
		t.Fatalf("expected reader acquisition, got %v", err)
	}
	if got := testutil.ToFloat64(observer.contentions.WithLabelValues("cache", "read")); got != 1 {
		t.Fatalf("expected one read contention, got %v", got)
	}
}

func waitForMetricValue(t *testing.T, value func() float64, expected float64) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		observed := value()
		if observed == expected {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("metric did not reach expected value: expected=%v observed=%v", expected, observed)
		default:
			runtime.Gosched()
		}
	}
}

func TestObserver_RejectsInvalidBucketsAndDuplicateRegistration(t *testing.T) {
	registry := promclient.NewRegistry()
	if _, err := New(nil); err == nil {
		t.Fatal("expected nil registerer error")
	}
	if _, err := NewWithBuckets(registry, nil, []float64{1}); err == nil {
		t.Fatal("expected empty wait bucket error")
	}
	if _, err := NewWithBuckets(registry, []float64{0}, []float64{1}); err == nil {
		t.Fatal("expected invalid wait bucket error")
	}
	if _, err := NewWithBuckets(registry, []float64{math.NaN()}, []float64{1}); err == nil {
		t.Fatal("expected non-finite wait bucket error")
	}
	if _, err := NewWithBuckets(registry, []float64{1, 1}, []float64{1}); err == nil {
		t.Fatal("expected non-increasing wait bucket error")
	}
	if _, err := NewWithBuckets(registry, []float64{1}, []float64{math.Inf(1)}); err == nil {
		t.Fatal("expected non-finite hold bucket error")
	}
	if _, err := New(registry); err != nil {
		t.Fatalf("expected first registration, got %v", err)
	}
	if _, err := New(registry); err == nil {
		t.Fatal("expected duplicate registration error")
	}
	expectObserverPanic(t, "registerer must not be nil", func() { MustNew(nil) })
}

func TestObserver_IgnoresDelayedStaleState(t *testing.T) {
	registry := promclient.NewRegistry()
	observer := MustNew(registry)
	observer.ObserveLock(powerlock.LockEvent{
		Name:   "cache",
		Mode:   powerlock.LockModeWrite,
		Kind:   powerlock.LockEventTryFailed,
		Result: powerlock.LockResultBusy,
		State:  powerlock.LockState{Name: "cache", Version: 2},
	})
	observer.ObserveLock(powerlock.LockEvent{
		Name:   "cache",
		Mode:   powerlock.LockModeWrite,
		Kind:   powerlock.LockEventTryFailed,
		Result: powerlock.LockResultBusy,
		State:  powerlock.LockState{Name: "cache", Version: 1, Writer: true},
	})
	if got := testutil.ToFloat64(observer.held.WithLabelValues("cache", "write")); got != 0 {
		t.Fatalf("expected stale state to be ignored, got held=%v", got)
	}
}

func TestObserver_SerializesVersionAndGaugeUpdate(t *testing.T) {
	held := &pausingGauge{
		Gauge:   promclient.NewGauge(promclient.GaugeOpts{Name: "serialized_held"}),
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	readMetrics := boundMetrics{
		waiting: promclient.NewGauge(promclient.GaugeOpts{Name: "serialized_read_waiting"}),
		held:    promclient.NewGauge(promclient.GaugeOpts{Name: "serialized_read_held"}),
	}
	writeMetrics := boundMetrics{
		waiting: promclient.NewGauge(promclient.GaugeOpts{Name: "serialized_write_waiting"}),
		held:    held,
	}
	observer := &Observer{versions: make(map[string]uint64)}
	firstDone := make(chan struct{})
	go func() {
		observer.updateState("cache", powerlock.LockState{Version: 1, Writer: true}, readMetrics, writeMetrics)
		close(firstDone)
	}()
	<-held.entered
	secondDone := make(chan struct{})
	go func() {
		observer.updateState("cache", powerlock.LockState{Version: 2}, readMetrics, writeMetrics)
		close(secondDone)
	}()
	assertChannelOpen(t, secondDone, "newer metric state completed while an older gauge update was paused")
	close(held.release)
	<-firstDone
	<-secondDone
	if got := testutil.ToFloat64(held); got != 0 {
		t.Fatalf("expected newest state to win, got held=%v", got)
	}
}

func waitForPrometheusState(t *testing.T, lock *powerlock.ObservedRWMutex, matches func(powerlock.LockState) bool) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		if matches(lock.Snapshot()) {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("lock state did not reach expected condition: %+v", lock.Snapshot())
		default:
			runtime.Gosched()
		}
	}
}

func assertChannelOpen(t *testing.T, channel <-chan struct{}, failure string) {
	t.Helper()
	timer := time.NewTimer(20 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-channel:
		t.Fatal(failure)
	case <-timer.C:
	}
}

func TestObserver_MapsEveryEventKindAndFailureResult(t *testing.T) {
	registry := promclient.NewRegistry()
	observer, err := NewWithBuckets(registry, []float64{1, 2}, []float64{1, 2})
	if err != nil {
		t.Fatalf("expected observer, got %v", err)
	}
	name := "event-matrix"
	observer.ObserveLock(powerlock.LockEvent{Name: name, Mode: powerlock.LockModeRead, Kind: powerlock.LockEventWaitStarted, Result: powerlock.LockResultBusy, Contended: true, State: powerlock.LockState{Name: name, Version: 1, WaitingReaders: 1}})
	observer.ObserveLock(powerlock.LockEvent{Name: name, Mode: powerlock.LockModeRead, Kind: powerlock.LockEventWaitExceeded, Result: powerlock.LockResultBusy, Contended: true, WaitDuration: 1500 * time.Millisecond, State: powerlock.LockState{Name: name, Version: 1, WaitingReaders: 1}})
	observer.ObserveLock(powerlock.LockEvent{Name: name, Mode: powerlock.LockModeRead, Kind: powerlock.LockEventAcquired, Result: powerlock.LockResultAcquired, Contended: true, WaitDuration: 1500 * time.Millisecond, State: powerlock.LockState{Name: name, Version: 2, Readers: 1}})
	observer.ObserveLock(powerlock.LockEvent{Name: name, Mode: powerlock.LockModeRead, Kind: powerlock.LockEventHoldExceeded, Result: powerlock.LockResultAcquired, HoldDuration: 1500 * time.Millisecond, HoldDurationKnown: true, State: powerlock.LockState{Name: name, Version: 2, Readers: 1}})
	observer.ObserveLock(powerlock.LockEvent{Name: name, Mode: powerlock.LockModeRead, Kind: powerlock.LockEventReleased, Result: powerlock.LockResultAcquired, HoldDuration: 1500 * time.Millisecond, HoldDurationKnown: true, State: powerlock.LockState{Name: name, Version: 3}})
	observer.ObserveLock(powerlock.LockEvent{Name: name, Mode: powerlock.LockModeWrite, Kind: powerlock.LockEventTryFailed, Result: powerlock.LockResultBusy, State: powerlock.LockState{Name: name, Version: 3}})
	for _, result := range []powerlock.LockResult{powerlock.LockResultQueueFull, powerlock.LockResultCancelled, powerlock.LockResultDeadlineExceeded} {
		observer.ObserveLock(powerlock.LockEvent{Name: name, Mode: powerlock.LockModeWrite, Kind: powerlock.LockEventRejected, Result: result, Contended: true, WaitDuration: time.Second, State: powerlock.LockState{Name: name, Version: 3}})
	}
	assertMetricValue(t, observer.contentions.WithLabelValues(name, "read"), 1)
	assertMetricValue(t, observer.waitExceeded.WithLabelValues(name, "read"), 1)
	assertMetricValue(t, observer.holdExceeded.WithLabelValues(name, "read"), 1)
	assertMetricValue(t, observer.acquisitions.WithLabelValues(name, "read", "acquired"), 1)
	for _, result := range []powerlock.LockResult{powerlock.LockResultBusy, powerlock.LockResultQueueFull, powerlock.LockResultCancelled, powerlock.LockResultDeadlineExceeded} {
		assertMetricValue(t, observer.acquisitions.WithLabelValues(name, "write", result.String()), 1)
	}
	waitCount, waitSum := histogramValues(t, registry, "powerlock_wait_duration_seconds", name, "read")
	holdCount, holdSum := histogramValues(t, registry, "powerlock_hold_duration_seconds", name, "read")
	if waitCount != 1 || waitSum != 1.5 || holdCount != 1 || holdSum != 1.5 {
		t.Fatalf("unexpected duration histograms: wait_count=%d wait_sum=%g hold_count=%d hold_sum=%g", waitCount, waitSum, holdCount, holdSum)
	}
}

func TestObserver_RejectsInvalidEvents(t *testing.T) {
	observer := MustNew(promclient.NewRegistry())
	tests := []struct {
		name  string
		event powerlock.LockEvent
		text  string
	}{
		{name: "mode", event: powerlock.LockEvent{Mode: 99, Kind: powerlock.LockEventAcquired, Result: powerlock.LockResultAcquired}, text: "invalid mode=99"},
		{name: "wait result", event: powerlock.LockEvent{Mode: powerlock.LockModeRead, Kind: powerlock.LockEventWaitStarted, Result: powerlock.LockResultAcquired}, text: "expected result=busy"},
		{name: "acquired result", event: powerlock.LockEvent{Mode: powerlock.LockModeWrite, Kind: powerlock.LockEventAcquired, Result: powerlock.LockResultBusy}, text: "expected result=acquired"},
		{name: "failure result", event: powerlock.LockEvent{Mode: powerlock.LockModeWrite, Kind: powerlock.LockEventRejected, Result: powerlock.LockResultAcquired}, text: "invalid failure result=1"},
		{name: "kind", event: powerlock.LockEvent{Mode: powerlock.LockModeWrite, Kind: 99, Result: powerlock.LockResultAcquired}, text: "invalid event kind=99"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expectObserverPanic(t, test.text, func() { observer.ObserveLock(test.event) })
		})
	}
}

func TestObserver_RollsBackPartialRegistration(t *testing.T) {
	registry := promclient.NewRegistry()
	held := promclient.NewGaugeVec(promclient.GaugeOpts{Namespace: "powerlock", Name: "held", Help: "Current number of held lock acquisitions by stable lock name and mode."}, []string{"name", "mode"})
	if err := registry.Register(held); err != nil {
		t.Fatalf("expected held metric setup, got %v", err)
	}
	if _, err := New(registry); err == nil || !strings.Contains(err.Error(), "register powerlock_held") {
		t.Fatalf("expected held metric registration failure, got %v", err)
	}
	waiting := promclient.NewGaugeVec(promclient.GaugeOpts{Namespace: "powerlock", Name: "waiting", Help: "Current number of lock acquisitions waiting by stable lock name and mode."}, []string{"name", "mode"})
	if err := registry.Register(waiting); err != nil {
		t.Fatalf("waiting metric was not rolled back: %v", err)
	}
}

func TestObserver_ConstructsDefaultWatchdog(t *testing.T) {
	observer := MustNew(promclient.NewRegistry())
	m := observer.NewWatchdogRWMutex("watchdog-metrics")
	if m.WaitThreshold() != powerlock.DefaultWaitThreshold || m.HoldThreshold() != powerlock.DefaultHoldThreshold {
		t.Fatalf("unexpected watchdog thresholds: wait=%s hold=%s", m.WaitThreshold(), m.HoldThreshold())
	}
	m.Lock()
	m.Unlock()
}

func assertMetricValue(t *testing.T, collector promclient.Collector, expected float64) {
	t.Helper()
	if observed := testutil.ToFloat64(collector); observed != expected {
		t.Fatalf("unexpected metric value: expected=%g observed=%g", expected, observed)
	}
}

func histogramValues(t *testing.T, registry *promclient.Registry, familyName string, name string, mode string) (uint64, float64) {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() != familyName {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := make(map[string]string, len(metric.GetLabel()))
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["name"] == name && labels["mode"] == mode {
				return metric.GetHistogram().GetSampleCount(), metric.GetHistogram().GetSampleSum()
			}
		}
	}
	t.Fatalf("histogram %s{name=%q,mode=%q} was not gathered", familyName, name, mode)
	return 0, 0
}

func expectObserverPanic(t *testing.T, expected string, operation func()) {
	t.Helper()
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected operation to panic")
		}
		if message := fmt.Sprint(recovered); !strings.Contains(message, expected) {
			t.Fatalf("panic omitted %q: %s", expected, message)
		}
	}()
	operation()
}
