package render

import (
	"fmt"
	"io/fs"
	"slices"
	"sync"

	"github.com/dvoyni/cog/gfx"
)

// textureLoader shares, counts and frees the textures render loads from
// files: one texture for each key, counted by the acquires holding it, its
// body written by render's decode job, and freed at the end of the frame its
// count reaches zero in. Its mutex guards what it holds; it takes the asset
// table's mutex inside its own, never the other way round, and reads no file
// holding it.
type textureLoader struct {
	mutex sync.Mutex
	// textures is every loaded texture by its key, and loadedTextures each
	// one's key and count by its handle, until it is freed.
	textures       map[textureKey]Texture
	loadedTextures map[Texture]loadedTexture
	// pendingLoads are the textures whose bodies the decode job has yet to
	// write.
	pendingLoads []pendingTextureLoad
	// releasedTextures are the textures whose count reached zero since the
	// last frame ended, freed when the frame ends unless acquired again by
	// then.
	releasedTextures []Texture
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
	descriptor, headerErr := key.readHeader(files)
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
	if l.textures == nil {
		l.textures = map[textureKey]Texture{}
		l.loadedTextures = map[Texture]loadedTexture{}
	}
	l.textures[key] = texture
	l.loadedTextures[texture] = loadedTexture{key: key, holderCount: 1}
	size := gfx.Extent3D{Width: descriptor.Width, Height: descriptor.Height, DepthOrArrayLayers: 1}
	if headerErr != nil {
		table.report(ReportKindLoadFailed, fmt.Errorf("render: texture %q failed to load, drawing magenta: %w", key.path, headerErr))
		gfxTexture, _ := table.findTexture(texture)
		writeTexels(table, device, gfxTexture, size, slices.Repeat([]byte{255, 0, 255, 255}, int(size.Width*size.Height)))
		return texture
	}
	l.pendingLoads = append(l.pendingLoads, pendingTextureLoad{texture: texture, key: key, size: size})
	return texture
}

// release counts one holder of texture fewer; at the last, the texture is
// freed when the frame ends, unless acquired again by then. A texture no
// acquire holds is refused and reported.
func (l *textureLoader) release(table *assetTable, texture Texture) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	entry, ok := l.loadedTextures[texture]
	if !ok || entry.holderCount == 0 {
		table.report(ReportKindReleaseRefused, fmt.Errorf("render: releasing texture %v, which no acquire holds", texture))
		return
	}
	entry.holderCount--
	l.loadedTextures[texture] = entry
	if entry.holderCount == 0 {
		l.releasedTextures = append(l.releasedTextures, texture)
	}
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
	var unmippedTextures []Texture
	for _, load := range loads {
		gfxTexture, ok := table.findTexture(load.texture)
		if !ok {
			continue
		}
		texels, err := load.key.decodeBody(files, load.size.Width, load.size.Height)
		if err != nil {
			table.report(ReportKindLoadFailed, fmt.Errorf("render: texture %q failed to load, drawing magenta: %w", load.key.path, err))
			texels = slices.Repeat([]byte{255, 0, 255, 255}, int(load.size.Width*load.size.Height))
		}
		writeTexels(table, device, gfxTexture, load.size, texels)
		if load.key.options.Mipmaps {
			unmippedTextures = append(unmippedTextures, load.texture)
		}
	}
	return unmippedTextures
}

// freeReleased removes from table each texture whose count reached zero
// since the last frame ended and is zero still, forgetting its key, so its
// next acquire loads it again and reports a failure again.
func (l *textureLoader) freeReleased(table *assetTable, device gfx.Device) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	for _, texture := range l.releasedTextures {
		if entry, ok := l.loadedTextures[texture]; ok && entry.holderCount == 0 {
			delete(l.textures, entry.key)
			delete(l.loadedTextures, texture)
			table.removeTexture(device, texture)
		}
	}
	l.releasedTextures = l.releasedTextures[:0]
}

// hold counts one more holder of key's texture and answers it; false when
// the loader holds none. l's mutex is held.
func (l *textureLoader) hold(key textureKey) (Texture, bool) {
	texture, ok := l.textures[key]
	if !ok {
		return Texture{}, false
	}
	entry := l.loadedTextures[texture]
	entry.holderCount++
	l.loadedTextures[texture] = entry
	return texture, true
}

// loadedTexture is what textureLoader keeps of one texture: its key, and how
// many acquires hold it.
type loadedTexture struct {
	key         textureKey
	holderCount int
}

// pendingTextureLoad is a texture whose body the decode job has yet to
// write: its handle, its key and its size.
type pendingTextureLoad struct {
	texture Texture
	key     textureKey
	size    gfx.Extent3D
}

// writeTexels writes texels, four bytes each, row by row, into mip level 0 of
// texture, of size. A write gfx refuses is logged through table, unless the
// device is lost.
func writeTexels(table *assetTable, device gfx.Device, texture gfx.Texture, size gfx.Extent3D, texels []byte) {
	err := device.WriteTexture(
		gfx.ImageCopyTexture{Texture: texture, Aspect: gfx.TextureAspectAll},
		texels,
		gfx.ImageDataLayout{BytesPerRow: 4 * size.Width, RowsPerImage: size.Height},
		size,
	)
	if err != nil {
		table.logUnlessLost(fmt.Errorf("render: writing texture %q: %w", device.TextureDescriptor(texture).Label, err))
	}
}
