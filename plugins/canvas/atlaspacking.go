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
// rectangle of its source in texels, its origin then its size, and its
// part is the axes its gutter wraps along, as Wrap's values are: bit 0 x,
// bit 1 y.
func (p *atlasPacking) makeInstance() instanceData {
	region := p.packed.region
	cell := p.packed.atlas.findCell(region)
	frameRect := p.frameRect.Float()
	return instanceData{
		region: [4]float32{-float32(region.X - cell.X), -float32(region.Y - cell.Y), float32(cell.Width), float32(cell.Height)},
		crop:   [4]float32{frameRect.X, frameRect.Y, frameRect.Width, frameRect.Height},
		part:   shapePart(p.wrap),
	}
}
