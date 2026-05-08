package emulator

// ChangeReason says what kind of change caused a region to change.
type ChangeReason int

const (
	CRText         ChangeReason = iota // text printed normally
	CRClear                            // area cleared
	CRScroll                           // area scrolled
	CRScreenSwitch                     // switched between main and alt screen
	CRRedraw                           // application requested full redraw
)

// LineDamage represents a changed region on a single row.
type LineDamage struct {
	Row    int
	X1     int
	X2     int
	Reason ChangeReason
}

// Pos represents a position on the screen.
type Pos struct {
	X int
	Y int
}
