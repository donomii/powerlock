# Benchmarks

These results measure the current working tree on 2026-07-09 using Go 1.26.2, darwin/arm64, and an Apple M5 Pro. Each row is the median of three 500 millisecond samples.

```text
go test -run '^$' -bench=. -benchmem -benchtime=500ms -count=3
```

| Benchmark | Time | Bytes | Allocations |
|---|---:|---:|---:|
| `sync.RWMutex` write | 8.395 ns | 0 | 0 |
| `CancelRWMutex` write | 35.80 ns | 0 | 0 |
| `ContextRWMutex` write | 35.67 ns | 0 | 0 |
| `MaxRWMutex` write | 36.10 ns | 0 | 0 |
| `ObservedRWMutex` write, observer disabled | 39.98 ns | 0 | 0 |
| `WatchdogRWMutex` write, observer disabled | 35.74 ns | 0 | 0 |
| `ObservedRWMutex` write, observer enabled | 219.3 ns | 0 | 0 |
| `WatchdogRWMutex` write, observer and thresholds enabled | 836.9 ns | 528 | 5 |
| Successful `ContextRWMutex.TryLock` | 34.64 ns | 0 | 0 |
| Busy `ContextRWMutex.TryLock` | 8.357 ns | 0 | 0 |
| Pre-cancelled `ContextRWMutex.LockContext` | 128.9 ns | 144 | 1 |
| Saturated `MaxRWMutex.LockContext` | 190.1 ns | 144 | 1 |
| `sync.RWMutex` read | 4.975 ns | 0 | 0 |
| `CancelRWMutex` read | 34.23 ns | 0 | 0 |
| `ContextRWMutex` read | 34.11 ns | 0 | 0 |
| `MaxRWMutex` read | 38.39 ns | 0 | 0 |
| `sync.RWMutex` parallel write | 98.42 ns | 0 | 0 |
| `ContextRWMutex` parallel write | 679.8 ns | 319 | 2 |
| `ContextRWMutex` parallel read | 233.3 ns | 0 | 0 |
| `ContextRWMutex` parallel mixed, one write per eight operations | 1197 ns | 275 | 1 |

The unobserved locks retain FIFO ordering, context support where exposed, state counts, and bounded or permanent cancellation behavior. They avoid event construction, call-stack capture, and hold-duration timing when no observer is configured. Contended write acquisition allocates a waiter record and readiness channel.

The enabled watchdog row uses the default wait and hold thresholds with an observer, so it includes call-stack capture, timer creation, and event delivery. It is a diagnostic mode; the disabled row shows the synchronization cost when no observer is attached.

## External diagnostic locks

These results were measured on 2026-07-11 using the same Go 1.26.2, darwin/arm64, and Apple M5 Pro environment. The isolated module under `benchmarks/diagnostics` pins each external dependency. Run its no-argument launcher from the repository root:

```text
./benchmarks/diagnostics/run.sh
```

The launcher enables [linkdata/deadlock v0.5.5](https://github.com/linkdata/deadlock/tree/v0.5.5) with its `deadlock` build tag. [sasha-s/go-deadlock v0.3.9](https://github.com/sasha-s/go-deadlock/tree/v0.3.9) uses its default lock-order and timeout diagnostics. [rwmutexplus v0.0.5](https://github.com/christophcemper/rwmutexplus/tree/v0.0.5) uses a one-second warning threshold. The Powerlock rows use `WatchdogRWMutex` with a no-op observer and the default wait and hold thresholds. Each row is the median of three 500 millisecond uncontended samples.

| Benchmark | Time | Bytes | Allocations |
|---|---:|---:|---:|
| `sync.RWMutex` write | 8.470 ns | 0 | 0 |
| linkdata/deadlock write | 283.9 ns | 32 | 1 |
| sasha-s/go-deadlock write | 368.1 ns | 24 | 1 |
| `WatchdogRWMutex` write | 817.1 ns | 528 | 5 |
| rwmutexplus write | 12.007 µs | 2,657 | 15 |
| `sync.RWMutex` read | 4.961 ns | 0 | 0 |
| linkdata/deadlock read | 281.7 ns | 32 | 1 |
| sasha-s/go-deadlock read | 371.1 ns | 24 | 1 |
| rwmutexplus read | 421.8 ns | 408 | 5 |
| `WatchdogRWMutex` read | 693.1 ns | 256 | 1 |

These libraries do not provide equivalent behavior. The two deadlock packages detect ordering conflicts and timeout-based stalls, rwmutexplus captures warning context, and Powerlock preserves FIFO ordering while emitting structured events, exact hold timing, and threshold stacks. The table measures the cost of each configured diagnostic path during successful uncontended acquisition; it does not rank diagnostic coverage or contended behavior.

Benchmark results are machine-specific. Rerun the command above on the target workload, especially when contention patterns differ from these loops.
