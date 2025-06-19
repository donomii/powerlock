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
	"sync"
	"testing"
	"time"
)

func TestCtxRWMutex_LockUnlock(t *testing.T) {
	m := NewCancelRWMutex("")

	m.Lock()
	m.Unlock()

	m.Lock()
	m.Unlock()
}

func TestCtxRWMutex_RLockRUnlock(t *testing.T) {
	m := NewCancelRWMutex("")

	m.RLock()
	m.RUnlock()

	m.RLock()
	m.RUnlock()
}

func TestCtxRWMutex_TryLock(t *testing.T) {
	m := NewCancelRWMutex("")

	ok := m.TryLock()
	if !ok {
		t.Fatal("expected TryLock to succeed")
	}
	defer m.Unlock()

	ok = m.TryLock()
	if ok {
		t.Fatal("expected second TryLock to fail while locked")
	}
}

func TestCtxRWMutex_TryRLock(t *testing.T) {
	m := NewCancelRWMutex("")

	ok := m.TryRLock()
	if !ok {
		t.Fatal("expected TryRLock to succeed")
	}
	defer m.RUnlock()

	ok = m.TryRLock()
	if !ok {
		t.Fatal("expected second TryRLock to succeed while locked")
	}

	m.RUnlock()
}





func TestCtxRWMutex_ParallelReaders(t *testing.T) {
	m := NewCancelRWMutex("")
	var wg sync.WaitGroup
	numReaders := 5
	started := make(chan struct{}, numReaders)

	wg.Add(numReaders)
	for i := 0; i < numReaders; i++ {
		go func(id int) {
			defer wg.Done()

			m.RLock()
			started <- struct{}{}             // signal this reader got the lock
			time.Sleep(50 * time.Millisecond) // simulate work
			m.RUnlock()
		}(i)
	}

	// Wait for all readers to start
	for i := 0; i < numReaders; i++ {
		select {
		case <-started:
			// good
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("reader %d did not start in time", i)
		}
	}

	wg.Wait()
}

func TestCtxRWMutex_WriterBlocksReader(t *testing.T) {
	m := NewCancelRWMutex("")
	m.Lock()

	var acquired bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Try to acquire read lock - should block
		if m.TryRLock() {
			acquired = true
			m.RUnlock()
		}
	}()

	time.Sleep(50 * time.Millisecond)
	m.Unlock()
	<-done

	if acquired {
		t.Fatal("reader should not have acquired lock while writer held it")
	}
}

func TestCtxRWMutex_WritersBlockEachOther(t *testing.T) {
	m := NewCancelRWMutex("")
	m.Lock()

	var acquired bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Try to acquire write lock - should fail
		acquired = m.TryLock()
		if acquired {
			m.Unlock()
		}
	}()

	time.Sleep(50 * time.Millisecond)
	m.Unlock()
	<-done

	if acquired {
		t.Fatal("second writer should not have acquired lock while first writer held it")
	}
}

func TestCtxRWMutex_NoZombieGoroutines(t *testing.T) {
	m := NewCancelRWMutex("test")

	// First goroutine holds the lock for a short time
	go func() {
		m.Lock()
		defer m.Unlock()
		time.Sleep(100 * time.Millisecond)
	}()

	// Give the first goroutine time to acquire the lock
	time.Sleep(50 * time.Millisecond)

	// Second goroutine tries but fails
	success := m.TryLock()
	if success {
		t.Fatal("expected TryLock to fail while lock is held")
		m.Unlock()
	}

	// Wait for the first lock to release
	time.Sleep(200 * time.Millisecond)

	// This should succeed immediately
	success = m.TryLock()
	if !success {
		t.Fatal("expected TryLock to succeed after lock was released")
	}
	m.Unlock()
}

// New tests for cancellation functionality

func TestCtxRWMutex_Cancel_PanicsOnLock(t *testing.T) {
	m := NewCancelRWMutex("test")
	m.Cancel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected Lock() to panic on cancelled mutex")
		}
	}()

	m.Lock()
}

func TestCtxRWMutex_Cancel_PanicsOnRLock(t *testing.T) {
	m := NewCancelRWMutex("test")
	m.Cancel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected RLock() to panic on cancelled mutex")
		}
	}()

	m.RLock()
}

func TestCtxRWMutex_Cancel_TryLockReturnsFalse(t *testing.T) {
	m := NewCancelRWMutex("test")
	m.Cancel()

	ok := m.TryLock()
	if ok {
		t.Fatal("expected TryLock to return false on cancelled mutex")
	}
}

func TestCtxRWMutex_Cancel_TryRLockReturnsFalse(t *testing.T) {
	m := NewCancelRWMutex("test")
	m.Cancel()

	ok := m.TryRLock()
	if ok {
		t.Fatal("expected TryRLock to return false on cancelled mutex")
	}
}





func TestCtxRWMutex_Cancel_WaitingGoroutinesPanic(t *testing.T) {
	m := NewCancelRWMutex("test")
	m.Lock() // Block other attempts

	var panicked bool
	done := make(chan struct{})

	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		m.Lock() // This will wait
	}()

	time.Sleep(50 * time.Millisecond) // Let goroutine start waiting
	m.Cancel()                        // Cancel should cause waiting goroutine to panic
	m.Unlock()                        // Release lock

	<-done
	if !panicked {
		t.Fatal("expected waiting goroutine to panic when cancelled")
	}
}

func TestCtxRWMutex_Cancel_WaitingRLockGoroutinesPanic(t *testing.T) {
	m := NewCancelRWMutex("test")
	m.Lock() // Block read attempts

	var panicked bool
	done := make(chan struct{})

	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		m.RLock() // This will wait
	}()

	time.Sleep(50 * time.Millisecond) // Let goroutine start waiting
	m.Cancel()                        // Cancel should cause waiting goroutine to panic
	m.Unlock()                        // Release lock

	<-done
	if !panicked {
		t.Fatal("expected waiting goroutine to panic when cancelled")
	}
}




