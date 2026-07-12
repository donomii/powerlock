package main

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/donomii/powerlock"
)

func main() {
	lock := powerlock.NewMaxRWMutexWithLimit("api-handler", 1)
	lock.Lock()

	waiterResult := make(chan error, 1)
	go func() {
		err := lock.LockContext(context.Background())
		waiterResult <- err
		if err == nil {
			lock.Unlock()
		}
	}()

	waitForWriter(lock)
	secondResult := lock.LockContext(context.Background())
	fmt.Printf("second waiter rejected: %v\n", secondResult)

	lock.Unlock()
	if err := <-waiterResult; err != nil {
		panic(err)
	}
	fmt.Println("first waiter completed")
}

func waitForWriter(lock *powerlock.MaxRWMutex) {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		if lock.Snapshot().WaitingWriters == 1 {
			return
		}
		select {
		case <-timer.C:
			panic("first writer did not enter the waiter queue")
		default:
			runtime.Gosched()
		}
	}
}
