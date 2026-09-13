package gfx

import (
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/extensions/gfx/internal"
)

// TextureDescr describes a texture by resource path (TextureWithResource),
// inline pixel bytes (TextureWithBytes), or a texture returned by
// ResourceQueue.BakeTexture.
type TextureDescr = internal.TextureDescr

const (
	TextureSourceResource = internal.TextureSourceResource
	TextureSourceBytes    = internal.TextureSourceBytes
	TextureSourceBaked    = internal.TextureSourceBaked
)

// TextureWithResource describes a texture loaded from storage.FileSystem. It is
// always sRGB: the loader decodes PNG and JPEG, both of which are gamma-encoded
// by definition, so there is nothing for a caller to choose and a caller that
// chose wrong would be a silently wrong picture. A data map - normals,
// metallic-roughness, occlusion - is not a picture and does not come through
// here; it comes through TextureWithBytes, which does take a format.
func TextureWithResource(path string) TextureDescr {
	return internal.TextureWithResource(path)
}

// TextureWithBytes describes a texture from inline pixel bytes. copyData
// snapshots pixels when true; when false, the caller must keep them unchanged
// until the recorded frame is consumed or dropped. mipmaps generates a full mip
// chain at bake time for smoother minification.
func TextureWithBytes(width, height int, format gpu.TextureFormat, pixels []byte, copyData, mipmaps bool) TextureDescr {
	return internal.TextureWithBytes(width, height, format, pixels, copyData, mipmaps)
}
