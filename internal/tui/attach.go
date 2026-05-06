package tui

import (
	"bufio"
	"io"
	"os"

	"github.com/charmbracelet/x/term"
)

// agentAttachCmd implements tea.ExecCommand for attaching to a PTY master fd.
// It replays the last N lines of a log file, then raw-proxies stdin/stdout
// until ctrl+] (0x1D) is received.
type agentAttachCmd struct {
	ptym    *os.File
	logPath string
	tailN   int
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
}

func (c *agentAttachCmd) SetStdin(r io.Reader)  { c.stdin = r }
func (c *agentAttachCmd) SetStdout(w io.Writer) { c.stdout = w }
func (c *agentAttachCmd) SetStderr(w io.Writer) { c.stderr = w }

func (c *agentAttachCmd) Run() error {
	// Replay last N lines of the log.
	replayLog(c.logPath, c.tailN, c.stdout)

	// Put terminal in raw mode so we can intercept individual bytes.
	fd := os.Stdin.Fd()
	oldState, err := term.MakeRaw(fd)
	if err == nil {
		defer term.Restore(fd, oldState)
	}

	// goroutine: copy PTY → stdout.
	done := make(chan struct{})
	go func() {
		defer close(done)
		io.Copy(c.stdout, c.ptym)
	}()

	// Main loop: read stdin one byte at a time, intercept ctrl+].
	buf := make([]byte, 256)
	for {
		n, err := c.stdin.Read(buf)
		if err != nil {
			break
		}
		for i := 0; i < n; i++ {
			if buf[i] == 0x1D { // ctrl+]
				return nil
			}
		}
		if _, werr := c.ptym.Write(buf[:n]); werr != nil {
			break
		}
	}

	<-done
	return nil
}

// replayLog writes the last n lines of logPath to w.
func replayLog(logPath string, n int, w io.Writer) {
	f, err := os.Open(logPath)
	if err != nil {
		return
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	start := len(lines) - n
	if start < 0 {
		start = 0
	}
	for _, l := range lines[start:] {
		io.WriteString(w, l+"\n")
	}
}
