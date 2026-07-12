package powerlock

import (
	"context"
	"errors"
	"testing"
)

func TestLockGuard_ReportsExactReadHold(t *testing.T) {
	observer := newRecordingObserver()
	m := NewObservedRWMutex("cache", observer)
	guard, err := m.RLockGuard(context.Background())
	if err != nil {
		t.Fatalf("expected read guard, got %v", err)
	}
	attemptID := guard.AttemptID()
	guard.Unlock()
	if !guard.Released() || guard.Mode() != LockModeRead {
		t.Fatalf("unexpected guard state: released=%t mode=%s", guard.Released(), guard.Mode())
	}

	events := observer.snapshot()
	found := false
	for _, event := range events {
		if event.Kind == LockEventReleased && event.AttemptID == attemptID && event.HoldDurationKnown {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected exact read release event, got %+v", events)
	}
}

func TestLockGuard_PanicsOnSecondRelease(t *testing.T) {
	m := NewObservedRWMutex("cache", nil)
	guard, err := m.LockGuard(context.Background())
	if err != nil {
		t.Fatalf("expected write guard, got %v", err)
	}
	guard.Unlock()
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected a second guard release to panic")
		}
	}()
	guard.Unlock()
}

func TestObservedRWMutex_TryGuards(t *testing.T) {
	m := NewObservedRWMutex("cache", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if guard, err := m.LockGuard(ctx); guard != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled write guard, guard=%v err=%v", guard, err)
	}
	writeGuard, acquired := m.TryLockGuard()
	if !acquired {
		t.Fatal("expected write guard")
	}
	if _, acquired := m.TryRLockGuard(); acquired {
		t.Fatal("expected read guard acquisition to fail while write locked")
	}
	writeGuard.Unlock()
	readGuard, acquired := m.TryRLockGuard()
	if !acquired {
		t.Fatal("expected read guard")
	}
	if writeGuard, acquired := m.TryLockGuard(); acquired || writeGuard != nil {
		t.Fatalf("expected held reader to reject write guard, guard=%v acquired=%t", writeGuard, acquired)
	}
	readGuard.Unlock()
}

func TestObservedRWMutex_CancelledQueuedGuardLeavesNoOwnership(t *testing.T) {
	m := NewObservedRWMutex("cache", nil)
	m.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	type guardResult struct {
		guard *LockGuard
		err   error
	}
	result := make(chan guardResult, 1)
	go func() {
		guard, err := m.RLockGuard(ctx)
		result <- guardResult{guard: guard, err: err}
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingReaders == 1 })
	cancel()
	observed := <-result
	if observed.guard != nil || !errors.Is(observed.err, context.Canceled) {
		t.Fatalf("expected cancelled guard acquisition, guard=%v err=%v", observed.guard, observed.err)
	}
	m.Unlock()
	state := m.Snapshot()
	if state.Readers != 0 || state.Writer || state.WaitingReaders != 0 || state.WaitingWriters != 0 {
		t.Fatalf("cancelled guard left ownership state: %+v", state)
	}
}

func TestLockGuard_ConcurrentReleaseEmitsExactlyOneRelease(t *testing.T) {
	observer := newRecordingObserver()
	m := NewObservedRWMutex("guard-race", observer)
	guard, err := m.LockGuard(context.Background())
	if err != nil {
		t.Fatalf("expected write guard, got %v", err)
	}
	panicked := make(chan bool, 2)
	for iteration := 0; iteration < 2; iteration++ {
		go func() {
			didPanic := false
			defer func() {
				if recover() != nil {
					didPanic = true
				}
				panicked <- didPanic
			}()
			guard.Unlock()
		}()
	}
	panicCount := 0
	for iteration := 0; iteration < 2; iteration++ {
		if <-panicked {
			panicCount++
		}
	}
	releaseCount := 0
	for _, event := range observer.snapshot() {
		if event.Kind == LockEventReleased && event.AttemptID == guard.AttemptID() {
			releaseCount++
		}
	}
	if panicCount != 1 || releaseCount != 1 || m.Snapshot().Writer {
		t.Fatalf("expected one release and one panic, panics=%d releases=%d state=%+v", panicCount, releaseCount, m.Snapshot())
	}
	var empty *LockGuard
	requirePanic(t, empty.Unlock)
}

func TestObservedRWMutex_ReadGuardsTrackExactOwners(t *testing.T) {
	observer := newRecordingObserver()
	m := NewObservedRWMutex("reader-guards", observer)
	first, err := m.RLockGuard(context.Background())
	if err != nil {
		t.Fatalf("expected first read guard, got %v", err)
	}
	second, err := m.RLockGuard(context.Background())
	if err != nil {
		t.Fatalf("expected second read guard, got %v", err)
	}
	if first.AttemptID() == 0 || second.AttemptID() == 0 || first.AttemptID() == second.AttemptID() {
		t.Fatalf("expected distinct read ownership identifiers, first=%d second=%d", first.AttemptID(), second.AttemptID())
	}
	if state := m.Snapshot(); state.Readers != 2 {
		t.Fatalf("expected two guarded readers, got %+v", state)
	}
	second.Unlock()
	if state := m.Snapshot(); state.Readers != 1 {
		t.Fatalf("releasing one guard disturbed the other reader: %+v", state)
	}
	assertExactReleaseCount(t, observer.snapshot(), second.AttemptID(), 1)
	assertExactReleaseCount(t, observer.snapshot(), first.AttemptID(), 0)
	first.Unlock()
	assertExactReleaseCount(t, observer.snapshot(), first.AttemptID(), 1)
}

func assertExactReleaseCount(t *testing.T, events []LockEvent, attemptID LockAttemptID, expected int) {
	t.Helper()
	observed := 0
	for _, event := range events {
		if event.Kind == LockEventReleased && event.AttemptID == attemptID && event.ExactHold && event.HoldDurationKnown {
			observed++
		}
	}
	if observed != expected {
		t.Fatalf("expected %d exact releases for attempt %d, got %d in %+v", expected, attemptID, observed, events)
	}
}
