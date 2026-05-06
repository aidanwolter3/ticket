// fake_agent is a test helper used by launcher_test.go.
// It prints a few lines, sleeps briefly (simulating "waiting for input"),
// then prints more lines and exits 0.
package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("fake_agent: starting")
	fmt.Println("fake_agent: working")

	// Simulate a pause (gives the test time to observe the running→waiting transition).
	time.Sleep(200 * time.Millisecond)

	fmt.Println("fake_agent: finishing")
}
