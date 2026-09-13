package gfx

import "github.com/dvoyni/cog/extensions/gfx/internal"

// Order places a pass in the frame's shared ordering space. gfx defines no
// conventions and reserves no ranges: recorders that must interleave - canvas
// layers and scene cameras - agree on numbers between themselves, because they
// record from separate update subscriptions and stream order between them is
// not defined.
type Order = internal.Order

// LoadOp says what a pass does with an attachment's existing contents.
type LoadOp = internal.LoadOp

const (
	// LoadPreserve keeps what is already in the attachment.
	LoadPreserve = internal.LoadPreserve
	// LoadClear overwrites it with the pass's clear value.
	LoadClear = internal.LoadClear
	// LoadDiscard declares the contents irrelevant, which lets the driver skip
	// reading them back in.
	LoadDiscard = internal.LoadDiscard
)

// StoreOp says whether a pass's results survive it.
type StoreOp = internal.StoreOp

const (
	StoreKeep    = internal.StoreKeep
	StoreDiscard = internal.StoreDiscard
)

// TargetDescr names a pass's colour attachment.
type TargetDescr = internal.TargetDescr

// ScreenTarget is the frame's screen attachment. It stays a sentinel the
// recorder cannot resolve: the swapchain view is per-frame and known only on
// the render thread.
func ScreenTarget() TargetDescr {
	return internal.ScreenTarget()
}

// TextureTarget renders into one mip level of one layer of a texture, which
// must have been allocated Renderable.
func TextureTarget(texture TextureDescr, mip, layer int) TargetDescr {
	return internal.TextureTarget(texture, mip, layer)
}

// NoTarget declares a pass with no colour attachment, such as a depth-only
// prepass.
func NoTarget() TargetDescr {
	return internal.NoTarget()
}

// DepthDescr names a pass's depth attachment.
type DepthDescr = internal.DepthDescr

// DepthAuto uses the backend's own depth texture for the target's size. Every
// DepthAuto pass at a given size shares one texture, so a pass that means to
// start from a clean depth buffer must clear depth or it inherits whatever the
// previous pass at that size left behind.
func DepthAuto() DepthDescr {
	return internal.DepthAuto()
}

// DepthNone declares a pass with no depth attachment.
func DepthNone() DepthDescr {
	return internal.DepthNone()
}

// DepthTarget renders depth into a texture, which must be FormatDepth32F and
// Renderable.
func DepthTarget(texture TextureDescr) DepthDescr {
	return internal.DepthTarget(texture)
}

// PassDescr declares one render pass: where it draws, in what order, and what
// happens to its attachments at either end.
type PassDescr = internal.PassDescr

// PassRef selects a pass declared earlier in the same frame. Its zero value
// refers to no pass.
type PassRef = internal.PassRef
