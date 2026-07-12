# Limitations

- FIFO ordering is easier to diagnose and prevents reader barging, but it can be slower than `sync.RWMutex` under contention.
- Blocking methods panic when a specialized lock rejects an acquisition because `sync.Locker` methods cannot return errors. Use the context methods when cancellation, deadlines, or capacity are expected outcomes.
- Lock values must not be copied after first use.
- Ordinary read unlocks cannot be paired with one specific reader. Use read guards when exact hold timing is required.
- Observer calls may be concurrent across acquisitions. They must be concurrency-safe, return promptly, not panic, and not acquire or release the lock emitting the callback. Reading `Name` or `Snapshot` is safe.
- Powerlock annotates watchdog thresholds in the Go execution trace but does not own a runtime trace flight recorder. Applications using the optional callback must start, snapshot, and stop their recorder themselves without blocking the observer call.
- Lock names become Prometheus labels. Use a stable, bounded set and never use request identifiers, user identifiers, URLs, or other unbounded values as names.
- Powerlock does not identify goroutine ownership, infer lock ordering, make locks reentrant, force releases, or recover protected state after an invalid unlock.
- Read ownership cannot be upgraded and write ownership cannot be downgraded. Release and reacquire when a non-atomic transition is acceptable.
- Prefer `sync.Mutex` or `sync.RWMutex` when named state, cancellation, bounded waiting, watchdog thresholds, keyed exclusion, or metrics are not needed.
