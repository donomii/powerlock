package powerlock

import (
	"context"
	"sync"
	"testing"
	"time"
)

type recordingObserver struct {
	mu     sync.Mutex
	events []LockEvent
	seen   chan LockEvent
}

func newRecordingObserver() *recordingObserver {
	return &recordingObserver{seen: make(chan LockEvent, 64)}
}

func (o *recordingObserver) ObserveLock(event LockEvent) {
	o.mu.Lock()
	o.events = append(o.events, event)
	o.mu.Unlock()
	o.seen <- event
}

func (o *recordingObserver) snapshot() []LockEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	events := make([]LockEvent, len(o.events))
	copy(events, o.events)
	return events
}

func waitForEvent(t *testing.T, observer *recordingObserver, kind LockEventKind) LockEvent {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-observer.seen:
			if event.Kind == kind {
				return event
			}
		case <-timer.C:
			t.Fatalf("observer did not receive %s; events=%+v", kind, observer.snapshot())
		}
	}
}

func TestObservedRWMutex_ReportsImmediateAcquireAndRelease(t *testing.T) {
	observer := newRecordingObserver()
	m := NewObservedRWMutex("cache", observer)
	m.Lock()
	m.Unlock()

	events := observer.snapshot()
	if len(events) != 2 {
		t.Fatalf("expected two events, got %+v", events)
	}
	if events[0].Kind != LockEventAcquired || events[0].Name != "cache" || events[0].Mode != LockModeWrite {
		t.Fatalf("unexpected acquisition event: %+v", events[0])
	}
	if events[1].Kind != LockEventReleased || !events[1].HoldDurationKnown {
		t.Fatalf("unexpected release event: %+v", events[1])
	}
}

func TestObservedRWMutex_ReportsContendedAcquisition(t *testing.T) {
	observer := newRecordingObserver()
	m := NewObservedRWMutex("cache", observer)
	m.Lock()

	result := make(chan error, 1)
	go func() {
		err := m.RLockContext(context.Background())
		result <- err
		if err == nil {
			m.RUnlock()
		}
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingReaders == 1
	})
	m.Unlock()
	if err := <-result; err != nil {
		t.Fatalf("expected reader acquisition, got %v", err)
	}

	events := observer.snapshot()
	foundWait := false
	foundContendedAcquire := false
	for _, event := range events {
		if event.Kind == LockEventWaitStarted && event.Mode == LockModeRead {
			foundWait = true
		}
		if event.Kind == LockEventAcquired && event.Mode == LockModeRead && event.Contended {
			foundContendedAcquire = true
		}
	}
	if !foundWait || !foundContendedAcquire {
		t.Fatalf("expected waiting and contended acquisition events, got %+v", events)
	}
}

func TestObservedRWMutex_ReportsFailedTry(t *testing.T) {
	observer := newRecordingObserver()
	m := NewObservedRWMutex("cache", observer)
	m.Lock()
	if m.TryRLock() {
		m.RUnlock()
		t.Fatal("expected TryRLock to fail")
	}
	m.Unlock()

	events := observer.snapshot()
	found := false
	for _, event := range events {
		if event.Kind == LockEventTryFailed && event.Mode == LockModeRead && event.Result == LockResultBusy {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected failed try event, got %+v", events)
	}
}

func TestObservedRWMutex_EarlyReleaseQueuesAfterAcquiredEvent(t *testing.T) {
	blocking := &blockingAcquireObserver{
		acquiredEntered:    make(chan struct{}),
		releaseAcquisition: make(chan struct{}),
		holdEntered:        make(chan struct{}),
	}
	defer blocking.release()
	recording := newRecordingObserver()
	m := NewObservedRWMutex("cache", LockObserverGroup{blocking, recording})
	lockDone := make(chan struct{})
	go func() {
		m.Lock()
		close(lockDone)
	}()
	waitForSignal(t, blocking.acquiredEntered, "acquisition observer did not start")
	releaseDone := make(chan struct{})
	go func() {
		m.Unlock()
		close(releaseDone)
	}()
	waitForSignal(t, releaseDone, "early release blocked on acquisition delivery")
	blocking.release()
	waitForSignal(t, lockDone, "acquisition delivery did not complete")

	events := recording.snapshot()
	if len(events) != 2 || events[0].Kind != LockEventAcquired || events[1].Kind != LockEventReleased {
		t.Fatalf("expected acquired then released ordering, events=%+v", events)
	}
}

func TestObservedRWMutex_ContendedGuardKeepsOneAttemptIdentity(t *testing.T) {
	observer := newRecordingObserver()
	m := NewObservedRWMutex("observed-lifecycle", observer)
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
	waitStarted := waitForEvent(t, observer, LockEventWaitStarted)
	if waitStarted.AttemptID == 0 || waitStarted.Mode != LockModeRead || !waitStarted.Contended || !waitStarted.State.Writer || waitStarted.State.WaitingReaders != 1 {
		t.Fatalf("unexpected wait-start event: %+v", waitStarted)
	}
	m.Unlock()
	observed := <-result
	if observed.err != nil {
		t.Fatalf("expected guarded reader acquisition, got %v", observed.err)
	}
	observed.guard.Unlock()
	var acquired LockEvent
	var released LockEvent
	for _, event := range observer.snapshot() {
		if event.AttemptID != waitStarted.AttemptID {
			continue
		}
		if event.Kind == LockEventAcquired {
			acquired = event
		}
		if event.Kind == LockEventReleased {
			released = event
		}
	}
	if acquired.Kind != LockEventAcquired || !acquired.Contended || !acquired.ExactHold || acquired.State.Readers != 1 || acquired.State.Writer {
		t.Fatalf("unexpected acquired event: %+v", acquired)
	}
	if released.Kind != LockEventReleased || !released.ExactHold || !released.HoldDurationKnown || released.State.Readers != 0 {
		t.Fatalf("unexpected released event: %+v", released)
	}
}

func TestObservedRWMutex_RLockerUsesSharedOwnership(t *testing.T) {
	m := NewObservedRWMutex("observed-rlocker", nil)
	reader := m.RLocker()
	reader.Lock()
	if state := m.Snapshot(); state.Readers != 1 || state.Writer {
		t.Fatalf("expected shared ownership through RLocker, got %+v", state)
	}
	reader.Unlock()
}
