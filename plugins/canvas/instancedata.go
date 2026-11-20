package canvas

import "github.com/dvoyni/cog/libs/m"

// instanceData mirrors canvas.wgsl's CanvasInstanceData without a material
// argument: the data of one instance canvas draws, read at its
// instance_index, 112 bytes, WGSL's size of the struct, the vec4<f32>
// fields first, where they align to 16 with no padding, and padded at its
// end to a multiple of their alignment.
type instanceData struct {
	tint   [4]float32
	region [4]float32
	// crop is the part of the unit square an instance drawing all of it
	// draws: its left, top, right and bottom edges.
	crop      [4]float32
	transform m.Mat2x3
	page      uint32
	part      shapePart
	// size, width and radius are in the entry's own space, the unit square
	// scaled by size, which transform maps into the world.
	size   m.Vec2
	width  float32
	radius float32
	// decode is 1 where canvas_sample decodes the image's colour from sRGB
	// as it samples it, as an image of a texture whose format encodes
	// nothing marked sRGB is, and 0 otherwise.
	decode uint32
	_      [3]uint32
}

// shapePart mirrors canvas.wgsl's CANVAS_PART constants: which part of its
// entry's unit square an instance draws.
type shapePart uint32

const (
	// shapePartWhole draws the whole unit square: a fill, and every other entry.
	shapePartWhole shapePart = iota
	// shapePartLine draws a band across the unit square's middle, its width's
	// fraction of the square tall, so a hairline is one target pixel.
	shapePartLine
	// shapePartStrokeTop, shapePartStrokeBottom, shapePartStrokeLeft and shapePartStrokeRight
	// draw a stroke's four sides, which do not overlap: the top and the
	// bottom span the square, and the left and the right the height between
	// them.
	shapePartStrokeTop
	shapePartStrokeBottom
	shapePartStrokeLeft
	shapePartStrokeRight
)

// strokeParts are a stroke's four instances' parts.
var strokeParts = [4]shapePart{shapePartStrokeTop, shapePartStrokeBottom, shapePartStrokeLeft, shapePartStrokeRight}

// appendShapeInstances appends to instances the instance data of shape, over
// white, the white texel as drawn, and answers them: a fill's and a line's
// one instance, and a stroke's four. Its tint is premultiplied, and a zero
// transform read as the identity.
func appendShapeInstances(instances []instanceData, shape *Shape, white *drawnImage) []instanceData {
	tint := shape.Tint
	instance := instanceData{
		tint:   [4]float32{tint.R * tint.A, tint.G * tint.A, tint.B * tint.A, tint.A},
		region: white.region,
		crop:   [4]float32{0, 0, 1, 1},
		page:   white.page,
	}
	instance.transform, instance.size = shape.findSquare()
	switch shape.Kind {
	case Line:
		instance.part = shapePartLine
		instance.width = max(shape.Width, 0)
	case Stroke:
		instance.width = max(shape.Width, 0)
		instance.radius = max(shape.Radius, 0)
	default:
		instance.radius = max(shape.Radius, 0)
	}
	if shape.Kind != Stroke {
		return append(instances, instance)
	}
	for _, part := range strokeParts {
		instance.part = part
		instances = append(instances, instance)
	}
	return instances
}

// appendSpriteInstance appends to instances the instance data of sprite, its
// image as drawn image, and answers them: one instance, over its image's
// region, decoding it where it is to be, its zero tint white and
// premultiplied, and a zero transform read as the identity.
func appendSpriteInstance(instances []instanceData, sprite *Sprite, image *drawnImage) []instanceData {
	tint := sprite.Tint
	if tint == (m.Color{}) {
		tint = m.White
	}
	instance := instanceData{
		tint:   [4]float32{tint.R * tint.A, tint.G * tint.A, tint.B * tint.A, tint.A},
		region: image.region,
		crop:   sprite.Crop.findRect(),
		page:   image.page,
	}
	if image.decode {
		instance.decode = 1
	}
	instance.transform, instance.size = sprite.findSquare(image.size, image.pivot)
	return append(instances, instance)
}

// makeLineSquare answers the transform from the unit square onto the
// line from from to to, width wide, and the square's size in the line's own
// space: x along the segment and y across it, the segment through the
// middle of the square. A hairline's, of zero width, is one unit across,
// which the vertex stage narrows to a target pixel. A line of no length
// answers a square of no length, which draws nothing.
func makeLineSquare(from, to m.Vec2, width float32) (m.Mat2x3, m.Vec2) {
	segment := to.Sub(from)
	length := segment.Length()
	thickness := float32(1)
	if width > 0 {
		thickness = width
	}
	// perpendicular is across the segment, as long as the line is thick.
	perpendicular := m.Vec2{Y: thickness}
	if length > 0 {
		perpendicular = m.Vec2{X: -segment.Y, Y: segment.X}.MulS(thickness / length)
	}
	origin := from.Sub(perpendicular.MulS(0.5))
	return m.Mat2x3{segment.X, segment.Y, perpendicular.X, perpendicular.Y, origin.X, origin.Y}, m.Vec2{X: length, Y: thickness}
}
