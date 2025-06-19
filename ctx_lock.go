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

package ctxlock

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

var ErrCtxTimeout = errors.New("ctxsync: lock acquisition timed out")

type CtxRWMutex struct {
	location string

	readerCount int32         // number of active readers
	writer      chan struct{} // single-slot semaphore for writers
	mu          sync.Mutex    // protects coordination
}

func NewCtxRWMutex(location string) *CtxRWMutex {
	return &CtxRWMutex{
		location: location,
		writer:   make(chan struct{}, 1),
	}
}

func (m *CtxRWMutex) CtxRWLocation(location string) {
	m.location = location
}

//
// Write locks
//

func (m *CtxRWMutex) Lock() {
	m.writer <- struct{}{}
	m.waitForReaders()
}

func (m *CtxRWMutex) Unlock() {
	select {
	case <-m.writer:
	default:
		panic("unlock called without a matching lock")
	}
}

func (m *CtxRWMutex) LockContext(ctx context.Context) error {
	select {
	case m.writer <- struct{}{}:
		// Got writer slot
	case <-ctx.Done():
		return ErrCtxTimeout
	}
	m.waitForReaders()
	return nil
}

//
// Read locks
//

func (m *CtxRWMutex) RLock() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If no writer currently holds the lock, increment readers
	if len(m.writer) == 0 {
		atomic.AddInt32(&m.readerCount, 1)
		return
	}

	// Wait until writer is gone
	for len(m.writer) > 0 {
		m.mu.Unlock()
		m.mu.Lock()
	}
	atomic.AddInt32(&m.readerCount, 1)
}

func (m *CtxRWMutex) RUnlock() {
	newCount := atomic.AddInt32(&m.readerCount, -1)
	if newCount < 0 {
		panic("read unlock without matching lock")
	}
}

func (m *CtxRWMutex) RLockContext(ctx context.Context) error {
	// Fast path: no writer
	m.mu.Lock()
	if len(m.writer) == 0 {
		atomic.AddInt32(&m.readerCount, 1)
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	// Slow path: wait for writer to release or ctx to timeout
	done := make(chan struct{})
	go func() {
		for {
			m.mu.Lock()
			if len(m.writer) == 0 {
				atomic.AddInt32(&m.readerCount, 1)
				m.mu.Unlock()
				close(done)
				return
			}
			m.mu.Unlock()
		}
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ErrCtxTimeout
	}
}

//
// Helpers
//

func (m *CtxRWMutex) waitForReaders() {
	// Writer waits until all readers exit
	for {
		if atomic.LoadInt32(&m.readerCount) == 0 {
			return
		}
	}
}
func (m *CtxRWMutex) TryLock() bool {
	select {
	case m.writer <- struct{}{}:
		m.waitForReaders()
		return true
	default:
		return false
	}
}

func (m *CtxRWMutex) TryRLock() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.writer) == 0 {
		atomic.AddInt32(&m.readerCount, 1)
		return true
	}

	return false
}
