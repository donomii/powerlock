package powerlock

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestKeyedMutex_DifferentKeysDoNotBlock(t *testing.T) {
	m := NewKeyedMutex[string]("jobs")
	m.Lock("a")
	if !m.TryLock("b") {
		t.Fatal("expected a different key to acquire")
	}
	if m.TryLock("a") {
		m.Unlock("a")
		t.Fatal("expected the held key to reject TryLock")
	}
	m.Unlock("b")
	m.Unlock("a")
	if m.ActiveKeys() != 0 {
		t.Fatalf("expected keyed entries to be removed, active=%d", m.ActiveKeys())
	}
}

func TestKeyedMutex_ContextCancellationReleasesReference(t *testing.T) {
	m := NewKeyedMutex[string]("jobs")
	m.Lock("a")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- m.LockContext(ctx, "a")
	}()
	waitForLockState(t, func() LockState {
		state, _ := m.Snapshot("a")
		return state
	}, func(state LockState) bool {
		return state.WaitingWriters == 1
	})
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	m.Unlock("a")
	if m.ActiveKeys() != 0 {
		t.Fatalf("expected cancelled waiter reference cleanup, active=%d", m.ActiveKeys())
	}
}

func TestKeyedMutex_EnforcesActiveKeyLimit(t *testing.T) {
	m := NewKeyedMutexWithLimit[string]("jobs", 1)
	m.Lock("a")
	err := m.LockContext(context.Background(), "b")
	if !errors.Is(err, ErrMaxKeys) {
		t.Fatalf("expected ErrMaxKeys, got %v", err)
	}
	var keyedErr *KeyedAcquisitionError[string]
	if !errors.As(err, &keyedErr) || keyedErr.ActiveKeys != 1 || keyedErr.MaxKeys != 1 || keyedErr.Key != "b" {
		t.Fatalf("unexpected keyed capacity error: %+v", keyedErr)
	}
	m.Unlock("a")
}

func TestKeyedMutex_GuardAndZeroValue(t *testing.T) {
	var m KeyedMutex[int]
	if m.MaxKeys() != DefaultMaxKeys {
		t.Fatalf("expected default maximum %d, got %d", DefaultMaxKeys, m.MaxKeys())
	}
	guard, err := m.LockGuard(context.Background(), 7)
	if err != nil {
		t.Fatalf("expected keyed guard, got %v", err)
	}
	if guard.Key() != 7 || guard.AttemptID() == 0 {
		t.Fatalf("unexpected keyed guard identity: key=%d attempt=%d", guard.Key(), guard.AttemptID())
	}
	guard.Unlock()
	if !guard.Released() || m.ActiveKeys() != 0 {
		t.Fatalf("unexpected keyed guard state: released=%t active=%d", guard.Released(), m.ActiveKeys())
	}
}

func TestKeyedMutex_NilContextDoesNotRetainKey(t *testing.T) {
	m := NewKeyedMutex[string]("jobs")
	message := panicText(t, func() { m.LockContext(nil, "a") })
	if !strings.Contains(message, `keyed lock "jobs" key=a`) || !strings.Contains(message, "received nil") {
		t.Fatalf("nil-context error omitted keyed diagnostic context: %s", message)
	}
	if m.ActiveKeys() != 0 {
		t.Fatalf("expected nil LockContext to retain no keys, active=%d", m.ActiveKeys())
	}
	m.Lock("held")
	requirePanic(t, func() { m.LockGuard(nil, "other") })
	if m.ActiveKeys() != 1 {
		t.Fatalf("expected nil LockGuard to retain no additional key, active=%d", m.ActiveKeys())
	}
	m.Unlock("held")
}

func TestKeyedMutex_PreCancelledContextWinsOverCapacity(t *testing.T) {
	m := NewKeyedMutexWithLimit[string]("jobs", 1)
	m.Lock("held")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.LockContext(ctx, "other"); !errors.Is(err, context.Canceled) || errors.Is(err, ErrMaxKeys) {
		t.Fatalf("expected context cancellation instead of capacity rejection, got %v", err)
	}
	if guard, err := m.LockGuard(ctx, "other"); guard != nil || !errors.Is(err, context.Canceled) || errors.Is(err, ErrMaxKeys) {
		t.Fatalf("expected cancelled guard without capacity rejection, guard=%v err=%v", guard, err)
	}
	if m.ActiveKeys() != 1 {
		t.Fatalf("expected cancelled operations to retain no additional key, active=%d", m.ActiveKeys())
	}
	m.Unlock("held")
}

func TestKeyedMutex_CancelledGuardReleasesReference(t *testing.T) {
	m := NewKeyedMutex[string]("jobs")
	m.Lock("a")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		guard, err := m.LockGuard(ctx, "a")
		if guard != nil {
			guard.Unlock()
		}
		result <- err
	}()
	waitForLockState(t, func() LockState {
		state, _ := m.Snapshot("a")
		return state
	}, func(state LockState) bool {
		return state.WaitingWriters == 1
	})
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled guard acquisition, got %v", err)
	}
	m.Unlock("a")
	if m.ActiveKeys() != 0 {
		t.Fatalf("expected cancelled guard reference cleanup, active=%d", m.ActiveKeys())
	}
}

func TestKeyGuard_ConcurrentReleaseAllowsExactlyOne(t *testing.T) {
	m := NewKeyedMutex[string]("jobs")
	guard, err := m.LockGuard(context.Background(), "a")
	if err != nil {
		t.Fatalf("expected keyed guard, got %v", err)
	}
	panicked := make(chan bool, 2)
	for index := 0; index < 2; index++ {
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
	for index := 0; index < 2; index++ {
		if <-panicked {
			panicCount++
		}
	}
	if panicCount != 1 || m.ActiveKeys() != 0 {
		t.Fatalf("expected one release and one panic, panics=%d active=%d", panicCount, m.ActiveKeys())
	}
}
