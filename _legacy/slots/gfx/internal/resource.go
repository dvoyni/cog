package internal

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"

	"github.com/dvoyni/cog/libs/assets"
)

// DecodeTexture turns an encoded image into the straight RGBA run a bake wants.
// It takes the bytes rather than a path because the read is the Library's: by
// the time a texture load reaches here the file has been opened, read and found,
// and what is left is the decode - which is the loader's own.
func DecodeTexture(data assets.Blob) (width, height int, pixels []byte, ok bool) {
	decoded, _, err := image.Decode(bytes.NewReader(data.Data()))
	if err != nil {
		return 0, 0, nil, false
	}
	bounds := decoded.Bounds()
	rgba := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(rgba, rgba.Bounds(), decoded, bounds.Min, draw.Src)
	return rgba.Bounds().Dx(), rgba.Bounds().Dy(), rgba.Pix, true
}
