// csi_agent sends a CSI c (device-attributes query) then prints a detectable
// line. Used by TestCSIcNoDeadlock to verify vtResponseLoop prevents a deadlock
// when the vt emulator writes a response to its internal io.Pipe.
package main

import (
	"fmt"
	"time"
)

func main() {
	// Send CSI c — device-attributes query. The vt emulator writes a response
	// to an internal io.Pipe; without vtResponseLoop that write blocks, stalling
	// ptyReadLoop which holds the emulator mutex.
	fmt.Printf("\x1b[c")
	time.Sleep(50 * time.Millisecond) // give vtResponseLoop time to drain
	fmt.Println("csi_agent: done")
	time.Sleep(100 * time.Millisecond)
}
