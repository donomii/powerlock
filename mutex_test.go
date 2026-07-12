package powerlock

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExclusiveMutexForms(t *testing.T) {
	contextMutex := NewContextMutex("context")
	contextMutex.Lock()
	if contextMutex.TryLock() {
		contextMutex.Unlock()
		t.Fatal("expected held ContextMutex to reject TryLock")
	}
	contextMutex.Unlock()

	fairMutex := NewFairMutex("fair")
	fairMutex.Lock()
	fairMutex.Unlock()

	maxMutex := NewMaxMutexWithLimit("max", 2)
	if maxMutex.MaxWaiting() != 2 {
		t.Fatalf("expected waiter limit 2, got %d", maxMutex.MaxWaiting())
	}
	maxMutex.Lock()
	maxMutex.Unlock()

	observedMutex := NewObservedMutex("observed", nil)
	guard, err := observedMutex.LockGuard(context.Background())
	if err != nil {
		t.Fatalf("expected observed guard, got %v", err)
	}
	guard.Unlock()
}

func TestCancelMutexRejectsAfterCancel(t *testing.T) {
	m := NewCancelMutex("cancel")
	m.Cancel()
	if !m.Cancelled() {
		t.Fatal("expected cancelled state")
	}
	if err := m.LockContext(context.Background()); !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected ErrCancelled, got %v", err)
	}
}

func TestFairMutexPreservesFIFOOrder(t *testing.T) {
	m := NewFairMutex("fair-exclusive")
	m.Lock()
	type acquisition struct {
		identifier int
		err        error
	}
	acquired := make(chan acquisition, 3)
	releases := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	for index, release := range releases {
		identifier := index + 1
		go func() {
			err := m.LockContext(context.Background())
			acquired <- acquisition{identifier: identifier, err: err}
			if err == nil {
				<-release
				m.Unlock()
			}
		}()
		waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingWriters == identifier })
	}
	m.Unlock()
	for index, release := range releases {
		observed := <-acquired
		if observed.err != nil || observed.identifier != index+1 {
			t.Fatalf("expected writer %d, acquired writer %d with err=%v", index+1, observed.identifier, observed.err)
		}
		close(release)
	}
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return !state.Writer })
}

func TestContextMutexContextCancellationAndIdentity(t *testing.T) {
	m := NewContextMutex("before")
	m.SetLocation("context-exclusive")
	if m.Name() != "context-exclusive" {
		t.Fatalf("expected updated name, got %q", m.Name())
	}
	m.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- m.LockContext(ctx) }()
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingWriters == 1 })
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	m.Unlock()
	if state := m.Snapshot(); state.Writer || state.WaitingWriters != 0 {
		t.Fatalf("cancelled waiter left state behind: %+v", state)
	}
}

func TestCancelMutexRejectsQueuedAndFutureAcquisitions(t *testing.T) {
	m := NewCancelMutex("before")
	m.SetLocation("cancel-exclusive")
	m.Lock()
	result := make(chan error, 1)
	go func() { result <- m.LockContext(context.Background()) }()
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingWriters == 1 })
	m.Cancel()
	if err := <-result; !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected queued acquisition cancellation, got %v", err)
	}
	if !m.Cancelled() || m.Name() != "cancel-exclusive" || m.TryLock() {
		t.Fatalf("unexpected cancelled lock state: name=%q state=%+v", m.Name(), m.Snapshot())
	}
	m.Unlock()
	message := panicText(t, m.Lock)
	if message == "" {
		t.Fatal("expected future blocking acquisition to panic")
	}
}

func TestMaxMutexLimitsOnlyBlockedAcquisitions(t *testing.T) {
	m := NewMaxMutex("before")
	m.SetLocation("bounded-exclusive")
	if m.Name() != "bounded-exclusive" || m.MaxWaiting() != DefaultMaxWaiting {
		t.Fatalf("unexpected bounded lock configuration: name=%q maximum=%d", m.Name(), m.MaxWaiting())
	}
	m.Lock()
	releaseWaiter := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		err := m.LockContext(context.Background())
		first <- err
		if err == nil {
			<-releaseWaiter
			m.Unlock()
		}
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingWriters == 1 })
	if err := m.LockContext(context.Background()); !errors.Is(err, ErrMaxWaiting) {
		t.Fatalf("expected waiter saturation, got %v", err)
	}
	m.Unlock()
	if err := <-first; err != nil {
		t.Fatalf("expected queued acquisition, got %v", err)
	}
	if state := m.Snapshot(); !state.Writer || state.WaitingWriters != 0 {
		t.Fatalf("expected waiter capacity to be released on acquisition, got %+v", state)
	}
	close(releaseWaiter)
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return !state.Writer })
	if !m.TryLock() {
		t.Fatal("expected lock to remain reusable after saturation")
	}
	m.Unlock()
}

func TestObservedMutexReportsExclusiveCancellationAndGuard(t *testing.T) {
	observer := newRecordingObserver()
	m := NewObservedMutex("before", observer)
	m.SetLocation("observed-exclusive")
	m.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- m.LockContext(ctx) }()
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingWriters == 1 })
	if m.TryLock() {
		m.Unlock()
		t.Fatal("expected held observed mutex to reject TryLock")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected observed cancellation, got %v", err)
	}
	m.Unlock()
	guard, acquired := m.TryLockGuard()
	if !acquired || guard.Mode() != LockModeWrite {
		t.Fatalf("expected exact write guard, guard=%v acquired=%t", guard, acquired)
	}
	guard.Unlock()
	if m.Name() != "observed-exclusive" {
		t.Fatalf("expected stable observed name, got %q", m.Name())
	}
	assertEventKinds(t, observer.snapshot(), LockEventWaitStarted, LockEventTryFailed, LockEventRejected)
}

func TestWatchdogMutexReportsExclusiveWaitAndHold(t *testing.T) {
	observer := newRecordingObserver()
	m := NewWatchdogMutexWithThresholds("before", time.Millisecond, time.Millisecond, observer)
	m.SetLocation("watchdog-exclusive")
	guard, err := m.LockGuard(context.Background())
	if err != nil {
		t.Fatalf("expected watchdog guard, got %v", err)
	}
	holdEvent := waitForEvent(t, observer, LockEventHoldExceeded)
	if holdEvent.AttemptID != guard.AttemptID() || holdEvent.Mode != LockModeWrite {
		t.Fatalf("unexpected hold threshold event: %+v", holdEvent)
	}
	result := make(chan error, 1)
	go func() { result <- m.LockContext(context.Background()) }()
	waitEvent := waitForEvent(t, observer, LockEventWaitExceeded)
	if waitEvent.Mode != LockModeWrite || waitEvent.Name != "watchdog-exclusive" {
		t.Fatalf("unexpected wait threshold event: %+v", waitEvent)
	}
	guard.Unlock()
	if err := <-result; err != nil {
		t.Fatalf("expected queued watchdog acquisition, got %v", err)
	}
	if m.Name() != "watchdog-exclusive" || !m.Snapshot().Writer {
		t.Fatalf("unexpected acquired watchdog state: name=%q state=%+v", m.Name(), m.Snapshot())
	}
	m.Unlock()
}

func assertEventKinds(t *testing.T, events []LockEvent, expected ...LockEventKind) {
	t.Helper()
	for _, kind := range expected {
		found := false
		for _, event := range events {
			if event.Kind == kind {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected event %s, got %+v", kind, events)
		}
	}
}
