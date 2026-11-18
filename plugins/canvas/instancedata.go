package canvas

import "github.com/dvoyni/cog/libs/m"

// instanceData mirrors canvas.wgsl's CanvasInstanceData without a material
// argument: the data of one instance canvas draws, read at its
// instance_index, 64 bytes, WGSL's size of the struct, the vec4<f32>
// fields first, where they align to 16 with no padding.
type instanceData struct {
	tint      [4]float32
	region    [4]float32
	transform m.Mat2x3
	page      uint32
	_         uint32
}

// makeShapeInstanceData answers the instance data of shape, a fill over the
// white texel: its rectangle mapped by its transform, a zero transform read
// as the identity, and its tint premultiplied.
func makeShapeInstanceData(shape Shape) instanceData {
	transform := shape.Transform
	if transform == (m.Mat2x3{}) {
		transform = m.Translation2x3(0, 0)
	}
	// The rectangle's own transform, column by column: the unit square
	// scaled to its size, then moved to its origin.
	rectangle := m.Mat2x3{shape.Rect.Width, 0, 0, shape.Rect.Height, shape.Rect.X, shape.Rect.Y}
	tint := shape.Tint
	return instanceData{
		tint:      [4]float32{tint.R * tint.A, tint.G * tint.A, tint.B * tint.A, tint.A},
		region:    [4]float32{0, 0, 1, 1},
		transform: transform.Mul(rectangle),
	}
}
