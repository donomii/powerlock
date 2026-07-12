package powerlock

import "sync/atomic"

// LockGuard owns one exact acquisition and releases it once.
type LockGuard struct {
	core      *rwCore
	mode      LockMode
	attemptID LockAttemptID
	released  atomic.Bool
}

func newLockGuard(core *rwCore, mode LockMode, attemptID LockAttemptID) *LockGuard {
	return &LockGuard{core: core, mode: mode, attemptID: attemptID}
}

// Unlock releases the acquisition and panics if the guard has already been released.
func (g *LockGuard) Unlock() {
	if g == nil || g.core == nil {
		panic("powerlock: unlock called on an empty lock guard")
	}
	if !g.released.CompareAndSwap(false, true) {
		panic("powerlock: lock guard released more than once")
	}
	g.core.release(g.mode, g.attemptID)
}

// Mode reports whether the guard owns a read or write acquisition.
func (g *LockGuard) Mode() LockMode {
	if g == nil {
		return 0
	}
	return g.mode
}

// AttemptID returns the diagnostic identifier for the acquisition.
func (g *LockGuard) AttemptID() LockAttemptID {
	if g == nil {
		return 0
	}
	return g.attemptID
}

// Released reports whether Unlock has already released the acquisition.
func (g *LockGuard) Released() bool {
	return g == nil || g.released.Load()
}
