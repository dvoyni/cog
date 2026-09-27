package types

import "strconv"

// LoadOp says what a pass does with an attachment's existing contents.
type LoadOp uint8

const (
	// LoadPreserve keeps what is already in the attachment.
	LoadPreserve LoadOp = iota
	// LoadClear overwrites it with the pass's clear value.
	LoadClear
	// LoadDiscard declares the contents irrelevant, which lets the driver skip
	// reading them back in.
	LoadDiscard
)

// String spells the load op for a debug document.
func (load LoadOp) String() string {
	switch load {
	case LoadPreserve:
		return "preserve"
	case LoadClear:
		return "clear"
	case LoadDiscard:
		return "discard"
	}
	return "unknown(" + strconv.Itoa(int(load)) + ")"
}
