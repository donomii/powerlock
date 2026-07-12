package powerlock

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func TestProfileObserver_TracksWaitersAndExactHolders(t *testing.T) {
	profiles := DefaultProfileObserver()
	observer := LockObserverGroup{profiles}
	m := NewObservedRWMutex("profiled", observer)
	guard, err := m.RLockGuard(context.Background())
	if err != nil {
		t.Fatalf("expected read guard, got %v", err)
	}
	if profiles.HolderCount() != 1 {
		t.Fatalf("expected one exact holder, got %d", profiles.HolderCount())
	}

	result := make(chan error, 1)
	go func() {
		result <- m.LockContext(context.Background())
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingWriters == 1
	})
	waitForProfileCount(t, profiles.WaiterCount, 1)
	guard.Unlock()
	if err := <-result; err != nil {
		t.Fatalf("expected writer acquisition, got %v", err)
	}
	m.Unlock()
	waitForProfileCount(t, profiles.WaiterCount, 0)
	waitForProfileCount(t, profiles.HolderCount, 0)
}

func TestProfileObserver_ReleaseBeforeAcquireDoesNotLeaveStaleHolder(t *testing.T) {
	profiles := DefaultProfileObserver()
	baseline := profiles.HolderCount()
	attemptID := LockAttemptID(nextLockAttemptID.Add(1))
	profiles.ObserveLock(LockEvent{Kind: LockEventReleased, Mode: LockModeWrite, AttemptID: attemptID, ExactHold: true})
	profiles.ObserveLock(LockEvent{Kind: LockEventAcquired, Mode: LockModeWrite, AttemptID: attemptID, ExactHold: true})
	if profiles.HolderCount() != baseline {
		t.Fatalf("release-before-acquire left a stale holder: baseline=%d observed=%d", baseline, profiles.HolderCount())
	}
}

func waitForProfileCount(t *testing.T, count func() int, expected int) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		if count() == expected {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("pprof count did not reach expected value: expected=%d observed=%d", expected, count())
		default:
			runtime.Gosched()
		}
	}
}
