package canvas

import (
	"cmp"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// Sprite is an entry that draws an image through a transform about its
// image's pivot. It is a literal, like every other entry:
// canvas.Sprite{Image: hero, Transform: m.Translation2x3(x, y), Layer: l}.
type Sprite struct {
	// Image is what the sprite draws, the zero Image the white texel. Its
	// first draw loads it, drawn that frame; one render cannot load draws
	// render's magenta, reported once by render.
	Image Image
	// Transform maps the sprite's own space into the world, a zero one
	// read as the identity; a negative scale mirrors the sprite about its
	// image's pivot.
	Transform m.Mat2x3
	// Size is the sprite's size in its own space, zero meaning its image's
	// in texels; a zero on one axis keeps the image's aspect ratio. The
	// image's pivot lies at the origin of the sprite's own space.
	Size m.Vec2
	// Tint multiplies the image's colour: straight alpha, premultiplied by
	// canvas, zero meaning white.
	Tint m.Color
	// Material is the 2D material the sprite is drawn with, the zero one
	// the default material.
	Material Material
	// Crop leaves part of the sprite out where it lies, so it keeps its
	// pivot, as a fill bar empties from one side; zero draws all of it.
	Crop Crop
	// Clip is a rectangle of the world outside which the sprite draws
	// nothing, zero meaning none, as a Shape's Clip is.
	Clip m.Rect
	// Filter is how the image's texels are read between their centres,
	// zero meaning Config.DefaultFilter. A change of filter from one entry
	// to the next in sort order breaks a batch, but for the zero Image,
	// which no filter changes.
	Filter Filter
	// Layer and Order are the sprite's sort key, as a Shape's are.
	Layer int32
	Order int32
}

// batchKey answers what the sprite shares with the entries it batches with,
// its image packed into the atlas whose texture is atlas, its filter
// defaultFilter when it names none, and none for the zero Image, the white
// texel, which no filter changes, so it batches with shapes.
func (s *Sprite) batchKey(atlas render.Texture, defaultFilter Filter) batchKey {
	key := batchKey{material: s.Material, atlas: atlas, clip: s.Clip}
	if s.Image != (Image{}) {
		key.filter = cmp.Or(s.Filter, defaultFilter)
	}
	return key
}

// findSquare answers the transform from the unit square onto the sprite in
// the world, its image naturalSize texels and placed about pivot, its own
// transform, a zero one read as the identity, after its size's, and the
// square's size in the sprite's own space: its Size, an axis of zero
// keeping naturalSize's aspect, naturalSize itself for no Size.
func (s *Sprite) findSquare(naturalSize m.Vec2, pivot Pivot) (m.Mat2x3, m.Vec2) {
	size := s.Size
	switch {
	case size.X == 0 && size.Y == 0:
		size = naturalSize
	case size.X == 0:
		size.X = size.Y * naturalSize.X / naturalSize.Y
	case size.Y == 0:
		size.Y = size.X * naturalSize.Y / naturalSize.X
	}
	// The pivot, from the square's top left in the sprite's own space,
	// lands on the origin.
	pivotPoint := pivot.Fraction.AddS(0.5).Mul(size).Add(pivot.Texels.Mul(size).Div(naturalSize))
	square := m.Mat2x3{size.X, 0, 0, size.Y, -pivotPoint.X, -pivotPoint.Y}
	if s.Transform == (m.Mat2x3{}) {
		return square, size
	}
	return s.Transform.Mul(square), size
}

// findBounds answers the world rectangle the sprite draws within, its image
// naturalSize texels and placed about pivot: the bounding box of the part
// of its unit square its crop leaves, in the world.
func (s *Sprite) findBounds(naturalSize m.Vec2, pivot Pivot) m.Rect {
	square, _ := s.findSquare(naturalSize, pivot)
	cropRect := s.Crop.findRect()
	return square.TransformRect(m.Rect{X: cropRect[0], Y: cropRect[1], Width: cropRect[2] - cropRect[0], Height: cropRect[3] - cropRect[1]})
}
