//                           _       _
// __      _____  __ ___   ___  __ _| |_ ___
// \ \ /\ / / _ \/ _` \ \ / / |/ _` | __/ _ \
//  \ V  V /  __/ (_| |\ V /| | (_| | ||  __/
//   \_/\_/ \___|\__,_| \_/ |_|\__,_|\__\___|
//
//  Copyright © 2016 - 2024 Weaviate B.V. All rights reserved.
//
//  CONTACT: hello@weaviate.io
//

package powerlockprometheus

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/donomii/powerlock"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newTestMeteredRWMutex(t *testing.T, location string) *MeteredRWMutex {
	reg := prometheus.NewRegistry()
	locksWaiting, locks := NewLockMetrics(reg)
	return NewMeteredRWMutex(location, locksWaiting, locks)
}

type legacyPausingGauge struct {
	prometheus.Gauge
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (g *legacyPausingGauge) Set(value float64) {
	g.once.Do(func() {
		close(g.entered)
		<-g.release
	})
	g.Gauge.Set(value)
}

func TestMeteredRWMutex_BasicLockUnlock(t *testing.T) {
	registry := prometheus.NewRegistry()
	locksWaiting, locks := NewLockMetrics(registry)
	m := NewMeteredRWMutex("test-metered", locksWaiting, locks)
	m.Lock()
	if got := testutil.ToFloat64(locks.WithLabelValues("test-metered")); got != 1 {
		t.Fatalf("expected one held writer, got %v", got)
	}
	m.Unlock()
	if got := testutil.ToFloat64(locks.WithLabelValues("test-metered")); got != 0 {
		t.Fatalf("expected no held writer, got %v", got)
	}
	m.RLock()
	if got := testutil.ToFloat64(locks.WithLabelValues("test-metered")); got != 1 {
		t.Fatalf("expected one held reader, got %v", got)
	}
	m.RUnlock()
}

func TestMeteredRWMutex_TryLock_Succeeds(t *testing.T) {
	m := newTestMeteredRWMutex(t, "test-metered")

	ok := m.TryLock()
	if !ok {
		t.Fatal("expected TryLock to succeed")
	}
	m.Unlock()
}

func TestMeteredRWMutex_TryLock_FailsWhenBusy(t *testing.T) {
	m := newTestMeteredRWMutex(t, "test-metered")

	m.Lock()
	defer m.Unlock()

	ok := m.TryLock()
	if ok {
		t.Fatal("expected TryLock to fail while locked")
	}
}

func TestMeteredRWMutex_TryRLock_Succeeds(t *testing.T) {
	m := newTestMeteredRWMutex(t, "test-metered")

	ok := m.TryRLock()
	if !ok {
		t.Fatal("expected TryRLock to succeed")
	}
	m.RUnlock()
}

func TestMeteredRWMutex_TryRLock_FailsWhenWriteLocked(t *testing.T) {
	m := newTestMeteredRWMutex(t, "test-metered")

	m.Lock()
	defer m.Unlock()

	ok := m.TryRLock()
	if ok {
		t.Fatal("expected TryRLock to fail when write locked")
	}
}

func TestMeteredRWMutex_ParallelReaders(t *testing.T) {
	m := newTestMeteredRWMutex(t, "test-metered")
	const numReaders = 5
	acquired := make(chan struct{}, numReaders)
	release := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.RLock()
			acquired <- struct{}{}
			<-release
			m.RUnlock()
		}()
	}
	for i := 0; i < numReaders; i++ {
		<-acquired
	}
	if state := m.Snapshot(); state.Readers != numReaders {
		t.Fatalf("expected %d readers, got %+v", numReaders, state)
	}
	close(release)
	wg.Wait()
}

func TestMeteredRWMutex_WriterBlocksReaders(t *testing.T) {
	m := newTestMeteredRWMutex(t, "test-metered")

	m.Lock()

	start := time.Now()
	ok := m.TryRLock()
	if ok {
		t.Fatal("expected TryRLock to fail while write locked")
	}
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Errorf("TryRLock took too long to fail: %v", elapsed)
	}

	m.Unlock()
}

func TestMeteredRWMutex_WaitingGauge(t *testing.T) {
	registry := prometheus.NewRegistry()
	locksWaiting, locks := NewLockMetrics(registry)
	m := NewMeteredRWMutex("test-metered", locksWaiting, locks)
	m.Lock()
	result := make(chan struct{})
	go func() {
		m.RLock()
		m.RUnlock()
		close(result)
	}()
	waitForLegacyState(t, m.Snapshot, func(state powerlock.LockState) bool {
		return state.WaitingReaders == 1
	})
	waitForLegacyGauge(t, locksWaiting.WithLabelValues("test-metered"), 1)
	m.Unlock()
	<-result
	waitForLegacyGauge(t, locksWaiting.WithLabelValues("test-metered"), 0)
}

func TestMeteredRWMutex_SetLocationBeforeUse(t *testing.T) {
	registry := prometheus.NewRegistry()
	locksWaiting, locks := NewLockMetrics(registry)
	m := NewMeteredRWMutex("old", locksWaiting, locks)
	m.SetLocation("new")
	m.Lock()
	if got := testutil.ToFloat64(locks.WithLabelValues("old")); got != 0 {
		t.Fatalf("expected old label to remain zero, got %v", got)
	}
	if got := testutil.ToFloat64(locks.WithLabelValues("new")); got != 1 {
		t.Fatalf("expected new label to report one holder, got %v", got)
	}
	m.Unlock()
}

func TestMeteredRWMutex_SetLocationPanicsAfterUse(t *testing.T) {
	m := newTestMeteredRWMutex(t, "test-metered")
	m.Lock()
	m.Unlock()
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected SetLocation to panic after first use")
		}
	}()
	m.SetLocation("changed")
}

func TestMeteredRWMutex_ZeroValueIsUnobserved(t *testing.T) {
	var m MeteredRWMutex
	m.Lock()
	m.Unlock()
	m.RLock()
	m.RUnlock()
}

func TestRegisterLockMetrics_ReturnsDuplicateRegistrationError(t *testing.T) {
	registry := prometheus.NewRegistry()
	if _, _, err := RegisterLockMetrics(registry); err != nil {
		t.Fatalf("expected first registration, got %v", err)
	}
	if _, _, err := RegisterLockMetrics(registry); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}

func TestLegacyPrometheusObserver_IgnoresDelayedStaleState(t *testing.T) {
	waiting := prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_waiting"})
	held := prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_held"})
	observer := &legacyPrometheusObserver{waiting: waiting, held: held}
	observer.ObserveLock(powerlock.LockEvent{State: powerlock.LockState{Version: 2}})
	observer.ObserveLock(powerlock.LockEvent{State: powerlock.LockState{Version: 1, Writer: true}})
	if got := testutil.ToFloat64(held); got != 0 {
		t.Fatalf("expected stale state to be ignored, got held=%v", got)
	}
}

func TestLegacyPrometheusObserver_SerializesVersionAndGaugeUpdate(t *testing.T) {
	waiting := prometheus.NewGauge(prometheus.GaugeOpts{Name: "serialized_waiting"})
	held := &legacyPausingGauge{
		Gauge:   prometheus.NewGauge(prometheus.GaugeOpts{Name: "serialized_held"}),
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	observer := &legacyPrometheusObserver{waiting: waiting, held: held}
	firstDone := make(chan struct{})
	go func() {
		observer.ObserveLock(powerlock.LockEvent{State: powerlock.LockState{Version: 1, Writer: true}})
		close(firstDone)
	}()
	<-held.entered
	secondDone := make(chan struct{})
	go func() {
		observer.ObserveLock(powerlock.LockEvent{State: powerlock.LockState{Version: 2}})
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

func TestMeteredRWMutex_CompleteCompatibilitySurface(t *testing.T) {
	m := newTestMeteredRWMutex(t, "complete-surface")
	if m.Name() != "complete-surface" {
		t.Fatalf("unexpected lock name: %q", m.Name())
	}
	if err := m.LockContext(context.Background()); err != nil {
		t.Fatalf("expected write acquisition, got %v", err)
	}
	if state := m.Snapshot(); !state.Writer {
		t.Fatalf("expected held writer, got %+v", state)
	}
	m.Unlock()
	if err := m.RLockContext(context.Background()); err != nil {
		t.Fatalf("expected read acquisition, got %v", err)
	}
	m.RUnlock()
	reader := m.RLocker()
	reader.Lock()
	reader.Unlock()
	writeGuard, err := m.LockGuard(context.Background())
	if err != nil {
		t.Fatalf("expected write guard, got %v", err)
	}
	if readGuard, acquired := m.TryRLockGuard(); acquired || readGuard != nil {
		t.Fatalf("expected held writer to reject read guard, guard=%v acquired=%t", readGuard, acquired)
	}
	writeGuard.Unlock()
	readGuard, err := m.RLockGuard(context.Background())
	if err != nil {
		t.Fatalf("expected read guard, got %v", err)
	}
	if writeGuard, acquired := m.TryLockGuard(); acquired || writeGuard != nil {
		t.Fatalf("expected held reader to reject write guard, guard=%v acquired=%t", writeGuard, acquired)
	}
	readGuard.Unlock()
	writeGuard, acquired := m.TryLockGuard()
	if !acquired {
		t.Fatal("expected write try-guard acquisition")
	}
	writeGuard.Unlock()
	readGuard, acquired = m.TryRLockGuard()
	if !acquired {
		t.Fatal("expected read try-guard acquisition")
	}
	readGuard.Unlock()
}

func TestMeteredRWMutex_ZeroValueSnapshotInitializesReadout(t *testing.T) {
	var m MeteredRWMutex
	state := m.Snapshot()
	if state.Name != "" || state.Writer || state.Readers != 0 || m.Name() != "" {
		t.Fatalf("unexpected zero-value readout: name=%q state=%+v", m.Name(), state)
	}
}

func TestLegacyRegistrationAndGaugeConfigurationFailures(t *testing.T) {
	if _, _, err := RegisterLockMetrics(nil); err == nil {
		t.Fatal("expected nil registerer error")
	}
	expectLegacyPanic(t, "registerer must not be nil", func() { NewLockMetrics(nil) })
	waiting := prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "powerlock", Name: "locks_waiting", Help: "Number of goroutines waiting on a lock at a specific location"}, []string{"location"})
	held := prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "powerlock", Name: "locks_held", Help: "Number of goroutines currently holding a lock at a specific location"}, []string{"location"})
	expectLegacyPanic(t, "both gauges must be supplied together", func() { NewMeteredRWMutex("partial", waiting, nil) })
	expectLegacyPanic(t, "both gauges must be supplied together", func() { NewMeteredRWMutex("partial", nil, held) })
}

func TestRegisterLockMetrics_RollsBackWaitingGauge(t *testing.T) {
	registry := prometheus.NewRegistry()
	held := prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "powerlock", Name: "locks_held", Help: "Number of goroutines currently holding a lock at a specific location"}, []string{"location"})
	if err := registry.Register(held); err != nil {
		t.Fatalf("expected held metric setup, got %v", err)
	}
	if _, _, err := RegisterLockMetrics(registry); err == nil || !strings.Contains(err.Error(), "register held-lock gauge") {
		t.Fatalf("expected held metric registration failure, got %v", err)
	}
	waiting := prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "powerlock", Name: "locks_waiting", Help: "Number of goroutines waiting on a lock at a specific location"}, []string{"location"})
	if err := registry.Register(waiting); err != nil {
		t.Fatalf("waiting metric was not rolled back: %v", err)
	}
}

func waitForLegacyGauge(t *testing.T, gauge prometheus.Gauge, expected float64) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		observed := testutil.ToFloat64(gauge)
		if observed == expected {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("legacy gauge did not reach expected value: expected=%v observed=%v", expected, observed)
		default:
			runtime.Gosched()
		}
	}
}

func waitForLegacyState(t *testing.T, snapshot func() powerlock.LockState, matches func(powerlock.LockState) bool) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		state := snapshot()
		if matches(state) {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("legacy lock state did not reach expected condition: %+v", state)
		default:
			runtime.Gosched()
		}
	}
}

func expectLegacyPanic(t *testing.T, expected string, operation func()) {
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
