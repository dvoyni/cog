package gfx

// TextureDescr describes a texture by resource path (TextureWithResource),
// inline pixel bytes (TextureWithBytes), or a texture returned by
// ResourceQueue.BakeTexture.
type TextureDescr struct {
	source        textureSource
	path          string
	width, height int
	format        TextureFormat
	pixels        []byte
	copyData      bool
	mipmaps       bool
	id            TextureID
}

// ID returns the baked texture identifier, or 0 when the descriptor is not baked.
func (t TextureDescr) ID() TextureID { return t.id }

// Path returns the resource path for a TextureWithResource descriptor (empty otherwise).
func (t TextureDescr) Path() string { return t.path }

// Size returns the texture's dimensions in texels. A texture loaded from a
// resource path reports zero until it is baked, since only the file knows.
func (t TextureDescr) Size() (width, height int) { return t.width, t.height }

// Format returns the texture's pixel format. A texture loaded from a resource
// path is always sRGB, because the loader decodes PNG and JPEG and both are
// gamma-encoded by definition.
func (t TextureDescr) Format() TextureFormat { return t.format }

// Mipmaps reports whether a full mip chain is generated when the texture is
// baked.
func (t TextureDescr) Mipmaps() bool { return t.mipmaps }

// PixelBytes reports how many bytes of inline pixel data the descriptor
// carries, and zero for a texture named by path or already baked. The pixels
// themselves stay inside the descriptor - a caller wanting to know that an
// upload is a megabyte should not have to hold the megabyte to find out.
func (t TextureDescr) PixelBytes() int { return len(t.pixels) }

// textureSource selects how a TextureDescr is resolved.
type textureSource int

const (
	TextureSourceResource textureSource = iota
	TextureSourceBytes
	TextureSourceBaked
)

// TextureWithResource describes a texture loaded from storage.FileSystem. It is
// always sRGB: the loader decodes PNG and JPEG, both of which are gamma-encoded
// by definition, so there is nothing for a caller to choose and a caller that
// chose wrong would be a silently wrong picture. A data map - normals,
// metallic-roughness, occlusion - is not a picture and does not come through
// here; it comes through TextureWithBytes, which does take a format.
func TextureWithResource(path string) TextureDescr {
	return TextureDescr{source: TextureSourceResource, path: path, format: FormatRGBA8Srgb}
}

// TextureWithBytes describes a texture from inline pixel bytes. copyData
// snapshots pixels when true; when false, the caller must keep them unchanged
// until the recorded frame is consumed or dropped. mipmaps generates a full mip
// chain at bake time for smoother minification.
func TextureWithBytes(width, height int, format TextureFormat, pixels []byte, copyData, mipmaps bool) TextureDescr {
	return TextureDescr{
		source: TextureSourceBytes, width: width, height: height, format: format,
		pixels: pixels, copyData: copyData, mipmaps: mipmaps,
	}
}
