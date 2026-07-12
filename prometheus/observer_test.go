package powerlockprometheus

import (
	"context"
	"math"
	"runtime"
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
	if _, err := NewWithBuckets(registry, []float64{0}, []float64{1}); err == nil {
		t.Fatal("expected invalid wait bucket error")
	}
	if _, err := NewWithBuckets(registry, []float64{math.NaN()}, []float64{1}); err == nil {
		t.Fatal("expected non-finite wait bucket error")
	}
	if _, err := New(registry); err != nil {
		t.Fatalf("expected first registration, got %v", err)
	}
	if _, err := New(registry); err == nil {
		t.Fatal("expected duplicate registration error")
	}
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
