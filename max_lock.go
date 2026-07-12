//                           _       _
// __      _____  __ ___   ___  __ _| |_ ___
// \ \ /\ / / _ \/ _` \ \ / / |/ _` | __/ _ \
//  \ V  V /  __/ (_| |\ V /| | (_| | ||  __/
//   \_/\_/ \___|\__,_| \_/ |_|\__,_|\__\___|
//
//  Copyright © 2016 - 2024 Weaviate B.V. All rights reserved.
//
//  CONTACT: hello@weaviate.io
//

package powerlock

import (
	"context"
	"fmt"
	"sync"
)

// DefaultMaxWaiting is the waiter limit used by NewMaxRWMutex and the zero value.
const DefaultMaxWaiting = 1

// MaxRWMutex is a FIFO read/write mutex that limits blocked acquisitions without limiting current holders.
type MaxRWMutex struct {
	core       rwCore
	maxWaiting int
}

// NewMaxRWMutex returns a bounded lock using DefaultMaxWaiting.
func NewMaxRWMutex(location string) *MaxRWMutex {
	return NewMaxRWMutexWithLimit(location, DefaultMaxWaiting)
}

// NewMaxRWMutexWithLimit returns a bounded lock that permits at most maxWaiting blocked acquisitions.
func NewMaxRWMutexWithLimit(location string, maxWaiting int) *MaxRWMutex {
	if maxWaiting < 1 {
		panic(invalidConfiguration(fmt.Sprintf("maximum waiting acquisitions must be at least 1, received=%d", maxWaiting)))
	}

	m := &MaxRWMutex{maxWaiting: maxWaiting}
	m.core.configure(location, nil, 0, 0, false)
	return m
}

// SetLocation changes the diagnostic name before the first operation and panics after first use.
func (m *MaxRWMutex) SetLocation(location string) {
	m.core.setName(location)
}

// Name returns the immutable diagnostic name.
func (m *MaxRWMutex) Name() string {
	return m.core.nameValue()
}

// MaxWaiting returns the configured waiter limit.
func (m *MaxRWMutex) MaxWaiting() int {
	return m.effectiveMaxWaiting()
}

// Lock acquires the write lock and panics when the waiter limit has already been reached.
func (m *MaxRWMutex) Lock() {
	if err := m.LockContext(context.Background()); err != nil {
		panic(err)
	}
}

// LockContext acquires the write lock or returns a context or queue-saturation error.
func (m *MaxRWMutex) LockContext(ctx context.Context) error {
	_, err := m.core.acquire(ctx, LockModeWrite, m.effectiveMaxWaiting(), false)
	return err
}

// TryLock attempts to acquire the write lock without joining the waiter queue.
func (m *MaxRWMutex) TryLock() bool {
	_, acquired := m.core.tryAcquire(LockModeWrite, false)
	return acquired
}

// Unlock releases the write lock.
func (m *MaxRWMutex) Unlock() {
	m.core.release(LockModeWrite, 0)
}

// RLock acquires a read lock and panics when the waiter limit has already been reached.
func (m *MaxRWMutex) RLock() {
	if err := m.RLockContext(context.Background()); err != nil {
		panic(err)
	}
}

// RLockContext acquires a read lock or returns a context or queue-saturation error.
func (m *MaxRWMutex) RLockContext(ctx context.Context) error {
	_, err := m.core.acquire(ctx, LockModeRead, m.effectiveMaxWaiting(), false)
	return err
}

// TryRLock attempts to acquire a read lock without joining or bypassing the waiter queue.
func (m *MaxRWMutex) TryRLock() bool {
	_, acquired := m.core.tryAcquire(LockModeRead, false)
	return acquired
}

// RUnlock releases one read lock.
func (m *MaxRWMutex) RUnlock() {
	m.core.release(LockModeRead, 0)
}

// RLocker returns a sync.Locker backed by RLock and RUnlock.
func (m *MaxRWMutex) RLocker() sync.Locker {
	return rwReadLocker{lock: m}
}

// Snapshot returns the current lock state.
func (m *MaxRWMutex) Snapshot() LockState {
	return m.core.snapshot()
}

func (m *MaxRWMutex) effectiveMaxWaiting() int {
	if m.maxWaiting == 0 {
		return DefaultMaxWaiting
	}
	return m.maxWaiting
}
