package powerlock

import (
	"context"
	"time"
)

const (
	// DefaultWaitThreshold reports acquisitions that wait for at least one second.
	DefaultWaitThreshold = time.Second
	// DefaultHoldThreshold reports exact acquisitions that remain held for at least five seconds.
	DefaultHoldThreshold = 5 * time.Second
)

// WatchdogRWMutex reports slow waits and long exact holds while providing FIFO context-aware locking.
type WatchdogRWMutex struct {
	configuredRWMutex
	waitThreshold time.Duration
	holdThreshold time.Duration
}

// NewWatchdogRWMutex returns a watchdog lock with the default wait and hold thresholds.
func NewWatchdogRWMutex(name string, observer LockObserver) *WatchdogRWMutex {
	return NewWatchdogRWMutexWithThresholds(name, DefaultWaitThreshold, DefaultHoldThreshold, observer)
}

// NewWatchdogRWMutexWithThresholds returns a watchdog lock with explicit thresholds. Zero disables that threshold.
func NewWatchdogRWMutexWithThresholds(name string, waitThreshold time.Duration, holdThreshold time.Duration, observer LockObserver) *WatchdogRWMutex {
	if name == "" {
		panic(invalidConfiguration("watchdog lock name must not be empty, received an empty name"))
	}
	m := &WatchdogRWMutex{waitThreshold: waitThreshold, holdThreshold: holdThreshold}
	m.core.configure(name, observer, waitThreshold, holdThreshold, true)
	return m
}

// SetLocation changes the non-empty diagnostic name before the first operation and panics after first use.
func (m *WatchdogRWMutex) SetLocation(name string) {
	if name == "" {
		panic(invalidConfiguration("watchdog lock name must not be empty, received an empty name"))
	}
	m.configuredRWMutex.SetLocation(name)
}

// WaitThreshold returns the duration after which a queued acquisition is reported.
func (m *WatchdogRWMutex) WaitThreshold() time.Duration {
	return m.waitThreshold
}

// HoldThreshold returns the duration after which an exact active acquisition is reported.
func (m *WatchdogRWMutex) HoldThreshold() time.Duration {
	return m.holdThreshold
}

// WatchdogMutex is the exclusive-only form of WatchdogRWMutex.
type WatchdogMutex struct {
	lock WatchdogRWMutex
}

// NewWatchdogMutex returns an exclusive watchdog lock with the default thresholds.
func NewWatchdogMutex(name string, observer LockObserver) *WatchdogMutex {
	return NewWatchdogMutexWithThresholds(name, DefaultWaitThreshold, DefaultHoldThreshold, observer)
}

// NewWatchdogMutexWithThresholds returns an exclusive watchdog lock with explicit thresholds. Zero disables that threshold.
func NewWatchdogMutexWithThresholds(name string, waitThreshold time.Duration, holdThreshold time.Duration, observer LockObserver) *WatchdogMutex {
	if name == "" {
		panic(invalidConfiguration("watchdog lock name must not be empty, received an empty name"))
	}
	m := &WatchdogMutex{}
	m.lock.waitThreshold = waitThreshold
	m.lock.holdThreshold = holdThreshold
	m.lock.core.configure(name, observer, waitThreshold, holdThreshold, true)
	return m
}

// Lock acquires the mutex.
func (m *WatchdogMutex) Lock() {
	m.lock.Lock()
}

// LockContext acquires the mutex or returns a cancellation or deadline error.
func (m *WatchdogMutex) LockContext(ctx context.Context) error {
	return m.lock.LockContext(ctx)
}

// Unlock releases the mutex.
func (m *WatchdogMutex) Unlock() {
	m.lock.Unlock()
}

// TryLock attempts to acquire the mutex without waiting.
func (m *WatchdogMutex) TryLock() bool {
	return m.lock.TryLock()
}

// LockGuard returns an exact acquisition guard.
func (m *WatchdogMutex) LockGuard(ctx context.Context) (*LockGuard, error) {
	return m.lock.LockGuard(ctx)
}

// TryLockGuard attempts to acquire the mutex and returns an exact release token on success.
func (m *WatchdogMutex) TryLockGuard() (*LockGuard, bool) {
	return m.lock.TryLockGuard()
}

// SetLocation changes the diagnostic name before the first operation and panics after first use.
func (m *WatchdogMutex) SetLocation(name string) {
	m.lock.SetLocation(name)
}

// Name returns the immutable diagnostic name.
func (m *WatchdogMutex) Name() string {
	return m.lock.Name()
}

// WaitThreshold returns the duration after which a queued acquisition is reported.
func (m *WatchdogMutex) WaitThreshold() time.Duration {
	return m.lock.WaitThreshold()
}

// HoldThreshold returns the duration after which an exact active acquisition is reported.
func (m *WatchdogMutex) HoldThreshold() time.Duration {
	return m.lock.HoldThreshold()
}

// Snapshot returns the current lock state.
func (m *WatchdogMutex) Snapshot() LockState {
	return m.lock.Snapshot()
}
