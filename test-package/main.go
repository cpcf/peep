package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	fmt.Println("Starting test program...")

	// Print command line arguments
	fmt.Printf("Command line arguments: %v\n", os.Args)

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
