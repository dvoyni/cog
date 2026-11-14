package render

import (
	"errors"
	"fmt"
	"sync"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/render/internal/handle"
)

// assetTable is the private resource of every asset render holds, behind the
// handles its users keep. Every job reads it; its mutex guards the slots, and
// loadedTextures guards itself.
type assetTable struct {
	reporter
	mutex sync.Mutex
	// textures is every texture render holds, by its handle.
	textures handle.SlotMap[Texture, textureSlot]
	// loadedTextures shares, counts and frees the textures loaded from
	// files.
	loadedTextures textureLoader
}

// textureSlot is the slot of one texture: the gfx texture behind its handle,
// with the views passes drew into, made as first needed.
type textureSlot struct {
	texture     gfx.Texture
	targetViews []targetView
}

// targetView is a view of one mip level and one layer of a texture, which a
// pass draws into.
type targetView struct {
	mipLevel, layer uint32
	view            gfx.TextureView
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

// findTargetView answers the view of texture's mip level and layer, made the
// first time a pass draws into it, with the descriptor gfx made the texture
// from; an error naming what it lacks when render holds no such texture, or
// it has no such mip level or layer.
func (t *assetTable) findTargetView(device gfx.Device, texture Texture, mipLevel, layer uint32) (gfx.TextureView, gfx.TextureDescriptor, error) {
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
	for _, target := range slot.targetViews {
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
	slot.targetViews = append(slot.targetViews, targetView{mipLevel: mipLevel, layer: layer, view: view})
	return view, descriptor, nil
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
	for _, target := range slot.targetViews {
		device.ReleaseTextureView(target.view)
	}
	device.ReleaseTexture(slot.texture)
	slot.targetViews = slot.targetViews[:0]
	t.textures.Release(texture)
}
