package internal

import (
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"

	"github.com/dvoyni/cog/libs/m"

	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/slots/storage"
)

const whiteAtlasKey = "\x00canvas.white"

type atlasCategory uint8

const (
	atlasSprite atlasCategory = iota
	atlasGlyph
	atlasGenerated
)

// AtlasEntry is one image placed in an atlas: the texture array it lives in,
// its uv rectangle and layer there, and its pixel size.
type AtlasEntry struct {
	Texture   gfx.TextureDescr
	UV        m.Vec4
	TexelSize float32
	Layer     int
	Width     int
	Height    int
	category  atlasCategory
	// arrayIndex and slot (the padded rectangle stored as X, Y, Z=width,
	// W=height) let an entry be reclaimed into the free list when its path is
	// unloaded.
	arrayIndex int
	slot       m.Vec4i
}

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

// StandaloneEntry is a full-image texture kept outside the atlas so it can be
// sampled with repeat addressing for tiled sprites.
type StandaloneEntry struct {
	Texture gfx.TextureDescr
	Width   int
	Height  int
}

// Atlas packs sprites or glyphs into texture arrays, and keeps the standalone
// textures tiled sprites sample. The Lookup resource holds two: one for
// sprites, one for glyphs.
type Atlas struct {
	config     Config
	arrays     []atlasArray
	entries    map[string]AtlasEntry
	standalone map[string]StandaloneEntry
	failed     map[string]struct{}
	free       map[m.Vec2i][]freeSlot
	bytes      int64
}

// NewAtlas builds an empty atlas sized by config.
func NewAtlas(config Config) *Atlas {
	return &Atlas{
		config:     config,
		entries:    map[string]AtlasEntry{},
		standalone: map[string]StandaloneEntry{},
		failed:     map[string]struct{}{},
		free:       map[m.Vec2i][]freeSlot{},
	}
}

// BeginFrame forgets which paths failed to load last frame, so a file that has
// since appeared is tried again.
func (a *Atlas) BeginFrame() { clear(a.failed) }

func (a *Atlas) arrayBytes() int64 {
	return int64(a.config.AtlasSize) * int64(a.config.AtlasSize) * 4 * int64(a.config.LayersPerArray)
}

func (a *Atlas) releasePath(path string, resources *gfx.ResourceQueue) {
	if path == "" {
		return
	}
	if entry, ok := a.entries[path]; ok {
		a.freeEntry(entry, resources)
		delete(a.entries, path)
	}
	delete(a.failed, path)
	if entry, ok := a.standalone[path]; ok {
		resources.ReleaseTexture(entry.Texture)
		delete(a.standalone, path)
	}
}

// freeEntry returns an entry's slot to the free list and releases its backing
// array once the array becomes empty.
func (a *Atlas) freeEntry(entry AtlasEntry, resources *gfx.ResourceQueue) {
	if entry.arrayIndex < 0 || entry.arrayIndex >= len(a.arrays) {
		return
	}
	array := &a.arrays[entry.arrayIndex]
	if array.released {
		return
	}
	size := m.Vec2i{X: entry.slot.Z, Y: entry.slot.W}
	a.free[size] = append(a.free[size], freeSlot{arrayIndex: entry.arrayIndex, layer: entry.Layer, pos: m.Vec2i{X: entry.slot.X, Y: entry.slot.Y}})
	array.live--
	if array.live <= 0 {
		a.releaseArray(entry.arrayIndex, resources)
	}
}

// releaseArray frees an empty array's GPU texture, tombstones its index for
// reuse, and purges any free slots that referenced it.
func (a *Atlas) releaseArray(index int, resources *gfx.ResourceQueue) {
	array := &a.arrays[index]
	if array.released {
		return
	}
	resources.ReleaseTexture(array.texture)
	array.released = true
	array.texture = gfx.TextureDescr{}
	array.layers = nil
	array.live = 0
	a.bytes -= a.arrayBytes()
	for size, list := range a.free {
		filtered := list[:0]
		for _, slot := range list {
			if slot.arrayIndex != index {
				filtered = append(filtered, slot)
			}
		}
		if len(filtered) == 0 {
			delete(a.free, size)
		} else {
			a.free[size] = filtered
		}
	}
}

func (a *Atlas) releaseAll(resources *gfx.ResourceQueue) {
	for i := range a.arrays {
		if !a.arrays[i].released {
			resources.ReleaseTexture(a.arrays[i].texture)
		}
	}
	for _, entry := range a.standalone {
		resources.ReleaseTexture(entry.Texture)
	}
	clear(a.entries)
	clear(a.standalone)
	clear(a.failed)
	clear(a.free)
	a.arrays = nil
	a.bytes = 0
}

// ResolveStandalone decodes an image into a full-image texture kept outside the
// atlas, caching it by path. Tiled sprites sample it with repeat addressing.
func (a *Atlas) ResolveStandalone(path string, filesystem storage.FileSystem, resources *gfx.ResourceQueue) (StandaloneEntry, bool) {
	if path == "" {
		return StandaloneEntry{}, false
	}
	if entry, ok := a.standalone[path]; ok {
		return entry, true
	}
	if _, failed := a.failed[path]; failed {
		return StandaloneEntry{}, false
	}
	width, height, pixels, ok := decodeResourceImage(filesystem, path)
	if !ok {
		a.failed[path] = struct{}{}
		return StandaloneEntry{}, false
	}
	entry := StandaloneEntry{
		// A decoded image is sRGB by definition, and there is no caller to say
		// otherwise: canvas draws pictures, never data maps.
		Texture: resources.BakeTexture(width, height, gpu.FormatRGBA8Srgb, pixels, true, false),
		Width:   width,
		Height:  height,
	}
	a.standalone[path] = entry
	return entry, true
}

// ResolveSprite returns the atlas entry for a sprite path, decoding and packing
// it on first use. The empty path is the white texel solid fills draw with.
func (a *Atlas) ResolveSprite(path string, filesystem storage.FileSystem, resources *gfx.ResourceQueue) (AtlasEntry, bool) {
	key := path
	if key == "" {
		key = whiteAtlasKey
	}
	if entry, ok := a.entries[key]; ok {
		return entry, true
	}
	if _, failed := a.failed[key]; failed {
		return AtlasEntry{}, false
	}
	if key == whiteAtlasKey {
		return a.insert(key, atlasGenerated, 1, 1, []byte{255, 255, 255, 255}, 0, false, resources)
	}
	width, height, pixels, ok := decodeResourceImage(filesystem, path)
	if !ok {
		a.failed[key] = struct{}{}
		return AtlasEntry{}, false
	}
	return a.insert(key, atlasSprite, width, height, pixels, 2, true, resources)
}

func (a *Atlas) insert(key string, category atlasCategory, width, height int, pixels []byte, padding int, extrude bool, resources *gfx.ResourceQueue) (AtlasEntry, bool) {
	// Reserve the white texel for solid fills; glyph-only atlases never need it.
	if key != whiteAtlasKey && category != atlasGlyph {
		if _, ok := a.entries[whiteAtlasKey]; !ok {
			if _, placed := a.insert(whiteAtlasKey, atlasGenerated, 1, 1, []byte{255, 255, 255, 255}, 0, false, resources); !placed {
				return AtlasEntry{}, false
			}
		}
	}
	slotWidth, slotHeight := width+padding*2, height+padding*2
	arrayIndex, layer, x, y, ok := a.place(slotWidth, slotHeight, resources)
	if !ok {
		a.failed[key] = struct{}{}
		return AtlasEntry{}, false
	}
	upload := paddedRGBA(pixels, width, height, padding, extrude)
	array := &a.arrays[arrayIndex]
	resources.UpdateTexture(array.texture, layer, gpu.Region{
		X: x, Y: y, Width: slotWidth, Height: slotHeight,
	}, upload, false)
	entry := AtlasEntry{
		Texture: array.texture,
		UV: m.Vec4{
			X: float32(x+padding) / float32(a.config.AtlasSize),
			Y: float32(y+padding) / float32(a.config.AtlasSize),
			Z: float32(x+padding+width) / float32(a.config.AtlasSize),
			W: float32(y+padding+height) / float32(a.config.AtlasSize),
		},
		TexelSize:  1 / float32(a.config.AtlasSize),
		Layer:      layer,
		Width:      width,
		Height:     height,
		category:   category,
		arrayIndex: arrayIndex,
		slot:       m.Vec4i{X: x, Y: y, Z: slotWidth, W: slotHeight},
	}
	if key == whiteAtlasKey {
		centerX := (float32(x) + 0.5) / float32(a.config.AtlasSize)
		centerY := (float32(y) + 0.5) / float32(a.config.AtlasSize)
		entry.UV = m.Vec4{X: centerX, Y: centerY, Z: centerX, W: centerY}
	}
	a.entries[key] = entry
	return entry, true
}

func (a *Atlas) place(width, height int, resources *gfx.ResourceQueue) (arrayIndex, layer, x, y int, ok bool) {
	// Reuse a previously freed slot of the exact same padded size first.
	sizeKey := m.Vec2i{X: width, Y: height}
	if list := a.free[sizeKey]; len(list) > 0 {
		slot := list[len(list)-1]
		a.free[sizeKey] = list[:len(list)-1]
		if len(a.free[sizeKey]) == 0 {
			delete(a.free, sizeKey)
		}
		a.arrays[slot.arrayIndex].live++
		return slot.arrayIndex, slot.layer, slot.pos.X, slot.pos.Y, true
	}
	for arrayIndex := range a.arrays {
		if a.arrays[arrayIndex].released {
			continue
		}
		for layer := range a.arrays[arrayIndex].layers {
			if x, y, ok := a.arrays[arrayIndex].layers[layer].place(a.config.AtlasSize, width, height); ok {
				a.arrays[arrayIndex].live++
				return arrayIndex, layer, x, y, true
			}
		}
	}
	if width > a.config.AtlasSize || height > a.config.AtlasSize {
		return 0, 0, 0, 0, false
	}
	// Reuse a tombstoned (previously released) array index when available so the
	// slice does not grow unbounded across load/unload cycles.
	index := -1
	for i := range a.arrays {
		if a.arrays[i].released {
			index = i
			break
		}
	}
	if index < 0 && a.bytes+a.arrayBytes() > int64(a.config.MaxAtlasBytes) {
		return 0, 0, 0, 0, false
	}
	// Sprites, glyphs and the generated white texel share one array and so
	// cannot take different colour spaces. sRGB is the one that is right for
	// the sprites, correct for the glyphs (they are RGB=255 with coverage in
	// alpha, and 1.0 is a fixed point of the transfer function), and correct
	// for the white texel for the same reason.
	texture := resources.AllocateTexture(a.config.AtlasSize, a.config.AtlasSize, a.config.LayersPerArray, gpu.FormatRGBA8Srgb)
	if index < 0 {
		a.arrays = append(a.arrays, atlasArray{texture: texture, layers: make([]atlasShelf, a.config.LayersPerArray)})
		index = len(a.arrays) - 1
	} else {
		a.arrays[index] = atlasArray{texture: texture, layers: make([]atlasShelf, a.config.LayersPerArray)}
	}
	a.bytes += a.arrayBytes()
	x, y, _ = a.arrays[index].layers[0].place(a.config.AtlasSize, width, height)
	a.arrays[index].live++
	return index, 0, x, y, true
}

func decodeResourceImage(filesystem storage.FileSystem, path string) (width, height int, pixels []byte, ok bool) {
	file, err := filesystem.Open(path)
	if err != nil {
		return 0, 0, nil, false
	}
	defer file.Close()
	decoded, _, err := image.Decode(file)
	if err != nil {
		return 0, 0, nil, false
	}
	bounds := decoded.Bounds()
	rgba := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(rgba, rgba.Bounds(), decoded, bounds.Min, draw.Src)
	return bounds.Dx(), bounds.Dy(), rgba.Pix, true
}

func paddedRGBA(source []byte, width, height, padding int, extrude bool) []byte {
	if padding == 0 {
		return append([]byte(nil), source...)
	}
	dstWidth, dstHeight := width+padding*2, height+padding*2
	destination := make([]byte, dstWidth*dstHeight*4)
	for y := 0; y < dstHeight; y++ {
		for x := 0; x < dstWidth; x++ {
			sourceX, sourceY := x-padding, y-padding
			inside := sourceX >= 0 && sourceX < width && sourceY >= 0 && sourceY < height
			if !inside && !extrude {
				continue
			}
			sourceX = min(max(sourceX, 0), width-1)
			sourceY = min(max(sourceY, 0), height-1)
			sourceOffset := (sourceY*width + sourceX) * 4
			destinationOffset := (y*dstWidth + x) * 4
			copy(destination[destinationOffset:destinationOffset+4], source[sourceOffset:sourceOffset+4])
		}
	}
	return destination
}
