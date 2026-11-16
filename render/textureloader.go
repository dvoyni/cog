package render

import (
	"fmt"
	"io/fs"
	"slices"

	"github.com/dvoyni/cog/gfx"
)

// textureLoader shares, counts and frees the textures render loads from
// files: one texture for each key, counted by the acquires holding it, its
// body written by render's decode job, and freed at the end of the frame its
// count reaches zero in. Its counter's mutex also guards its pending
// loads and reloads; it reads no file holding it.
type textureLoader struct {
	assetCounter[textureKey, Texture]
	// pendingLoads are the textures whose bodies the decode job has yet to
	// write, and pendingReloads those ReloadChanged reloaded, to be swapped
	// in at the start of the next frame.
	pendingLoads   []pendingTextureLoad
	pendingReloads []assetReload[Texture, gfx.Texture]
}

// acquire hands out the texture of key, counting one more holder, and makes
// it in table from its file's header when the loader holds none, leaving its
// body for the decode job to write. A header that fails is reported, and gets
// a texture of one magenta texel. A texture gfx refuses to make is logged,
// unless the device is lost, and the zero Texture answered.
func (l *textureLoader) acquire(table *assetTable, device gfx.Device, files fs.FS, key textureKey) Texture {
	l.mutex.Lock()
	texture, ok := l.hold(key)
	l.mutex.Unlock()
	if ok {
		return texture
	}
	descriptor, stamp, headerErr := key.readHeader(files)
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if texture, ok := l.hold(key); ok {
		return texture
	}
	texture, err := table.addTexture(device, descriptor)
	if err != nil {
		table.logUnlessLost(fmt.Errorf("render: acquiring texture %q: %w", key.path, err))
		return Texture{}
	}
	l.add(key, texture)
	l.setStamp(texture, stamp)
	if headerErr != nil {
		table.report(ReportKindLoadFailed, fmt.Errorf("render: texture %q failed to load, drawing magenta: %w", key.path, headerErr))
		l.markFailed(texture)
		gfxTexture, _ := table.findTexture(texture)
		size := gfx.Extent3D{Width: descriptor.Width, Height: descriptor.Height, DepthOrArrayLayers: 1}
		writeTexels(table, device, texture, gfx.ImageCopyTexture{Texture: gfxTexture, Aspect: gfx.TextureAspectAll}, slices.Repeat([]byte{255, 0, 255, 255}, int(size.Width*size.Height)), 4, size)
		return texture
	}
	l.pendingLoads = append(l.pendingLoads, pendingTextureLoad{texture: texture, key: key})
	return texture
}

// writePending decodes the body of each texture pending and still held in
// table, one after another, holding no mutex, and writes it into mip level
// 0, and answers those of them acquired with Mipmaps, whose mips are yet to
// be generated. A body that fails is reported, and magenta written in its
// place.
func (l *textureLoader) writePending(table *assetTable, device gfx.Device, files fs.FS) []Texture {
	l.mutex.Lock()
	loads := l.pendingLoads
	l.pendingLoads = nil
	l.mutex.Unlock()
	return l.write(table, device, files, loads)
}

// writeHeld writes the body of every texture l holds again, as writePending
// writes those pending, taking the loads pending, which it holds too: after
// a device loss, which left them empty. A texture whose load failed is
// written magenta again, its failure not reported again.
func (l *textureLoader) writeHeld(table *assetTable, device gfx.Device, files fs.FS) []Texture {
	l.mutex.Lock()
	l.pendingLoads = nil
	var loads []pendingTextureLoad
	for texture, entry := range l.entries {
		loads = append(loads, pendingTextureLoad{texture: texture, key: entry.key, failed: entry.failed})
	}
	l.mutex.Unlock()
	return l.write(table, device, files, loads)
}

// reloadChanged reloads, in the caller's job, each texture l holds whose
// file's stamp changed since it was loaded or last reloaded: it decodes the
// file again and, when its size or mip level count changed, makes a gfx
// texture of the new descriptor on device, labelled with the texture's
// handle, which swapReloads puts behind the handle at the start of the next
// frame, writing the texels then. A file that fails to decode is reported
// once for that change, and the texture keeps drawing what it drew.
func (l *textureLoader) reloadChanged(table *assetTable, device gfx.Device, files fs.FS) {
	for _, changed := range l.claimChanged(files, func(key textureKey) string { return key.path }) {
		descriptor, _, err := changed.key.readHeader(files)
		var texels []byte
		if err == nil {
			texels, err = changed.key.decodeBody(files, descriptor.Width, descriptor.Height)
		}
		if err != nil {
			table.report(ReportKindLoadFailed, fmt.Errorf("render: texture %q failed to reload, drawing what it drew: %w", changed.key.path, err))
			continue
		}
		gfxTexture, ok := table.findTexture(changed.handle)
		if !ok {
			continue
		}
		reload := assetReload[Texture, gfx.Texture]{handle: changed.handle, stamp: changed.stamp, contents: texels, size: gfx.Extent3D{Width: descriptor.Width, Height: descriptor.Height, DepthOrArrayLayers: 1}, mipmaps: changed.key.options.Mipmaps}
		if current := device.TextureDescriptor(gfxTexture); current.Size != reload.size || current.MipLevelCount != descriptor.MipLevelCount {
			if reload.replacement, err = createLabelledTexture(device, changed.handle, descriptor); err != nil {
				table.logUnlessLost(fmt.Errorf("render: reloading texture %q: %w", changed.key.path, err))
				continue
			}
		}
		l.queueReload(&l.pendingReloads, reload, device.ReleaseTexture)
	}
}

// write decodes and writes the body of each of loads still held in table, at
// the size its texture was made, as writePending says, holding no mutex but
// to mark a failed load.
func (l *textureLoader) write(table *assetTable, device gfx.Device, files fs.FS, loads []pendingTextureLoad) []Texture {
	var unmippedTextures []Texture
	for _, load := range loads {
		gfxTexture, ok := table.findTexture(load.texture)
		if !ok {
			continue
		}
		size := device.TextureDescriptor(gfxTexture).Size
		var texels []byte
		var err error
		if !load.failed {
			texels, err = load.key.decodeBody(files, size.Width, size.Height)
		}
		if err != nil {
			table.report(ReportKindLoadFailed, fmt.Errorf("render: texture %q failed to load, drawing magenta: %w", load.key.path, err))
			l.mutex.Lock()
			l.markFailed(load.texture)
			l.mutex.Unlock()
		}
		if load.failed || err != nil {
			texels = slices.Repeat([]byte{255, 0, 255, 255}, int(size.Width*size.Height))
		}
		writeTexels(table, device, load.texture, gfx.ImageCopyTexture{Texture: gfxTexture, Aspect: gfx.TextureAspectAll}, texels, 4, size)
		if load.key.options.Mipmaps {
			unmippedTextures = append(unmippedTextures, load.texture)
		}
	}
	return unmippedTextures
}

// pendingTextureLoad is a texture whose body the decode job has yet to
// write: its handle, its key, and whether its load failed, to be written
// magenta.
type pendingTextureLoad struct {
	texture Texture
	key     textureKey
	failed  bool
}

