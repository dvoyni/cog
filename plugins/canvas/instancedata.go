package canvas

import (
	"unsafe"

	"github.com/dvoyni/cog/libs/m"
)

// instanceData mirrors canvas.wgsl's CanvasInstanceData without a material
// argument: the data of one instance canvas draws, read at its
// instance_index, 144 bytes, WGSL's size of the struct, the vec4<f32>
// fields first, where they align to 16 with no padding, its size a
// multiple of their alignment.
type instanceData struct {
	tint   [4]float32
	region [4]float32
	// crop is the part of the unit square an instance drawing all of it
	// draws: its left, top, right and bottom edges.
	crop [4]float32
	// sliceFrame and sliceSquare are a sliced sprite's centre cell, within
	// its frame and within the unit square, each its left, top, right and
	// bottom edges as fractions.
	sliceFrame  [4]float32
	sliceSquare [4]float32
	transform   m.Mat2x3
	page        uint32
	part        shapePart
	// size, width and radius are in the entry's own space, the unit square
	// scaled by size, which transform maps into the world.
	size   m.Vec2
	width  float32
	radius float32
	// decode is 1 where canvas_sample decodes the image's colour from sRGB
	// as it samples it, as an image of a texture whose format encodes
	// nothing marked sRGB is, and 0 otherwise.
	decode uint32
	mode   instanceMode
	// tiles is how many of a tiled sprite's tiles span the unit square
	// along each axis.
	tiles m.Vec2
}

// instanceDataSize is the size of canvas's fixed part of an instance's data,
// instanceData's, which is WGSL's of CanvasInstanceData with no argument.
const instanceDataSize = uint32(unsafe.Sizeof(instanceData{}))

// instanceMode mirrors canvas.wgsl's CANVAS_MODE constants: how
// canvas_sample maps a point of its instance's unit square into its frame.
type instanceMode uint32

const (
	// instanceModeStretched maps the square onto the frame: a shape, and
	// a Stretched sprite.
	instanceModeStretched instanceMode = iota
	// instanceModeSliced maps it piecewise, by sliceSquare onto sliceFrame.
	instanceModeSliced
	// instanceModeSlicedHollow maps it as instanceModeSliced does, its
	// centre cell drawing nothing.
	instanceModeSlicedHollow
	// instanceModeTiled maps it onto tiles of the frame, wrapped.
	instanceModeTiled
)

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
// region, a converted image's output placed within its frame, decoding it
// where it is to be, sliced or tiled as its draw mode says, its zero tint
// white and premultiplied, and a zero transform read as the identity.
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
	instance.transform, instance.size = sprite.findSquare(image)
	texelScale := sprite.TexelScale
	if texelScale <= 0 {
		texelScale = 1
	}
	// A negative size mirrors the square, its corners and tiles the same.
	squareSize := instance.size.Abs()
	switch sprite.DrawMode {
	case Sliced:
		instance.mode = instanceModeSliced
		if image.options.SliceHollow {
			instance.mode = instanceModeSlicedHollow
		}
		instance.sliceFrame, instance.sliceSquare = image.options.Slice.findSliceEdges(image.size, squareSize, texelScale)
	case Tiled:
		instance.mode = instanceModeTiled
		instance.tiles = squareSize.Div(image.size.MulS(texelScale))
	}
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
