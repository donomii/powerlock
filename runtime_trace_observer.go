package powerlock

import (
	"context"
	"runtime/trace"
)

// FlightRecorderCallback receives watchdog threshold events after their runtime trace annotation is written.
// Applications using Go 1.25 or later can use it to enqueue a snapshot of an active runtime trace flight recorder.
type FlightRecorderCallback func(LockEvent)

// RuntimeTraceObserver writes watchdog threshold events to the Go execution trace.
type RuntimeTraceObserver struct {
	onThreshold FlightRecorderCallback
}

// NewRuntimeTraceObserver returns a runtime trace observer with an optional flight-recorder callback.
func NewRuntimeTraceObserver(onThreshold FlightRecorderCallback) *RuntimeTraceObserver {
	return &RuntimeTraceObserver{onThreshold: onThreshold}
}

// ObserveLock records wait and hold threshold events under the powerlock trace category.
func (o *RuntimeTraceObserver) ObserveLock(event LockEvent) {
	if event.Kind != LockEventWaitExceeded && event.Kind != LockEventHoldExceeded {
		return
	}
	trace.Log(context.Background(), "powerlock", event.String())
	if o != nil && o.onThreshold != nil {
		o.onThreshold(event)
	}
}
