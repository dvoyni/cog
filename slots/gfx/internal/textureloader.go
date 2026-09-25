package internal

import (
	"io/fs"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
)

// texture is the texture cache's value: what one load produced, and never the
// descriptor that asked for it. A descriptor is a request and learns nothing;
// this is the answer.
//
// It is stored by value. Nothing mutates it after the load, and four words per
// entry cost no allocation.
//
// The decoded width and height are kept because the loader has them in hand and
// dropping them was the defect - the old cache wrote BakedTexture(id, 0, 0) and
// threw both away. Nothing reads them yet: emitResources wants an id and
// SetTexture takes one.
type texture struct {
	id            types.TextureID
	width, height int
	format        descriptors.TextureFormat
}

// textureUserData is what the texture loader needs that only the render handler
// holds: a backend to mint an id from, and the frame's op queue to emit the bake
// and the release into. It is U, the cache's opaque pass-through, so it travels
// per call and the loader stores none of it.
type textureUserData struct {
	backend Backend
	ops     *Queue
}

// textureLoader decodes an image into a baked texture. It is stateless and is
// built once with the translator, which is what lets everything lock-bound
// arrive per call.
type textureLoader struct{}

// Load decodes the bytes the Library read and uploads them. A file that opened
// but does not decode yields the zero texture, which is cached like any other
// value: the decode is attempted one time per path rather than once a frame,
// and the draw is dropped on the zero id exactly as it was before.
func (textureLoader) Load(
	_ kernel.Kernel, data assets.Blob, params descriptors.TextureDescrParams, _ fs.FS, userData textureUserData,
) texture {
	width, height, pixels, ok := DecodeTexture(data)
	if !ok {
		return texture{}
	}
	format := descriptors.TextureParamsFormat(params)
	id := userData.backend.NewTexture()
	userData.ops.BakeTexture(id, width, height, format, pixels, false)
	return texture{id: id, width: width, height: height, format: format}
}

// Default is the zero texture, which binds id 0 - and id 0 is the driver's
// white, gfx's stated rule for an unset texture binding rather than a
// substitution chosen here. What makes that sound now is that choosing id 0 no
// longer means choosing silence: the Library has already reported the missing
// file under this descriptor.
func (textureLoader) Default(_ assets.Descr[descriptors.TextureDescrParams], _ textureUserData) texture {
	return texture{}
}

// Free hands the baked texture back through the frame's op queue, which is the
// same release the translator emitted itself before. A failed entry has no id
// and nothing to hand back.
func (textureLoader) Free(value texture, userData textureUserData) {
	if value.id != 0 {
		userData.ops.ReleaseTexture(value.id)
	}
}
