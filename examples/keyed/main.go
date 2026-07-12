package main

import (
	"fmt"

	"github.com/donomii/powerlock"
)

func main() {
	lock := powerlock.NewKeyedMutex[string]("customer-update")
	lock.Lock("customer-a")
	fmt.Printf("same customer available: %t\n", lock.TryLock("customer-a"))
	if lock.TryLock("customer-b") {
		fmt.Println("different customer acquired")
		lock.Unlock("customer-b")
	}
	lock.Unlock("customer-a")
	fmt.Printf("active keys after release: %d\n", lock.ActiveKeys())
}
