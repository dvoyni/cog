package render

import (
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg" // render loads JPEG textures.
	_ "image/png"  // render loads PNG textures.
	"io/fs"
	"math/bits"
)

// textureKey is the asset key of a loaded texture: the path of its file in
// platform's FS and the options it was acquired with.
type textureKey struct {
	path    string
	options TextureOptions
}

// readHeader answers the descriptor of k's texture from its file's header
// alone: its size, its format by its colour space, and every mip level down
// to one texel with Mipmaps, else one. When the file is missing, in no format
// render decodes, or of no texels, it answers the error, with the descriptor
// of one texel to draw magenta in.
func (k textureKey) readHeader(files fs.FS) (TextureDescriptor, error) {
	descriptor := TextureDescriptor{Label: k.path, Width: 1, Height: 1, Format: k.options.ColorSpace.makeTextureFormat()}.fillDefaults()
	file, err := files.Open(k.path)
	if err != nil {
		return descriptor, err
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return descriptor, err
	}
	if config.Width <= 0 || config.Height <= 0 {
		return descriptor, fmt.Errorf("an image of %d×%d texels", config.Width, config.Height)
	}
	descriptor.Width = uint32(config.Width)
	descriptor.Height = uint32(config.Height)
	if k.options.Mipmaps {
		descriptor.MipLevelCount = uint32(bits.Len32(max(descriptor.Width, descriptor.Height)))
	}
	return descriptor, nil
}

// decodeBody answers the texels of k's file, of 8 bits per channel, red,
// green, blue and alpha, alpha not premultiplied, row by row; an error when
// the file is missing or does not decode, or its size is not width by
// height, which its header said. An opaque picture, whose premultiplied
// texels are its plain ones, is converted along image/draw's fast paths.
func (k textureKey) decodeBody(files fs.FS, width, height uint32) ([]byte, error) {
	file, err := files.Open(k.path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	picture, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}
	bounds := picture.Bounds()
	if bounds.Dx() != int(width) || bounds.Dy() != int(height) {
		return nil, fmt.Errorf("an image of %d×%d texels, where its header said %d×%d", bounds.Dx(), bounds.Dy(), width, height)
	}
	texelCount := bounds.Dx() * bounds.Dy()
	switch typed := picture.(type) {
	case *image.NRGBA:
		if typed.Stride == 4*bounds.Dx() {
			return typed.Pix[:4*texelCount], nil
		}
	case *image.RGBA:
		if typed.Stride == 4*bounds.Dx() && typed.Opaque() {
			return typed.Pix[:4*texelCount], nil
		}
	}
	if opaque, ok := picture.(interface{ Opaque() bool }); ok && opaque.Opaque() {
		texels := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
		draw.Draw(texels, texels.Rect, picture, bounds.Min, draw.Src)
		return texels.Pix, nil
	}
	texels := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(texels, texels.Rect, picture, bounds.Min, draw.Src)
	return texels.Pix, nil
}
