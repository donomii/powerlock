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
	"errors"
	"strings"
	"testing"
	"time"
)

func newTestMaxRWMutex(limit int) *MaxRWMutex {
	return NewMaxRWMutexWithLimit("test", limit)
}

func TestMaxRWMutex_BasicLockUnlock(t *testing.T) {
	m := newTestMaxRWMutex(5)

	m.Lock()
	m.Unlock()

	m.RLock()
	m.RUnlock()
}

func TestMaxRWMutex_TryLock(t *testing.T) {
	m := newTestMaxRWMutex(5)
	if !m.TryLock() {
		t.Fatal("expected TryLock to succeed")
	}
	if m.TryLock() {
		t.Fatal("expected TryLock to fail while locked")
	}
	m.Unlock()
}

func TestMaxRWMutex_TryRLock(t *testing.T) {
	m := newTestMaxRWMutex(5)
	if !m.TryRLock() {
		t.Fatal("expected TryRLock to succeed")
	}
	if !m.TryRLock() {
		t.Fatal("expected a compatible TryRLock to succeed")
	}
	m.RUnlock()
	m.RUnlock()

	m.Lock()
	if m.TryRLock() {
		t.Fatal("expected TryRLock to fail while write locked")
	}
	m.Unlock()
}

func TestMaxRWMutex_HoldersDoNotConsumeWaiterCapacity(t *testing.T) {
	m := newTestMaxRWMutex(1)
	m.RLock()
	m.RLock()
	state := m.Snapshot()
	if state.Readers != 2 || state.WaitingReaders != 0 || state.WaitingWriters != 0 {
		t.Fatalf("expected two holders and no waiters, got %+v", state)
	}
	m.RUnlock()
	m.RUnlock()
}

func TestMaxRWMutex_LimitRejectsOnlyExcessBlockedWaiters(t *testing.T) {
	m := newTestMaxRWMutex(1)
	m.Lock()

	waiterResult := make(chan error, 1)
	waiterRelease := make(chan struct{})
	waiterDone := make(chan struct{})
	go func() {
		err := m.LockContext(context.Background())
		waiterResult <- err
		if err == nil {
			<-waiterRelease
			m.Unlock()
		}
		close(waiterDone)
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingWriters == 1
	})

	err := m.LockContext(context.Background())
	if !errors.Is(err, ErrMaxWaiting) {
		t.Fatalf("expected ErrMaxWaiting, got %v", err)
	}

	m.Unlock()
	if err := <-waiterResult; err != nil {
		t.Fatalf("expected queued acquisition to succeed, got %v", err)
	}
	state := m.Snapshot()
	if !state.Writer || state.WaitingWriters != 0 {
		t.Fatalf("expected acquired writer and released waiter capacity, got %+v", state)
	}
	close(waiterRelease)
	<-waiterDone
}

func TestMaxRWMutex_WriterQueuePreventsReaderBypass(t *testing.T) {
	m := newTestMaxRWMutex(2)
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
	if m.TryRLock() {
		m.RUnlock()
		t.Fatal("expected TryRLock not to bypass a queued writer")
	}
	m.RUnlock()
	<-writerAcquired
	close(writerRelease)
}

func TestMaxRWMutex_CancelledWaiterReleasesCapacity(t *testing.T) {
	m := newTestMaxRWMutex(1)
	m.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- m.LockContext(ctx)
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingWriters == 1
	})
	cancel()
	if err := <-firstResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}

	secondResult := make(chan error, 1)
	go func() {
		err := m.LockContext(context.Background())
		secondResult <- err
		if err == nil {
			m.Unlock()
		}
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingWriters == 1
	})
	m.Unlock()
	if err := <-secondResult; err != nil {
		t.Fatalf("expected capacity to be reusable, got %v", err)
	}
}

func TestMaxRWMutex_ZeroValueUsesDefaultLimit(t *testing.T) {
	var m MaxRWMutex
	if m.MaxWaiting() != DefaultMaxWaiting {
		t.Fatalf("expected default maximum %d, got %d", DefaultMaxWaiting, m.MaxWaiting())
	}
	m.Lock()
	m.Unlock()
}

func TestMaxRWMutex_TryRLockAndRLocker(t *testing.T) {
	m := newTestMaxRWMutex(1)
	if !m.TryRLock() {
		t.Fatal("expected TryRLock to succeed")
	}
	m.RUnlock()

	reader := m.RLocker()
	reader.Lock()
	reader.Unlock()
}

func TestMaxRWMutex_RemovesExpiredWaiterBeforeCapacityCheck(t *testing.T) {
	m := newTestMaxRWMutex(1)
	m.Lock()
	staleContext, cancel := context.WithCancel(context.Background())
	cancel()
	stale := &rwWaiter{
		mode:       LockModeWrite,
		ctx:        staleContext,
		ready:      make(chan struct{}),
		queuedAt:   time.Now(),
		maxWaiting: m.MaxWaiting(),
	}
	m.core.mu.Lock()
	m.core.waiters = append(m.core.waiters, stale)
	m.core.version++
	m.core.mu.Unlock()

	result := make(chan error, 1)
	go func() {
		err := m.LockContext(context.Background())
		result <- err
		if err == nil {
			m.Unlock()
		}
	}()
	<-stale.ready
	var staleErr *AcquisitionError
	if !errors.As(stale.err, &staleErr) || staleErr.MaxWaiting != m.MaxWaiting() {
		t.Fatalf("expected expired waiter error to retain maximum=%d, got %+v", m.MaxWaiting(), stale.err)
	}
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingWriters == 1 })
	m.Unlock()
	if err := <-result; err != nil {
		t.Fatalf("expected replacement waiter to use released capacity, got %v", err)
	}
}

func TestMaxRWMutex_DefaultConstructorAndStableIdentity(t *testing.T) {
	m := NewMaxRWMutex("before")
	m.SetLocation("bounded-rw")
	if m.Name() != "bounded-rw" || m.MaxWaiting() != DefaultMaxWaiting {
		t.Fatalf("unexpected default lock configuration: name=%q maximum=%d", m.Name(), m.MaxWaiting())
	}
	m.RLock()
	m.RUnlock()
	message := panicText(t, func() { m.SetLocation("changed") })
	if !strings.Contains(message, `current="bounded-rw"`) || !strings.Contains(message, `requested="changed"`) {
		t.Fatalf("post-use name error omitted identities: %s", message)
	}
}

func TestMaxRWMutex_SharesCapacityAcrossReadAndWriteWaiters(t *testing.T) {
	m := NewMaxRWMutexWithLimit("mixed-capacity", 2)
	m.Lock()
	type acquisition struct {
		mode LockMode
		err  error
	}
	acquired := make(chan acquisition, 2)
	readerRelease := make(chan struct{})
	go func() {
		err := m.RLockContext(context.Background())
		acquired <- acquisition{mode: LockModeRead, err: err}
		if err == nil {
			<-readerRelease
			m.RUnlock()
		}
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingReaders == 1 })
	writerRelease := make(chan struct{})
	go func() {
		err := m.LockContext(context.Background())
		acquired <- acquisition{mode: LockModeWrite, err: err}
		if err == nil {
			<-writerRelease
			m.Unlock()
		}
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingReaders == 1 && state.WaitingWriters == 1
	})
	assertQueueFullError(t, m.RLockContext(context.Background()), LockModeRead, 2)
	assertQueueFullError(t, m.LockContext(context.Background()), LockModeWrite, 2)
	if message := panicText(t, m.RLock); !strings.Contains(message, ErrMaxWaiting.Error()) {
		t.Fatalf("RLock saturation panic omitted cause: %s", message)
	}
	if message := panicText(t, m.Lock); !strings.Contains(message, ErrMaxWaiting.Error()) {
		t.Fatalf("Lock saturation panic omitted cause: %s", message)
	}
	state := m.Snapshot()
	if state.WaitingReaders != 1 || state.WaitingWriters != 1 {
		t.Fatalf("rejected acquisitions changed queue state: %+v", state)
	}
	m.Unlock()
	first := <-acquired
	if first.mode != LockModeRead || first.err != nil {
		t.Fatalf("expected queued reader first, got mode=%s err=%v", first.mode, first.err)
	}
	close(readerRelease)
	second := <-acquired
	if second.mode != LockModeWrite || second.err != nil {
		t.Fatalf("expected queued writer second, got mode=%s err=%v", second.mode, second.err)
	}
	close(writerRelease)
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return !state.Writer })
}

func assertQueueFullError(t *testing.T, err error, mode LockMode, maximum int) {
	t.Helper()
	var acquisitionErr *AcquisitionError
	if !errors.As(err, &acquisitionErr) || acquisitionErr.Mode != mode || acquisitionErr.Result != LockResultQueueFull || acquisitionErr.MaxWaiting != maximum {
		t.Fatalf("unexpected %s queue-full error: %+v", mode, acquisitionErr)
	}
}
