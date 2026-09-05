package gfx

import "github.com/dvoyni/cog/m"

// Order places a pass in the frame's shared ordering space. gfx defines no
// conventions and reserves no ranges: recorders that must interleave - canvas
// layers and scene cameras - agree on numbers between themselves, because they
// record from separate update subscriptions and stream order between them is
// not defined.
type Order int

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

// StoreOp says whether a pass's results survive it.
type StoreOp uint8

const (
	StoreKeep StoreOp = iota
	StoreDiscard
)

type targetKind uint8

const (
	targetNone targetKind = iota
	targetScreen
	targetTexture
)

// TargetDescr names a pass's colour attachment.
type TargetDescr struct {
	kind       targetKind
	texture    TextureID
	mip, layer int
}

// ScreenTarget is the frame's screen attachment. It stays a sentinel the
// recorder cannot resolve: the swapchain view is per-frame and known only on
// the render thread.
func ScreenTarget() TargetDescr { return TargetDescr{kind: targetScreen} }

// TextureTarget renders into one mip level of one layer of a texture, which
// must have been allocated Renderable.
func TextureTarget(texture TextureDescr, mip, layer int) TargetDescr {
	return TargetDescr{kind: targetTexture, texture: texture.id, mip: mip, layer: layer}
}

// NoTarget declares a pass with no colour attachment, such as a depth-only
// prepass.
func NoTarget() TargetDescr { return TargetDescr{kind: targetNone} }

type depthKind uint8

const (
	depthKindNone depthKind = iota
	depthKindAuto
	depthKindTexture
)

// DepthDescr names a pass's depth attachment.
type DepthDescr struct {
	kind    depthKind
	texture TextureID
}

// DepthAuto uses the backend's own depth texture for the target's size. Every
// DepthAuto pass at a given size shares one texture, so a pass that means to
// start from a clean depth buffer must clear depth or it inherits whatever the
// previous pass at that size left behind.
func DepthAuto() DepthDescr { return DepthDescr{kind: depthKindAuto} }

// DepthNone declares a pass with no depth attachment.
func DepthNone() DepthDescr { return DepthDescr{} }

// DepthTarget renders depth into a texture, which must be FormatDepth32F and
// Renderable.
func DepthTarget(texture TextureDescr) DepthDescr {
	return DepthDescr{kind: depthKindTexture, texture: texture.id}
}

// PassDescr declares one render pass: where it draws, in what order, and what
// happens to its attachments at either end.
type PassDescr struct {
	Order      Order
	Target     TargetDescr
	Depth      DepthDescr
	Load       LoadOp
	Clear      m.Color
	Store      StoreOp
	DepthLoad  LoadOp
	DepthClear float32
	DepthStore StoreOp
	Label      string
}

// PassRef selects a pass declared earlier in the same frame. Its zero value
// refers to no pass.
type PassRef int

// passRecord is one declared pass and the position that breaks Order ties.
type passRecord struct {
	desc PassDescr
	seq  int
}

// Pass declares a pass and selects it: every op recorded afterwards appends to
// it, until another Pass or SetPass call. Passes run in Order, not in the order
// they were declared.
func (q *OpQueue) Pass(desc PassDescr) PassRef {
	q.passes = append(q.passes, passRecord{desc: desc, seq: len(q.passes)})
	q.current = len(q.passes) - 1
	return PassRef(len(q.passes))
}

// SetPass re-selects a pass declared earlier this frame. An unknown reference
// is ignored.
func (q *OpQueue) SetPass(ref PassRef) {
	if ref > 0 && int(ref) <= len(q.passes) {
		q.current = int(ref) - 1
	}
}

// selectedPass returns the index of the pass ops are appended to, or -1 when no
// pass is selected. Every draw names a pass: there is no implicit one, because
// a default screen pass would silently absorb draws that belonged in a camera's
// target, and it would have to guess an Order.
func (q *OpQueue) selectedPass() int {
	if q.current < 0 || q.current >= len(q.passes) {
		return -1
	}
	return q.current
}

// sameAttachments reports whether two passes render into the same places. Two
// DepthAuto passes count as the same attachment because they share a colour
// target, and therefore a size, and therefore the backend's one depth texture
// for that size.
func sameAttachments(a, b PassDescr) bool {
	return a.Target == b.Target && a.Depth == b.Depth
}

// mergesInto reports whether successor is by definition indistinguishable from
// continuing predecessor: same attachments, nothing to load, nothing lost.
// Merging then cannot change results, and it is what makes canvas's pass per
// layer cost one GPU pass.
func mergesInto(successor, predecessor PassDescr) bool {
	return sameAttachments(successor, predecessor) &&
		successor.Load == LoadPreserve && successor.DepthLoad == LoadPreserve &&
		predecessor.Store == StoreKeep && predecessor.DepthStore == StoreKeep
}

// hasEffect reports whether a pass is observable. Draws make it observable, and
// so does any attachment that loads: "clear this target and nothing else" and a
// camera that culled everything are both legitimate frames.
func (p PassDescr) hasEffect(draws int) bool {
	loads := func(op LoadOp) bool { return op == LoadClear || op == LoadDiscard }
	return draws > 0 || loads(p.Load) || loads(p.DepthLoad)
}
