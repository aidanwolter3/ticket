package emulator

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"github.com/google/uuid"
)

// oscParseState is a minimal parser state used by sanitizeOSCC1 to track
// whether the byte stream is currently inside an OSC string sequence.
type oscParseState uint8

const (
	oscGround   oscParseState = iota // normal ground state
	oscEsc                           // just saw 0x1B
	oscInString                      // inside an OSC (or DCS/SOS/APC/PM) string
	oscInStrEsc                      // saw 0x1B inside a string (potential 7-bit ST)
)

// Emulator is a headless terminal emulator that maintains internal state
// and renders to a framebuffer.
type Emulator struct {
	mu sync.RWMutex
	id string

	// oscState tracks whether we are inside an OSC/DCS/SOS/APC/PM string so
	// that sanitizeOSCC1 can replace C1-range bytes (0x80–0x9F) that appear
	// as UTF-8 continuation bytes and would otherwise be misinterpreted as C1
	// control codes (e.g. 0x9C as STRING TERMINATOR) by the x/ansi parser.
	oscState oscParseState

	// vt is the underlying VT100 emulator from charmbracelet/x/vt.
	vt *vt.Emulator

	// PTY pair for process communication.
	pty, tty *os.File

	// Pipe-based I/O (alternative to PTY for testing or log replay).
	reader io.Reader
	writer io.WriteCloser
	isPipe bool

	// Process tracking.
	cmd           *exec.Cmd
	processExited bool
	onExit        func(string, error) // called when process exits: id, exit error

	// onRawOutput is called with raw PTY bytes (before sanitization) on each read.
	onRawOutput func([]byte)

	// damageCh is signaled after each vt.Write so callers can poll GetScreen.
	damageCh chan struct{}

	// stopChan closes when Close() is called.
	stopChan chan struct{}

	// Damage tracking.
	lastRender string
	damaged    bool

	// Screen dimensions.
	width, height int
}

// EmittedFrame represents a rendered frame from the terminal.
type EmittedFrame struct {
	Rows   []string     // each row is a string with ANSI escape codes embedded
	Damage []LineDamage // lines that changed since the last GetScreen call
}

// New creates a new headless terminal emulator backed by a PTY.
func New(cols, rows int) (*Emulator, error) {
	e := &Emulator{
		vt:       vt.NewEmulator(cols, rows),
		id:       uuid.New().String(),
		stopChan: make(chan struct{}),
		damageCh: make(chan struct{}, 1),
		width:    cols,
		height:   rows,
		damaged:  true,
	}

	var err error
	e.pty, e.tty, err = pty.Open()
	if err != nil {
		return nil, err
	}

	if err := e.resize(cols, rows); err != nil {
		e.tty.Close()
		e.pty.Close()
		return nil, err
	}

	go e.ptyReadLoop()
	go e.vtResponseLoop()

	return e, nil
}

// NewFromPipes creates a headless terminal emulator that reads output from r
// and writes input to w, instead of using a PTY. Useful for feeding a captured
// log for one-shot rendering.
func NewFromPipes(cols, rows int, r io.Reader, w io.WriteCloser) (*Emulator, error) {
	e := &Emulator{
		vt:      vt.NewEmulator(cols, rows),
		id:      uuid.New().String(),
		stopChan: make(chan struct{}),
		damageCh: make(chan struct{}, 1),
		reader:  r,
		writer:  w,
		isPipe:  true,
		width:   cols,
		height:  rows,
		damaged: true,
	}

	go e.ptyReadLoop()
	go e.vtResponseLoop()

	return e, nil
}

func (e *Emulator) ID() string { return e.id }

// SetOnExit registers a callback invoked when the process exits.
// The callback receives the emulator ID and the process exit error (nil on clean exit).
func (e *Emulator) SetOnExit(cb func(id string, exitErr error)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onExit = cb
}

// SetOnRawOutput registers a callback invoked with raw PTY bytes before
// they are processed by the VT emulator. Use this to write an output log.
func (e *Emulator) SetOnRawOutput(cb func([]byte)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onRawOutput = cb
}

// DamageChan returns a channel that receives a value after each chunk of PTY
// output is processed. The channel is buffered (capacity 1); a blocked sender
// is skipped so callers never slow down the emulator. Poll GetScreen() after
// receiving from this channel.
func (e *Emulator) DamageChan() <-chan struct{} { return e.damageCh }

// Resize changes the terminal dimensions.
func (e *Emulator) Resize(cols, rows int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.resize(cols, rows)
}

func (e *Emulator) resize(cols, rows int) error {
	if !e.isPipe {
		if err := pty.Setsize(e.pty, &pty.Winsize{
			Rows: uint16(rows),
			Cols: uint16(cols),
			X:    uint16(cols * 8),
			Y:    uint16(rows * 16),
		}); err != nil {
			return err
		}
	}
	e.vt.Resize(cols, rows)
	e.width = cols
	e.height = rows
	e.damaged = true
	return nil
}

// GetScreen returns the current rendered screen as ANSI strings.
func (e *Emulator) GetScreen() EmittedFrame {
	e.mu.Lock()
	defer e.mu.Unlock()

	rendered := e.vt.Render()

	var damage []LineDamage
	if rendered != e.lastRender || e.damaged {
		for y := 0; y < e.height; y++ {
			damage = append(damage, LineDamage{Row: y, X1: 0, X2: e.width, Reason: CRText})
		}
		e.lastRender = rendered
		e.damaged = false
	}

	return EmittedFrame{Rows: splitIntoRows(rendered, e.height, e.width), Damage: damage}
}

// FeedBytes feeds raw bytes directly into the VT emulator (bypassing PTY).
// Useful for one-shot log replay without starting a process.
func (e *Emulator) FeedBytes(data []byte) {
	clean := e.sanitizeOSCC1(data)
	e.mu.Lock()
	e.vt.Write(clean)
	e.damaged = true
	e.mu.Unlock()
}

// Cursor returns the current cursor position and visibility.
func (e *Emulator) Cursor() (Pos, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	pos := e.vt.CursorPosition()
	return Pos{X: pos.X, Y: pos.Y}, true
}

// IsProcessExited reports whether the process has exited.
func (e *Emulator) IsProcessExited() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.processExited
}

// StartCommand starts a command inside the terminal's PTY.
// Not supported for pipe-based emulators.
func (e *Emulator) StartCommand(cmd *exec.Cmd) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.isPipe {
		return fmt.Errorf("StartCommand not supported on pipe-based emulators")
	}
	if e.pty == nil {
		return ErrPTYNotInitialized
	}

	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	termSet := false
	for i, env := range cmd.Env {
		if len(env) >= 5 && env[:5] == "TERM=" {
			cmd.Env[i] = "TERM=xterm-256color"
			termSet = true
			break
		}
	}
	if !termSet {
		cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	}

	cmd.Stdout = e.tty
	cmd.Stdin = e.tty
	cmd.Stderr = e.tty

	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setctty = true
	cmd.SysProcAttr.Setsid = true

	e.cmd = cmd
	e.processExited = false

	if err := cmd.Start(); err != nil {
		return err
	}

	go e.monitorProcess()
	return nil
}

// monitorProcess waits for the process to exit and calls the exit callback.
func (e *Emulator) monitorProcess() {
	if e.cmd == nil {
		return
	}

	exitErr := e.cmd.Wait()

	// Close tty to unblock ptyReadLoop promptly on macOS (EIO from the master
	// side is not immediate until all slave references are closed).
	if e.tty != nil {
		e.tty.Close()
	}

	e.mu.Lock()
	e.processExited = true
	onExit := e.onExit
	id := e.id
	e.mu.Unlock()

	if onExit != nil {
		onExit(id, exitErr)
	}
}

// Write sends data to the PTY or pipe (keyboard input).
func (e *Emulator) Write(data []byte) (int, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.isPipe {
		if e.writer == nil {
			return 0, ErrPTYNotInitialized
		}
		return e.writer.Write(data)
	}

	if e.pty == nil {
		return 0, ErrPTYNotInitialized
	}
	return e.pty.Write(data)
}

// Close shuts down the emulator and frees resources.
func (e *Emulator) Close() error {
	close(e.stopChan)

	if e.isPipe {
		if e.writer != nil {
			e.writer.Close()
		}
		return nil
	}

	if e.tty != nil {
		e.tty.Close()
	}
	if e.pty != nil {
		e.pty.Close()
	}
	return e.vt.Close()
}

// sanitizeOSCC1 replaces C1 control bytes (0x80–0x9F) inside OSC/DCS/SOS/APC/PM
// string sequences with 0x3F ('?'). This prevents the x/ansi parser from
// treating a UTF-8 continuation byte such as 0x9C in ✳ (U+2733) as a C1
// STRING TERMINATOR, which would prematurely dispatch the OSC and leak the
// remainder of the title string as visible cell text.
func (e *Emulator) sanitizeOSCC1(in []byte) []byte {
	hasHigh := false
	for _, b := range in {
		if b >= 0x80 {
			hasHigh = true
			break
		}
	}
	if !hasHigh && e.oscState == oscGround {
		return in
	}

	out := make([]byte, 0, len(in))
	for _, b := range in {
		switch e.oscState {
		case oscGround:
			if b == 0x1B {
				e.oscState = oscEsc
			}
			out = append(out, b)

		case oscEsc:
			if b == ']' || b == 'P' || b == 'X' || b == '^' || b == '_' {
				e.oscState = oscInString
			} else {
				e.oscState = oscGround
			}
			out = append(out, b)

		case oscInString:
			switch {
			case b == 0x07:
				e.oscState = oscGround
				out = append(out, b)
			case b == 0x1B:
				e.oscState = oscInStrEsc
				out = append(out, b)
			case b >= 0x80 && b <= 0x9F:
				out = append(out, '?')
			default:
				out = append(out, b)
			}

		case oscInStrEsc:
			if b == '\\' {
				e.oscState = oscGround
			} else {
				e.oscState = oscInString
			}
			out = append(out, b)
		}
	}
	return out
}

// ptyReadLoop reads from the PTY/pipe and feeds bytes to the VT emulator.
func (e *Emulator) ptyReadLoop() {
	var source io.Reader
	if e.isPipe {
		source = e.reader
	} else {
		source = e.pty
	}

	buf := make([]byte, 4096)
	for {
		select {
		case <-e.stopChan:
			return
		default:
		}

		n, err := source.Read(buf)
		if err != nil {
			return
		}

		if n > 0 {
			// Call raw output callback before sanitization.
			e.mu.RLock()
			cb := e.onRawOutput
			e.mu.RUnlock()
			if cb != nil {
				cb(buf[:n])
			}

			clean := e.sanitizeOSCC1(buf[:n])
			e.mu.Lock()
			e.vt.Write(clean)
			e.damaged = true
			e.mu.Unlock()

			// Signal damage so callers can poll GetScreen.
			select {
			case e.damageCh <- struct{}{}:
			default:
			}
		}
	}
}

// vtResponseLoop reads terminal responses from the vt emulator's internal pipe
// (e.g. device-attribute replies to CSI c) and forwards them back to the child
// process. Without this goroutine the synchronous io.Pipe inside the vt emulator
// blocks the very first response write, which stalls ptyReadLoop while it holds
// e.mu and deadlocks the entire emulator.
func (e *Emulator) vtResponseLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := e.vt.Read(buf)
		if err != nil {
			return
		}
		if n > 0 {
			if e.isPipe {
				if e.writer != nil {
					e.writer.Write(buf[:n]) //nolint:errcheck
				}
			} else {
				if e.pty != nil {
					e.pty.Write(buf[:n]) //nolint:errcheck
				}
			}
		}
	}
}

// splitIntoRows splits the rendered VT output into individual padded rows.
func splitIntoRows(rendered string, height, width int) []string {
	rows := make([]string, height)
	currentRow := 0
	var currentLine string

	for _, r := range rendered {
		if r == '\n' {
			if currentRow < height {
				rows[currentRow] = padRow(currentLine, width)
				currentRow++
			}
			currentLine = ""
		} else {
			currentLine += string(r)
		}
	}

	if currentRow < height && currentLine != "" {
		rows[currentRow] = padRow(currentLine, width)
		currentRow++
	}

	emptyRow := strings.Repeat(" ", width)
	for i := currentRow; i < height; i++ {
		if rows[i] == "" {
			rows[i] = emptyRow
		}
	}

	return rows
}

// padRow pads a row to the specified width, accounting for ANSI escape codes.
func padRow(row string, width int) string {
	visibleLen := 0
	inEscape := false
	for _, r := range row {
		if r == '\033' {
			inEscape = true
		} else if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '~' {
				inEscape = false
			}
		} else {
			visibleLen++
		}
	}
	if visibleLen < width {
		return row + strings.Repeat(" ", width-visibleLen)
	}
	return row
}
