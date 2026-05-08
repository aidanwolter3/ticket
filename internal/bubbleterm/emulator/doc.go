// Package emulator provides a headless VT100 terminal emulator backed by
// charmbracelet/x/vt. It includes two critical patches for Claude Code sessions:
//
//  1. vtResponseLoop: drains the vt emulator's internal response pipe and
//     forwards device-attribute replies back to the child PTY. Without this,
//     CSI c (device-attributes query) causes a deadlock where ptyReadLoop holds
//     the emulator mutex forever — the pane stays empty indefinitely.
//
//  2. sanitizeOSCC1: replaces C1 control bytes (0x80–0x9F) inside OSC strings
//     with '?' before feeding bytes to the vt parser. Without this, UTF-8
//     continuation bytes in OSC window titles (like 0x9C in the ✳ U+2733 glyph
//     Claude Code emits) are misinterpreted as C1 STRING TERMINATOR, causing
//     the OSC to dispatch prematurely and leaking the title text as visible
//     screen content.
package emulator
