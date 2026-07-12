package powerlock

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrBusy identifies an acquisition that could not proceed immediately.
	ErrBusy = errors.New("powerlock: lock is busy")
	// ErrCancelled identifies an acquisition rejected by context or permanent lock cancellation.
	ErrCancelled = errors.New("powerlock: lock was cancelled")
	// ErrMaxKeys identifies an acquisition rejected by a KeyedMutex active-key limit.
	ErrMaxKeys = errors.New("powerlock: maximum keyed locks reached")
	// ErrMaxWaiting identifies an acquisition rejected by a MaxRWMutex waiter limit.
	ErrMaxWaiting = errors.New("powerlock: maximum waiting acquisitions reached")
	// ErrInvalidConfig identifies an invalid constructor value or forbidden post-use reconfiguration.
	ErrInvalidConfig = errors.New("powerlock: invalid configuration")
)

// Error returns the complete acquisition failure.
func (e *AcquisitionError) Error() string {
	switch e.Result {
	case LockResultBusy:
		return fmt.Sprintf("powerlock: %s acquisition for lock %q did not acquire: cause=%v readers=%d writer=%t waiting_readers=%d waiting_writers=%d", e.Mode, e.Name, e.Cause, e.State.Readers, e.State.Writer, e.State.WaitingReaders, e.State.WaitingWriters)
	case LockResultQueueFull:
		return fmt.Sprintf("powerlock: %s acquisition for lock %q rejected: cause=%v waiting=%d reached maximum=%d", e.Mode, e.Name, e.Cause, e.State.WaitingReaders+e.State.WaitingWriters, e.MaxWaiting)
	case LockResultCancelled:
		return fmt.Sprintf("powerlock: %s acquisition for lock %q cancelled before acquisition: cause=%v readers=%d writer=%t waiting_readers=%d waiting_writers=%d", e.Mode, e.Name, e.Cause, e.State.Readers, e.State.Writer, e.State.WaitingReaders, e.State.WaitingWriters)
	case LockResultDeadlineExceeded:
		return fmt.Sprintf("powerlock: %s acquisition for lock %q exceeded its deadline before acquisition: cause=%v readers=%d writer=%t waiting_readers=%d waiting_writers=%d", e.Mode, e.Name, e.Cause, e.State.Readers, e.State.Writer, e.State.WaitingReaders, e.State.WaitingWriters)
	default:
		return fmt.Sprintf("powerlock: %s acquisition for lock %q failed with result=%d: cause=%v readers=%d writer=%t waiting_readers=%d waiting_writers=%d", e.Mode, e.Name, e.Result, e.Cause, e.State.Readers, e.State.Writer, e.State.WaitingReaders, e.State.WaitingWriters)
	}
}

// Unwrap returns the underlying acquisition failure.
func (e *AcquisitionError) Unwrap() error {
	return e.Cause
}

// String returns the diagnostic name for a lock mode.
func (m LockMode) String() string {
	switch m {
	case LockModeWrite:
		return "write"
	case LockModeRead:
		return "read"
	default:
		return "unknown"
	}
}

// String returns the metric and diagnostic name for an acquisition result.
func (r LockResult) String() string {
	switch r {
	case LockResultAcquired:
		return "acquired"
	case LockResultBusy:
		return "busy"
	case LockResultQueueFull:
		return "queue_full"
	case LockResultCancelled:
		return "cancelled"
	case LockResultDeadlineExceeded:
		return "deadline_exceeded"
	default:
		return "unknown"
	}
}

// String returns the diagnostic name for a lock event kind.
func (k LockEventKind) String() string {
	switch k {
	case LockEventWaitStarted:
		return "wait_started"
	case LockEventWaitExceeded:
		return "wait_exceeded"
	case LockEventAcquired:
		return "acquired"
	case LockEventReleased:
		return "released"
	case LockEventTryFailed:
		return "try_failed"
	case LockEventRejected:
		return "rejected"
	case LockEventHoldExceeded:
		return "hold_exceeded"
	default:
		return "unknown"
	}
}

func acquisitionResult(err error) LockResult {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return LockResultDeadlineExceeded
	case errors.Is(err, context.Canceled), errors.Is(err, ErrCancelled):
		return LockResultCancelled
	case errors.Is(err, ErrMaxWaiting):
		return LockResultQueueFull
	default:
		return LockResultBusy
	}
}

func newAcquisitionError(name string, mode LockMode, cause error, maxWaiting int, state LockState) *AcquisitionError {
	return &AcquisitionError{
		Name:       name,
		Mode:       mode,
		Result:     acquisitionResult(cause),
		MaxWaiting: maxWaiting,
		State:      state,
		Cause:      cause,
	}
}

func invalidConfiguration(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfig, message)
}
