package powerlock

import "time"

// LockMode identifies whether an acquisition is exclusive or shared.
type LockMode uint8

const (
	// LockModeWrite identifies an exclusive acquisition.
	LockModeWrite LockMode = iota + 1
	// LockModeRead identifies a shared acquisition.
	LockModeRead
)

// LockResult identifies the result of an acquisition attempt.
type LockResult uint8

const (
	// LockResultAcquired means ownership was granted.
	LockResultAcquired LockResult = iota + 1
	// LockResultBusy means a non-blocking acquisition found incompatible ownership or an earlier waiter.
	LockResultBusy
	// LockResultQueueFull means a bounded waiter queue had no remaining capacity.
	LockResultQueueFull
	// LockResultCancelled means a context or permanently cancellable lock was cancelled.
	LockResultCancelled
	// LockResultDeadlineExceeded means the acquisition context expired before ownership was granted.
	LockResultDeadlineExceeded
)

// LockEventKind identifies a lock state transition reported to an observer.
type LockEventKind uint8

const (
	// LockEventWaitStarted reports that an acquisition joined the waiter queue.
	LockEventWaitStarted LockEventKind = iota + 1
	// LockEventWaitExceeded reports that a queued acquisition crossed its wait threshold.
	LockEventWaitExceeded
	// LockEventAcquired reports that ownership was granted.
	LockEventAcquired
	// LockEventReleased reports that ownership was released.
	LockEventReleased
	// LockEventTryFailed reports that a non-blocking acquisition did not acquire ownership.
	LockEventTryFailed
	// LockEventRejected reports that cancellation, expiry, or capacity ended an acquisition.
	LockEventRejected
	// LockEventHoldExceeded reports that an exact acquisition crossed its hold threshold.
	LockEventHoldExceeded
)

// LockAttemptID uniquely identifies one acquisition attempt within the process.
type LockAttemptID uint64

// LockState is a point-in-time view of a lock.
type LockState struct {
	Name                     string
	Version                  uint64
	Readers                  int
	Writer                   bool
	WaitingReaders           int
	WaitingWriters           int
	Cancelled                bool
	OldestWaitDuration       time.Duration
	WriterHoldDuration       time.Duration
	ReaderCohortHoldDuration time.Duration
}

// LockEvent describes one acquisition or release transition. Observers must treat the value and Callers slice as read-only.
type LockEvent struct {
	Name              string
	Mode              LockMode
	Kind              LockEventKind
	Result            LockResult
	AttemptID         LockAttemptID
	Contended         bool
	WaitDuration      time.Duration
	HoldDuration      time.Duration
	HoldDurationKnown bool
	ExactHold         bool
	Callers           []uintptr
	State             LockState
}

// LockObserver receives ordered lock events. Calls may be concurrent across acquisitions, so implementations must be
// concurrency-safe, return promptly, not panic, and not acquire or release the lock emitting the event.
type LockObserver interface {
	ObserveLock(LockEvent)
}

// LockObserverFunc adapts a function to LockObserver.
type LockObserverFunc func(LockEvent)

// ObserveLock calls the observer function.
func (f LockObserverFunc) ObserveLock(event LockEvent) {
	f(event)
}

// LockObserverGroup delivers each event to its observers in order and must not be mutated while in use.
type LockObserverGroup []LockObserver

// ObserveLock delivers event to each non-nil observer.
func (g LockObserverGroup) ObserveLock(event LockEvent) {
	for _, observer := range g {
		if observer != nil {
			observer.ObserveLock(event)
		}
	}
}

// AcquisitionError reports why a lock acquisition did not complete.
type AcquisitionError struct {
	Name       string
	Mode       LockMode
	Result     LockResult
	MaxWaiting int
	State      LockState
	Cause      error
}

// KeyedAcquisitionError reports a keyed-lock failure with its capacity state.
type KeyedAcquisitionError[K comparable] struct {
	Name       string
	Key        K
	MaxKeys    int
	ActiveKeys int
	Cause      error
}
