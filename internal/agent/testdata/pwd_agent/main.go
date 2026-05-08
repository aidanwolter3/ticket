// pwd_agent prints its working directory and exits.
// Used by TestLaunch_SetsWorkingDirectory to verify cmd.Dir is set correctly.
package main

import (
	"fmt"
	"os"
)

func main() {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pwd_agent: getwd:", err)
		os.Exit(1)
	}
	fmt.Println("pwd_agent:", dir)
}
