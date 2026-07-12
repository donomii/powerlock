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
