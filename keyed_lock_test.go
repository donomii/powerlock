package powerlock

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"testing"
	"time"
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

func TestKeyedMutex_RejectsNonReflexiveKeys(t *testing.T) {
	t.Run("float64", func(t *testing.T) {
		checkKeyedMutexRejectsInvalidKey(t, math.NaN(), float64(7))
	})
	t.Run("float32", func(t *testing.T) {
		checkKeyedMutexRejectsInvalidKey(t, float32(math.NaN()), float32(7))
	})
	t.Run("complex real", func(t *testing.T) {
		checkKeyedMutexRejectsInvalidKey(t, complex(math.NaN(), 0), complex(7, 0))
	})
	t.Run("complex imaginary", func(t *testing.T) {
		checkKeyedMutexRejectsInvalidKey(t, complex64(complex(0, math.NaN())), complex64(7))
	})
	t.Run("struct", func(t *testing.T) {
		type key struct{ value float64 }
		checkKeyedMutexRejectsInvalidKey(t, key{math.NaN()}, key{7})
	})
	t.Run("array", func(t *testing.T) {
		checkKeyedMutexRejectsInvalidKey(t, [1]float64{math.NaN()}, [1]float64{7})
	})
	t.Run("nested complex", func(t *testing.T) {
		type key struct{ values [2]complex128 }
		checkKeyedMutexRejectsInvalidKey(t, key{[2]complex128{0, complex(0, math.NaN())}}, key{})
	})
}

func TestKeyedMutex_RejectsNonComparableDynamicKeys(t *testing.T) {
	checkKeyedMutexRejectsInvalidKey[fmt.Stringer](t, net.IPv4(127, 0, 0, 1), nil)
	type key struct{ address fmt.Stringer }
	checkKeyedMutexRejectsInvalidKey(t, key{net.IPv4(127, 0, 0, 1)}, key{})
}

func checkKeyedMutexRejectsInvalidKey[K comparable](t *testing.T, invalidKey, validKey K) {
	t.Helper()
	for _, occupied := range []bool{false, true} {
		m := NewKeyedMutexWithLimit[K]("invalid-keys", 1)
		expectedActive := 0
		if occupied {
			m.Lock(validKey)
			expectedActive = 1
		}
		assertInvalidKeyAcquisitions(t, m, invalidKey, expectedActive)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := m.LockContext(ctx, invalidKey); !errors.Is(err, context.Canceled) || errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("expected context cancellation before invalid-key rejection, got %v", err)
		}
		if guard, err := m.LockGuard(ctx, invalidKey); guard != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation without a guard, guard=%v err=%v", guard, err)
		}
		message := panicText(t, func() { m.LockContext(nil, invalidKey) })
		if !strings.Contains(message, "requires a non-nil context") {
			t.Fatalf("expected nil-context rejection before invalid-key rejection, got %s", message)
		}
		message = panicText(t, func() { m.LockGuard(nil, invalidKey) })
		if !strings.Contains(message, "requires a non-nil context") {
			t.Fatalf("expected nil-context guard rejection before invalid-key rejection, got %s", message)
		}
		if active := m.ActiveKeys(); active != expectedActive {
			t.Fatalf("invalid key changed capacity: expected=%d received=%d", expectedActive, active)
		}
		if occupied {
			m.Unlock(validKey)
		}
		if !m.TryLock(validKey) {
			t.Fatal("invalid key prevented a valid key from acquiring available capacity")
		}
		m.Unlock(validKey)
		if active := m.ActiveKeys(); active != 0 {
			t.Fatalf("expected complete keyed cleanup, active=%d", active)
		}
	}
}

func assertInvalidKeyAcquisitions[K comparable](t *testing.T, m *KeyedMutex[K], key K, expectedActive int) {
	t.Helper()
	err := m.LockContext(context.Background(), key)
	var keyedErr *KeyedAcquisitionError[K]
	if !errors.Is(err, ErrInvalidConfig) || !errors.As(err, &keyedErr) {
		t.Fatalf("expected typed invalid-key error, got %v", err)
	}
	if keyedErr.Name != "invalid-keys" || keyedErr.MaxKeys != 1 || keyedErr.ActiveKeys != expectedActive {
		t.Fatalf("invalid-key error omitted capacity or identity: %+v", keyedErr)
	}
	if guard, err := m.LockGuard(context.Background(), key); guard != nil || !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected invalid-key error without a guard, guard=%v err=%v", guard, err)
	}
	if m.TryLock(key) {
		t.Fatal("expected invalid key to reject TryLock")
	}
	message := panicText(t, func() { m.Lock(key) })
	if !strings.Contains(message, ErrInvalidConfig.Error()) || !strings.Contains(message, "equal to themselves") {
		t.Fatalf("blocking invalid-key panic omitted its cause: %s", message)
	}
}

func TestKeyedMutex_PreCancelledContextReportsExistingKeyState(t *testing.T) {
	m := NewKeyedMutex[string]("accounts")
	m.Lock("held")
	defer m.Unlock("held")
	waiterContext, cancelWaiter := context.WithCancel(context.Background())
	defer cancelWaiter()
	waiterResult := make(chan error, 1)
	go func() {
		err := m.LockContext(waiterContext, "held")
		if err == nil {
			m.Unlock("held")
		}
		waiterResult <- err
	}()
	waitForLockState(t, func() LockState {
		state, _ := m.Snapshot("held")
		return state
	}, func(state LockState) bool { return state.WaitingWriters == 1 })
	before, _ := m.Snapshot("held")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancelExpired()
	for _, ctx := range []context.Context{cancelled, expired} {
		for _, guarded := range []bool{false, true} {
			var err error
			if guarded {
				_, err = m.LockGuard(ctx, "held")
			} else {
				err = m.LockContext(ctx, "held")
			}
			var acquisitionErr *AcquisitionError
			if !errors.Is(err, ctx.Err()) || !errors.As(err, &acquisitionErr) {
				t.Fatalf("expected typed context error, guarded=%t err=%v", guarded, err)
			}
			state := acquisitionErr.State
			if state.Name != before.Name || state.Version != before.Version || !state.Writer || state.WaitingWriters != 1 {
				t.Fatalf("context error omitted held-key state: before=%+v received=%+v", before, state)
			}
			if state.OldestWaitDuration < before.OldestWaitDuration || !strings.Contains(err.Error(), "writer=true") {
				t.Fatalf("context error lost wait duration or held state: before=%+v received=%+v err=%v", before, state, err)
			}
		}
	}
	cancelWaiter()
	if err := <-waiterResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected queued acquisition cancellation, got %v", err)
	}
}
