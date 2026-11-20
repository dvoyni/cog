package canvas

import (
	"image"
	imagedraw "image/draw"
	"io/fs"

	"github.com/dvoyni/cog/libs/m"
)

// Converter converts an image's texels on the CPU before canvas packs it,
// as ImageOptions.Convert: a trim, a palette swap, an outline's extraction,
// or DistanceField's bake. Its implementations are comparable values, plain
// structs, so images of equal converters share their texels; one that is
// not is refused. Convert is called with each frame apart, its bounds
// beginning at 0, 0, in the job that first draws the frame, and answers its
// output, placed by its bounds within the frame: a SubImage keeps the
// frame's coordinates, so a trimmed sprite stays where it was, and a grown
// image's bounds begin above and left of 0, 0. An image's size and pivot,
// a Sprite's Size, and Assets.ImageSize measure the frame, its input, so a
// DistanceField of a sprite, given the sprite's transform and size, lines
// up with it; a Sprite's Crop is of the output. canvas reads and decodes
// the file itself, rather than through render's loader, and makes a
// texture of the output to pack; a file it cannot decode loads through
// render, unconverted, which draws its magenta and reports it. A Converter
// that is not comparable, or one of an image of a render texture, is
// refused: the image is drawn unconverted, reported once as
// ReportKindConverterRefused.
type Converter interface {
	Convert(frame image.Image) image.Image
}

// convertFrame answers the output of converter over the frame of picture,
// a file decoded, that lies at frameRect, as straight-alpha texels tight
// row by row, of its bounds within the frame.
func convertFrame(picture image.Image, frameRect m.Recti, converter Converter) *image.NRGBA {
	frame := image.NewNRGBA(image.Rect(0, 0, frameRect.Width, frameRect.Height))
	imagedraw.Draw(frame, frame.Rect, picture, image.Point{X: picture.Bounds().Min.X + frameRect.X, Y: picture.Bounds().Min.Y + frameRect.Y}, imagedraw.Src)
	output := converter.Convert(frame)
	if output == nil {
		return image.NewNRGBA(image.Rectangle{})
	}
	if nrgba, ok := output.(*image.NRGBA); ok && nrgba.Stride == 4*nrgba.Rect.Dx() {
		return nrgba
	}
	texels := image.NewNRGBA(output.Bounds())
	imagedraw.Draw(texels, texels.Rect, output, output.Bounds().Min, imagedraw.Src)
	return texels
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
