package types

import (
	"github.com/dvoyni/cog/libs/m"
)

// Order places a pass in the frame's shared ordering space. gfx defines no
// conventions and reserves no ranges: recorders that must interleave - canvas
// layers and scene cameras - agree on numbers between themselves, because they
// record from separate update subscriptions and stream order between them is
// not defined.
type Order int

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

// HasSameAttachmentsAs reports whether two passes render into the same places.
// Two DepthDescrAuto passes count as the same attachment because they share a
// colour target, and therefore a size, and therefore the backend's one depth
// texture for that size.
func (p PassDescr) HasSameAttachmentsAs(other PassDescr) bool {
	return p.Target.EqualsTo(other.Target) && p.Depth.EqualsTo(other.Depth)
}

// MergesInto reports whether the pass is by definition indistinguishable from
// continuing predecessor: same attachments, nothing to load, nothing lost.
// Merging then cannot change results, and it is what makes canvas's pass per
// layer cost one GPU pass.
func (p PassDescr) MergesInto(predecessor PassDescr) bool {
	return p.HasSameAttachmentsAs(predecessor) &&
		p.Load == LoadPreserve && p.DepthLoad == LoadPreserve &&
		predecessor.Store == StoreKeep && predecessor.DepthStore == StoreKeep
}

// IsObservable reports whether running the pass would change anything. Draws
// make it observable, and so does any attachment that loads: "clear this target
// and nothing else" and a camera that culled everything are both legitimate
// frames.
func (p PassDescr) IsObservable(draws int) bool {
	loads := func(op LoadOp) bool { return op == LoadClear || op == LoadDiscard }
	return draws > 0 || loads(p.Load) || loads(p.DepthLoad)
}
