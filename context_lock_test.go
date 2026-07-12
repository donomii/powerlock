package powerlock

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestContextRWMutex_LockUnlock(t *testing.T) {
	m := NewContextRWMutex("test")

	if err := m.LockContext(context.Background()); err != nil {
		t.Fatalf("expected LockContext to succeed, got %v", err)
	}
	m.Unlock()

	if err := m.RLockContext(context.Background()); err != nil {
		t.Fatalf("expected RLockContext to succeed, got %v", err)
	}
	m.RUnlock()
}

func TestContextRWMutex_LockContextReturnsCanceled(t *testing.T) {
	m := NewContextRWMutex("test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.LockContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestContextRWMutex_RLockContextReturnsCanceled(t *testing.T) {
	m := NewContextRWMutex("test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.RLockContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestContextRWMutex_WaitingLockContextReturnsDeadlineExceeded(t *testing.T) {
	m := NewContextRWMutex("test")
	m.Lock()
	defer m.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := m.LockContext(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
}

func TestContextRWMutex_WaitingRLockContextReturnsDeadlineExceeded(t *testing.T) {
	m := NewContextRWMutex("test")
	m.Lock()
	defer m.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := m.RLockContext(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
}

func TestContextRWMutex_TryLock(t *testing.T) {
	m := NewContextRWMutex("test")

	if !m.TryLock() {
		t.Fatal("expected TryLock to succeed")
	}
	if m.TryLock() {
		t.Fatal("expected second TryLock to fail")
	}
	m.Unlock()
}

func TestContextRWMutex_TryRLock(t *testing.T) {
	m := NewContextRWMutex("test")

	if !m.TryRLock() {
		t.Fatal("expected TryRLock to succeed")
	}
	if !m.TryRLock() {
		t.Fatal("expected second TryRLock to succeed")
	}
	m.RUnlock()
	m.RUnlock()
}

func TestContextRWMutex_CanceledWriterUnblocksReaders(t *testing.T) {
	m := NewContextRWMutex("test")
	m.RLock()

	ctx, cancel := context.WithCancel(context.Background())
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- m.LockContext(ctx)
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingWriters == 1
	})

	readerDone := make(chan error, 1)
	go func() {
		err := m.RLockContext(context.Background())
		if err == nil {
			m.RUnlock()
		}
		readerDone <- err
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingWriters == 1 && state.WaitingReaders == 1
	})
	cancel()
	if err := <-writerDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if err := <-readerDone; err != nil {
		t.Fatalf("expected reader to acquire after writer cancellation, got %v", err)
	}
	m.RUnlock()
}

func TestContextRWMutex_ZeroValueNameAndRLocker(t *testing.T) {
	var m ContextRWMutex
	m.SetLocation("zero")
	if m.Name() != "zero" {
		t.Fatalf("expected name zero, got %q", m.Name())
	}
	reader := m.RLocker()
	reader.Lock()
	reader.Unlock()
	if !m.TryRLock() {
		t.Fatal("expected TryRLock to succeed")
	}
	m.RUnlock()
}

func TestContextRWMutex_AcquisitionErrorIncludesState(t *testing.T) {
	m := NewContextRWMutex("state")
	m.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := m.RLockContext(ctx)
	m.Unlock()

	var acquisitionErr *AcquisitionError
	if !errors.As(err, &acquisitionErr) {
		t.Fatalf("expected AcquisitionError, got %T: %v", err, err)
	}
	if acquisitionErr.Name != "state" || acquisitionErr.Mode != LockModeRead || !acquisitionErr.State.Writer {
		t.Fatalf("expected named read failure with active writer, got %+v", acquisitionErr)
	}
}

func TestContextRWMutex_StateVersionIncreases(t *testing.T) {
	m := NewContextRWMutex("version")
	initial := m.Snapshot().Version
	m.Lock()
	acquired := m.Snapshot().Version
	m.Unlock()
	released := m.Snapshot().Version
	if !(initial < acquired && acquired < released) {
		t.Fatalf("expected increasing state versions, got initial=%d acquired=%d released=%d", initial, acquired, released)
	}
}

func TestContextRWMutex_UnobservedQueuedHolderHasNoDurationReadout(t *testing.T) {
	m := NewContextRWMutex("duration")
	m.Lock()
	acquired := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		m.Lock()
		close(acquired)
		<-release
		m.Unlock()
		close(done)
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingWriters == 1 })
	m.Unlock()
	<-acquired
	if duration := m.Snapshot().WriterHoldDuration; duration != 0 {
		t.Fatalf("expected unobserved hold duration to be zero, got %s", duration)
	}
	close(release)
	<-done
}

func TestContextRWMutex_InvalidUnlockIdentifiesLockAndState(t *testing.T) {
	m := NewContextRWMutex("cache")
	message := panicText(t, m.Unlock)
	if !strings.Contains(message, `lock "cache"`) || !strings.Contains(message, "writer=false") || !strings.Contains(message, "readers=0") {
		t.Fatalf("invalid unlock omitted diagnostic state: %s", message)
	}
}

func TestContextRWMutex_NilContextIdentifiesOperationAndLock(t *testing.T) {
	m := NewContextRWMutex("cache")
	message := panicText(t, func() { m.LockContext(nil) })
	if !strings.Contains(message, `write acquisition for lock "cache"`) || !strings.Contains(message, "received nil") {
		t.Fatalf("nil-context error omitted diagnostic context: %s", message)
	}
}

func TestContextRWMutex_WritersAcquireInFIFOOrder(t *testing.T) {
	m := NewContextRWMutex("fifo-writers")
	m.Lock()
	acquired := make(chan int, 3)
	releases := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	for index := range releases {
		identifier := index + 1
		release := releases[index]
		go func() {
			m.Lock()
			acquired <- identifier
			<-release
			m.Unlock()
		}()
		waitForLockState(t, m.Snapshot, func(state LockState) bool {
			return state.WaitingWriters == identifier
		})
	}
	m.Unlock()
	for expected, release := range releases {
		if observed := <-acquired; observed != expected+1 {
			t.Fatalf("expected writer %d, acquired writer %d", expected+1, observed)
		}
		close(release)
	}
}

func TestContextRWMutex_BatchesReadersAtQueueFront(t *testing.T) {
	m := NewContextRWMutex("fifo-readers")
	m.Lock()
	readerAcquired := make(chan int, 2)
	releaseReaders := make(chan struct{})
	for identifier := 1; identifier <= 2; identifier++ {
		identifier := identifier
		go func() {
			m.RLock()
			readerAcquired <- identifier
			<-releaseReaders
			m.RUnlock()
		}()
		waitForLockState(t, m.Snapshot, func(state LockState) bool {
			return state.WaitingReaders == identifier
		})
	}
	writerAcquired := make(chan struct{})
	writerRelease := make(chan struct{})
	go func() {
		m.Lock()
		close(writerAcquired)
		<-writerRelease
		m.Unlock()
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingReaders == 2 && state.WaitingWriters == 1
	})
	m.Unlock()
	<-readerAcquired
	<-readerAcquired
	select {
	case <-writerAcquired:
		t.Fatal("writer acquired before the front reader batch released")
	default:
	}
	close(releaseReaders)
	<-writerAcquired
	close(writerRelease)
}

func TestContextRWMutex_MiddleCancellationPreservesFIFOOrder(t *testing.T) {
	m := NewContextRWMutex("fifo-cancel")
	m.Lock()
	firstAcquired := make(chan struct{})
	firstRelease := make(chan struct{})
	go func() {
		m.Lock()
		close(firstAcquired)
		<-firstRelease
		m.Unlock()
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingWriters == 1 })

	middleContext, cancelMiddle := context.WithCancel(context.Background())
	middleResult := make(chan error, 1)
	go func() { middleResult <- m.LockContext(middleContext) }()
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingWriters == 2 })

	thirdAcquired := make(chan struct{})
	thirdRelease := make(chan struct{})
	go func() {
		m.Lock()
		close(thirdAcquired)
		<-thirdRelease
		m.Unlock()
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingWriters == 3 })
	cancelMiddle()
	if err := <-middleResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected middle cancellation, got %v", err)
	}
	m.Unlock()
	<-firstAcquired
	select {
	case <-thirdAcquired:
		t.Fatal("third writer bypassed the first writer")
	default:
	}
	close(firstRelease)
	<-thirdAcquired
	close(thirdRelease)
}

func TestContextRWMutex_CancellationUnlockRace(t *testing.T) {
	for iteration := 0; iteration < 25; iteration++ {
		m := NewContextRWMutex("cancel-unlock-race")
		m.Lock()
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			err := m.LockContext(ctx)
			if err == nil {
				m.Unlock()
			}
			result <- err
		}()
		waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingWriters == 1 })
		start := make(chan struct{})
		unlocked := make(chan struct{})
		go func() {
			<-start
			cancel()
		}()
		go func() {
			<-start
			m.Unlock()
			close(unlocked)
		}()
		close(start)
		<-unlocked
		if err := <-result; err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("iteration %d returned unexpected result: %v", iteration, err)
		}
		state := m.Snapshot()
		if state.Writer || state.Readers != 0 || state.WaitingWriters != 0 || state.WaitingReaders != 0 {
			t.Fatalf("iteration %d left lock state behind: %+v", iteration, state)
		}
	}
}

func TestContextRWMutex_CancelledFrontReaderPreservesFollowingWriter(t *testing.T) {
	m := NewContextRWMutex("cancel-front-reader")
	m.Lock()
	readerContext, cancelReader := context.WithCancel(context.Background())
	readerResult := make(chan error, 1)
	go func() { readerResult <- m.RLockContext(readerContext) }()
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingReaders == 1 })
	writerAcquired := make(chan struct{})
	writerRelease := make(chan struct{})
	go func() {
		m.Lock()
		close(writerAcquired)
		<-writerRelease
		m.Unlock()
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingReaders == 1 && state.WaitingWriters == 1
	})
	cancelReader()
	if err := <-readerResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected front reader cancellation, got %v", err)
	}
	state := m.Snapshot()
	if !state.Writer || state.WaitingReaders != 0 || state.WaitingWriters != 1 {
		t.Fatalf("following writer did not remain queued: %+v", state)
	}
	m.Unlock()
	waitForSignal(t, writerAcquired, "following writer did not acquire")
	close(writerRelease)
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return !state.Writer })
}

func TestContextRWMutex_CancelAfterAcquisitionDoesNotRevokeOwnership(t *testing.T) {
	m := NewContextRWMutex("context-lifetime")
	writeContext, cancelWrite := context.WithCancel(context.Background())
	if err := m.LockContext(writeContext); err != nil {
		t.Fatalf("expected write acquisition, got %v", err)
	}
	cancelWrite()
	if state := m.Snapshot(); !state.Writer {
		t.Fatalf("write context cancellation revoked ownership: %+v", state)
	}
	m.Unlock()
	readContext, cancelRead := context.WithCancel(context.Background())
	if err := m.RLockContext(readContext); err != nil {
		t.Fatalf("expected read acquisition, got %v", err)
	}
	cancelRead()
	if state := m.Snapshot(); state.Readers != 1 {
		t.Fatalf("read context cancellation revoked ownership: %+v", state)
	}
	m.RUnlock()
}

func TestFairRWMutex_PreservesMixedFIFOOrder(t *testing.T) {
	m := NewFairRWMutex("fair-mixed")
	m.RLock()
	acquired := make(chan string, 3)
	firstWriterRelease := make(chan struct{})
	go func() {
		m.Lock()
		acquired <- "writer-1"
		<-firstWriterRelease
		m.Unlock()
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return state.WaitingWriters == 1 })
	readerRelease := make(chan struct{})
	go func() {
		m.RLock()
		acquired <- "reader-2"
		<-readerRelease
		m.RUnlock()
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingWriters == 1 && state.WaitingReaders == 1
	})
	lastWriterRelease := make(chan struct{})
	go func() {
		m.Lock()
		acquired <- "writer-3"
		<-lastWriterRelease
		m.Unlock()
	}()
	waitForLockState(t, m.Snapshot, func(state LockState) bool {
		return state.WaitingWriters == 2 && state.WaitingReaders == 1
	})
	m.RUnlock()
	expectAcquisition(t, acquired, "writer-1")
	close(firstWriterRelease)
	expectAcquisition(t, acquired, "reader-2")
	close(readerRelease)
	expectAcquisition(t, acquired, "writer-3")
	close(lastWriterRelease)
	waitForLockState(t, m.Snapshot, func(state LockState) bool { return !state.Writer && state.Readers == 0 })
}

func expectAcquisition(t *testing.T, acquired <-chan string, expected string) {
	t.Helper()
	select {
	case observed := <-acquired:
		if observed != expected {
			t.Fatalf("expected %s to acquire, got %s", expected, observed)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", expected)
	}
}
