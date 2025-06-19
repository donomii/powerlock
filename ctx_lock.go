//                           _       _
// __      _____  __ ___   ___  __ _| |_ ___
// \ \ /\ / / _ \/ _` \ \ / / |/ _` | __/ _ \
//  \V  V /  __/ (_| |\ V /| | (_| | ||  __/
//   \_/\_/ \___|\__,_| \_/ |_|\__,_|\__\___|
//
//  Copyright © 2016 - 2024 Weaviate B.V. All rights reserved.
//
//  CONTACT: hello@weaviate.io
//

package powerlock

import (
	"errors"
	"sync"
	"sync/atomic"
)

var ErrCancelled = errors.New("powerlock: lock was cancelled")

type waiter struct {
	done chan struct{}
	isWriter bool
}

type CancelRWMutex struct {
	location string

	readerCount int32         // number of active readers
	writer      chan struct{} // single-slot semaphore for writers
	mu          sync.Mutex    // protects coordination and waiters list
	cancelled   int32         // atomic flag for cancellation state
	waiters     []*waiter     // list of waiting goroutines
}

func NewCancelRWMutex(location string) *CancelRWMutex {
	return &CancelRWMutex{
		location: location,
		writer:   make(chan struct{}, 1),
		waiters:  make([]*waiter, 0),
	}
}

func (m *CancelRWMutex) SetLocation(location string) {
	m.location = location
}

func (m *CancelRWMutex) isCancelled() bool {
	return atomic.LoadInt32(&m.cancelled) == 1
}

// Cancel marks this mutex as cancelled and rejects all current and future waiters
func (m *CancelRWMutex) Cancel() {
	atomic.StoreInt32(&m.cancelled, 1)
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Notify all waiting goroutines
	for _, w := range m.waiters {
		close(w.done)
	}
	m.waiters = m.waiters[:0] // Clear the slice
}

func (m *CancelRWMutex) addWaiter(isWriter bool) *waiter {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.isCancelled() {
		return nil
	}
	
	w := &waiter{
		done: make(chan struct{}),
		isWriter: isWriter,
	}
	m.waiters = append(m.waiters, w)
	return w
}

func (m *CancelRWMutex) removeWaiter(target *waiter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	for i, w := range m.waiters {
		if w == target {
			// Remove from slice
			m.waiters = append(m.waiters[:i], m.waiters[i+1:]...)
			break
		}
	}
}

//
// Write locks
//

func (m *CancelRWMutex) Lock() {
	if m.isCancelled() {
		panic("powerlock: attempt to lock cancelled mutex")
	}
	
	select {
	case m.writer <- struct{}{}:
		// Got writer slot immediately
		m.waitForReaders()
		return
	default:
		// Need to wait
	}
	
	w := m.addWaiter(true)
	if w == nil {
		panic("powerlock: attempt to lock cancelled mutex")
	}
	defer m.removeWaiter(w)
	
	for {
		if m.isCancelled() {
			panic("powerlock: mutex was cancelled while waiting")
		}
		
		select {
		case m.writer <- struct{}{}:
			m.waitForReaders()
			return
		case <-w.done:
			panic("powerlock: mutex was cancelled while waiting")
		default:
			// Keep trying
		}
	}
}

func (m *CancelRWMutex) Unlock() {
	select {
	case <-m.writer:
	default:
		panic("unlock called without a matching lock")
	}
}



//
// Read locks
//

func (m *CancelRWMutex) RLock() {
	if m.isCancelled() {
		panic("powerlock: attempt to lock cancelled mutex")
	}
	
	m.mu.Lock()
	// Fast path: no writer
	if len(m.writer) == 0 {
		atomic.AddInt32(&m.readerCount, 1)
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	
	w := m.addWaiter(false)
	if w == nil {
		panic("powerlock: attempt to lock cancelled mutex")
	}
	defer m.removeWaiter(w)
	
	// Slow path: wait for writer to release
	for {
		if m.isCancelled() {
			panic("powerlock: mutex was cancelled while waiting")
		}
		
		m.mu.Lock()
		if len(m.writer) == 0 {
			atomic.AddInt32(&m.readerCount, 1)
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()
		
		select {
		case <-w.done:
			panic("powerlock: mutex was cancelled while waiting")
		default:
			// Keep trying
		}
	}
}

func (m *CancelRWMutex) RUnlock() {
	newCount := atomic.AddInt32(&m.readerCount, -1)
	if newCount < 0 {
		panic("read unlock without matching lock")
	}
}



//
// Helpers
//

func (m *CancelRWMutex) waitForReaders() {
	// Writer waits until all readers exit
	for {
		if atomic.LoadInt32(&m.readerCount) == 0 {
			return
		}
	}
}

func (m *CancelRWMutex) TryLock() bool {
	if m.isCancelled() {
		return false
	}
	
	select {
	case m.writer <- struct{}{}:
		m.waitForReaders()
		return true
	default:
		return false
	}
}

func (m *CancelRWMutex) TryRLock() bool {
	if m.isCancelled() {
		return false
	}
	
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.writer) == 0 {
		atomic.AddInt32(&m.readerCount, 1)
		return true
	}

	return false
}
