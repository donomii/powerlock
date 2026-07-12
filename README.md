# powerlock

This repository contains alternative lock implementations to help with debugging and monitoring Go applications.

- **CancelRWMutex** - Cancellable locks that can reject all current and future waiters
- **MaxRWMutex** - Locks that limit the number of waiting goroutines
- **ContextRWMutex** - Locks that return when a context is cancelled or times out
- **ObservedRWMutex** - Structured acquisition, release, contention, and rejection events
- **WatchdogRWMutex** - Wait and hold threshold diagnostics with acquisition stacks
- **KeyedMutex** - Independent exclusion by comparable key with bounded key counts

## Overview

A Go library providing enhanced read-write mutex implementations with advanced features for monitoring, debugging, and resource management.

See [Choosing a lock](LOCKS.md) for a side-by-side comparison of failure behavior, observation, and zero-value support.

## Features

This library offers specialized read-write and exclusive mutex types for different concurrent programming challenges:

### CancelRWMutex - Cancellable Locking

A read-write mutex that can be cancelled to reject all current and future lock attempts.

**Key capabilities:**
- Cancellation support - `Cancel()` method rejects all waiters and future attempts
- `Lock()` and `RLock()` panic when cancelled; try-methods return false
- Non-blocking try-lock operations for both read and write locks
- Compatible with standard `sync.RWMutex` interface

**Usage:**
```go
import "github.com/donomii/powerlock"

mutex := powerlock.NewCancelRWMutex("database-access")

// Normal locking operations
mutex.Lock()
defer mutex.Unlock()
// Critical section

// Non-blocking operations
if mutex.TryLock() {
    defer mutex.Unlock()
    // Critical section
}

// Cancel the mutex to reject all current and future waiters
mutex.Cancel()
// After cancellation:
// - Lock() and RLock() will panic
// - TryLock() and TryRLock() will return false
```

### MaxRWMutex - Limited Waiter Queue

A read-write mutex that limits the number of goroutines that can wait for the lock.  Excess goroutines will be rejected immediately.

**Key capabilities:**
- Configurable maximum number of waiting goroutines
- Fails fast when wait limit is exceeded
- Prevents system overload from unbounded lock queues

**Usage:**
```go
mutex := powerlock.NewMaxRWMutexWithLimit("api-handler", 64)

// Will fail immediately if too many goroutines are already waiting
if mutex.TryLock() {
    defer mutex.Unlock()
    // Critical section
} else {
    // Handle the lock being unavailable without joining the queue.
}
```

`NewMaxRWMutex(location)` uses a default limit of 1.

### ObservedRWMutex and WatchdogRWMutex - Diagnostics and Prometheus

Observed locks emit structured state transitions. Watchdog locks add configurable slow-wait and long-hold reports with acquisition stacks. The optional `powerlock/prometheus` adapter converts those events into bounded metrics without adding Prometheus imports to the core package.

**Key capabilities:**
- Read/write waiter and holder gauges
- Acquisition result and contention counters
- Wait and exact-hold duration histograms
- Wait and hold threshold counters
- Cached metric handles for stable lock names
- Runtime trace annotations with an optional application-owned flight-recorder callback

**Usage:**
```go
import (
    powerlockprometheus "github.com/donomii/powerlock/prometheus"
    "github.com/prometheus/client_golang/prometheus"
)

reg := prometheus.NewRegistry()
metrics := powerlockprometheus.MustNew(reg)
mutex := metrics.NewWatchdogRWMutex("cache-update")

mutex.Lock()
defer mutex.Unlock()
// Structured events and Prometheus metrics are updated automatically.
```

### ContextRWMutex - Context-Aware Locking

A read-write mutex whose blocking lock operations return if the supplied context is cancelled or reaches its deadline.

**Key capabilities:**
- `LockContext(ctx)` and `RLockContext(ctx)` return typed acquisition errors that preserve `ctx.Err()` through `errors.Is`
- `Lock()` and `RLock()` remain available for standard blocking use
- Non-blocking try-lock operations for both read and write locks

**Usage:**
```go
mutex := powerlock.NewContextRWMutex("request-state")

ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
defer cancel()

if err := mutex.LockContext(ctx); err != nil {
    // Handle timeout or cancellation
    return
}
defer mutex.Unlock()
```

## Installation

```bash
go get github.com/donomii/powerlock@latest
```

## Requirements

- Go 1.23 or later
- Prometheus client library only when importing the optional `powerlock/prometheus` adapter

## Use Cases

- **CancelRWMutex**: Graceful shutdown scenarios, resource cleanup, or any situation where you need to cancel pending operations
- **MaxRWMutex**: High-throughput services where you want to prevent lock queue buildup
- **ContextRWMutex**: Request-scoped work where waiting for a lock must respect cancellation and deadlines
- **ObservedRWMutex**: Structured diagnostics, metrics adapters, and exact acquisition guards
- **WatchdogRWMutex**: Slow-wait and long-hold incidents with acquisition callers
- **KeyedMutex**: Per-customer, per-resource, or per-job exclusion without a permanent lock map

## Common Interface

All read-write mutex types provide these methods:
- `Lock()` / `Unlock()` - Exclusive write access
- `RLock()` / `RUnlock()` - Shared read access  
- `TryLock()` / `TryRLock()` - Non-blocking lock attempts
- `SetLocation(string)` - Set the stable diagnostic name before first use

Each type extends this base interface with its specialized capabilities:
- **CancelRWMutex** adds: `Cancel()` method for rejecting waiters
- **MaxRWMutex** adds: waiter limiting behavior
- **ContextRWMutex** adds: `LockContext(context.Context)` and `RLockContext(context.Context)`
- **ObservedRWMutex** adds: structured events and exact acquisition guards
- **WatchdogRWMutex** adds: wait and exact-hold threshold reports

Exclusive-only forms omit the read methods. `KeyedMutex` accepts a key on acquisition and release.

## Examples

```bash
go run ./examples/cancel
go run ./examples/max
go run ./examples/metrics
go run ./examples/context
go run ./examples/keyed
go run ./examples/watchdog
```

## Benchmarks

```bash
go test -bench=.
```

## License and notices

See [LICENSE](LICENSE) for Powerlock's license and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for retained upstream notices.
