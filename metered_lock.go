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
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// MeteredRWMutex is a FIFO context-aware read/write lock with legacy Prometheus gauges.
type MeteredRWMutex struct {
	configuredRWMutex
	locksWaiting *prometheus.GaugeVec
	locks        *prometheus.GaugeVec
}

// NewMeteredRWMutex returns a metered lock. Supplying two nil gauges creates an unobserved lock.
func NewMeteredRWMutex(location string, locksWaiting *prometheus.GaugeVec, locks *prometheus.GaugeVec) *MeteredRWMutex {
	m := &MeteredRWMutex{
		locksWaiting: locksWaiting,
		locks:        locks,
	}
	m.core.configure(location, m.legacyObserver(location), 0, 0, false)
	return m
}

// SetLocation changes the metric name before the first operation and panics after first use.
func (m *MeteredRWMutex) SetLocation(location string) {
	m.core.configure(location, m.legacyObserver(location), 0, 0, false)
}

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
		return nil, nil, invalidConfiguration("Prometheus registerer must not be nil")
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

func (m *MeteredRWMutex) legacyObserver(location string) LockObserver {
	if m.locksWaiting == nil && m.locks == nil {
		return nil
	}
	if m.locksWaiting == nil || m.locks == nil {
		panic(invalidConfiguration(fmt.Sprintf("both legacy Prometheus gauges must be supplied together: waiting_nil=%t held_nil=%t", m.locksWaiting == nil, m.locks == nil)))
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

func (o *legacyPrometheusObserver) ObserveLock(event LockEvent) {
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
