package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("Starting test program...")

	// Use functions from other files
	helper := NewHelper()
	helper.DoSomething()

	// Simulate some work
	for i := 0; i < 5; i++ {
		fmt.Printf("Iteration %d\n", i)
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Println("Test program completed!")
}
