package powerlock

import (
	"context"
	"sync"
)

// ContextRWMutex is a FIFO read/write lock whose queued acquisitions can be cancelled.
type ContextRWMutex struct {
	core rwCore
}

// NewContextRWMutex returns a context-aware lock with the supplied diagnostic name.
func NewContextRWMutex(location string) *ContextRWMutex {
	m := &ContextRWMutex{}
	m.core.configure(location, nil, 0, 0, false)
	return m
}

// SetLocation changes the diagnostic name before the first operation and panics after first use.
func (m *ContextRWMutex) SetLocation(location string) {
	m.core.setName(location)
}

// Name returns the immutable diagnostic name.
func (m *ContextRWMutex) Name() string {
	return m.core.nameValue()
}

// Lock acquires the write lock.
func (m *ContextRWMutex) Lock() {
	if err := m.LockContext(context.Background()); err != nil {
		panic(err)
	}
}

// LockContext acquires the write lock or returns a cancellation or deadline error.
func (m *ContextRWMutex) LockContext(ctx context.Context) error {
	_, err := m.core.acquire(ctx, LockModeWrite, unlimitedWaiters, false)
	return err
}

// Unlock releases the write lock.
func (m *ContextRWMutex) Unlock() {
	m.core.release(LockModeWrite, 0)
}

// RLock acquires a read lock.
func (m *ContextRWMutex) RLock() {
	if err := m.RLockContext(context.Background()); err != nil {
		panic(err)
	}
}

// RLockContext acquires a read lock or returns a cancellation or deadline error.
func (m *ContextRWMutex) RLockContext(ctx context.Context) error {
	_, err := m.core.acquire(ctx, LockModeRead, unlimitedWaiters, false)
	return err
}

// RUnlock releases one read lock.
func (m *ContextRWMutex) RUnlock() {
	m.core.release(LockModeRead, 0)
}

// TryLock attempts to acquire the write lock without waiting.
func (m *ContextRWMutex) TryLock() bool {
	_, acquired := m.core.tryAcquire(LockModeWrite, false)
	return acquired
}

// TryRLock attempts to acquire a read lock without waiting or bypassing queued acquisitions.
func (m *ContextRWMutex) TryRLock() bool {
	_, acquired := m.core.tryAcquire(LockModeRead, false)
	return acquired
}

// RLocker returns a sync.Locker backed by RLock and RUnlock.
func (m *ContextRWMutex) RLocker() sync.Locker {
	return rwReadLocker{lock: m}
}

// Snapshot returns the current lock state.
func (m *ContextRWMutex) Snapshot() LockState {
	return m.core.snapshot()
}
