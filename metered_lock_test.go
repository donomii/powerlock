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

package powerlock

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newTestMeteredRWMutex(t *testing.T, location string) *MeteredRWMutex {
	reg := prometheus.NewRegistry()
	locksWaiting, locks := NewLockMetrics(reg)
	return NewMeteredRWMutex(location, locksWaiting, locks)
}

type pausingGauge struct {
	prometheus.Gauge
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
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
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
	observer.ObserveLock(LockEvent{State: LockState{Version: 2}})
	observer.ObserveLock(LockEvent{State: LockState{Version: 1, Writer: true}})
	if got := testutil.ToFloat64(held); got != 0 {
		t.Fatalf("expected stale state to be ignored, got held=%v", got)
	}
}

func TestLegacyPrometheusObserver_SerializesVersionAndGaugeUpdate(t *testing.T) {
	waiting := prometheus.NewGauge(prometheus.GaugeOpts{Name: "serialized_waiting"})
	held := &pausingGauge{
		Gauge:   prometheus.NewGauge(prometheus.GaugeOpts{Name: "serialized_held"}),
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	observer := &legacyPrometheusObserver{waiting: waiting, held: held}
	firstDone := make(chan struct{})
	go func() {
		observer.ObserveLock(LockEvent{State: LockState{Version: 1, Writer: true}})
		close(firstDone)
	}()
	<-held.entered
	secondDone := make(chan struct{})
	go func() {
		observer.ObserveLock(LockEvent{State: LockState{Version: 2}})
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
