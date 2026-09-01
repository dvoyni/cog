package gfx

import (
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"

	"github.com/dvoyni/cog/storage"
)

func loadTextureResource(filesystem storage.ReadFS, name string) (width, height int, pixels []byte, ok bool) {
	file, err := filesystem.Open(name)
	if err != nil {
		return 0, 0, nil, false
	}
	defer file.Close()

	decoded, _, err := image.Decode(file)
	if err != nil {
		return 0, 0, nil, false
	}
	bounds := decoded.Bounds()
	rgba := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(rgba, rgba.Bounds(), decoded, bounds.Min, draw.Src)
	return rgba.Bounds().Dx(), rgba.Bounds().Dy(), rgba.Pix, true
}

func loadShaderResource(filesystem storage.ReadFS, name string) (code []byte, ok bool) {
	file, err := filesystem.Open(name)
	if err != nil {
		return nil, false
	}
	defer file.Close()

	code, err = io.ReadAll(file)
	return code, err == nil
}
