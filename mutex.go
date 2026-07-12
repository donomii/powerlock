package powerlock

import (
	"context"
	"fmt"
)

// ContextMutex is the exclusive-only form of ContextRWMutex.
type ContextMutex struct {
	lock ContextRWMutex
}

// NewContextMutex returns a FIFO context-aware exclusive lock.
func NewContextMutex(name string) *ContextMutex {
	m := &ContextMutex{}
	m.lock.core.configure(name, nil, 0, 0, false)
	return m
}

// Lock acquires the mutex.
func (m *ContextMutex) Lock() { m.lock.Lock() }

// LockContext acquires the mutex or returns a cancellation or deadline error.
func (m *ContextMutex) LockContext(ctx context.Context) error { return m.lock.LockContext(ctx) }

// TryLock attempts to acquire the mutex without waiting.
func (m *ContextMutex) TryLock() bool { return m.lock.TryLock() }

// Unlock releases the mutex.
func (m *ContextMutex) Unlock() { m.lock.Unlock() }

// SetLocation changes the diagnostic name before the first operation and panics after first use.
func (m *ContextMutex) SetLocation(name string) { m.lock.SetLocation(name) }

// Name returns the immutable diagnostic name.
func (m *ContextMutex) Name() string { return m.lock.Name() }

// Snapshot returns the current lock state.
func (m *ContextMutex) Snapshot() LockState { return m.lock.Snapshot() }

// FairMutex is the explicitly named FIFO form of ContextMutex.
type FairMutex = ContextMutex

// NewFairMutex returns a FIFO context-aware exclusive lock.
func NewFairMutex(name string) *FairMutex { return NewContextMutex(name) }

// CancelMutex is the exclusive-only form of CancelRWMutex.
type CancelMutex struct {
	lock CancelRWMutex
}

// NewCancelMutex returns a cancellable exclusive lock.
func NewCancelMutex(name string) *CancelMutex {
	m := &CancelMutex{}
	m.lock.core.configure(name, nil, 0, 0, false)
	return m
}

// Lock acquires the mutex and panics after cancellation.
func (m *CancelMutex) Lock() { m.lock.Lock() }

// LockContext acquires the mutex or returns a cancellation or deadline error.
func (m *CancelMutex) LockContext(ctx context.Context) error { return m.lock.LockContext(ctx) }

// TryLock attempts to acquire the mutex without waiting.
func (m *CancelMutex) TryLock() bool { return m.lock.TryLock() }

// Unlock releases the mutex.
func (m *CancelMutex) Unlock() { m.lock.Unlock() }

// Cancel permanently rejects queued and future acquisitions.
func (m *CancelMutex) Cancel() { m.lock.Cancel() }

// Cancelled reports whether Cancel has been called.
func (m *CancelMutex) Cancelled() bool { return m.lock.Cancelled() }

// SetLocation changes the diagnostic name before the first operation and panics after first use.
func (m *CancelMutex) SetLocation(name string) { m.lock.SetLocation(name) }

// Name returns the immutable diagnostic name.
func (m *CancelMutex) Name() string { return m.lock.Name() }

// Snapshot returns the current lock state.
func (m *CancelMutex) Snapshot() LockState { return m.lock.Snapshot() }

// MaxMutex is the exclusive-only form of MaxRWMutex.
type MaxMutex struct {
	lock MaxRWMutex
}

// NewMaxMutex returns a bounded exclusive lock using DefaultMaxWaiting.
func NewMaxMutex(name string) *MaxMutex { return NewMaxMutexWithLimit(name, DefaultMaxWaiting) }

// NewMaxMutexWithLimit returns a bounded exclusive lock with the supplied waiter limit.
func NewMaxMutexWithLimit(name string, maxWaiting int) *MaxMutex {
	if maxWaiting < 1 {
		panic(invalidConfiguration(fmt.Sprintf("maximum waiting acquisitions must be at least 1, received=%d", maxWaiting)))
	}
	m := &MaxMutex{}
	m.lock.maxWaiting = maxWaiting
	m.lock.core.configure(name, nil, 0, 0, false)
	return m
}

// Lock acquires the mutex and panics when the waiter limit has already been reached.
func (m *MaxMutex) Lock() { m.lock.Lock() }

// LockContext acquires the mutex or returns a context or queue-saturation error.
func (m *MaxMutex) LockContext(ctx context.Context) error { return m.lock.LockContext(ctx) }

// TryLock attempts to acquire the mutex without joining the waiter queue.
func (m *MaxMutex) TryLock() bool { return m.lock.TryLock() }

// Unlock releases the mutex.
func (m *MaxMutex) Unlock() { m.lock.Unlock() }

// MaxWaiting returns the configured waiter limit.
func (m *MaxMutex) MaxWaiting() int { return m.lock.MaxWaiting() }

// SetLocation changes the diagnostic name before the first operation and panics after first use.
func (m *MaxMutex) SetLocation(name string) { m.lock.SetLocation(name) }

// Name returns the immutable diagnostic name.
func (m *MaxMutex) Name() string { return m.lock.Name() }

// Snapshot returns the current lock state.
func (m *MaxMutex) Snapshot() LockState { return m.lock.Snapshot() }

// ObservedMutex is the exclusive-only form of ObservedRWMutex.
type ObservedMutex struct {
	lock ObservedRWMutex
}

// NewObservedMutex returns an exclusive lock that emits structured events.
func NewObservedMutex(name string, observer LockObserver) *ObservedMutex {
	m := &ObservedMutex{}
	m.lock.core.configure(name, observer, 0, 0, false)
	return m
}

// Lock acquires the mutex.
func (m *ObservedMutex) Lock() { m.lock.Lock() }

// LockContext acquires the mutex or returns a cancellation or deadline error.
func (m *ObservedMutex) LockContext(ctx context.Context) error { return m.lock.LockContext(ctx) }

// TryLock attempts to acquire the mutex without waiting.
func (m *ObservedMutex) TryLock() bool { return m.lock.TryLock() }

// Unlock releases the mutex.
func (m *ObservedMutex) Unlock() { m.lock.Unlock() }

// LockGuard returns an exact acquisition guard.
func (m *ObservedMutex) LockGuard(ctx context.Context) (*LockGuard, error) {
	return m.lock.LockGuard(ctx)
}

// TryLockGuard attempts to acquire the mutex and returns an exact release token on success.
func (m *ObservedMutex) TryLockGuard() (*LockGuard, bool) { return m.lock.TryLockGuard() }

// SetLocation changes the diagnostic name before the first operation and panics after first use.
func (m *ObservedMutex) SetLocation(name string) { m.lock.SetLocation(name) }

// Name returns the immutable diagnostic name.
func (m *ObservedMutex) Name() string { return m.lock.Name() }

// Snapshot returns the current lock state.
func (m *ObservedMutex) Snapshot() LockState { return m.lock.Snapshot() }
