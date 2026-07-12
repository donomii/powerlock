package powerlock

import "testing"

func TestRuntimeTraceObserverFiltersThresholdEvents(t *testing.T) {
	events := make([]LockEvent, 0, 2)
	observer := NewRuntimeTraceObserver(func(event LockEvent) {
		events = append(events, event)
	})

	observer.ObserveLock(LockEvent{Kind: LockEventAcquired})
	observer.ObserveLock(LockEvent{Kind: LockEventWaitExceeded})
	observer.ObserveLock(LockEvent{Kind: LockEventReleased})
	observer.ObserveLock(LockEvent{Kind: LockEventHoldExceeded})

	if len(events) != 2 {
		t.Fatalf("expected two threshold callbacks, got %d", len(events))
	}
	if events[0].Kind != LockEventWaitExceeded || events[1].Kind != LockEventHoldExceeded {
		t.Fatalf("expected wait and hold callbacks, got %s and %s", events[0].Kind, events[1].Kind)
	}
}

func TestRuntimeTraceObserverZeroValue(t *testing.T) {
	var observer RuntimeTraceObserver
	observer.ObserveLock(LockEvent{Kind: LockEventWaitExceeded})
	observer.ObserveLock(LockEvent{Kind: LockEventHoldExceeded})
}
