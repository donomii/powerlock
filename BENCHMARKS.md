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

Benchmark results are machine-specific. Rerun the command above on the target workload, especially when contention patterns differ from these loops.
