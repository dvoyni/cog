package types

// CaptureDesc names one colour target to read back. Screen selects the frame
// buffer, which only the backend can resolve; Texture names any other colour
// texture, and zero means none.
//
// A capture always reads mip 0, layer 0. Texture is a TextureID rather than a
// TextureViewID because a texture-to-buffer copy names a texture, and because
// TextureTransition.Texture already names one.
type CaptureDesc struct {
	Screen  bool
	Texture TextureID
}
