package atlas

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// Atlas is images of one format packed into pages: one 2D-array texture, a
// layer for each page, so entries over any of them bind one texture. Each
// image is packed as a cell of its own, the image rounded up to the atlas
// grid and its gutter around it, taken from its page by the page's shelf
// allocator.
//
// It grows itself as it packs, doubling its pages up to its ceiling into a
// new texture, and records the copy of the pages it had at the next
// RecordCopies. What lies in each region is its user's to keep: the atlas
// answers where an image is packed, and forgets it only when freed. It is
// not safe for concurrent use: one task at a time packs, puts and records
// it.
type Atlas struct {
	// descriptor is the one it was made of, its zeros given their meaning
	// and its gutter raised as Descriptor.Gutter says, and texelSize how
	// many bytes a texel of its format takes.
	descriptor Descriptor
	texelSize  uint32
	// texture holds the pages, the zero Texture when render could not make
	// it, which render logged; a growth replaces it.
	texture render.Texture
	// pages allocate each page's cells, nil once the atlas is released.
	pages []shelfAllocator
	// growth is the copy of the pages it had before growing, still to
	// record.
	growth growth
	// stagedCells are the cells Put filled still to copy, from stagedBytes,
	// their rows laid out as a copy from a buffer needs them.
	stagedCells []stagedCell
	stagedBytes []byte
	// mipPages are the pages packed into since RecordMips last recorded
	// their mips, in the order first packed into, kept only when its pages
	// have mips.
	mipPages []uint32
}

// New answers an atlas of descriptor, of one page, its texture made through
// inventory, transparent black. A format it holds no texels of, a mip level
// count past 32, and a page side of zero or no multiple of the atlas grid
// panic.
func New(inventory render.Inventory, descriptor Descriptor) *Atlas {
	descriptor.MipLevelCount = cmp.Or(descriptor.MipLevelCount, 1)
	descriptor.LayerCeiling = cmp.Or(descriptor.LayerCeiling, 1)
	if descriptor.MipLevelCount > 32 {
		panic(fmt.Sprintf("atlas: %q's pages of %d mip levels are more than a texture of 32-bit sides has", descriptor.Label, descriptor.MipLevelCount))
	}
	atlasGrid := uint32(1) << (descriptor.MipLevelCount - 1)
	if descriptor.PageSize == 0 || descriptor.PageSize%atlasGrid != 0 {
		panic(fmt.Sprintf("atlas: %q's page of %d texels is no multiple of its atlas grid of %d", descriptor.Label, descriptor.PageSize, atlasGrid))
	}
	if descriptor.MipLevelCount > 1 {
		descriptor.Gutter = max(roundUpToMultiple(descriptor.Gutter, atlasGrid), atlasGrid)
	}
	atlas := &Atlas{descriptor: descriptor, texelSize: findTexelSize(descriptor)}
	atlas.texture = atlas.makeTexture(inventory, 1)
	atlas.pages = []shelfAllocator{makeShelfAllocator(int32(descriptor.PageSize), int32(descriptor.PageSize))}
	return atlas
}

// Texture answers the texture holding the atlas's pages as it is now, the
// zero Texture when render could not make it. Pack replaces it as the atlas
// grows, so it is read as a draw binds it, not kept.
func (a *Atlas) Texture() render.Texture {
	return a.texture
}

// PageCount answers how many pages the atlas has.
func (a *Atlas) PageCount() uint32 {
	return uint32(len(a.pages))
}

// Pack answers where an image of width by height texels lies once packed as
// a cell of its own, on the first page with room for it: its page as the
// region's layer, and its rectangle there in texels, the gutter in from its
// cell's top left, on the atlas grid. With no room on any page, it doubles
// its pages, up to its ceiling, into a new texture made through inventory,
// and packs the image on the first new one. A growth whose copy is still to
// record merges into it, so the pages it had are copied once, into the
// last texture. It answers ErrTooLarge for an image whose cell no page
// holds, and ErrFull for one no page has room for at the ceiling. An image
// of no texels, or an atlas released, panics.
func (a *Atlas) Pack(inventory render.Inventory, width, height uint32) (render.TextureRegion, error) {
	switch {
	case a.pages == nil:
		panic(fmt.Sprintf("atlas: packing into %q, which was released", a.descriptor.Label))
	case width == 0 || height == 0:
		panic(fmt.Sprintf("atlas: packing an image of %d by %d texels into %q, which holds none", width, height, a.descriptor.Label))
	case width > a.descriptor.PageSize || height > a.descriptor.PageSize:
		// The image's own sides are checked before its cell's, whose size,
		// rounded up and its gutter added, would wrap past 2^32 near it.
		return render.TextureRegion{}, ErrTooLarge
	}
	cellWidth, cellHeight := a.findCellSize(width, height)
	if cellWidth > a.descriptor.PageSize || cellHeight > a.descriptor.PageSize {
		return render.TextureRegion{}, ErrTooLarge
	}
	if region, packed := a.packOnPages(0, width, height, cellWidth, cellHeight); packed {
		return region, nil
	}
	if a.PageCount() >= a.descriptor.LayerCeiling {
		return render.TextureRegion{}, ErrFull
	}
	firstNewPage := len(a.pages)
	a.grow(inventory)
	region, _ := a.packOnPages(firstNewPage, width, height, cellWidth, cellHeight)
	return region, nil
}

// Free gives back region, which Pack answered, so another image can be
// packed in its cell, and forgets a Put of it still to record.
func (a *Atlas) Free(region render.TextureRegion) {
	cell := a.Cell(region)
	a.pages[region.Layer].free(int32(cell.X), int32(cell.Y))
	a.stagedCells = slices.DeleteFunc(a.stagedCells, func(pending stagedCell) bool { return pending.cell == cell })
}

// Put fills the cell of region, which Pack answered, with texels: the image's
// rows, top to bottom, in the atlas's format, each texel's bytes after the
// one before with no padding, copied at once, so the caller may reuse them.
// It writes the whole cell, the rounding to the atlas grid and the gutter
// included, so a cell packed again keeps nothing of the image before: from
// the opposite edge along an axis wrap names and the edge continued along
// any other, or transparent black under Descriptor.ClearGutter. The next
// RecordCopies copies it into the atlas's texture. Texels of another length
// than region's, or an atlas released, panic.
func (a *Atlas) Put(region render.TextureRegion, texels []byte, wrap Wrap) {
	if a.pages == nil {
		panic(fmt.Sprintf("atlas: putting texels into %q, which was released", a.descriptor.Label))
	}
	rowSize := region.Width * a.texelSize
	if uint64(len(texels)) != uint64(rowSize)*uint64(region.Height) {
		panic(fmt.Sprintf("atlas: putting %d bytes into a region of %d by %d texels of %q, of %d bytes a texel", len(texels), region.Width, region.Height, a.descriptor.Label, a.texelSize))
	}
	cell := a.Cell(region)
	cellRowSize := cell.Width * a.texelSize
	rowPitch := roundUpToMultiple(cellRowSize, copyRowAlignment)
	offset := roundUpToMultiple(uint32(len(a.stagedBytes)), copyRowAlignment)
	end := offset + (cell.Height-1)*rowPitch + cellRowSize
	a.stagedBytes = slices.Grow(a.stagedBytes, int(end)-len(a.stagedBytes))[:end]
	cellRow := func(cellY uint32) []byte { return a.stagedBytes[offset+cellY*rowPitch:][:cellRowSize] }
	gutter := a.descriptor.Gutter
	wrapsX := wrap == WrapX || wrap == WrapBoth
	for imageY := range region.Height {
		a.fillRow(cellRow(gutter+imageY), texels[imageY*rowSize:][:rowSize], region.Width, wrapsX)
	}
	// Each row above and below the image's, the rounding's among them, is a
	// copy of the image's row it continues or wraps to.
	wrapsY := wrap == WrapY || wrap == WrapBoth
	for cellY := range cell.Height {
		switch {
		case cellY >= gutter && cellY < gutter+region.Height:
		case a.descriptor.ClearGutter:
			clear(cellRow(cellY))
		default:
			copy(cellRow(cellY), cellRow(gutter+uint32(findImageTexel(int(cellY)-int(gutter), int(region.Height), wrapsY))))
		}
	}
	a.stagedCells = append(a.stagedCells, stagedCell{cell: cell, offset: uint64(offset), size: uint64(end - offset)})
}

// Cell answers the cell of region, which Pack answered: on its page, from the
// gutter above and left of it, as wide and tall as the image rounded up to
// the atlas grid and its gutter on both sides. A caller filling it on the
// GPU draws into it, between RecordCopies and RecordMips.
func (a *Atlas) Cell(region render.TextureRegion) render.TextureRegion {
	width, height := a.findCellSize(region.Width, region.Height)
	return render.TextureRegion{Layer: region.Layer, X: region.X - a.descriptor.Gutter, Y: region.Y - a.descriptor.Gutter, Width: width, Height: height}
}

// TextureRect answers region's rectangle of its page in texture
// coordinates, from 0 to 1 across the page, its page itself region.Layer.
func (a *Atlas) TextureRect(region render.TextureRegion) m.Rect {
	pageSize := float32(a.descriptor.PageSize)
	return m.Rect{X: float32(region.X) / pageSize, Y: float32(region.Y) / pageSize, Width: float32(region.Width) / pageSize, Height: float32(region.Height) / pageSize}
}

// RecordCopies records into list, before anything samples the atlas in it,
// the copy of every mip level of the pages it had into the texture a growth
// replaced it with, releasing the one replaced through inventory, then the
// copy of each cell Put filled, from one temporary buffer. It records and
// allocates nothing when nothing is pending. An atlas whose texture render
// could not make copies nothing.
func (a *Atlas) RecordCopies(list *render.CommandList, inventory render.Inventory) {
	// With no growth to copy, its source is the zero Texture.
	if a.growth.source != (render.Texture{}) && a.texture != (render.Texture{}) {
		for layer := range a.growth.layerCount {
			for mipLevel := range a.descriptor.MipLevelCount {
				region := render.TextureRegion{MipLevel: mipLevel, Layer: layer}
				list.CopyTexture(a.growth.source, region, a.texture, region)
			}
		}
	}
	a.releaseGrowth(inventory)
	if len(a.stagedCells) > 0 && a.texture != (render.Texture{}) {
		if staged := list.AcquireTemporaryBuffer(a.stagedBytes); staged.Buffer != (render.Buffer{}) {
			for _, pending := range a.stagedCells {
				list.CopyBufferToTexture(render.BufferRange{Buffer: staged.Buffer, Offset: staged.Offset + pending.offset, Size: pending.size}, a.texture, pending.cell)
			}
		}
	}
	a.stagedCells = a.stagedCells[:0]
	a.stagedBytes = a.stagedBytes[:0]
}

// RecordMips records into list the mips of each page packed into since it
// last did, once a page, from its first level: after RecordCopies and after
// any pass filling a cell on the GPU, before anything samples the atlas. It
// records and allocates nothing when nothing is pending, nor for pages of
// one mip level.
func (a *Atlas) RecordMips(list *render.CommandList) {
	if a.texture != (render.Texture{}) {
		for _, page := range a.mipPages {
			list.GenerateMips(a.texture, page, 1)
		}
	}
	a.mipPages = a.mipPages[:0]
}

// Empty frees every region of the atlas, as a lost device leaves its
// texture empty, keeping as many pages, and forgets what is still to
// record: a growth's copy, releasing the texture it replaced through
// inventory, each Put and each page's mips.
func (a *Atlas) Empty(inventory render.Inventory) {
	for page := range a.pages {
		a.pages[page] = makeShelfAllocator(int32(a.descriptor.PageSize), int32(a.descriptor.PageSize))
	}
	a.releaseGrowth(inventory)
	a.stagedCells = a.stagedCells[:0]
	a.stagedBytes = a.stagedBytes[:0]
	a.mipPages = a.mipPages[:0]
}

// Release releases the atlas's texture through inventory, and the one a
// growth still to copy replaced. Packing into it, or putting texels, after
// panics.
func (a *Atlas) Release(inventory render.Inventory) {
	a.releaseGrowth(inventory)
	if a.texture != (render.Texture{}) {
		inventory.ReleaseTexture(a.texture)
	}
	a.texture = render.Texture{}
	a.pages = nil
	a.stagedCells = nil
	a.stagedBytes = nil
	a.mipPages = nil
}

// findCellSize answers the size of the cell an image of width by height
// texels is packed as: the image rounded up to the atlas grid, then the
// gutter on every side.
func (a *Atlas) findCellSize(width, height uint32) (uint32, uint32) {
	atlasGrid := uint32(1) << (a.descriptor.MipLevelCount - 1)
	return roundUpToMultiple(width, atlasGrid) + 2*a.descriptor.Gutter, roundUpToMultiple(height, atlasGrid) + 2*a.descriptor.Gutter
}

// packOnPages answers where an image of width by height texels lies once
// packed as a cell of cellWidth by cellHeight on the first page from
// firstPage with room for it, or false when none has, marking its page for
// RecordMips.
func (a *Atlas) packOnPages(firstPage int, width, height, cellWidth, cellHeight uint32) (render.TextureRegion, bool) {
	for page := firstPage; page < len(a.pages); page++ {
		x, y, ok := a.pages[page].allocate(int32(cellWidth), int32(cellHeight))
		if !ok {
			continue
		}
		if a.descriptor.MipLevelCount > 1 && !slices.Contains(a.mipPages, uint32(page)) {
			a.mipPages = append(a.mipPages, uint32(page))
		}
		return render.TextureRegion{Layer: uint32(page), X: uint32(x) + a.descriptor.Gutter, Y: uint32(y) + a.descriptor.Gutter, Width: width, Height: height}, true
	}
	return render.TextureRegion{}, false
}

// grow doubles the atlas's pages, to no more than its ceiling, the new ones
// empty, into a new texture made through inventory. The copy of the pages
// it had is recorded at the next RecordCopies; a growth still to copy keeps
// the texture it grew from, and the texture grown into since, which nothing
// was copied or drawn into, is released.
func (a *Atlas) grow(inventory render.Inventory) {
	layerCount := min(2*a.PageCount(), a.descriptor.LayerCeiling)
	switch {
	case a.growth.layerCount == 0:
		a.growth = growth{source: a.texture, layerCount: a.PageCount()}
	case a.texture != (render.Texture{}):
		inventory.ReleaseTexture(a.texture)
	}
	a.texture = a.makeTexture(inventory, layerCount)
	for a.PageCount() < layerCount {
		a.pages = append(a.pages, makeShelfAllocator(int32(a.descriptor.PageSize), int32(a.descriptor.PageSize)))
	}
}

// releaseGrowth releases through inventory the texture a growth still to
// copy replaced, and forgets the growth.
func (a *Atlas) releaseGrowth(inventory render.Inventory) {
	if a.growth.source != (render.Texture{}) {
		inventory.ReleaseTexture(a.growth.source)
	}
	a.growth = growth{}
}

// fillRow fills row, a row of a cell, from imageRow, a row of its image of
// width texels: the image's texels a gutter in from its left, and the
// gutter on either side, the rounding to the atlas grid included, wrapped
// from the opposite edge when wraps is set and the edge continued
// otherwise, or transparent black under Descriptor.ClearGutter.
func (a *Atlas) fillRow(row, imageRow []byte, width uint32, wraps bool) {
	gutter := a.descriptor.Gutter
	imageStart := gutter * a.texelSize
	imageEnd := imageStart + uint32(len(imageRow))
	copy(row[imageStart:imageEnd], imageRow)
	if a.descriptor.ClearGutter {
		clear(row[:imageStart])
		clear(row[imageEnd:])
		return
	}
	cellWidth := uint32(len(row)) / a.texelSize
	for _, columns := range [2][2]uint32{{0, gutter}, {gutter + width, cellWidth}} {
		for cellX := columns[0]; cellX < columns[1]; cellX++ {
			imageX := uint32(findImageTexel(int(cellX)-int(gutter), int(width), wraps))
			copy(row[cellX*a.texelSize:][:a.texelSize], imageRow[imageX*a.texelSize:][:a.texelSize])
		}
	}
}

// makeTexture makes, through inventory, the atlas's texture of layerCount
// pages, of its mip levels, transparent black.
func (a *Atlas) makeTexture(inventory render.Inventory, layerCount uint32) render.Texture {
	return inventory.MakeTexture[byte](render.TextureDescriptor{
		Label:         a.descriptor.Label,
		Width:         a.descriptor.PageSize,
		Height:        a.descriptor.PageSize,
		LayerCount:    layerCount,
		MipLevelCount: a.descriptor.MipLevelCount,
		Format:        a.descriptor.Format,
	}, nil)
}

// growth is an atlas grown whose pages are still to be copied into its new
// texture: the texture it had, and how many pages that held, none when
// there is no growth to copy.
type growth struct {
	source     render.Texture
	layerCount uint32
}

// stagedCell is a cell Put filled, still to copy into the atlas's texture:
// the cell, and where its rows lie in the atlas's staged bytes, each a
// multiple of copyRowAlignment after the one before.
type stagedCell struct {
	cell   render.TextureRegion
	offset uint64
	size   uint64
}

// copyRowAlignment is how many bytes apart a copy from a buffer into a
// texture needs its rows to start, at most: a multiple of it.
const copyRowAlignment = 256

// findTexelSize answers how many bytes a texel of descriptor's format takes,
// and panics for a format an atlas holds no texels of.
func findTexelSize(descriptor Descriptor) uint32 {
	switch descriptor.Format {
	case render.TextureFormatR8Unorm:
		return 1
	case render.TextureFormatRG8Unorm:
		return 2
	case render.TextureFormatRGBA8Unorm, render.TextureFormatRGBA8UnormSrgb, render.TextureFormatBGRA8Unorm, render.TextureFormatBGRA8UnormSrgb:
		return 4
	}
	panic(fmt.Sprintf("atlas: %q's format %d is none an atlas holds: R8Unorm, RG8Unorm, RGBA8Unorm, RGBA8UnormSrgb, BGRA8Unorm or BGRA8UnormSrgb", descriptor.Label, descriptor.Format))
}

// findImageTexel answers which of an image's size texels along one axis
// fills position along it, counted from the image's first texel: past an
// edge, the texel from the opposite edge when wraps is set, and the edge's
// own otherwise.
func findImageTexel(position, size int, wraps bool) int {
	switch {
	case position >= 0 && position < size:
		return position
	case wraps:
		return (position%size + size) % size
	}
	return min(max(position, 0), size-1)
}

// roundUpToMultiple answers the smallest multiple of step no smaller than
// value.
func roundUpToMultiple(value, step uint32) uint32 {
	return (value + step - 1) / step * step
}
