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

package powerlockprometheus

import (
	"context"
	"fmt"
	"sync"

	"github.com/donomii/powerlock"
	"github.com/prometheus/client_golang/prometheus"
)

// MeteredRWMutex is the adapter-scoped compatibility lock for the original aggregate Prometheus gauges.
type MeteredRWMutex struct {
	mu           sync.Mutex
	location     string
	used         bool
	lock         *powerlock.ObservedRWMutex
	locksWaiting *prometheus.GaugeVec
	locks        *prometheus.GaugeVec
}

// NewMeteredRWMutex returns a metered lock. Supplying two nil gauges creates an unobserved lock.
func NewMeteredRWMutex(location string, locksWaiting *prometheus.GaugeVec, locks *prometheus.GaugeVec) *MeteredRWMutex {
	m := &MeteredRWMutex{
		location:     location,
		locksWaiting: locksWaiting,
		locks:        locks,
	}
	m.lock = powerlock.NewObservedRWMutex(location, m.legacyObserver(location))
	return m
}

// SetLocation changes the metric name before the first operation and panics after first use.
func (m *MeteredRWMutex) SetLocation(location string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.used {
		panic(fmt.Errorf("powerlock Prometheus legacy adapter: lock name cannot change after first use: current=%q requested=%q", m.location, location))
	}
	m.location = location
	m.lock = powerlock.NewObservedRWMutex(location, m.legacyObserver(location))
}

// Name returns the immutable metric and diagnostic name.
func (m *MeteredRWMutex) Name() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.location
}

// Lock acquires the write lock.
func (m *MeteredRWMutex) Lock() { m.lockForOperation().Lock() }

// LockContext acquires the write lock or returns a cancellation or deadline error.
func (m *MeteredRWMutex) LockContext(ctx context.Context) error {
	return m.lockForOperation().LockContext(ctx)
}

// TryLock attempts to acquire the write lock without waiting.
func (m *MeteredRWMutex) TryLock() bool { return m.lockForOperation().TryLock() }

// Unlock releases the write lock.
func (m *MeteredRWMutex) Unlock() { m.lockForOperation().Unlock() }

// RLock acquires a read lock.
func (m *MeteredRWMutex) RLock() { m.lockForOperation().RLock() }

// RLockContext acquires a read lock or returns a cancellation or deadline error.
func (m *MeteredRWMutex) RLockContext(ctx context.Context) error {
	return m.lockForOperation().RLockContext(ctx)
}

// TryRLock attempts to acquire a read lock without waiting.
func (m *MeteredRWMutex) TryRLock() bool { return m.lockForOperation().TryRLock() }

// RUnlock releases one read lock.
func (m *MeteredRWMutex) RUnlock() { m.lockForOperation().RUnlock() }

// RLocker returns a sync.Locker backed by RLock and RUnlock.
func (m *MeteredRWMutex) RLocker() sync.Locker { return legacyReadLocker{lock: m} }

// LockGuard acquires the write lock and returns an exact release token.
func (m *MeteredRWMutex) LockGuard(ctx context.Context) (*powerlock.LockGuard, error) {
	return m.lockForOperation().LockGuard(ctx)
}

// RLockGuard acquires a read lock and returns an exact release token.
func (m *MeteredRWMutex) RLockGuard(ctx context.Context) (*powerlock.LockGuard, error) {
	return m.lockForOperation().RLockGuard(ctx)
}

// TryLockGuard attempts to acquire the write lock and returns an exact release token on success.
func (m *MeteredRWMutex) TryLockGuard() (*powerlock.LockGuard, bool) {
	return m.lockForOperation().TryLockGuard()
}

// TryRLockGuard attempts to acquire a read lock and returns an exact release token on success.
func (m *MeteredRWMutex) TryRLockGuard() (*powerlock.LockGuard, bool) {
	return m.lockForOperation().TryRLockGuard()
}

// Snapshot returns the current lock state.
func (m *MeteredRWMutex) Snapshot() powerlock.LockState { return m.lockForReadout().Snapshot() }

// NewLockMetrics creates the Prometheus metrics needed for MeteredRWMutex.
// It panics when registration fails; RegisterLockMetrics provides the error-returning form.
func NewLockMetrics(registerer prometheus.Registerer) (*prometheus.GaugeVec, *prometheus.GaugeVec) {
	locksWaiting, locks, err := RegisterLockMetrics(registerer)
	if err != nil {
		panic(err)
	}
	return locksWaiting, locks
}

// RegisterLockMetrics creates and registers the legacy MeteredRWMutex gauges.
func RegisterLockMetrics(registerer prometheus.Registerer) (*prometheus.GaugeVec, *prometheus.GaugeVec, error) {
	if registerer == nil {
		return nil, nil, fmt.Errorf("powerlock Prometheus legacy adapter: registerer must not be nil")
	}
	locksWaiting := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "powerlock",
		Name:      "locks_waiting",
		Help:      "Number of goroutines waiting on a lock at a specific location",
	}, []string{"location"})

	locks := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "powerlock",
		Name:      "locks_held",
		Help:      "Number of goroutines currently holding a lock at a specific location",
	}, []string{"location"})

	if err := registerer.Register(locksWaiting); err != nil {
		return nil, nil, fmt.Errorf("powerlock: register waiting-lock gauge: %w", err)
	}
	if err := registerer.Register(locks); err != nil {
		registerer.Unregister(locksWaiting)
		return nil, nil, fmt.Errorf("powerlock: register held-lock gauge: %w", err)
	}
	return locksWaiting, locks, nil
}

func (m *MeteredRWMutex) legacyObserver(location string) powerlock.LockObserver {
	if m.locksWaiting == nil && m.locks == nil {
		return nil
	}
	if m.locksWaiting == nil || m.locks == nil {
		panic(fmt.Errorf("powerlock Prometheus legacy adapter: both gauges must be supplied together: waiting_nil=%t held_nil=%t", m.locksWaiting == nil, m.locks == nil))
	}
	return &legacyPrometheusObserver{
		waiting: m.locksWaiting.WithLabelValues(location),
		held:    m.locks.WithLabelValues(location),
	}
}

type legacyPrometheusObserver struct {
	waiting prometheus.Gauge
	held    prometheus.Gauge
	mu      sync.Mutex
	version uint64
}

type legacyReadLocker struct {
	lock *MeteredRWMutex
}

func (l legacyReadLocker) Lock()   { l.lock.RLock() }
func (l legacyReadLocker) Unlock() { l.lock.RUnlock() }

func (o *legacyPrometheusObserver) ObserveLock(event powerlock.LockEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if event.State.Version < o.version {
		return
	}
	o.version = event.State.Version
	o.waiting.Set(float64(event.State.WaitingReaders + event.State.WaitingWriters))
	held := event.State.Readers
	if event.State.Writer {
		held++
	}
	o.held.Set(float64(held))
}

func (m *MeteredRWMutex) lockForOperation() *powerlock.ObservedRWMutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.used = true
	if m.lock == nil {
		m.lock = powerlock.NewObservedRWMutex(m.location, m.legacyObserver(m.location))
	}
	return m.lock
}

func (m *MeteredRWMutex) lockForReadout() *powerlock.ObservedRWMutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lock == nil {
		m.lock = powerlock.NewObservedRWMutex(m.location, m.legacyObserver(m.location))
	}
	return m.lock
}
