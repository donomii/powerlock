package powerlock

import (
	"context"
	"sync"
)

type configuredRWMutex struct {
	core rwCore
}

// SetLocation changes the diagnostic name before the first operation and panics after first use.
func (m *configuredRWMutex) SetLocation(location string) {
	m.core.setName(location)
}

// Name returns the immutable diagnostic name.
func (m *configuredRWMutex) Name() string {
	return m.core.nameValue()
}

// Lock acquires the write lock.
func (m *configuredRWMutex) Lock() {
	if err := m.LockContext(context.Background()); err != nil {
		panic(err)
	}
}

// LockContext acquires the write lock or returns a cancellation or deadline error.
func (m *configuredRWMutex) LockContext(ctx context.Context) error {
	_, err := m.core.acquire(ctx, LockModeWrite, unlimitedWaiters, false)
	return err
}

// Unlock releases the write lock.
func (m *configuredRWMutex) Unlock() {
	m.core.release(LockModeWrite, 0)
}

// RLock acquires a read lock.
func (m *configuredRWMutex) RLock() {
	if err := m.RLockContext(context.Background()); err != nil {
		panic(err)
	}
}

// RLockContext acquires a read lock or returns a cancellation or deadline error.
func (m *configuredRWMutex) RLockContext(ctx context.Context) error {
	_, err := m.core.acquire(ctx, LockModeRead, unlimitedWaiters, false)
	return err
}

// RUnlock releases one read lock.
func (m *configuredRWMutex) RUnlock() {
	m.core.release(LockModeRead, 0)
}

// TryLock attempts to acquire the write lock without waiting.
func (m *configuredRWMutex) TryLock() bool {
	_, acquired := m.core.tryAcquire(LockModeWrite, false)
	return acquired
}

// TryRLock attempts to acquire a read lock without waiting or bypassing queued acquisitions.
func (m *configuredRWMutex) TryRLock() bool {
	_, acquired := m.core.tryAcquire(LockModeRead, false)
	return acquired
}

// RLocker returns a sync.Locker backed by RLock and RUnlock.
func (m *configuredRWMutex) RLocker() sync.Locker {
	return rwReadLocker{lock: m}
}

// LockGuard acquires the write lock and returns an exact release token.
func (m *configuredRWMutex) LockGuard(ctx context.Context) (*LockGuard, error) {
	attemptID, err := m.core.acquire(ctx, LockModeWrite, unlimitedWaiters, true)
	if err != nil {
		return nil, err
	}
	return newLockGuard(&m.core, LockModeWrite, attemptID), nil
}

// RLockGuard acquires a read lock and returns an exact release token.
func (m *configuredRWMutex) RLockGuard(ctx context.Context) (*LockGuard, error) {
	attemptID, err := m.core.acquire(ctx, LockModeRead, unlimitedWaiters, true)
	if err != nil {
		return nil, err
	}
	return newLockGuard(&m.core, LockModeRead, attemptID), nil
}

// TryLockGuard attempts to acquire the write lock and returns an exact release token on success.
func (m *configuredRWMutex) TryLockGuard() (*LockGuard, bool) {
	attemptID, acquired := m.core.tryAcquire(LockModeWrite, true)
	if !acquired {
		return nil, false
	}
	return newLockGuard(&m.core, LockModeWrite, attemptID), true
}

// TryRLockGuard attempts to acquire a read lock and returns an exact release token on success.
func (m *configuredRWMutex) TryRLockGuard() (*LockGuard, bool) {
	attemptID, acquired := m.core.tryAcquire(LockModeRead, true)
	if !acquired {
		return nil, false
	}
	return newLockGuard(&m.core, LockModeRead, attemptID), true
}

// Snapshot returns the current lock state.
func (m *configuredRWMutex) Snapshot() LockState {
	return m.core.snapshot()
}

// ObservedRWMutex is a FIFO context-aware read/write lock that emits structured events.
type ObservedRWMutex struct {
	configuredRWMutex
}

// NewObservedRWMutex returns an observed lock. A nil observer disables event delivery.
func NewObservedRWMutex(name string, observer LockObserver) *ObservedRWMutex {
	m := &ObservedRWMutex{}
	m.core.configure(name, observer, 0, 0, false)
	return m
}
