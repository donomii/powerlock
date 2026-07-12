// Package powerlock provides named, context-aware, bounded, cancellable, observable, and watchdog mutexes.
//
// The read/write lock types use FIFO waiter ordering. Context methods return typed errors whose causes remain
// available through errors.Is and errors.As. Blocking Lock methods retain sync.Locker behavior and panic when a
// specialized lock rejects an acquisition. Try methods never join or bypass an existing waiter queue.
//
// Observed locks emit structured transition events. Watchdog locks add wait and exact-hold threshold events,
// while LockGuard permits exact pairing of individual reader acquisitions. DefaultProfileObserver exposes current
// waiters and exactly paired holders through runtime pprof profiles.
//
// Use ContextRWMutex for cancellable FIFO waiting, CancelRWMutex for irreversible shutdown, MaxRWMutex to bound
// blocked acquisitions, ObservedRWMutex for complete event and metric reporting, WatchdogRWMutex for slow wait and
// hold diagnostics, and KeyedMutex to serialize work independently by key. Exclusive forms omit the read methods.
// LockEvent.String formats an event as a single actionable diagnostic line. Prefer sync.Mutex or sync.RWMutex when
// these controls and diagnostics are not needed.
package powerlock
