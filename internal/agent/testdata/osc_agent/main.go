// osc_agent emits an OSC window title containing ✳ (U+2733, UTF-8: e2 9c b3)
// then prints a detectable line. Used by TestOSCAgentNoLeak to verify that the
// 0x9C continuation byte inside the OSC does not leak as visible screen text.
package main

import (
	"fmt"
	"time"
)

func main() {
	// Emit an OSC 0 (window title) sequence containing ✳. Without the
	// sanitizeOSCC1 fix the 0x9C byte in ✳ is mistaken for C1 ST, causing the
	// remainder " Claude Code" to land as visible cell text.
	fmt.Printf("\x1b]0;✳ Claude Code\x07")
	fmt.Println("osc_agent: ready")
	time.Sleep(100 * time.Millisecond)
}
