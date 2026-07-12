// Copyright (c) 2026 Donomii.
// Licensed under the GNU AGPLv3; see LICENSE.

package powerlock

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestCancelRWMutex_LockUnlock(t *testing.T) {
	m := NewCancelRWMutex("")

	m.Lock()
	m.Unlock()

	m.Lock()
	m.Unlock()
}

func TestCancelRWMutex_RLockRUnlock(t *testing.T) {
	m := NewCancelRWMutex("")

	m.RLock()
	m.RUnlock()

	m.RLock()
	m.RUnlock()
}

func TestCancelRWMutex_TryLock(t *testing.T) {
	m := NewCancelRWMutex("")
	if !m.TryLock() {
		t.Fatal("expected TryLock to succeed")
	}
	if m.TryLock() {
		t.Fatal("expected second TryLock to fail while locked")
	}
	m.Unlock()
}

func TestCancelRWMutex_TryRLock(t *testing.T) {
	m := NewCancelRWMutex("")

	if !m.TryRLock() {
		t.Fatal("expected TryRLock to succeed")
	}
	if !m.TryRLock() {
		t.Fatal("expected second TryRLock to succeed while locked")
	}
	m.RUnlock()
	m.RUnlock()
}

func TestCancelRWMutex_ParallelReaders(t *testing.T) {
	m := NewCancelRWMutex("")
	const numReaders = 5
	release := make(chan struct{})
	started := make(chan struct{}, numReaders)
	var wg sync.WaitGroup
	wg.Add(numReaders)
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			m.RLock()
			started <- struct{}{}
			<-release
			m.RUnlock()
		}()
	}
	for i := 0; i < numReaders; i++ {
		<-started
	}
	state := m.Snapshot()
	if state.Readers != numReaders {
		t.Fatalf("expected %d concurrent readers, got %+v", numReaders, state)
	}
	close(release)
	wg.Wait()
}

func TestCancelRWMutex_WriterBlocksReader(t *testing.T) {
	m := NewCancelRWMutex("")
	m.Lock()
	if m.TryRLock() {
		m.RUnlock()
		t.Fatal("reader should not have acquired lock while writer held it")
	}
	m.Unlock()
}

func TestCancelRWMutex_WritersBlockEachOther(t *testing.T) {
	m := NewCancelRWMutex("")
	m.Lock()
	if m.TryLock() {
		m.Unlock()
		t.Fatal("second writer should not have acquired lock while first writer held it")
	}
	m.Unlock()
}

func TestCancelRWMutex_NoStaleWaiters(t *testing.T) {
	m := NewCancelRWMutex("test")
	m.Lock()
	if m.TryLock() {
		m.Unlock()
		t.Fatal("expected TryLock to fail while lock is held")
	}
	m.Unlock()
	if !m.TryLock() {
		t.Fatal("expected TryLock to succeed after lock was released")
	}
	m.Unlock()
}

func TestCancelRWMutex_Cancel_PanicsOnLock(t *testing.T) {
	m := NewCancelRWMutex("test")
	m.Cancel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected Lock() to panic on cancelled mutex")
		}
	}()

	m.Lock()
}

func TestCancelRWMutex_Cancel_PanicsOnRLock(t *testing.T) {
	m := NewCancelRWMutex("test")
	m.Cancel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected RLock() to panic on cancelled mutex")
		}
	}()

	m.RLock()
}

func TestCancelRWMutex_Cancel_TryLockReturnsFalse(t *testing.T) {
	m := NewCancelRWMutex("test")
	m.Cancel()

	if m.TryLock() {
		t.Fatal("expected TryLock to return false on cancelled mutex")
	}
}

func TestCancelRWMutex_Cancel_TryRLockReturnsFalse(t *testing.T) {
	m := NewCancelRWMutex("test")
	m.Cancel()

	if m.TryRLock() {
		t.Fatal("expected TryRLock to return false on cancelled mutex")
	}
}
func TestCancelRWMutex_CancelRejectsWaitingWriter(t *testing.T) {
	m := NewCancelRWMutex("test")
	m.Lock()
	result := make(chan error, 1)
	go func() {
		result <- m.LockContext(context.Background())
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingWriters == 1
	})
	m.Cancel()
	m.Unlock()
	if err := <-result; !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected ErrCancelled, got %v", err)
	}
}

func TestCancelRWMutex_CancelRejectsWaitingReader(t *testing.T) {
	m := NewCancelRWMutex("test")
	m.Lock()
	result := make(chan error, 1)
	go func() {
		result <- m.RLockContext(context.Background())
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingReaders == 1
	})
	m.Cancel()
	m.Unlock()
	if err := <-result; !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected ErrCancelled, got %v", err)
	}
}

func TestCancelRWMutex_FIFORejectsReaderBypass(t *testing.T) {
	m := NewCancelRWMutex("test")
	m.RLock()

	writerAcquired := make(chan struct{})
	writerRelease := make(chan struct{})
	go func() {
		m.Lock()
		close(writerAcquired)
		<-writerRelease
		m.Unlock()
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingWriters == 1
	})

	readerAcquired := make(chan struct{})
	go func() {
		m.RLock()
		close(readerAcquired)
		m.RUnlock()
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingReaders == 1 && state.WaitingWriters == 1
	})

	m.RUnlock()
	<-writerAcquired
	state := m.Snapshot()
	if !state.Writer || state.WaitingReaders != 1 {
		t.Fatalf("expected the queued writer before the reader, got %+v", state)
	}
	close(writerRelease)
	<-readerAcquired
}

func TestCancelRWMutex_ContextCancellationAndRLocker(t *testing.T) {
	m := NewCancelRWMutex("test")
	m.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.RLockContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	m.Unlock()

	reader := m.RLocker()
	reader.Lock()
	reader.Unlock()
}

func TestCancelRWMutex_ZeroValueAndStableName(t *testing.T) {
	var m CancelRWMutex
	m.SetLocation("zero")
	if m.Name() != "zero" {
		t.Fatalf("expected name zero, got %q", m.Name())
	}
	m.Lock()
	m.Unlock()

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected SetLocation to panic after first use")
		}
	}()
	m.SetLocation("changed")
}

func TestCancelRWMutex_CancelRejectsMixedQueueAndPreservesHolderRelease(t *testing.T) {
	m := NewCancelRWMutex("mixed")
	m.RLock()
	writerResult := make(chan error, 1)
	go func() { writerResult <- m.LockContext(context.Background()) }()
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingWriters == 1 })
	readerResult := make(chan error, 1)
	go func() { readerResult <- m.RLockContext(context.Background()) }()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingWriters == 1 && state.WaitingReaders == 1
	})
	m.Cancel()
	m.Cancel()
	if err := <-writerResult; !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected queued writer cancellation, got %v", err)
	}
	if err := <-readerResult; !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected queued reader cancellation, got %v", err)
	}
	m.RUnlock()
	state := m.Snapshot()
	if !state.Cancelled || state.Readers != 0 || state.Writer || state.WaitingReaders != 0 || state.WaitingWriters != 0 {
		t.Fatalf("unexpected state after mixed cancellation: %+v", state)
	}
}
