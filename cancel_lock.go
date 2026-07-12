// Copyright (c) 2026 Donomii.
// Licensed under the GNU AGPLv3; see LICENSE.

package powerlock

import (
	"context"
	"sync"
)

// CancelRWMutex is a FIFO read/write lock that permanently rejects acquisitions after cancellation.
type CancelRWMutex struct {
	core rwCore
}

// NewCancelRWMutex returns a cancellable lock with the supplied diagnostic name.
func NewCancelRWMutex(location string) *CancelRWMutex {
	m := &CancelRWMutex{}
	m.core.configure(location, nil, 0, 0, false)
	return m
}

// SetLocation changes the diagnostic name before the first operation and panics after first use.
func (m *CancelRWMutex) SetLocation(location string) {
	m.core.setName(location)
}

// Name returns the immutable diagnostic name.
func (m *CancelRWMutex) Name() string {
	return m.core.nameValue()
}

// Cancel permanently rejects queued and future acquisitions while allowing current holders to unlock.
func (m *CancelRWMutex) Cancel() {
	m.core.cancel()
}

// Cancelled reports whether Cancel has been called.
func (m *CancelRWMutex) Cancelled() bool {
	return m.core.isCancelled()
}

//
// Write locks
//

// Lock acquires the write lock and panics if the lock has been cancelled.
func (m *CancelRWMutex) Lock() {
	if err := m.LockContext(context.Background()); err != nil {
		panic(err)
	}
}

// LockContext acquires the write lock or returns a cancellation or deadline error.
func (m *CancelRWMutex) LockContext(ctx context.Context) error {
	_, err := m.core.acquire(ctx, LockModeWrite, unlimitedWaiters, false)
	return err
}

// Unlock releases the write lock.
func (m *CancelRWMutex) Unlock() {
	m.core.release(LockModeWrite, 0)
}

//
// Read locks
//

// RLock acquires the read lock and panics if the lock has been cancelled.
func (m *CancelRWMutex) RLock() {
	if err := m.RLockContext(context.Background()); err != nil {
		panic(err)
	}
}

// RLockContext acquires the read lock or returns a cancellation or deadline error.
func (m *CancelRWMutex) RLockContext(ctx context.Context) error {
	_, err := m.core.acquire(ctx, LockModeRead, unlimitedWaiters, false)
	return err
}

// RUnlock releases one read lock.
func (m *CancelRWMutex) RUnlock() {
	m.core.release(LockModeRead, 0)
}

//
// Helpers
//

// TryLock attempts to acquire the write lock without waiting.
func (m *CancelRWMutex) TryLock() bool {
	_, acquired := m.core.tryAcquire(LockModeWrite, false)
	return acquired
}

// TryRLock attempts to acquire a read lock without waiting or bypassing queued acquisitions.
func (m *CancelRWMutex) TryRLock() bool {
	_, acquired := m.core.tryAcquire(LockModeRead, false)
	return acquired
}

// RLocker returns a sync.Locker backed by RLock and RUnlock.
func (m *CancelRWMutex) RLocker() sync.Locker {
	return rwReadLocker{lock: m}
}

// Snapshot returns the current lock state.
func (m *CancelRWMutex) Snapshot() LockState {
	return m.core.snapshot()
}
