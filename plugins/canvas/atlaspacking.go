package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// atlasPacking is a frame packed whose cell is still to fill: where it is
// packed, the source it is filled from, its rectangle of that source, and
// the axes its gutter wraps along.
type atlasPacking struct {
	packed    *packedImage
	source    render.Texture
	frameRect m.Recti
	wrap      Wrap
}

// makeInstance answers the instance data of p's quad over its cell, which
// canvas_pack_fragment reads as no entry's: its region spans the cell in
// the frame's texels from the frame's top left, its crop is the frame's
// rectangle of its source in texels, and its part is the axes its gutter
// wraps along, as Wrap's values are: bit 0 x, bit 1 y.
func (p *atlasPacking) makeInstance() instanceData {
	region := p.packed.region
	cell := p.packed.findCell()
	frameRect := p.frameRect.Float()
	return instanceData{
		region: m.Rect{X: -float32(region.X - cell.X), Y: -float32(region.Y - cell.Y), Width: float32(cell.Width), Height: float32(cell.Height)},
		crop:   m.Box2{Min: m.Vec2{X: frameRect.X, Y: frameRect.Y}, Max: m.Vec2{X: frameRect.X + frameRect.Width, Y: frameRect.Y + frameRect.Height}},
		part:   shapePart(p.wrap),
	}
}
