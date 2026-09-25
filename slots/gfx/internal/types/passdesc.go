package types

import (
	"strconv"

	"github.com/dvoyni/cog/libs/m"
)

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

// Name spells the load op for a debug document.
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

// StoreOp says whether a pass's results survive it.
type StoreOp uint8

const (
	StoreKeep StoreOp = iota
	StoreDiscard
)

// Name spells the store op for a debug document.
func (store StoreOp) String() string {
	switch store {
	case StoreKeep:
		return "keep"
	case StoreDiscard:
		return "discard"
	}
	return "unknown(" + strconv.Itoa(int(store)) + ")"
}

// PassDesc is one render pass for the backend to encode. Screen selects the
// frame buffer, which only the backend can resolve because it is sized from the
// surface; Target names any other colour attachment, and zero means none.
type PassDesc struct {
	Screen bool
	// NoColor is set by a pass that declares no colour attachment at all, which
	// a zero Target cannot say on its own: a texture target whose view does not
	// exist yet resolves to zero too, and the two want opposite handling. A
	// backend must not read "depth-only pass" off a zero Target.
	NoColor    bool
	Target     TextureViewID
	Depth      TextureViewID
	DepthAuto  bool
	Load       LoadOp
	Clear      m.Color
	Store      StoreOp
	DepthLoad  LoadOp
	DepthClear float32
	DepthStore StoreOp
	Label      string
}

// CaptureDesc names one colour target to read back. Screen selects the frame
// buffer, which only the backend can resolve; Texture names any other colour
// texture, and zero means none. It mirrors PassDesc's addressing exactly.
//
// A capture always reads mip 0, layer 0. Texture is a TextureID rather than a
// TextureViewID because a texture-to-buffer copy names a texture, and because
// TextureTransition.Texture already names one.
type CaptureDesc struct {
	Screen  bool
	Texture TextureID
}
