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

package ctxlock

import (
	"sync"
	"testing"
	"time"
)

func newTestMaxRWMutex(limit int) *MaxRWMutex {
	m := &MaxRWMutex{
		location:   "test",
		maxWaiting: limit,
		waiting:    make(chan struct{}, limit),
	}
	return m
}

func TestMaxRWMutex_BasicLockUnlock(t *testing.T) {
	m := newTestMaxRWMutex(5) // Give plenty of waiter slots

	m.Lock()
	m.Unlock()

	m.RLock()
	m.RUnlock()
}

func TestMaxRWMutex_TryLock_Succeeds(t *testing.T) {
	m := newTestMaxRWMutex(5)

	ok := m.TryLock()
	if !ok {
		t.Fatal("expected TryLock to succeed")
	}
	m.Unlock()
}

func TestMaxRWMutex_TryLock_FailsWhenBusy(t *testing.T) {
	m := newTestMaxRWMutex(5)

	m.Lock()
	defer m.Unlock()

	ok := m.TryLock()
	if ok {
		t.Fatal("expected TryLock to fail while locked")
	}
}

func TestMaxRWMutex_TryRLock_Succeeds(t *testing.T) {
	m := newTestMaxRWMutex(5)

	ok := m.TryRLock()
	if !ok {
		t.Fatal("expected TryRLock to succeed")
	}
	m.RUnlock()
}

func TestMaxRWMutex_TryRLock_FailsWhenBusy(t *testing.T) {
	m := newTestMaxRWMutex(5)

	m.Lock() // block writers and readers
	defer m.Unlock()

	ok := m.TryRLock()
	if ok {
		t.Fatal("expected TryRLock to fail while write locked")
	}
}

func TestMaxRWMutex_ParallelReaders(t *testing.T) {
	m := newTestMaxRWMutex(10) // Large waiter queue

	const numReaders = 5
	var wg sync.WaitGroup

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			m.RLock()
			time.Sleep(20 * time.Millisecond)
			m.RUnlock()
		}(i)
	}

	wg.Wait()
}

func TestMaxRWMutex_RTLockAlias(t *testing.T) {
	m := newTestMaxRWMutex(5)

	ok := m.RTryLock()
	if !ok {
		t.Fatal("expected RTryLock to succeed")
	}
	m.RUnlock()
}

// Test that Lock() panics when waiter queue is full
func TestMaxRWMutex_Lock_PanicsWhenWaiterQueueFull(t *testing.T) {
	m := newTestMaxRWMutex(1) // Only 1 waiter slot

	// Fill the waiter queue
	m.Lock()

	// Try to add another - should panic
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected Lock() to panic when waiter queue is full")
		}
		if r != ErrMaxWaiting {
			t.Fatalf("expected panic with ErrMaxWaiting, got %v", r)
		}
	}()

	m.Lock() // This should panic immediately
}

// Test that RLock() panics when waiter queue is full  
func TestMaxRWMutex_RLock_PanicsWhenWaiterQueueFull(t *testing.T) {
	m := newTestMaxRWMutex(1) // Only 1 waiter slot

	// Fill the waiter queue  
	m.RLock()

	// Try to add another - should panic
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected RLock() to panic when waiter queue is full")
		}
		if r != ErrMaxWaiting {
			t.Fatalf("expected panic with ErrMaxWaiting, got %v", r)
		}
	}()

	m.RLock() // This should panic immediately
}

// Test that TryLock returns false when waiter queue is full
func TestMaxRWMutex_TryLock_FailsWhenWaiterQueueFull(t *testing.T) {
	m := newTestMaxRWMutex(1) // Only 1 waiter slot

	// Fill the waiter queue
	m.Lock()
	defer m.Unlock()

	// Try to add another with TryLock - should return false
	ok := m.TryLock()
	if ok {
		m.Unlock() // cleanup if somehow succeeded
		t.Fatal("expected TryLock to fail when waiter queue is full")
	}
}

// Test that TryRLock returns false when waiter queue is full
func TestMaxRWMutex_TryRLock_FailsWhenWaiterQueueFull(t *testing.T) {
	m := newTestMaxRWMutex(1) // Only 1 waiter slot

	// Fill the waiter queue
	m.RLock()
	defer m.RUnlock()

	// Try to add another with TryRLock - should return false  
	ok := m.TryRLock()
	if ok {
		m.RUnlock() // cleanup if somehow succeeded
		t.Fatal("expected TryRLock to fail when waiter queue is full")
	}
}

// Test multiple operations with larger waiter queue
func TestMaxRWMutex_MultipleOperationsWithLimitedQueue(t *testing.T) {
	m := newTestMaxRWMutex(3) // Allow 3 waiters

	// First operation: read lock (takes one slot and succeeds)
	m.RLock()

	// Second operation: another read lock should succeed (takes second slot, readers can share)
	ok := m.TryRLock()
	if !ok {
		t.Fatal("expected second TryRLock to succeed - readers should be able to share")
	}

	// Third operation: another read lock should succeed (takes third slot, readers can share)
	ok2 := m.TryRLock()
	if !ok2 {
		t.Fatal("expected third TryRLock to succeed - readers should be able to share")
	}

	// Fourth operation: should fail (queue full)
	ok3 := m.TryLock()
	if ok3 {
		m.Unlock()
		t.Fatal("expected fourth TryLock to fail due to full waiter queue")
	}

	// Fifth operation: should fail (queue full)
	ok4 := m.TryRLock()
	if ok4 {
		m.RUnlock()
		t.Fatal("expected fifth TryRLock to fail due to full waiter queue")
	}

	// Clean up all read locks
	m.RUnlock() // third operation
	m.RUnlock() // second operation
	m.RUnlock() // first operation

	// Now operations should succeed again
	ok5 := m.TryLock()
	if !ok5 {
		t.Fatal("expected TryLock to succeed after queue cleared")
	}
	m.Unlock()
}
