package render

import (
	"sync"

	"github.com/dvoyni/cog/gfx"
)

// texturePool is the pool of temporary textures, by descriptor: each texture
// is held from its acquire until the frame ends, and freed once it has gone
// unused for as many frames as render is configured to wait.
type texturePool struct {
	// mutex guards entries, every texture the pool has made and not freed.
	mutex   sync.Mutex
	entries []pooledTexture
}

// pooledTexture is one texture of the pool: the descriptor it was made from,
// with its counts filled in, and whether a user holds it this frame.
type pooledTexture struct {
	poolHold
	descriptor TextureDescriptor
	texture    Texture
}

// acquire hands out a texture of descriptor, whose counts are filled in, that
// no one holds this frame, making one in table when the pool has none.
func (p *texturePool) acquire(descriptor TextureDescriptor, table *assetTable, device gfx.Device) (Texture, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	for index := range p.entries {
		if entry := &p.entries[index]; !entry.held && entry.descriptor == descriptor {
			entry.held = true
			return entry.texture, nil
		}
	}
	texture, err := table.addTexture(device, descriptor)
	if err != nil {
		return Texture{}, err
	}
	p.entries = append(p.entries, pooledTexture{poolHold: poolHold{held: true}, descriptor: descriptor, texture: texture})
	return texture, nil
}

// endFrame takes back every texture held this frame, and removes from table
// those that have now gone unused for idleFrames frames.
func (p *texturePool) endFrame(idleFrames int, table *assetTable, device gfx.Device) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	keptEntries := p.entries[:0]
	for _, entry := range p.entries {
		if entry.endFrame(idleFrames) {
			table.removeTexture(device, entry.texture)
			continue
		}
		keptEntries = append(keptEntries, entry)
	}
	clear(p.entries[len(keptEntries):])
	p.entries = keptEntries
}
