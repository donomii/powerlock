package powerlock

import (
	"context"
	"errors"
	"testing"
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
