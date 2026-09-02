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

// textureKey identifies a cached resource texture by path.
type textureKey string

func textureKeyOf(texture TextureDescr) textureKey { return textureKey(texture.path) }

// ID returns the baked texture identifier, or 0 when the descriptor is not baked.
func (t TextureDescr) ID() TextureID { return t.id }

// Path returns the resource path for a TextureWithResource descriptor (empty otherwise).
func (t TextureDescr) Path() string { return t.path }

// textureSource selects how a TextureDescr is resolved.
type textureSource int

const (
	TextureSourceResource textureSource = iota
	TextureSourceBytes
	TextureSourceBaked
)

// TextureWithResource describes a texture loaded from storage.FileSystem.
func TextureWithResource(path string) TextureDescr {
	return TextureDescr{source: TextureSourceResource, path: path}
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
