package powerlock

import (
	"runtime/pprof"
	"sync"
)

const (
	defaultWaiterProfileName = "github.com/donomii/powerlock/waiters"
	defaultHolderProfileName = "github.com/donomii/powerlock/holders"
)

type profileEntry struct {
	attemptID LockAttemptID
	name      string
	mode      LockMode
}

// ProfileObserver exposes current waiters and exactly paired holders through runtime pprof profiles.
type ProfileObserver struct {
	mu       sync.Mutex
	waiters  *pprof.Profile
	holders  *pprof.Profile
	waiting  map[LockAttemptID]*profileEntry
	held     map[LockAttemptID]*profileEntry
	released map[LockAttemptID]struct{}
}

var (
	defaultProfileObserverOnce sync.Once
	defaultProfileObserver     *ProfileObserver
)

// DefaultProfileObserver returns the process-wide Powerlock pprof observer.
func DefaultProfileObserver() *ProfileObserver {
	defaultProfileObserverOnce.Do(func() {
		defaultProfileObserver = &ProfileObserver{
			waiters:  pprof.NewProfile(defaultWaiterProfileName),
			holders:  pprof.NewProfile(defaultHolderProfileName),
			waiting:  make(map[LockAttemptID]*profileEntry),
			held:     make(map[LockAttemptID]*profileEntry),
			released: make(map[LockAttemptID]struct{}),
		}
	})
	return defaultProfileObserver
}

// ObserveLock updates the current waiter and exactly paired holder profiles.
func (o *ProfileObserver) ObserveLock(event LockEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch event.Kind {
	case LockEventWaitStarted:
		entry := &profileEntry{attemptID: event.AttemptID, name: event.Name, mode: event.Mode}
		o.waiting[event.AttemptID] = entry
		o.waiters.Add(entry, 3)
	case LockEventAcquired:
		o.removeWaiter(event.AttemptID)
		if event.ExactHold {
			if _, released := o.released[event.AttemptID]; released {
				delete(o.released, event.AttemptID)
				return
			}
			entry := &profileEntry{attemptID: event.AttemptID, name: event.Name, mode: event.Mode}
			o.held[event.AttemptID] = entry
			o.holders.Add(entry, 3)
		}
	case LockEventReleased:
		if event.ExactHold && event.AttemptID != 0 && !o.removeHolder(event.AttemptID) {
			o.released[event.AttemptID] = struct{}{}
		}
	case LockEventRejected:
		o.removeWaiter(event.AttemptID)
	case LockEventWaitExceeded, LockEventTryFailed, LockEventHoldExceeded:
		return
	default:
		panic("powerlock: pprof observer received an unknown lock event kind")
	}
}

// WaiterProfileName returns the pprof profile containing current waiters.
func (o *ProfileObserver) WaiterProfileName() string {
	return o.waiters.Name()
}

// HolderProfileName returns the pprof profile containing exactly paired current holders.
func (o *ProfileObserver) HolderProfileName() string {
	return o.holders.Name()
}

// WaiterCount returns the number of acquisitions currently represented in the waiter profile.
func (o *ProfileObserver) WaiterCount() int {
	return o.waiters.Count()
}

// HolderCount returns the number of acquisitions currently represented in the holder profile.
func (o *ProfileObserver) HolderCount() int {
	return o.holders.Count()
}

func (o *ProfileObserver) removeWaiter(attemptID LockAttemptID) {
	entry, ok := o.waiting[attemptID]
	if !ok {
		return
	}
	o.waiters.Remove(entry)
	delete(o.waiting, attemptID)
}

func (o *ProfileObserver) removeHolder(attemptID LockAttemptID) bool {
	entry, ok := o.held[attemptID]
	if !ok {
		return false
	}
	o.holders.Remove(entry)
	delete(o.held, attemptID)
	return true
}
