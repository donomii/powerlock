package powerlock

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

func waitForLockState(t *testing.T, snapshot func() LockState, matches func(LockState) bool) LockState {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		state := snapshot()
		if matches(state) {
			return state
		}
		select {
		case <-timer.C:
			t.Fatalf("lock state did not reach the expected condition: final state=%+v", state)
		default:
			runtime.Gosched()
		}
	}
}

func assertChannelOpen(t *testing.T, channel <-chan struct{}, failure string) {
	t.Helper()
	timer := time.NewTimer(20 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-channel:
		t.Fatal(failure)
	case <-timer.C:
	}
}

func waitForSignal(t *testing.T, channel <-chan struct{}, failure string) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-channel:
	case <-timer.C:
		t.Fatal(failure)
	}
}

func requirePanic(t *testing.T, operation func()) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected operation to panic")
		}
	}()
	operation()
}

func panicText(t *testing.T, operation func()) (message string) {
	t.Helper()
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected operation to panic")
		}
		message = fmt.Sprint(recovered)
	}()
	operation()
	return ""
}
