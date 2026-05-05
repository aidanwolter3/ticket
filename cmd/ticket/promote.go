package main

import "fmt"

// runPromote is a deprecated alias for runReady.
func runPromote(args []string, defaultDB string) {
	fmt.Println("warning: 'ticket promote' is deprecated; use 'ticket ready' instead")
	runReady(args, defaultDB)
}
