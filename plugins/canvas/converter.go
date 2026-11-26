package canvas

import (
	"image"
	imagedraw "image/draw"
	"io/fs"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// Converter converts an image's texels on the CPU before canvas packs it,
// as ImageOptions.Convert: a trim, a palette swap, an outline's extraction,
// or DistanceField's bake. Its implementations are comparable values, plain
// structs, and canvas keeps an atlas for each, by value, holding texels of
// the format it answers: images of equal converters share that atlas and
// their texels, so their entries batch together, and those of unequal ones,
// DistanceField{Spread: 8} and DistanceField{Spread: 16} among them, draw
// from atlases of their own. One that is not comparable is refused.
//
// Format answers the format of the atlas its output is packed into, for an
// image of space: R8Unorm, RG8Unorm, RGBA8Unorm, RGBA8UnormSrgb, BGRA8Unorm
// or BGRA8UnormSrgb, as an atlas holds, any other panicking as the atlas is
// made. A trim's is its space's RGBA8 format, RGBA8UnormSrgb for
// ColorSpaceSrgb and RGBA8Unorm for ColorSpaceLinear, the plain atlas's,
// and its sprites still batch apart from plain sprites and shapes, its
// atlas its own.
//
// Convert is called with each frame apart, its bounds beginning at 0, 0,
// its texels straight alpha as the file holds them, encoded as space says,
// in the job that first draws the frame. It answers its output: texels in
// the format Format answers for space, row by row, each texel's bytes after
// the one before with no padding, colour premultiplied, by alpha in linear,
// so an sRGB texel's colour is decoded, multiplied and encoded again, as
// m.NewColorSrgb8, Premultiply and Srgb8 do; and their bounds within the
// frame, so a trimmed sprite stays where it was, and a grown image's bounds
// begin above and left of 0, 0. Empty bounds draw nothing. Texels of
// another length than their bounds hold panic as they are put into the
// atlas, or, for an image drawn from a texture of its own, are refused by
// render, which reports it.
//
// An image's size and pivot, a Sprite's Size, and Assets.ImageSize measure
// the frame, its input, so a DistanceField of a sprite, given the sprite's
// transform and size, lines up with it; a Sprite's Crop is of the output.
// canvas reads and decodes the file itself, rather than through render's
// loader; a file it cannot decode loads through render, unconverted, which
// draws its magenta and reports it. A Converter that is not comparable, or
// one of an image of a render texture, is refused: the image is drawn
// unconverted, reported once as ReportKindConverterRefused.
type Converter interface {
	Format(space ColorSpace) render.TextureFormat
	Convert(frame *image.NRGBA, space ColorSpace) (texels []byte, bounds image.Rectangle)
}

// convertFrame answers the output of converter over the frame of picture,
// a file decoded, that lies at frameRect, encoded as space says: its
// texels and their bounds within the frame.
func convertFrame(picture image.Image, frameRect m.Recti, converter Converter, space ColorSpace) (texels []byte, bounds image.Rectangle) {
	frame := image.NewNRGBA(image.Rect(0, 0, frameRect.Width, frameRect.Height))
	imagedraw.Draw(frame, frame.Rect, picture, image.Point{X: picture.Bounds().Min.X + frameRect.X, Y: picture.Bounds().Min.Y + frameRect.Y}, imagedraw.Src)
	return converter.Convert(frame, space)
}

// decodeImageFile answers the image file at path in files, decoded in any
// format render loads, or nil when it cannot be read or decoded, or holds
// no texel, so it loads through render, which draws its magenta and
// reports it.
func decodeImageFile(files fs.FS, path string) image.Image {
	file, err := files.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	picture, _, err := image.Decode(file)
	if err != nil || picture.Bounds().Empty() {
		return nil
	}
	return picture
}
