package render

import (
	"cmp"
	"errors"
	"fmt"
	"sync"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/render/internal/handle"
)

// assetTable is the private resource of every asset render holds, behind the
// handles its users keep. Every job reads it; its mutex guards the slots, and
// the loaders and made assets guard themselves.
type assetTable struct {
	reporter
	mutex sync.Mutex
	// textures is every texture render holds, and buffers every buffer, by
	// its handle.
	textures handle.SlotMap[Texture, textureSlot]
	buffers  handle.SlotMap[Buffer, bufferSlot]
	// loadedTextures and loadedBuffers share, count and free the textures
	// and buffers loaded from files.
	loadedTextures textureLoader
	loadedBuffers  bufferLoader
	// madeTextures and madeBuffers hold the made textures and buffers, each
	// by its own handle, once, until its maker releases it.
	madeTextures assetCounter[Texture, Texture]
	madeBuffers  assetCounter[Buffer, Buffer]
}

// addTexture makes a gfx texture of descriptor, whose counts are filled in,
// with every usage its format allows and a label naming its handle, and
// answers its handle.
func (t *assetTable) addTexture(device gfx.Device, descriptor TextureDescriptor) (Texture, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	texture, slot := t.textures.Acquire()
	gfxTexture, err := device.CreateTexture(gfx.TextureDescriptor{
		Label:         handle.MakeLabel(texture, descriptor.Label),
		Size:          gfx.Extent3D{Width: descriptor.Width, Height: descriptor.Height, DepthOrArrayLayers: descriptor.LayerCount},
		MipLevelCount: descriptor.MipLevelCount,
		SampleCount:   descriptor.SampleCount,
		Dimension:     gfx.TextureDimension2D,
		Format:        gfx.TextureFormat(descriptor.Format),
		Usage:         descriptor.Format.makeTextureUsage(descriptor.SampleCount),
	})
	if err != nil {
		t.textures.Release(texture)
		return Texture{}, err
	}
	slot.texture = gfxTexture
	return texture, nil
}

// addBuffer makes a gfx buffer of size bytes rounded up to a multiple of 4,
// the alignment of gfx's writes, with usage and the copy destination render's
// writes need, and a label naming its handle, and answers its handle and the
// gfx buffer; a size of zero makes none, so nothing writes or binds it.
func (t *assetTable) addBuffer(device gfx.Device, label string, size uint64, usage BufferUsageFlags) (Buffer, gfx.Buffer, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	buffer, slot := t.buffers.Acquire()
	if size = (size + 3) &^ 3; size > 0 {
		gfxBuffer, err := device.CreateBuffer(gfx.BufferDescriptor{Label: handle.MakeLabel(buffer, label), Size: size, Usage: gfx.BufferUsageFlags(usage) | gfx.BufferUsageFlagCopyDst})
		if err != nil {
			t.buffers.Release(buffer)
			return Buffer{}, gfx.Buffer{}, err
		}
		slot.buffer = gfxBuffer
		slot.size = size
	}
	return buffer, slot.buffer, nil
}

// findTexture answers the gfx texture behind texture; false when render holds
// no such texture.
func (t *assetTable) findTexture(texture Texture) (gfx.Texture, bool) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, ok := t.textures.Find(texture)
	if !ok {
		return gfx.Texture{}, false
	}
	return slot.texture, true
}

// findBuffer answers the gfx buffer behind buffer and its size; false when
// render holds no such buffer, or it has no bytes.
func (t *assetTable) findBuffer(buffer Buffer) (gfx.Buffer, uint64, bool) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, ok := t.buffers.Find(buffer)
	if !ok || slot.buffer == (gfx.Buffer{}) {
		return gfx.Buffer{}, 0, false
	}
	return slot.buffer, slot.size, true
}

// findLevelView answers the view of texture's mip level and layer, made the
// first time a pass draws into or samples it, with the descriptor gfx made the texture
// from; an error naming what it lacks when render holds no such texture, or
// it has no such mip level or layer.
func (t *assetTable) findLevelView(device gfx.Device, texture Texture, mipLevel, layer uint32) (gfx.TextureView, gfx.TextureDescriptor, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, ok := t.textures.Find(texture)
	if !ok {
		return gfx.TextureView{}, gfx.TextureDescriptor{}, errors.New("a texture render does not hold")
	}
	descriptor := device.TextureDescriptor(slot.texture)
	if mipLevel >= descriptor.MipLevelCount || layer >= descriptor.Size.DepthOrArrayLayers {
		return gfx.TextureView{}, gfx.TextureDescriptor{}, fmt.Errorf("%q at mip level %d and layer %d, of %d mip levels and %d layers", descriptor.Label, mipLevel, layer, descriptor.MipLevelCount, descriptor.Size.DepthOrArrayLayers)
	}
	for _, target := range slot.levelViews {
		if target.mipLevel == mipLevel && target.layer == layer {
			return target.view, descriptor, nil
		}
	}
	view, err := device.CreateTextureView(slot.texture, gfx.TextureViewDescriptor{
		Dimension:       gfx.TextureViewDimension2D,
		Aspect:          gfx.TextureAspectAll,
		BaseMipLevel:    mipLevel,
		MipLevelCount:   1,
		BaseArrayLayer:  layer,
		ArrayLayerCount: 1,
	})
	if err != nil {
		return gfx.TextureView{}, gfx.TextureDescriptor{}, err
	}
	slot.levelViews = append(slot.levelViews, levelView{mipLevel: mipLevel, layer: layer, view: view})
	return view, descriptor, nil
}

// writeBuffer writes data into buffer at offset, for gfx's next submit. A
// write that does not fit is refused and reported: into a buffer render does
// not hold or of no bytes, at an offset or of a length not a multiple of 4,
// or ending past the buffer.
func (t *assetTable) writeBuffer(device gfx.Device, buffer Buffer, offset uint64, data []byte) {
	gfxBuffer, size, ok := t.findBuffer(buffer)
	if !ok {
		t.report(ReportKindWriteRefused, fmt.Errorf("render: writing buffer %v, which render does not hold, or which has no bytes", buffer))
		return
	}
	length := uint64(len(data))
	if offset%4 != 0 || length%4 != 0 || offset > size || length > size-offset {
		t.report(ReportKindWriteRefused, fmt.Errorf("render: writing %d bytes at offset %d into buffer %v, of %d bytes, where the offset and the length are multiples of 4 and the write ends within the buffer", length, offset, buffer, size))
		return
	}
	writeBytes(t, device, gfxBuffer, offset, data)
}

// writeTexture writes data, rows of texels packed tight, into region of
// texture, for gfx's next submit. A write that does not fit is refused and
// reported: into a texture render does not hold, of several samples or of a
// format gfx's writes cannot fill, at a mip level or layer it lacks, into a
// region past its level's edges, or of data not as long as the region.
func (t *assetTable) writeTexture(device gfx.Device, texture Texture, region TextureRegion, data []byte) {
	gfxTexture, ok := t.findTexture(texture)
	if !ok {
		t.report(ReportKindWriteRefused, fmt.Errorf("render: writing texture %v, which render does not hold", texture))
		return
	}
	descriptor := device.TextureDescriptor(gfxTexture)
	texelSize := TextureFormat(descriptor.Format).info().texelSize
	if texelSize == 0 || descriptor.SampleCount > 1 || region.MipLevel >= descriptor.MipLevelCount || region.Layer >= descriptor.Size.DepthOrArrayLayers {
		t.report(ReportKindWriteRefused, fmt.Errorf("render: writing texture %q at mip level %d and layer %d, of %d mip levels, %d layers and %d samples in format %d, where it has the level and the layer, one sample, and a format gfx's writes fill", descriptor.Label, region.MipLevel, region.Layer, descriptor.MipLevelCount, descriptor.Size.DepthOrArrayLayers, descriptor.SampleCount, descriptor.Format))
		return
	}
	levelWidth := max(descriptor.Size.Width>>region.MipLevel, 1)
	levelHeight := max(descriptor.Size.Height>>region.MipLevel, 1)
	width := cmp.Or(region.Width, levelWidth-min(region.X, levelWidth))
	height := cmp.Or(region.Height, levelHeight-min(region.Y, levelHeight))
	if region.X > levelWidth || width > levelWidth-region.X || region.Y > levelHeight || height > levelHeight-region.Y || uint64(len(data)) != uint64(width)*uint64(height)*uint64(texelSize) {
		t.report(ReportKindWriteRefused, fmt.Errorf("render: writing %d bytes into %d×%d texels at %d,%d of texture %q at mip level %d, of %d×%d texels of %d bytes each, where the region is within the level and the bytes fill it", len(data), width, height, region.X, region.Y, descriptor.Label, region.MipLevel, levelWidth, levelHeight, texelSize))
		return
	}
	writeTexels(t, device,
		gfx.ImageCopyTexture{Texture: gfxTexture, MipLevel: region.MipLevel, Origin: gfx.Origin3D{X: region.X, Y: region.Y, Z: region.Layer}, Aspect: gfx.TextureAspectAll},
		data, texelSize, gfx.Extent3D{Width: width, Height: height, DepthOrArrayLayers: 1})
}

// freeReleased removes every loaded texture and buffer whose last acquire was
// released since the last frame ended and is not acquired again, and every
// made one released since then.
func (t *assetTable) freeReleased(device gfx.Device) {
	removeTexture := func(texture Texture) { t.removeTexture(device, texture) }
	removeBuffer := func(buffer Buffer) { t.removeBuffer(device, buffer) }
	t.loadedTextures.freeReleased(removeTexture)
	t.loadedBuffers.freeReleased(removeBuffer)
	t.madeTextures.freeReleased(removeTexture)
	t.madeBuffers.freeReleased(removeBuffer)
}

// removeTexture releases the gfx texture behind texture and its views, and
// frees its slot for a handle of the next generation.
func (t *assetTable) removeTexture(device gfx.Device, texture Texture) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, ok := t.textures.Find(texture)
	if !ok {
		return
	}
	for _, target := range slot.levelViews {
		device.ReleaseTextureView(target.view)
	}
	device.ReleaseTexture(slot.texture)
	slot.levelViews = slot.levelViews[:0]
	t.textures.Release(texture)
}

// removeBuffer releases the gfx buffer behind buffer, and frees its slot for
// a handle of the next generation.
func (t *assetTable) removeBuffer(device gfx.Device, buffer Buffer) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, ok := t.buffers.Find(buffer)
	if !ok {
		return
	}
	if slot.buffer != (gfx.Buffer{}) {
		device.ReleaseBuffer(slot.buffer)
	}
	*slot = bufferSlot{}
	t.buffers.Release(buffer)
}

// textureSlot is the slot of one texture: the gfx texture behind its handle,
// with the views of single levels and layers passes drew into or sampled,
// made as first needed.
type textureSlot struct {
	texture    gfx.Texture
	levelViews []levelView
}

// bufferSlot is the slot of one buffer: the gfx buffer behind its handle,
// none for a buffer of no bytes, and its size.
type bufferSlot struct {
	buffer gfx.Buffer
	size   uint64
}

// levelView is a view of one mip level and one layer of a texture, which a
// pass draws into or render's mip shader samples.
type levelView struct {
	mipLevel, layer uint32
	view            gfx.TextureView
}

// writeTexels writes texels, rows of texels of texelSize bytes each packed
// tight, layer after layer, into the size region at destination. A write gfx
// refuses is logged through table, unless the device is lost.
func writeTexels(table *assetTable, device gfx.Device, destination gfx.ImageCopyTexture, texels []byte, texelSize uint32, size gfx.Extent3D) {
	err := device.WriteTexture(destination, texels, gfx.ImageDataLayout{BytesPerRow: texelSize * size.Width, RowsPerImage: size.Height}, size)
	if err != nil {
		table.logUnlessLost(fmt.Errorf("render: writing texture %q: %w", device.TextureDescriptor(destination.Texture).Label, err))
	}
}

// writeBytes writes data, whose length is a multiple of 4, into buffer at
// offset, a multiple of 4. A write gfx refuses is logged through table,
// unless the device is lost.
func writeBytes(table *assetTable, device gfx.Device, buffer gfx.Buffer, offset uint64, data []byte) {
	if err := device.WriteBuffer(buffer, offset, data); err != nil {
		table.logUnlessLost(fmt.Errorf("render: writing buffer %q: %w", device.BufferDescriptor(buffer).Label, err))
	}
}

// padBytes answers data with zeros appended up to a multiple of 4 bytes,
// leaving data's own array as it was.
func padBytes(data []byte) []byte {
	if size := (uint64(len(data)) + 3) &^ 3; size != uint64(len(data)) {
		return append(data[:len(data):len(data)], make([]byte, size-uint64(len(data)))...)
	}
	return data
}
