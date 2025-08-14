package main

import (
	"fmt"
	"math/rand"
	"time"
)

type Helper struct {
	id int
}

func NewHelper() *Helper {
	return &Helper{
		id: rand.Intn(1000),
	}
}

func (h *Helper) DoSomething() {
	fmt.Printf("Helper %d is doing something...\n", h.id)
	// Simulate some work
	time.Sleep(50 * time.Millisecond)
}
