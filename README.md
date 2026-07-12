# powerlock

This repository contains alternative lock implementations to help with debugging and monitoring Go applications.

- **CancelRWMutex** - Cancellable locks that can reject all current and future waiters
- **MaxRWMutex** - Locks that limit the number of waiting goroutines
- **MeteredRWMutex** - Prometheus-instrumented locks for observability
- **ContextRWMutex** - Locks that return when a context is cancelled or times out

## Overview

A Go library providing enhanced read-write mutex implementations with advanced features for monitoring, debugging, and resource management.

See [Choosing a lock](LOCKS.md) for a side-by-side comparison of failure behavior, observation, and zero-value support.

## Features

This library offers four specialized mutex types, each addressing different concurrent programming challenges:

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
    // Handle case where lock is busy or queue is full
}
```

`NewMaxRWMutex(location)` uses a default limit of 1.

### MeteredRWMutex - Prometheus Monitoring

A read-write mutex instrumented with Prometheus metrics for observability into lock contention and usage patterns.

**Key capabilities:**
- Tracks number of goroutines waiting for locks
- Monitors number of goroutines currently holding locks
- Location-based labeling for detailed analysis
- Prometheus gauge updates on every lock and unlock path

**Usage:**
```go
// Set up metrics
reg := prometheus.NewRegistry()
locksWaiting, locks := powerlock.NewLockMetrics(reg)

// Create metered mutex
mutex := powerlock.NewMeteredRWMutex("cache-update", locksWaiting, locks)

mutex.Lock()
defer mutex.Unlock()
// Metrics automatically updated during lock operations
```

### ContextRWMutex - Context-Aware Locking

A read-write mutex whose blocking lock operations return if the supplied context is cancelled or reaches its deadline.

**Key capabilities:**
- `LockContext(ctx)` and `RLockContext(ctx)` return `ctx.Err()` instead of blocking forever
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
- Prometheus client library (for MeteredRWMutex)

## Use Cases

- **CancelRWMutex**: Graceful shutdown scenarios, resource cleanup, or any situation where you need to cancel pending operations
- **MaxRWMutex**: High-throughput services where you want to prevent lock queue buildup
- **MeteredRWMutex**: Production systems requiring observability into lock contention patterns
- **ContextRWMutex**: Request-scoped work where waiting for a lock must respect cancellation and deadlines

## Common Interface

All mutex types implement a common interface with these methods:
- `Lock()` / `Unlock()` - Exclusive write access
- `RLock()` / `RUnlock()` - Shared read access  
- `TryLock()` / `TryRLock()` - Non-blocking lock attempts
- `SetLocation(string)` - Update location label for debugging/metrics

Each type extends this base interface with its specialized capabilities:
- **CancelRWMutex** adds: `Cancel()` method for rejecting waiters
- **MaxRWMutex** adds: waiter limiting behavior
- **MeteredRWMutex** adds: automatic Prometheus metrics collection
- **ContextRWMutex** adds: `LockContext(context.Context)` and `RLockContext(context.Context)`

## Examples

```bash
go run ./examples/cancel
go run ./examples/max
go run ./examples/metrics
go run ./examples/context
```

## Benchmarks

```bash
go test -bench=.
```
