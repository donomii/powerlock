# Choosing a lock

Start with `sync.Mutex` or `sync.RWMutex`. Choose Powerlock when the lock itself needs cancellation, bounded waiting, diagnostics, metrics, or keyed exclusion.

| Type | Use it when | Waiting and failure behavior | Observation | Zero value |
|---|---|---|---|---|
| `ContextRWMutex` | A queued acquisition must obey a context | FIFO; context methods return typed cancellation or deadline errors | Snapshot | Ready, empty name |
| `FairRWMutex` | The FIFO guarantee should be explicit in the type name | Alias of `ContextRWMutex` | Snapshot | Ready, empty name |
| `CancelRWMutex` | Shutdown must permanently reject queued and future work | FIFO; cancellation is irreversible; blocking methods panic after cancellation | Snapshot | Ready, empty name |
| `MaxRWMutex` | The number of blocked acquisitions must be bounded | FIFO; excess blocking calls return `ErrMaxWaiting`, while `Lock` and `RLock` panic | Snapshot | Ready, one waiter maximum |
| `ObservedRWMutex` | Every wait, acquisition, release, rejection, and failed try needs structured data | FIFO; context-aware | Structured events, exact guards, snapshots, optional pprof and Prometheus adapters | Ready with observation disabled |
| `WatchdogRWMutex` | Slow waits and long holds need caller stacks and threshold reports | FIFO; diagnostics never force release | Observed events plus wait and exact-hold threshold events | Ready with an empty name; thresholds and observation disabled |
| `KeyedMutex[K]` | Work sharing a key must serialize while different keys proceed independently | FIFO per key; context-aware; new keys can be bounded with `ErrMaxKeys` | Per-key snapshot and exact key guard | Ready, 1024 active keys maximum |
| `MeteredRWMutex` | Existing code already uses the original two Prometheus gauges | FIFO; context-aware | Legacy aggregate waiting and held gauges | Ready with metrics disabled |

`ContextMutex`, `FairMutex`, `CancelMutex`, `MaxMutex`, `ObservedMutex`, and `WatchdogMutex` are exclusive-only forms for code that does not need reader sharing.

See `BENCHMARKS.md` for measured overhead, `LIMITATIONS.md` for tradeoffs, and `SPEC.md` for exact behavior.
