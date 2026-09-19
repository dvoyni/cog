package types

import (
	"github.com/dvoyni/cog/libs/assets"
)

// TextureDescrParams is everything about a texture that is neither its path nor
// its pixels: the bake options that make one source several textures, and the id
// of one already baked.
//
// Its fields are unexported on purpose. TextureDescr's own fields are exported,
// so anyone can write a descriptor down - but only with a zero Params, which is
// exactly what TextureWithResource produces with no options: canonical, legal,
// harmless. What must not be forgeable is id, because a hand-written baked id is
// one gfx never minted.
type TextureDescrParams struct {
	width, height int
	// layers is how many array layers the texture was asked for, and zero when
	// the descriptor cannot say. Only an allocation names one, so a bake sets 1
	// and BakedTexture - an id and nothing behind it - leaves 0. Zero is
	// deliberately not 1: a check that read it as 1 would call an array texture
	// flat and refuse a draw that was correct.
	layers   int
	format   TextureFormat
	mipmaps  bool
	copyData bool
	id       TextureID
}

// TextureDescr describes a texture by resource path (TextureWithResource),
// inline pixel bytes (TextureWithBytes), or a texture returned by
// ResourceQueue.BakeTexture.
//
// It is an assets.Descr: Name is the resource path, Blob is the inline pixel run
// - static, for the reason BufferDescr.bytes gives - and Params is everything
// else. That is also what makes it the key of gfx's texture cache rather than
// merely the request handed to one.
//
// The three cases are disjoint and are told apart by which field carries the
// answer: a Params.id for a baked texture, a Name for a path, a Blob for inline
// pixels. There is no source enum, because it restated exactly that.
//
// A defined type inherits no methods from the type it is defined over, so every
// accessor below is gfx's own and the descriptor offers precisely the surface it
// always did.
type TextureDescr assets.Descr[TextureDescrParams]

// ID returns the baked texture identifier, or 0 when the descriptor is not baked.
func (t TextureDescr) ID() TextureID { return t.Params.id }

// Path returns the resource path for a TextureWithResource descriptor (empty otherwise).
func (t TextureDescr) Path() string { return t.Name }

// Size returns the texture's dimensions in texels. A texture loaded from a
// resource path reports zero until it is baked, since only the file knows - and
// goes on reporting zero afterwards, because a descriptor is the request and a
// request learns nothing from the load it names.
func (t TextureDescr) Size() (width, height int) { return t.Params.width, t.Params.height }

// Layers returns how many array layers the texture was allocated with, and zero
// when the descriptor cannot say - a texture named by path, inline pixels, or a
// bare baked id. More than one layer is a 2D-array texture, which is what a
// binding declared texture_2d_array needs; zero means unknown and nothing may
// conclude from it.
func (t TextureDescr) Layers() int { return t.Params.layers }

// Format returns the texture's pixel format. A texture loaded from a resource
// path is always sRGB, because the loader decodes PNG and JPEG and both are
// gamma-encoded by definition.
func (t TextureDescr) Format() TextureFormat { return t.Params.format }

// Mipmaps reports whether a full mip chain is generated when the texture is
// baked.
func (t TextureDescr) Mipmaps() bool { return t.Params.mipmaps }

// PixelBytes reports how many bytes of inline pixel data the descriptor
// carries, and zero for a texture named by path or already baked. The pixels
// themselves stay inside the descriptor - a caller wanting to know that an
// upload is a megabyte should not have to hold the megabyte to find out.
func (t TextureDescr) PixelBytes() int { return t.Blob.Len() }

// BakedTexture is the descriptor of a texture already baked under id.
func BakedTexture(id TextureID, width, height int) TextureDescr {
	return TextureDescr{Params: TextureDescrParams{id: id, width: width, height: height}}
}

// TextureWithResource describes a texture loaded from storage.FileSystem. It is
// always sRGB: the loader decodes PNG and JPEG, both of which are gamma-encoded
// by definition, so there is nothing for a caller to choose and a caller that
// chose wrong would be a silently wrong picture. A data map - normals,
// metallic-roughness, occlusion - is not a picture and does not come through
// here; it comes through TextureWithBytes, which does take a format.
func TextureWithResource(path string) TextureDescr {
	return TextureDescr{Name: path, Params: TextureDescrParams{format: FormatRGBA8Srgb}}
}

// TextureWithBytes describes a texture from inline pixel bytes. copyData
// snapshots pixels when true; when false, the caller must keep them unchanged
// until the recorded frame is consumed or dropped. mipmaps generates a full mip
// chain at bake time for smoother minification.
func TextureWithBytes(width, height int, format TextureFormat, pixels []byte, copyData, mipmaps bool) TextureDescr {
	return TextureDescr{
		Blob: assets.NewBlob(pixels),
		Params: TextureDescrParams{
			width: width, height: height, format: format, copyData: copyData, mipmaps: mipmaps,
		},
	}
}
