package main

import (
	"fmt"

	"github.com/donomii/powerlock"
)

func printWaitWarning(event powerlock.LockEvent) { fmt.Printf("wait warning: %s\n", event) }

func printHoldWarning(event powerlock.LockEvent) { fmt.Printf("hold warning: %s\n", event) }
