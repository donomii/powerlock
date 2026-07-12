package powerlock

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestDiagnosticStringValues(t *testing.T) {
	modes := []struct {
		value LockMode
		want  string
	}{
		{value: LockModeWrite, want: "write"},
		{value: LockModeRead, want: "read"},
		{value: 99, want: "unknown"},
	}
	for _, test := range modes {
		if got := test.value.String(); got != test.want {
			t.Fatalf("unexpected mode string: value=%d expected=%q received=%q", test.value, test.want, got)
		}
	}

	results := []struct {
		value LockResult
		want  string
	}{
		{value: LockResultAcquired, want: "acquired"},
		{value: LockResultBusy, want: "busy"},
		{value: LockResultQueueFull, want: "queue_full"},
		{value: LockResultCancelled, want: "cancelled"},
		{value: LockResultDeadlineExceeded, want: "deadline_exceeded"},
		{value: 99, want: "unknown"},
	}
	for _, test := range results {
		if got := test.value.String(); got != test.want {
			t.Fatalf("unexpected result string: value=%d expected=%q received=%q", test.value, test.want, got)
		}
	}

	kinds := []struct {
		value LockEventKind
		want  string
	}{
		{value: LockEventWaitStarted, want: "wait_started"},
		{value: LockEventWaitExceeded, want: "wait_exceeded"},
		{value: LockEventAcquired, want: "acquired"},
		{value: LockEventReleased, want: "released"},
		{value: LockEventTryFailed, want: "try_failed"},
		{value: LockEventRejected, want: "rejected"},
		{value: LockEventHoldExceeded, want: "hold_exceeded"},
		{value: 99, want: "unknown"},
	}
	for _, test := range kinds {
		if got := test.value.String(); got != test.want {
			t.Fatalf("unexpected event string: value=%d expected=%q received=%q", test.value, test.want, got)
		}
	}
}

func TestAcquisitionErrorMessagesAndCause(t *testing.T) {
	state := LockState{Readers: 2, Writer: true, WaitingReaders: 3, WaitingWriters: 4}
	tests := []struct {
		result LockResult
		cause  error
		text   string
	}{
		{result: LockResultBusy, cause: ErrBusy, text: ErrBusy.Error()},
		{result: LockResultQueueFull, cause: ErrMaxWaiting, text: "reached maximum=7"},
		{result: LockResultCancelled, cause: ErrCancelled, text: "cancelled before acquisition"},
		{result: LockResultDeadlineExceeded, cause: context.DeadlineExceeded, text: "exceeded its deadline"},
		{result: 99, cause: ErrBusy, text: "failed with result=99"},
	}
	for _, test := range tests {
		err := &AcquisitionError{Name: "cache", Mode: LockModeWrite, Result: test.result, MaxWaiting: 7, State: state, Cause: test.cause}
		if !strings.Contains(err.Error(), test.text) {
			t.Fatalf("expected error text %q in %q", test.text, err.Error())
		}
		if !errors.Is(err, test.cause) {
			t.Fatalf("expected wrapped cause %v in %v", test.cause, err)
		}
	}
}

func TestObserverGroupAndEmptyGuards(t *testing.T) {
	count := 0
	group := LockObserverGroup{nil, LockObserverFunc(func(LockEvent) { count++ })}
	group.ObserveLock(LockEvent{})
	if count != 1 {
		t.Fatalf("expected one observer call, got %d", count)
	}

	var guard *LockGuard
	if guard.Mode() != 0 || guard.AttemptID() != 0 || !guard.Released() {
		t.Fatal("unexpected empty lock guard readouts")
	}
	var keyedGuard *KeyGuard[string]
	if keyedGuard.Key() != "" || keyedGuard.AttemptID() != 0 || !keyedGuard.Released() {
		t.Fatal("unexpected empty keyed guard readouts")
	}
}

func TestInvalidConfigurations(t *testing.T) {
	expectPanic(t, func() { NewMaxRWMutexWithLimit("max", 0) })
	expectPanic(t, func() { NewKeyedMutexWithLimit[string]("keys", 0) })
	expectPanic(t, func() { NewWatchdogRWMutex("", nil) })
	expectPanic(t, func() { NewWatchdogRWMutexWithThresholds("watchdog", -time.Second, 0, nil) })
	expectPanic(t, func() {
		waiting := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "invalid_waiting"}, []string{"location"})
		NewMeteredRWMutex("metered", waiting, nil)
	})
}

func TestAdditionalConstructorSurfaces(t *testing.T) {
	fair := NewFairRWMutex("fair")
	fair.RLock()
	fair.RUnlock()

	watchdog := NewWatchdogMutexWithThresholds("watchdog", 2*time.Second, 3*time.Second, nil)
	if watchdog.WaitThreshold() != 2*time.Second || watchdog.HoldThreshold() != 3*time.Second {
		t.Fatalf("unexpected exclusive watchdog thresholds: wait=%s hold=%s", watchdog.WaitThreshold(), watchdog.HoldThreshold())
	}

	profiles := DefaultProfileObserver()
	if profiles.WaiterProfileName() == "" || profiles.HolderProfileName() == "" {
		t.Fatal("expected non-empty pprof profile names")
	}
}

func expectPanic(t *testing.T, operation func()) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected operation to panic")
		}
	}()
	operation()
}
