package internal

import (
	"github.com/dvoyni/cog/libs/m"

	"github.com/dvoyni/cog/slots/gfx"
)

// AtlasEntry is one image placed in an atlas: the texture array it lives in,
// its uv rectangle and layer there, and its pixel size.
//
// The zero value is an image that did not load, which is what a sprite cache
// holds for a missing file and for either packer refusal alike. A draw handed
// one draws nothing.
type AtlasEntry struct {
	Texture   gfx.TextureDescr
	UV        m.Vec4
	TexelSize float32
	Layer     int
	Width     int
	Height    int
	// arrayIndex and slot (the padded rectangle stored as X, Y, Z=width,
	// W=height) let an entry be reclaimed into the free list when it is freed.
	arrayIndex int
	slot       m.Vec4i
}

// insertion is one image handed to the packer: its pixels and their size, the
// border to surround them with, what fills that border, and whether the entry's
// uv rectangle collapses to the centre of a single texel.
type insertion struct {
	pixels   []byte
	width    int
	height   int
	padding  int
	fill     gutterFill
	centreUV bool
}

// gutterFill is what goes in the border around a packed image, and it is a
// property of how the image will be *drawn* rather than of the image. Bilinear
// filtering at the content boundary samples the gutter, so the gutter decides
// what a sprite's edge blends into, and the two answers are mutually exclusive:
// an edge that repeats wants the edge it wraps around to, an edge that ends
// wants its own texels continued.
//
// **It is per axis.** An axis that tiles wraps and an axis that does not
// extrudes, which is the same split the repeat sampler made before the wrap
// moved into the shader - tileSampler repeated only the tiled axes and clamped
// the rest. Wrapping both axes of a strip that tiles on one puts the far edge's
// texels along the near edge, which on a border side is a thin dark line down
// the length of it.
//
// That is why the sprite tier keys its cache by this as well as by path - see
// spriteDescrParams. Filling every gutter one way would make one of the kinds
// wrong everywhere.
type gutterFill uint8

const (
	// fillTransparent leaves the border zeroed. The generated texel takes it by
	// taking no padding at all; nothing else uses it.
	fillTransparent gutterFill = iota
	// fillExtrude repeats the nearest edge texel outwards on both axes, so
	// filtering at a page boundary samples the sprite rather than its neighbour.
	fillExtrude
	// fillWrapX copies the opposite edge inwards along x and extrudes along y:
	// the fill for a strip that tiles horizontally.
	fillWrapX
	// fillWrapY is its transpose.
	fillWrapY
	// fillWrapBoth wraps both axes, for a sprite that tiles on both.
	fillWrapBoth
)

// wrapsX and wrapsY say which axes this fill wraps; the others extrude.
func (f gutterFill) wrapsX() bool { return f == fillWrapX || f == fillWrapBoth }
func (f gutterFill) wrapsY() bool { return f == fillWrapY || f == fillWrapBoth }

// packRefusal says why the packer would not place an image, or that it did.
//
// Both refusals are development-stage errors: a sprite bigger than a page and
// a resident set past the byte budget both surface the first time a scene is
// assembled, not in front of a player. Both are terminal - the loader caches
// the zero entry either refusal produces, which is the same value a missing
// file produces - and both are reported through the loading kernel.
type packRefusal uint8

const (
	packPlaced packRefusal = iota
	// packTooLarge is an image whose padded rectangle is larger than one atlas
	// page. A sprite is padded by 2 on each side, so the largest that can ever
	// pack is AtlasSize-4: 4092 at canvas's defaults.
	packTooLarge
	// packOverBudget is every layer of every array full, no tombstoned array
	// index free to reuse, and another array over MaxAtlasBytes. It is
	// contingent on what else is resident, and it stays terminal anyway: a game
	// that fills its configured budget and then streams across it is over that
	// budget, and unloading at a level boundary is what answers it.
	packOverBudget
)

type atlasShelf struct {
	penX, penY int
	rowHeight  int
}

func (s *atlasShelf) place(pageSize, width, height int) (x, y int, ok bool) {
	if width > pageSize || height > pageSize {
		return 0, 0, false
	}
	if s.penX+width > pageSize {
		s.penX = 0
		s.penY += s.rowHeight
		s.rowHeight = 0
	}
	if s.penY+height > pageSize {
		return 0, 0, false
	}
	x, y = s.penX, s.penY
	s.penX += width
	s.rowHeight = max(s.rowHeight, height)
	return x, y, true
}

type atlasArray struct {
	texture  gfx.TextureDescr
	layers   []atlasShelf
	live     int  // occupied slots; when it reaches zero the array is released
	released bool // texture freed, index reusable
}

// freeSlot is a reclaimed atlas rectangle (padding included) available for reuse
// by a future insert of the same slot dimensions.
type freeSlot struct {
	arrayIndex int
	layer      int
	pos        m.Vec2i
}

// packer packs images into texture arrays: the shelf allocator, the free list,
// the tombstoned array indices and the running byte count MaxAtlasBytes is
// weighed against, and nothing else. It holds no table of what it packed,
// because an asset cache holds that; the glyph side has no table at all and
// reaches the packer directly.
//
// It is persistent state that must outlive a handler, so it lives on the Lookup
// and travels to the sprite loader in the loader's user data. The Lookup holds
// two, one for sprites and one for glyphs, and each carries its own config and
// so its own byte budget.
type packer struct {
	config Config
	arrays []atlasArray
	free   map[m.Vec2i][]freeSlot
	bytes  int64
}

// newPacker builds an empty packer sized by config.
func newPacker(config Config) *packer {
	return &packer{config: config, free: map[m.Vec2i][]freeSlot{}}
}

func (p *packer) arrayBytes() int64 {
	return int64(p.config.AtlasSize) * int64(p.config.AtlasSize) * 4 * int64(p.config.LayersPerArray)
}

// insert packs one image and uploads it, or says why it would not.
func (p *packer) insert(source insertion, resources *gfx.ResourceQueue) (AtlasEntry, packRefusal) {
	slotWidth, slotHeight := source.width+source.padding*2, source.height+source.padding*2
	arrayIndex, layer, x, y, refusal := p.place(slotWidth, slotHeight, resources)
	if refusal != packPlaced {
		return AtlasEntry{}, refusal
	}
	upload := paddedRGBA(source.pixels, source.width, source.height, source.padding, source.fill)
	array := &p.arrays[arrayIndex]
	resources.UpdateTexture(array.texture, layer, gfx.Region{
		X: x, Y: y, Width: slotWidth, Height: slotHeight,
	}, upload, false)
	entry := AtlasEntry{
		Texture: array.texture,
		UV: m.Vec4{
			X: float32(x+source.padding) / float32(p.config.AtlasSize),
			Y: float32(y+source.padding) / float32(p.config.AtlasSize),
			Z: float32(x+source.padding+source.width) / float32(p.config.AtlasSize),
			W: float32(y+source.padding+source.height) / float32(p.config.AtlasSize),
		},
		TexelSize:  1 / float32(p.config.AtlasSize),
		Layer:      layer,
		Width:      source.width,
		Height:     source.height,
		arrayIndex: arrayIndex,
		slot:       m.Vec4i{X: x, Y: y, Z: slotWidth, W: slotHeight},
	}
	if source.centreUV {
		centerX := (float32(x) + 0.5) / float32(p.config.AtlasSize)
		centerY := (float32(y) + 0.5) / float32(p.config.AtlasSize)
		entry.UV = m.Vec4{X: centerX, Y: centerY, Z: centerX, W: centerY}
	}
	return entry, packPlaced
}

func (p *packer) place(width, height int, resources *gfx.ResourceQueue) (arrayIndex, layer, x, y int, refusal packRefusal) {
	// A rectangle larger than a page is refused before anything is walked: no
	// shelf of any layer of any array can take it, and no freed slot can ever
	// have been that size.
	if width > p.config.AtlasSize || height > p.config.AtlasSize {
		return 0, 0, 0, 0, packTooLarge
	}
	// Reuse a previously freed slot of the exact same padded size first.
	sizeKey := m.Vec2i{X: width, Y: height}
	if list := p.free[sizeKey]; len(list) > 0 {
		slot := list[len(list)-1]
		p.free[sizeKey] = list[:len(list)-1]
		if len(p.free[sizeKey]) == 0 {
			delete(p.free, sizeKey)
		}
		p.arrays[slot.arrayIndex].live++
		return slot.arrayIndex, slot.layer, slot.pos.X, slot.pos.Y, packPlaced
	}
	for arrayIndex := range p.arrays {
		if p.arrays[arrayIndex].released {
			continue
		}
		for layer := range p.arrays[arrayIndex].layers {
			if x, y, ok := p.arrays[arrayIndex].layers[layer].place(p.config.AtlasSize, width, height); ok {
				p.arrays[arrayIndex].live++
				return arrayIndex, layer, x, y, packPlaced
			}
		}
	}
	// Reuse a tombstoned (previously released) array index when available so the
	// slice does not grow unbounded across load/unload cycles.
	index := -1
	for i := range p.arrays {
		if p.arrays[i].released {
			index = i
			break
		}
	}
	if index < 0 && p.bytes+p.arrayBytes() > int64(p.config.MaxAtlasBytes) {
		return 0, 0, 0, 0, packOverBudget
	}
	// Sprites, glyphs and the generated white texel share one array and so
	// cannot take different colour spaces. sRGB is the one that is right for
	// the sprites, correct for the glyphs (they are RGB=255 with coverage in
	// alpha, and 1.0 is a fixed point of the transfer function), and correct
	// for the white texel for the same reason.
	texture := resources.AllocateTexture(p.config.AtlasSize, p.config.AtlasSize, p.config.LayersPerArray, gfx.FormatRGBA8Srgb)
	if index < 0 {
		p.arrays = append(p.arrays, atlasArray{texture: texture, layers: make([]atlasShelf, p.config.LayersPerArray)})
		index = len(p.arrays) - 1
	} else {
		p.arrays[index] = atlasArray{texture: texture, layers: make([]atlasShelf, p.config.LayersPerArray)}
	}
	p.bytes += p.arrayBytes()
	x, y, _ = p.arrays[index].layers[0].place(p.config.AtlasSize, width, height)
	p.arrays[index].live++
	return index, 0, x, y, packPlaced
}

// freeEntry returns an entry's slot to the free list and releases its backing
// array once the array becomes empty.
//
// A zero entry is one that never packed - a missing file, or either refusal -
// and reclaims nothing. It is told apart by its slot rather than by its array
// index, because index zero is a live array.
func (p *packer) freeEntry(entry AtlasEntry, resources *gfx.ResourceQueue) {
	if entry.slot.Z <= 0 || entry.slot.W <= 0 {
		return
	}
	if entry.arrayIndex < 0 || entry.arrayIndex >= len(p.arrays) {
		return
	}
	array := &p.arrays[entry.arrayIndex]
	if array.released {
		return
	}
	size := m.Vec2i{X: entry.slot.Z, Y: entry.slot.W}
	p.free[size] = append(p.free[size], freeSlot{arrayIndex: entry.arrayIndex, layer: entry.Layer, pos: m.Vec2i{X: entry.slot.X, Y: entry.slot.Y}})
	array.live--
	if array.live <= 0 {
		p.releaseArray(entry.arrayIndex, resources)
	}
}

// releaseArray frees an empty array's GPU texture, tombstones its index for
// reuse, and purges any free slots that referenced it.
func (p *packer) releaseArray(index int, resources *gfx.ResourceQueue) {
	array := &p.arrays[index]
	if array.released {
		return
	}
	resources.ReleaseTexture(array.texture)
	array.released = true
	array.texture = gfx.TextureDescr{}
	array.layers = nil
	array.live = 0
	p.bytes -= p.arrayBytes()
	for size, list := range p.free {
		filtered := list[:0]
		for _, slot := range list {
			if slot.arrayIndex != index {
				filtered = append(filtered, slot)
			}
		}
		if len(filtered) == 0 {
			delete(p.free, size)
		} else {
			p.free[size] = filtered
		}
	}
}

// releaseAll frees every array the packer allocated and empties it. Whatever
// holds entries pointing into those arrays has to be emptied with it; the glyph
// side, which has no table, empties the baked faces that memoise its glyphs.
func (p *packer) releaseAll(resources *gfx.ResourceQueue) {
	for i := range p.arrays {
		if !p.arrays[i].released {
			resources.ReleaseTexture(p.arrays[i].texture)
		}
	}
	clear(p.free)
	p.arrays = nil
	p.bytes = 0
}

func paddedRGBA(source []byte, width, height, padding int, fill gutterFill) []byte {
	if padding == 0 {
		return append([]byte(nil), source...)
	}
	dstWidth, dstHeight := width+padding*2, height+padding*2
	destination := make([]byte, dstWidth*dstHeight*4)
	for y := 0; y < dstHeight; y++ {
		for x := 0; x < dstWidth; x++ {
			sourceX, sourceY := x-padding, y-padding
			inside := sourceX >= 0 && sourceX < width && sourceY >= 0 && sourceY < height
			if !inside && fill == fillTransparent {
				continue
			}
			// Euclidean remainder on a wrapped axis: a gutter texel one to the
			// left of the content is the content's rightmost column, which is
			// the texel the tile before this one ended on. A clamped axis
			// continues its own edge instead.
			if fill.wrapsX() {
				sourceX = ((sourceX % width) + width) % width
			} else {
				sourceX = min(max(sourceX, 0), width-1)
			}
			if fill.wrapsY() {
				sourceY = ((sourceY % height) + height) % height
			} else {
				sourceY = min(max(sourceY, 0), height-1)
			}
			sourceOffset := (sourceY*width + sourceX) * 4
			destinationOffset := (y*dstWidth + x) * 4
			copy(destination[destinationOffset:destinationOffset+4], source[sourceOffset:sourceOffset+4])
		}
	}
	return destination
}
