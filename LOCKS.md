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

`ContextMutex`, `FairMutex`, `CancelMutex`, `MaxMutex`, `ObservedMutex`, and `WatchdogMutex` are exclusive-only forms for code that does not need reader sharing.

## Lock-order diagnostics

Powerlock does not infer which locks one goroutine owns. Go exposes no supported current-goroutine identity, so automatic ownership tracking would depend on runtime details that can change and would make reports unreliable.

A future opt-in diagnostic package can instead require an explicit logical-operation scope. Guarded acquisitions would add scope-local ordering edges with acquisition stacks and reject an edge that closes a cycle with a typed conflict report. The design remains deferred because every participating acquisition must carry the scope; partial adoption could otherwise imply safety it cannot provide.

## Read/write conversion

Powerlock does not currently upgrade read ownership or downgrade write ownership. A blocking upgrade can deadlock when multiple readers retain their locks while waiting to become the writer, and releasing a read lock before an ordinary write acquisition is not atomic.

If conversion is added after v0.1, it will be guard-only. `TryUpgrade` would retain the read guard on failure and succeed only when that guard is the sole reader and no waiter is queued. `Downgrade` would atomically replace an exact write guard with a read guard. Either successful conversion would use a fresh acquisition identifier so diagnostics continue to pair one mode and hold interval exactly.

See `BENCHMARKS.md` for measured overhead, `LIMITATIONS.md` for tradeoffs, and `SPEC.md` for exact behavior.
