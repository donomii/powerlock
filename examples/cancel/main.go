package main

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/donomii/powerlock"
)

func main() {
	lock := powerlock.NewCancelRWMutex("shutdown")
	lock.Lock()

	waiterResult := make(chan error, 1)
	go func() {
		waiterResult <- lock.RLockContext(context.Background())
	}()

	waitForReader(lock)
	lock.Cancel()
	lock.Unlock()

	fmt.Printf("waiter rejected: %v\n", <-waiterResult)
	fmt.Printf("future TryLock succeeds: %t\n", lock.TryLock())
}

func waitForReader(lock *powerlock.CancelRWMutex) {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		if lock.Snapshot().WaitingReaders == 1 {
			return
		}
		select {
		case <-timer.C:
			panic("reader did not enter the waiter queue")
		default:
			runtime.Gosched()
		}
	}
}
