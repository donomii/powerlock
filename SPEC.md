# Powerlock specification

## Summary

Powerlock is a Go synchronization library for programs that need more information or control than an unnamed standard mutex provides. It supplies FIFO read/write and exclusive locks with context cancellation, permanent cancellation, bounded waiter queues, structured observation, threshold diagnostics, Prometheus reporting, keyed exclusion, exact acquisition guards, and live pprof state.

The library stores no files and performs no network access. Observers supplied by callers may perform their own work.

## Terms

- **Lock name**: A stable string used in errors, events, state snapshots, and metrics. It may be changed only before the first lock operation.
- **Write acquisition**: Exclusive ownership. No reader or other writer is active at the same time.
- **Read acquisition**: Shared ownership. Multiple readers may be active together.
- **Waiter**: An acquisition that could not proceed immediately and has joined the FIFO queue.
- **Exact acquisition**: An acquisition whose release can be paired with its acquisition identifier. All write acquisitions and all guard-based read acquisitions are exact.
- **Reader cohort**: The uninterrupted interval during which at least one reader is active.
- **Observer**: A caller-supplied destination for structured lock events. Observer calls may occur concurrently across acquisitions and must be handled safely, promptly, and without panicking. An observer may inspect `Name` or `Snapshot`, but must not acquire or release the lock emitting its callback.

## Common guarantees

All Powerlock lock values must not be copied after first use.

All queued acquisitions use FIFO order. Consecutive readers at the front of the queue may acquire together. A new acquisition never bypasses a queued acquisition. Try methods return immediately and never join the queue.

All context-aware methods reject a context that is already cancelled or expired. When cancellation races with a grant, the acquisition succeeds only if it was granted before cancellation became observable to the queue.

An acquired event is delivered before any hold-threshold event for the same acquisition, including when the configured threshold expires during the acquired observer call.

Acquisition, rejection, and try events are delivered before their method returns. Release events are normally delivered before release returns. If another goroutine releases ownership before acquisition delivery completes, due hold and release events are queued until pending acquired delivery completes, preserving acquired-before-hold-before-release order.

Unlocking without a matching acquisition panics with a message that identifies the invalid operation. Releasing a guard twice panics.

`SetLocation` is a compatibility method for setting the lock name. It succeeds before first use and panics after first use. `Name` returns the current lock name.

`Snapshot` returns a point-in-time view. Its durations are readouts, not timers:

- `Readers`: count of current read acquisitions.
- `Version`: monotonically increasing state version used to reject delayed stale state reports.
- `Writer`: whether a write acquisition is current.
- `WaitingReaders`: count of queued readers.
- `WaitingWriters`: count of queued writers.
- `Cancelled`: whether permanent cancellation occurred.
- `OldestWaitDuration`: current duration of the oldest queued acquisition, or zero when none wait.
- `WriterHoldDuration`: current write hold duration when an observer is configured, or zero otherwise.
- `ReaderCohortHoldDuration`: duration since the first reader in the current cohort acquired when an observer is configured, or zero otherwise.

## Common data types

### LockMode

`LockModeWrite` identifies exclusive acquisition. `LockModeRead` identifies shared acquisition.

### LockResult

- `LockResultAcquired`: acquisition succeeded.
- `LockResultBusy`: a non-blocking attempt found incompatible state or an earlier waiter.
- `LockResultQueueFull`: a bounded queue had reached its configured waiter count.
- `LockResultCancelled`: a per-call context or the lock itself was cancelled.
- `LockResultDeadlineExceeded`: the per-call deadline expired before acquisition.

### AcquisitionError

An acquisition error contains the lock name, mode, result, configured waiter limit when relevant, observed lock state, and underlying cause. `errors.Is` reaches the underlying cause. `errors.As` reaches `AcquisitionError`.

### LockEvent

A lock event contains the stable name, mode, event kind, result, process-wide acquisition identifier, contention flag, measured wait duration, measured hold duration when exact, acquisition call stack when enabled, and current lock state.

Observers treat the event and its caller slice as read-only.

Event kinds are wait started, wait threshold exceeded, acquired, released, try failed, rejected, and hold threshold exceeded.

`String` returns one diagnostic line containing kind, name, mode, result, contention, wait and hold timing, ownership, queue counts, cancellation state, and the first captured caller outside Powerlock. `Caller` returns that caller alone or `unavailable`.

### LockObserver

`ObserveLock` receives one event synchronously. `LockObserverFunc` adapts a function. `LockObserverGroup` sends each event to each non-nil observer in order and must not be mutated while in use.

### LockGuard

A guard owns one exact read or write acquisition. `Unlock` releases it once. `Mode`, `AttemptID`, and `Released` are readouts describing that acquisition.

## ContextRWMutex

`ContextRWMutex` is a FIFO read/write lock. Its zero value is ready for use with an empty name.

- `Lock` and `RLock` wait without a per-call cancellation condition.
- `LockContext` and `RLockContext` return when acquired, cancelled, or expired.
- `TryLock` and `TryRLock` return immediately without bypassing queued work.
- `Unlock` and `RUnlock` release ownership.
- `RLocker` exposes the read methods as `sync.Locker`.

## CancelRWMutex

`CancelRWMutex` has the ContextRWMutex behavior plus irreversible `Cancel`.

`Cancel` rejects all queued acquisitions with `ErrCancelled`, rejects future acquisitions, and does not release current holders. Current holders may unlock normally. Repeated cancellation is harmless. `Cancelled` reports the permanent state.

Its zero value is ready for use with an empty name.

## MaxRWMutex

`MaxRWMutex` limits only blocked acquisitions. Current readers or a current writer do not consume waiter capacity. A waiter stops consuming capacity immediately when it acquires, cancels, expires, or is rejected.

`DefaultMaxWaiting` is 1. `NewMaxRWMutex` and the zero value use that default. `NewMaxRWMutexWithLimit` requires a positive limit. `MaxWaiting` reports the configured limit.

`LockContext` and `RLockContext` return an `AcquisitionError` wrapping `ErrMaxWaiting` when the queue is full. `Lock` and `RLock` panic with that error because their signatures cannot return it.

## FairRWMutex

`FairRWMutex` is the explicit FIFO name for `ContextRWMutex`. `FairMutex` is its exclusive-only form.

## ObservedRWMutex

`ObservedRWMutex` has the context-aware FIFO behavior and sends every state transition to its observer. A nil observer disables event delivery without changing synchronization behavior.

Its zero value is a ready unobserved lock with an empty name.

`LockGuard`, `RLockGuard`, `TryLockGuard`, and `TryRLockGuard` return exact release tokens. Ordinary read methods report reader counts and cohort duration but cannot identify individual reader hold duration.

`ObservedMutex` is the exclusive-only form.

## WatchdogRWMutex

`WatchdogRWMutex` captures acquisition call stacks and emits threshold events.

- `DefaultWaitThreshold` is 1 second. A queued acquisition still waiting at that duration emits one wait-threshold event.
- `DefaultHoldThreshold` is 5 seconds. A write acquisition or guard-based read acquisition still held at that duration emits one hold-threshold event.
- A zero threshold disables its corresponding threshold.
- `WaitThreshold` and `HoldThreshold` report the configured durations.
- A watchdog reports but never releases ownership automatically.
- If a threshold timer expires but its callback is delayed by scheduling, completion emits the due threshold event before the acquired, rejected, or released event. An elapsed threshold is not lost.

`NewWatchdogRWMutex` requires a non-empty name and uses both defaults. `NewWatchdogRWMutexWithThresholds` requires non-negative thresholds. `WatchdogMutex` is the exclusive-only form using the defaults.

Changing a watchdog name to an empty string is invalid even before first use.

The zero value is a ready unobserved lock with an empty name and both thresholds disabled. Use a constructor to enable watchdog behavior.

## Runtime trace observer

`RuntimeTraceObserver` accepts lock events but writes only wait-threshold and hold-threshold events to the Go execution trace. Each annotation uses the category `powerlock` and the event's single-line diagnostic string.

`NewRuntimeTraceObserver` accepts an optional `FlightRecorderCallback`. The callback runs synchronously after the trace annotation and receives the same event. It runs even when runtime tracing is disabled so an application using Go 1.25 or later can enqueue a snapshot from its own active flight recorder. The callback must return promptly, not panic, perform no blocking I/O, and not acquire or release the emitting lock.

Powerlock does not start, stop, configure, snapshot, or store a runtime trace flight recorder. Applications own that lifecycle and combine the runtime trace observer with other observers through `LockObserverGroup`.

## KeyedMutex

`KeyedMutex[K]` serializes acquisitions sharing a comparable key while allowing different keys to proceed independently.

`DefaultMaxKeys` is 1024. `NewKeyedMutex` and the zero value use that default. `NewKeyedMutexWithLimit` requires a positive maximum. The maximum counts keys with a holder or waiter. A key entry is removed after its final holder or waiter exits.

`LockContext` returns `KeyedAcquisitionError` wrapping `ErrMaxKeys` when adding a new referenced key would exceed the maximum. The error includes the lock name, key, maximum, active count, and underlying cause. Keys are not automatically copied into metric labels.

A nil, already-cancelled, or already-expired context is rejected before a key entry is created or referenced. Context failure therefore takes precedence over active-key capacity.

`LockGuard` returns `KeyGuard`, which pairs release with its key and acquisition identifier. `Key`, `AttemptID`, and `Released` describe the guard. `ActiveKeys`, `MaxKeys`, `Name`, and `Snapshot` describe the keyed lock.

## Exclusive lock forms

`ContextMutex`, `FairMutex`, `CancelMutex`, `MaxMutex`, `ObservedMutex`, and `WatchdogMutex` expose only exclusive acquisition methods. Their behavior and defaults match their read/write counterparts.

## Pprof profiles

`DefaultProfileObserver` returns one process-wide observer with these profiles:

- `github.com/donomii/powerlock/waiters`: current queued acquisitions.
- `github.com/donomii/powerlock/holders`: current write acquisitions and guard-based read acquisitions.

Ordinary read acquisitions are omitted from the holder profile because their releases cannot be paired individually. `WaiterCount`, `HolderCount`, `WaiterProfileName`, and `HolderProfileName` report profile state and names.

The profiles render captured acquisition stacks and counts. Lock names and modes remain available in structured observer events rather than pprof output.

Attempt identifiers prevent a release event delivered before its acquired event from leaving a stale holder profile entry.

## Prometheus observer

Package `github.com/donomii/powerlock/prometheus` registers these metrics:

- `powerlock_waiting{name,mode}`: current waiter gauge.
- `powerlock_held{name,mode}`: current holder gauge.
- `powerlock_wait_duration_seconds{name,mode}`: wait duration histogram.
- `powerlock_hold_duration_seconds{name,mode}`: exact hold duration histogram.
- `powerlock_acquisitions_total{name,mode,result}`: acquisition result counter.
- `powerlock_contentions_total{name,mode}`: queued acquisition counter.
- `powerlock_wait_threshold_exceeded_total{name,mode}`: wait threshold counter.
- `powerlock_hold_threshold_exceeded_total{name,mode}`: exact hold threshold counter.

Names must be stable and drawn from a bounded set. Modes and results are fixed enumerations. Acquisition identifiers and call stacks never become labels.

Each simultaneously live observed lock should have a unique name. This lets state-version filtering prevent delayed events from restoring stale gauges.

Version acceptance and all four state-gauge updates are serialized so a paused older event cannot overwrite gauges from a newer event.

`New` uses the Prometheus default duration buckets. `NewWithBuckets` accepts finite, positive, strictly increasing bucket boundaries expressed in seconds. `MustNew` panics on registration failure. Convenience constructors attach the observer to observed or watchdog locks.

The adapter package also contains `MeteredRWMutex`, `NewLockMetrics`, and `RegisterLockMetrics` for migration from the original aggregate gauges. These compatibility names are not exported by the core package.

## Unsupported behavior

Powerlock does not force lock release, expire ownership, provide a spin lock, provide distributed exclusion, provide a weighted semaphore, or make a lock reentrant. It does not associate ordinary mutex ownership with a goroutine.

## Behavioral algorithms

The language-neutral algorithms are in [pseudocode/powerlock.pseduocode](pseudocode/powerlock.pseduocode) and [pseudocode/prometheus.pseduocode](pseudocode/prometheus.pseduocode).
