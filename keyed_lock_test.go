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

func TestKeyedMutex_SameKeyAcquiresInFIFOOrderAndCleansUp(t *testing.T) {
	m := NewKeyedMutex[string]("before")
	m.SetLocation("accounts")
	m.Lock("customer-7")
	type acquisition struct {
		identifier int
		err        error
	}
	acquired := make(chan acquisition, 2)
	releases := []chan struct{}{make(chan struct{}), make(chan struct{})}
	done := make(chan struct{}, len(releases))
	for index, release := range releases {
		identifier := index + 1
		go func() {
			err := m.LockContext(context.Background(), "customer-7")
			acquired <- acquisition{identifier: identifier, err: err}
			if err == nil {
				<-release
				m.Unlock("customer-7")
			}
			done <- struct{}{}
		}()
		waitForLockState(t, func() LockState {
			state, _ := m.Snapshot("customer-7")
			return state
		}, func(state LockState) bool { return state.WaitingWriters == identifier })
	}
	if m.ActiveKeys() != 1 {
		t.Fatalf("expected one shared keyed entry, active=%d", m.ActiveKeys())
	}
	m.Unlock("customer-7")
	for index, release := range releases {
		observed := <-acquired
		if observed.err != nil || observed.identifier != index+1 {
			t.Fatalf("expected acquisition %d, got identifier=%d err=%v", index+1, observed.identifier, observed.err)
		}
		close(release)
	}
	for range releases {
		<-done
	}
	if m.ActiveKeys() != 0 || m.Name() != "accounts" {
		t.Fatalf("expected keyed entry cleanup with stable name, name=%q active=%d", m.Name(), m.ActiveKeys())
	}
	if state, found := m.Snapshot("customer-7"); found || state.Name != "accounts" {
		t.Fatalf("unexpected missing-key snapshot: found=%t state=%+v", found, state)
	}
	message := panicText(t, func() { m.SetLocation("changed") })
	if !strings.Contains(message, `current="accounts"`) || !strings.Contains(message, `requested="changed"`) {
		t.Fatalf("post-use name error omitted identities: %s", message)
	}
}

func TestKeyedMutex_CapacityFailuresRetainNoEntries(t *testing.T) {
	m := NewKeyedMutexWithLimit[string]("accounts", 1)
	m.Lock("held")
	if m.TryLock("other") {
		m.Unlock("other")
		t.Fatal("expected active-key limit to reject TryLock")
	}
	guard, guardErr := m.LockGuard(context.Background(), "other")
	if guard != nil || !errors.Is(guardErr, ErrMaxKeys) {
		t.Fatalf("expected guard capacity rejection, guard=%v err=%v", guard, guardErr)
	}
	err := m.LockContext(context.Background(), "other")
	if !errors.Is(err, ErrMaxKeys) || !strings.Contains(err.Error(), `key=other`) || !strings.Contains(err.Error(), `active_keys=1 maximum_keys=1`) {
		t.Fatalf("unexpected keyed capacity error: %v", err)
	}
	if state, found := m.Snapshot("other"); found || state.Name != "accounts" {
		t.Fatalf("capacity failure retained an entry: found=%t state=%+v", found, state)
	}
	message := panicText(t, func() { m.Lock("other") })
	if !strings.Contains(message, ErrMaxKeys.Error()) {
		t.Fatalf("blocking capacity panic omitted cause: %s", message)
	}
	unknown := panicText(t, func() { m.Unlock("missing") })
	if !strings.Contains(unknown, `key missing`) || !strings.Contains(unknown, `"accounts"`) {
		t.Fatalf("unknown-key unlock omitted identity: %s", unknown)
	}
	if m.ActiveKeys() != 1 {
		t.Fatalf("capacity failures changed active key count: %d", m.ActiveKeys())
	}
	m.Unlock("held")
}

func TestKeyedMutex_SameKeyHandoffRetainsDistinctKeyCapacity(t *testing.T) {
	m := NewKeyedMutexWithLimit[string]("accounts", 1)
	m.Lock("account-a")
	waiterAcquired := make(chan error, 1)
	waiterRelease := make(chan struct{})
	waiterDone := make(chan struct{})
	go func() {
		err := m.LockContext(context.Background(), "account-a")
		waiterAcquired <- err
		if err == nil {
			<-waiterRelease
			m.Unlock("account-a")
		}
		close(waiterDone)
	}()
	waitForLockState(t, func() LockState {
		state, _ := m.Snapshot("account-a")
		return state
	}, func(state LockState) bool { return state.WaitingWriters == 1 })
	if m.ActiveKeys() != 1 {
		t.Fatalf("same-key waiter consumed another key slot: active=%d", m.ActiveKeys())
	}
	if err := m.LockContext(context.Background(), "account-b"); !errors.Is(err, ErrMaxKeys) {
		t.Fatalf("expected distinct-key capacity rejection before handoff, got %v", err)
	}
	m.Unlock("account-a")
	if err := <-waiterAcquired; err != nil {
		t.Fatalf("expected same-key waiter to acquire, got %v", err)
	}
	if m.TryLock("account-b") {
		m.Unlock("account-b")
		t.Fatal("expected handed-off holder to retain the distinct-key slot")
	}
	close(waiterRelease)
	<-waiterDone
	if !m.TryLock("account-b") {
		t.Fatal("expected capacity reuse after the final same-key reference exited")
	}
	m.Unlock("account-b")
	if m.ActiveKeys() != 0 {
		t.Fatalf("expected complete keyed cleanup, active=%d", m.ActiveKeys())
	}
}
