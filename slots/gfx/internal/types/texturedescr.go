package types

import (
	"github.com/dvoyni/cog/libs/assets"
)

// TextureDescrParams is everything about a texture that is neither its path nor
// its pixels: the bake options that make one source several textures, and the id
// of one already baked.
type TextureDescrParams struct {
	// Width and Height are the texture's dimensions in texels. A texture
	// loaded from a resource path has zero until it is baked, since only the
	// file knows - and goes on having zero afterwards, because a descriptor is
	// the request and a request learns nothing from the load it names.
	Width, Height int
	// Layers is how many array layers the texture was asked for, and zero when
	// the descriptor cannot say. Only an allocation names one, so a bake sets 1
	// and BakedTexture - an id and nothing behind it - leaves 0. Zero is
	// deliberately not 1: a check that read it as 1 would call an array texture
	// flat and refuse a draw that was correct.
	Layers int
	// Format is the texture's pixel format. A texture loaded from a resource
	// path is always sRGB, because the loader decodes PNG and JPEG and both
	// are gamma-encoded by definition.
	Format TextureFormat
	// Mipmaps generates a full mip chain when the texture is baked.
	Mipmaps bool
	// CopyData snapshots the pixels when the descriptor is recorded.
	CopyData bool
	// ID is the baked texture's identifier, and zero when the descriptor is
	// not baked.
	ID TextureID
}

// TextureDescr describes a texture by resource path (TextureWithResource),
// inline pixel bytes (TextureWithBytes), or a texture returned by
// ResourceQueue.NewTexture.
//
// It is an assets.Descr: Name is the resource path, Blob is the inline pixel run
// - static, for the reason BufferDescr.Bytes gives - and Params is everything
// else. That is also what makes it the key of gfx's texture cache rather than
// merely the request handed to one.
//
// The three cases are disjoint and are told apart by which field carries the
// answer: a Params.ID for a baked texture, a Name for a path, a Blob for inline
// pixels. There is no source enum, because it restated exactly that.
type TextureDescr assets.Descr[TextureDescrParams]

// BakedTextureWith describes a texture a queue has already baked or allocated,
// as BakedTexture does, with its layer count and format too. format may be zero
// where the bake does not say, and layers zero where the count is unknown.
func BakedTextureWith(id TextureID, width, height, layers int, format TextureFormat) TextureDescr {
	return TextureDescr{Params: TextureDescrParams{ID: id, Width: width, Height: height, Layers: layers, Format: format}}
}

// BakedTexture is the descriptor of a texture already baked under id.
func BakedTexture(id TextureID, width, height int) TextureDescr {
	return TextureDescr{Params: TextureDescrParams{ID: id, Width: width, Height: height}}
}

// TextureWithResource describes a texture loaded from storage.FileSystem. It is
// always sRGB: the loader decodes PNG and JPEG, both of which are gamma-encoded
// by definition, so there is nothing for a caller to choose and a caller that
// chose wrong would be a silently wrong picture. A data map - normals,
// metallic-roughness, occlusion - is not a picture and does not come through
// here; it comes through TextureWithBytes, which does take a format.
func TextureWithResource(path string) TextureDescr {
	return TextureDescr{Name: path, Params: TextureDescrParams{Format: FormatRGBA8Srgb}}
}

// TextureWithBytes describes a texture from inline pixel bytes. copyData
// snapshots pixels when true; when false, the caller must keep them unchanged
// until the recorded frame is consumed or dropped. mipmaps generates a full mip
// chain at bake time for smoother minification.
func TextureWithBytes(width, height int, format TextureFormat, pixels []byte, copyData, mipmaps bool) TextureDescr {
	return TextureDescr{
		Blob: assets.NewBlob(pixels),
		Params: TextureDescrParams{
			Width: width, Height: height, Format: format, CopyData: copyData, Mipmaps: mipmaps,
		},
	}
}
