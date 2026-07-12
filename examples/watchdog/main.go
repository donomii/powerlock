package main

import (
	"context"
	"time"

	"github.com/donomii/powerlock"
)

func main() {
	events := make(chan powerlock.LockEvent, 2)
	observer := powerlock.LockObserverFunc(func(event powerlock.LockEvent) {
		if event.Kind == powerlock.LockEventWaitExceeded || event.Kind == powerlock.LockEventHoldExceeded {
			events <- event
		}
	})
	waitLock := powerlock.NewWatchdogRWMutexWithThresholds("cache-update", 5*time.Millisecond, 0, observer)

	waitLock.Lock()
	readerDone := make(chan error, 1)
	go func() {
		err := waitLock.RLockContext(context.Background())
		readerDone <- err
		if err == nil {
			waitLock.RUnlock()
		}
	}()
	waitEvent := <-events
	printWaitWarning(waitEvent)
	waitLock.Unlock()
	if err := <-readerDone; err != nil {
		panic(err)
	}

	holdLock := powerlock.NewWatchdogRWMutexWithThresholds("cache-update", 0, 5*time.Millisecond, observer)
	guard, err := holdLock.RLockGuard(context.Background())
	if err != nil {
		panic(err)
	}
	holdEvent := <-events
	printHoldWarning(holdEvent)
	guard.Unlock()
}
