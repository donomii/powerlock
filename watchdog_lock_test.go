package powerlock

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockingAcquireObserver struct {
	once               sync.Once
	releaseOnce        sync.Once
	holdOnce           sync.Once
	targetAttempt      atomic.Uint64
	contendedOnly      bool
	acquiredEntered    chan struct{}
	releaseAcquisition chan struct{}
	holdEntered        chan struct{}
}

func (o *blockingAcquireObserver) ObserveLock(event LockEvent) {
	switch event.Kind {
	case LockEventAcquired:
		if o.contendedOnly && !event.Contended {
			return
		}
		o.targetAttempt.Store(uint64(event.AttemptID))
		o.once.Do(func() { close(o.acquiredEntered) })
		<-o.releaseAcquisition
	case LockEventHoldExceeded:
		if uint64(event.AttemptID) == o.targetAttempt.Load() {
			o.holdOnce.Do(func() { close(o.holdEntered) })
		}
	}
}

func (o *blockingAcquireObserver) release() {
	o.releaseOnce.Do(func() { close(o.releaseAcquisition) })
}

func TestWatchdogRWMutex_ReportsWaitThreshold(t *testing.T) {
	observer := newRecordingObserver()
	m := NewWatchdogRWMutexWithThresholds("cache", time.Millisecond, 0, observer)
	m.Lock()

	result := make(chan error, 1)
	go func() {
		err := m.RLockContext(context.Background())
		result <- err
		if err == nil {
			m.RUnlock()
		}
	}()
	event := waitForEvent(t, observer, LockEventWaitExceeded)
	if event.Name != "cache" || event.Mode != LockModeRead || event.WaitDuration < time.Millisecond || len(event.Callers) == 0 {
		t.Fatalf("unexpected wait threshold event: %+v", event)
	}
	m.Unlock()
	if err := <-result; err != nil {
		t.Fatalf("expected reader acquisition, got %v", err)
	}
}

func TestWatchdogRWMutex_ReportsGuardHoldThreshold(t *testing.T) {
	observer := newRecordingObserver()
	m := NewWatchdogRWMutexWithThresholds("cache", 0, time.Millisecond, observer)
	guard, err := m.RLockGuard(context.Background())
	if err != nil {
		t.Fatalf("expected read guard, got %v", err)
	}
	event := waitForEvent(t, observer, LockEventHoldExceeded)
	if event.AttemptID != guard.AttemptID() || event.Mode != LockModeRead || event.HoldDuration < time.Millisecond {
		t.Fatalf("unexpected hold threshold event: %+v", event)
	}
	if state := m.Snapshot(); state.Readers != 1 {
		t.Fatalf("hold report changed ownership: %+v", state)
	}
	guard.Unlock()
	holdReports := 0
	for _, observed := range observer.snapshot() {
		if observed.Kind == LockEventHoldExceeded && observed.AttemptID == guard.AttemptID() {
			holdReports++
		}
	}
	if holdReports != 1 {
		t.Fatalf("expected one hold report, got %d in %+v", holdReports, observer.snapshot())
	}
}

func TestWatchdogRWMutex_Defaults(t *testing.T) {
	m := NewWatchdogRWMutex("cache", nil)
	if m.WaitThreshold() != DefaultWaitThreshold || m.HoldThreshold() != DefaultHoldThreshold {
		t.Fatalf("unexpected thresholds: wait=%s hold=%s", m.WaitThreshold(), m.HoldThreshold())
	}
}

func TestWatchdogMutex_ExclusiveSurface(t *testing.T) {
	m := NewWatchdogMutex("cache", nil)
	guard, acquired := m.TryLockGuard()
	if !acquired {
		t.Fatal("expected TryLockGuard to acquire")
	}
	if m.TryLock() {
		m.Unlock()
		t.Fatal("expected TryLock to fail while held")
	}
	guard.Unlock()
	m.Lock()
	m.Unlock()
}

func TestWatchdogRWMutex_RejectsEmptyNameChange(t *testing.T) {
	m := NewWatchdogRWMutex("cache", nil)
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected empty watchdog name to panic")
		}
	}()
	m.SetLocation("")
}

func TestWatchdogRWMutex_AcquiredPrecedesHoldThreshold(t *testing.T) {
	observer := &blockingAcquireObserver{
		acquiredEntered:    make(chan struct{}),
		releaseAcquisition: make(chan struct{}),
		holdEntered:        make(chan struct{}),
	}
	defer observer.release()
	m := NewWatchdogRWMutexWithThresholds("cache", 0, time.Nanosecond, observer)
	acquired := make(chan struct{})
	go func() {
		m.Lock()
		close(acquired)
	}()
	waitForSignal(t, observer.acquiredEntered, "acquisition observer did not start")

	timer := time.NewTimer(20 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-observer.holdEntered:
		t.Fatal("hold threshold event arrived before acquisition delivery completed")
	case <-timer.C:
	}
	observer.release()
	waitForSignal(t, acquired, "acquisition did not complete")
	waitForSignal(t, observer.holdEntered, "hold threshold event did not arrive")
	m.Unlock()
}

func TestWatchdogRWMutex_QueuedGuardAcquiredPrecedesHoldThreshold(t *testing.T) {
	observer := &blockingAcquireObserver{
		contendedOnly:      true,
		acquiredEntered:    make(chan struct{}),
		releaseAcquisition: make(chan struct{}),
		holdEntered:        make(chan struct{}),
	}
	defer observer.release()
	m := NewWatchdogRWMutexWithThresholds("cache", 0, time.Nanosecond, observer)
	m.Lock()
	type guardResult struct {
		guard *LockGuard
		err   error
	}
	result := make(chan guardResult, 1)
	go func() {
		guard, err := m.RLockGuard(context.Background())
		result <- guardResult{guard: guard, err: err}
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingReaders == 1 })
	m.Unlock()
	waitForSignal(t, observer.acquiredEntered, "queued acquisition observer did not start")
	assertChannelOpen(t, observer.holdEntered, "queued hold threshold arrived before acquisition delivery completed")
	observer.release()
	observed := <-result
	if observed.err != nil {
		t.Fatalf("expected queued guard acquisition, got %v", observed.err)
	}
	waitForSignal(t, observer.holdEntered, "queued hold threshold event did not arrive")
	observed.guard.Unlock()
}

func TestWatchdogRWMutex_CompletionReportsElapsedHoldThreshold(t *testing.T) {
	observer := newRecordingObserver()
	m := NewWatchdogRWMutexWithThresholds("cache", 0, time.Hour, observer)
	m.Lock()
	m.core.mu.Lock()
	m.core.writerAcquiredAt = time.Now().Add(-2 * time.Hour)
	m.core.mu.Unlock()
	m.Unlock()

	holdIndex := -1
	releaseIndex := -1
	for index, event := range observer.snapshot() {
		if event.Kind == LockEventHoldExceeded {
			holdIndex = index
		}
		if event.Kind == LockEventReleased {
			releaseIndex = index
		}
	}
	if holdIndex < 0 || releaseIndex < 0 || holdIndex >= releaseIndex {
		t.Fatalf("expected elapsed hold report before release, events=%+v", observer.snapshot())
	}
}

func TestWatchdogRWMutex_CompletionReportsElapsedWaitThreshold(t *testing.T) {
	observer := newRecordingObserver()
	m := NewWatchdogRWMutexWithThresholds("cache", time.Hour, 0, observer)
	m.Lock()
	result := make(chan error, 1)
	go func() {
		err := m.RLockContext(context.Background())
		result <- err
		if err == nil {
			m.RUnlock()
		}
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingReaders == 1 })
	m.core.mu.Lock()
	m.core.waiters[0].queuedAt = time.Now().Add(-2 * time.Hour)
	m.core.mu.Unlock()
	m.Unlock()
	if err := <-result; err != nil {
		t.Fatalf("expected queued reader acquisition, got %v", err)
	}

	waitIndex := -1
	acquiredIndex := -1
	for index, event := range observer.snapshot() {
		if event.Mode == LockModeRead && event.Kind == LockEventWaitExceeded {
			waitIndex = index
		}
		if event.Mode == LockModeRead && event.Kind == LockEventAcquired {
			acquiredIndex = index
		}
	}
	if waitIndex < 0 || acquiredIndex < 0 || waitIndex >= acquiredIndex {
		t.Fatalf("expected elapsed wait report before acquisition, events=%+v", observer.snapshot())
	}
}

func TestWatchdogRWMutex_EarlyReleaseQueuesOrderedEvents(t *testing.T) {
	blocking := &blockingAcquireObserver{
		acquiredEntered:    make(chan struct{}),
		releaseAcquisition: make(chan struct{}),
		holdEntered:        make(chan struct{}),
	}
	defer blocking.release()
	recording := newRecordingObserver()
	m := NewWatchdogRWMutexWithThresholds("cache", 0, time.Hour, LockObserverGroup{blocking, recording})
	lockDone := make(chan struct{})
	go func() {
		m.Lock()
		close(lockDone)
	}()
	waitForSignal(t, blocking.acquiredEntered, "acquisition observer did not start")
	m.core.mu.Lock()
	m.core.writerAcquiredAt = time.Now().Add(-2 * time.Hour)
	m.core.mu.Unlock()
	releaseDone := make(chan struct{})
	go func() {
		m.Unlock()
		close(releaseDone)
	}()
	waitForSignal(t, releaseDone, "early release blocked on acquisition delivery")
	blocking.release()
	waitForSignal(t, lockDone, "acquisition delivery did not complete")

	events := recording.snapshot()
	if len(events) != 3 || events[0].Kind != LockEventAcquired || events[1].Kind != LockEventHoldExceeded || events[2].Kind != LockEventReleased {
		t.Fatalf("expected acquired, hold threshold, and release ordering, events=%+v", events)
	}
}

func TestWatchdogRWMutex_ZeroThresholdsSuppressElapsedReports(t *testing.T) {
	observer := newRecordingObserver()
	m := NewWatchdogRWMutexWithThresholds("disabled-thresholds", 0, 0, observer)
	guard, err := m.LockGuard(context.Background())
	if err != nil {
		t.Fatalf("expected write guard, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- m.LockContext(ctx) }()
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingWriters == 1 })
	m.core.mu.Lock()
	m.core.writerAcquiredAt = time.Now().Add(-2 * time.Hour)
	m.core.waiters[0].queuedAt = time.Now().Add(-2 * time.Hour)
	m.core.mu.Unlock()
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected queued cancellation, got %v", err)
	}
	guard.Unlock()
	for _, event := range observer.snapshot() {
		if event.Kind == LockEventWaitExceeded || event.Kind == LockEventHoldExceeded {
			t.Fatalf("disabled threshold emitted %s: %+v", event.Kind, event)
		}
	}
}
