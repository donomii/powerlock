package ctxlock

import (
	"fmt"
)

func testMaxRWMutex() {
	// Create a MaxRWMutex with waiter limit of 1
	m := NewMaxRWMutex("test")
	
	fmt.Println("Taking first lock...")
	m.Lock()
	fmt.Println("First lock acquired")
	
	// Try to take second lock - should panic
	fmt.Println("Trying second lock...")
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Second lock panicked as expected: %v\n", r)
		}
	}()
	
	m.Lock() // Should panic
	fmt.Println("Second lock acquired - this shouldn't happen!")
}

func main() {
	testMaxRWMutex()
}
