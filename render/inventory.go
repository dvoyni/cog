package render

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

// Inventory is the task parameter a task reaches render's assets through, to
// acquire and release them, declared in its signature as kernel.Read is:
//
//	func loadLevel(k *kernel.Kernel, level Level, inventory render.Inventory) kernel.NoResult
//
// It locks render's asset table, gfx's Device and platform's FS, all for
// read, so tasks holding it run together and beside recording tasks, and its
// user names neither gfx nor its locks. A task that records and acquires
// declares it beside Frame. It is valid only while the task's job runs.
type Inventory struct {
	table  kernel.Read[*assetTable]
	device kernel.Read[gfx.Device]
	files  kernel.Read[platform.FS]
}

// Declare declares Inventory's locks and answers it bound to what they guard.
func (Inventory) Declare(access *kernel.Access) Inventory {
	return Inventory{table: access.Read[*assetTable](), device: access.Read[gfx.Device](), files: access.Read[platform.FS]()}
}

// AcquireTexture hands out the texture of path in platform's FS with
// options, counting one more holder of it: every acquire of the same path
// and options shares one texture until the last of them is released. The
// texture exists when the call returns, made from the file's header alone,
// with its size, the format of its colour space, 8 bits per channel, and
// with Mipmaps every mip level; render's jobs write its body before gfx
// submits the frame, whether it was acquired before the frame or while it
// records, so a draw recorded after the acquire reads it, and with Mipmaps
// draws every level below the first from it in render's own command list,
// which gfx submits before every user's. One acquired by a task the frame
// does not wait for, after its recording has ended, is written, and its mips
// drawn, before the next frame's submit instead. A file render
// cannot read or decode is reported once, and draws magenta: one texel when
// its header fails, its own size when its body does. A texture gfx refuses
// to make is logged, and the zero Texture answered. PNG and JPEG files load.
func (i Inventory) AcquireTexture(path string, options TextureOptions) Texture {
	table := i.table.Get()
	return table.loadedTextures.acquire(table, i.device.Get(), i.files.Get(), textureKey{path: path, options: options})
}

// ReleaseTexture gives back one acquire of texture. When the last is given
// back, the texture is freed at the end of that frame, after gfx's present,
// so the frame's draws still read it; an acquire of its path and options
// before then holds it again. A texture no acquire holds, such as a
// temporary, one already released as often as it was acquired, or the zero
// Texture, is refused and reported.
func (i Inventory) ReleaseTexture(texture Texture) {
	table := i.table.Get()
	table.loadedTextures.release(table, texture)
}
