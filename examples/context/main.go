package main

import (
	"context"
	"fmt"
	"time"

	"github.com/donomii/powerlock"
)

func main() {
	lock := powerlock.NewContextRWMutex("request-state")
	lock.Lock()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := lock.RLockContext(ctx)
	fmt.Printf("read lock result: %v\n", err)

	lock.Unlock()
}
