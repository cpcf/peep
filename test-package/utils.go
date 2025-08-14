package main

import (
	"fmt"
	"strings"
)

// Utility functions that might be used by the main program
func FormatMessage(msg string) string {
	return strings.ToUpper(msg)
}

func PrintBanner() {
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("TEST PACKAGE UTILITIES")
	fmt.Println(strings.Repeat("=", 50))
}
