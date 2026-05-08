// echo_agent reads lines from stdin and echoes each one prefixed with "got:".
// Used by TestAttachKeyForwarding to verify that key input is forwarded to the
// agent PTY.
package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	fmt.Println("echo_agent: ready")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Printf("got:%s\n", line)
	}
}
