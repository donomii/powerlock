package powerlock_test

import (
	"fmt"
	"time"

	"github.com/donomii/powerlock"
)

func ExampleLockEvent() {
	event := powerlock.LockEvent{
		Name:         "cache",
		Mode:         powerlock.LockModeWrite,
		Kind:         powerlock.LockEventWaitExceeded,
		Result:       powerlock.LockResultBusy,
		Contended:    true,
		WaitDuration: 2 * time.Second,
		State: powerlock.LockState{
			Name:           "cache",
			Readers:        1,
			WaitingWriters: 1,
		},
	}
	fmt.Println(event)
	// Output:
	// powerlock event=wait_exceeded lock="cache" mode=write result=busy contended=true wait=2s hold=unmeasured readers=1 writer=false waiting_readers=0 waiting_writers=1 cancelled=false caller=unavailable
}

func ExampleContextRWMutex() {
	lock := powerlock.NewContextRWMutex("cache")
	lock.Lock()
	fmt.Printf("name=%s writer=%t\n", lock.Name(), lock.Snapshot().Writer)
	lock.Unlock()
	// Output:
	// name=cache writer=true
}
