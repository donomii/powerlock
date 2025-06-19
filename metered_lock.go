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

// Package ctxlock provides a read/write mutex with Prometheus-based metering of lock usage.
// It allows observability into how many goroutines are waiting or holding a lock at specific locations.
package ctxlock

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// MeteredRWMutex is a read/write mutex with metering for lock usage.  It provides insight into how many threads are waiting on each lock, and how many are currently holding the lock (or read lock).
type MeteredRWMutex struct {
	rwlock       sync.RWMutex
	location     string
	locksWaiting *prometheus.GaugeVec
	locks        *prometheus.GaugeVec
}

func NewMeteredRWMutex(location string, locksWaiting *prometheus.GaugeVec, locks *prometheus.GaugeVec) *MeteredRWMutex {
	return &MeteredRWMutex{
		location:     location,
		locksWaiting: locksWaiting,
		locks:        locks,
	}
}

// CtxRWLocation updates the lock location label for metric reporting.
func (m *MeteredRWMutex) CtxRWLocation(location string) {
	m.location = location
}

// Lock acquires the write lock and updates the Prometheus metrics.
func (m *MeteredRWMutex) Lock() {
	m.locksWaiting.WithLabelValues(m.location).Inc()
	m.rwlock.Lock()
	m.locksWaiting.WithLabelValues(m.location).Dec()
	m.locks.WithLabelValues(m.location).Inc()
}

// TryLock attempts to acquire the write lock and updates the metrics if successful.
func (m *MeteredRWMutex) TryLock() bool {
	ok := m.rwlock.TryLock()
	if ok {
		m.locks.WithLabelValues(m.location).Inc()
	}
	return ok
}

// Unlock releases the write lock and updates the metrics.
func (m *MeteredRWMutex) Unlock() {
	m.locks.WithLabelValues(m.location).Dec()
	m.rwlock.Unlock()
}

// RLock acquires the read lock and updates the Prometheus metrics.
func (m *MeteredRWMutex) RLock() {
	m.locksWaiting.WithLabelValues(m.location).Inc()
	m.rwlock.RLock()
	m.locksWaiting.WithLabelValues(m.location).Dec()
	m.locks.WithLabelValues(m.location).Inc()
}

// TryRLock attempts to acquire the read lock and updates the metrics if successful.
func (m *MeteredRWMutex) TryRLock() bool {
	ok := m.rwlock.TryRLock()
	if ok {
		m.locks.WithLabelValues(m.location).Inc()
	}
	return ok
}

// RTryLock is an alias for TryRLock.
func (m *MeteredRWMutex) RTryLock() bool {
	return m.TryRLock()
}

// RUnlock releases the read lock and updates the metrics.
func (m *MeteredRWMutex) RUnlock() {
	m.locks.WithLabelValues(m.location).Dec()
	m.rwlock.RUnlock()
}

// NewLockMetrics creates the Prometheus metrics needed for MeteredRWMutex.
// It registers them with the provided Prometheus registerer (e.g., prometheus.DefaultRegisterer).
func NewLockMetrics(registerer prometheus.Registerer) (*prometheus.GaugeVec, *prometheus.GaugeVec) {
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

	registerer.MustRegister(locksWaiting, locks)

	return locksWaiting, locks
}
