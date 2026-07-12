# Release notes

## v0.1.0 — named locks that explain contention

Powerlock's first tagged release provides FIFO Go locks for the cases where an ordinary mutex cannot explain why work is stuck or stop waiting safely.

### Included

- Context-aware, permanently cancellable, bounded-waiter, fair, observed, watchdog, keyed, and exclusive lock forms.
- Structured acquisition events, typed errors, exact acquisition guards, and live state snapshots.
- Wait and exact-hold threshold diagnostics with captured acquisition callers.
- Runtime trace annotations, live pprof waiter and holder profiles, and bounded Prometheus metrics.
- A Prometheus compatibility adapter for the original aggregate-gauge API without Prometheus imports in the core package.

### Diagnostic output

```text
powerlock event=wait_exceeded lock="cache" mode=write result=busy contended=true wait=2s hold=unmeasured readers=1 writer=false waiting_readers=0 waiting_writers=1 cancelled=false caller=unavailable
```

### Measured overhead

On an Apple M5 Pro with Go 1.26.2, the uncontended write benchmarks measured 8.395 ns for `sync.RWMutex`, 35.67 ns for `ContextRWMutex`, 35.74 ns for `WatchdogRWMutex` with observation disabled, and 836.9 ns with watchdog observation and thresholds enabled. The enabled watchdog path allocated 528 bytes in five allocations for caller capture, timing, event delivery, and threshold scheduling.

Results are machine-specific. [BENCHMARKS.md](https://github.com/donomii/powerlock/blob/v0.1.0/BENCHMARKS.md) contains the reproducible commands, read and contention cases, and external diagnostic-library comparison.

### Compatibility

The minimum supported version is Go 1.23. See [LOCKS.md](https://github.com/donomii/powerlock/blob/v0.1.0/LOCKS.md) before choosing a lock and [COMPATIBILITY.md](https://github.com/donomii/powerlock/blob/v0.1.0/COMPATIBILITY.md) for the pre-v1 change policy.
