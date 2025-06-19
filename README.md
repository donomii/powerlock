# powerlock

A Go library providing enhanced read-write mutex implementations with advanced features for monitoring, debugging, and resource management.

## Features

This library offers three specialized mutex types, each addressing different concurrent programming challenges:

### CtxRWMutex - Context-Aware Locking

A read-write mutex that supports context-based timeouts and cancellation.

**Key capabilities:**
- Context-aware lock acquisition with timeout support
- Non-blocking try-lock operations for both read and write locks
- Compatible with standard `sync.RWMutex` interface
- Prevents deadlocks through timeout mechanisms

**Usage:**
```go
import "powerlock"

mutex := ctxlock.NewCtxRWMutex("database-access")

// Context-aware locking with timeout
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

if err := mutex.LockContext(ctx); err != nil {
    // Handle timeout or cancellation
    return err
}
defer mutex.Unlock()

// Non-blocking operations
if mutex.TryLock() {
    defer mutex.Unlock()
    // Critical section
}
```

### MaxRWMutex - Limited Waiter Queue

A read-write mutex that limits the number of goroutines that can wait for lock acquisition, preventing resource exhaustion from excessive queueing.

**Key capabilities:**
- Configurable maximum number of waiting goroutines
- Fails fast when waiter limit is exceeded
- Prevents system overload from unbounded lock queues
- Standard and try-lock operations

**Usage:**
```go
mutex := ctxlock.NewMaxRWMutex("api-handler")

// Will fail immediately if too many goroutines are already waiting
if mutex.TryLock() {
    defer mutex.Unlock()
    // Critical section
} else {
    // Handle case where lock is busy or queue is full
}
```

### MeteredRWMutex - Prometheus Monitoring

A read-write mutex instrumented with Prometheus metrics for observability into lock contention and usage patterns.

**Key capabilities:**
- Tracks number of goroutines waiting for locks
- Monitors number of goroutines currently holding locks
- Location-based labeling for detailed analysis
- Zero-overhead when metrics are not collected

**Usage:**
```go
// Set up metrics
reg := prometheus.NewRegistry()
locksWaiting, locks := ctxlock.NewLockMetrics(reg)

// Create metered mutex
mutex := ctxlock.NewMeteredRWMutex("cache-update", locksWaiting, locks)

mutex.Lock()
defer mutex.Unlock()
// Metrics automatically updated during lock operations
```

## Installation

```bash
go get powerlock
```

## Requirements

- Go 1.24.2 or later
- Prometheus client library (for MeteredRWMutex)

## Use Cases

- **CtxRWMutex**: Network operations, database connections, or any scenario where you need timeout control
- **MaxRWMutex**: High-throughput services where you want to prevent lock queue buildup
- **MeteredRWMutex**: Production systems requiring observability into lock contention patterns

## Common Interface

All mutex types implement a common interface with these methods:
- `Lock()` / `Unlock()` - Exclusive write access
- `RLock()` / `RUnlock()` - Shared read access  
- `TryLock()` / `TryRLock()` - Non-blocking lock attempts
- `CtxRWLocation(string)` - Update location label for debugging/metrics

Each type extends this base interface with its specialized capabilities (context support, waiter limits, or metrics collection).
