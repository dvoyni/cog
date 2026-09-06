package scene

import "github.com/dvoyni/cog/m"

// The debug vocabulary: Box, Sphere, Plane, Line3D and WireBox, the scene twin
// of canvas's FillRect, StrokeRect and Line. Each is sugar over one of scene's
// own unit meshes and the bundled PBR, so a caller can draw a shape with no
// assets at all: no mesh handle, no material, no shader.
//
// Box, Sphere and Plane are lit. Line3D and WireBox are self-lit — base colour
// black, emissiveFactor the given colour, through the same shader — so a debug
// line stays visible in a frame with no sun, which is precisely the frame
// being debugged.
//
// A shape with nothing to draw — a sphere of zero radius, a plane with a zero
// side, a line of zero length or zero thickness — records nothing at all rather
// than a collapsed instance. It is not reported: the request was for something
// of no size, and that is what was drawn.

// Box records a unit cube at transform, lit by the camera's sun and ambient.
func (q *opQueue) Box(layers LayerMask, transform Transform, color m.Color) {
	q.calls = append(q.calls, Op{Kind: OpBox, Layers: layers, Transform: transform, Color: color})
	q.draw(drawRecord{shape: shapeBox, layers: layers, transform: transform, color: color})
}

// Sphere records a lit sphere of the given radius about center. Its
// tessellation is fixed: a 16 x 12 UV sphere, 352 triangles, smooth-shaded. A
// sphere that needs to be smoother or cheaper than that is a Mesh.
func (q *opQueue) Sphere(layers LayerMask, center m.Vec3, radius float32, color m.Color) {
	q.calls = append(q.calls, Op{Kind: OpSphere, Layers: layers, Center: center, Radius: radius, Color: color})
	if radius <= 0 {
		return
	}
	q.draw(drawRecord{
		shape: shapeSphere, layers: layers, color: color,
		transform: Transform{Position: center, Scale: radius},
	})
}

// Plane records a lit horizontal quad centred on center, size.X across X and
// size.Y across Z, facing +Y. It is two-sided: a ground plane seen from below
// renders rather than vanishes, which is what a debug shape is for.
func (q *opQueue) Plane(layers LayerMask, center m.Vec3, size m.Vec2, color m.Color) {
	q.calls = append(q.calls, Op{Kind: OpPlane, Layers: layers, Center: center, Size: m.Vec3{X: size.X, Z: size.Y}, Color: color})
	if size.X <= 0 || size.Y <= 0 {
		return
	}
	q.draw(drawRecord{
		shape: shapePlane, layers: layers, color: color,
		transform: Transform{Position: center},
		stretch:   m.Vec3{X: size.X, Y: 1, Z: size.Y},
	})
}

// Line3D records a self-lit line from start to end.
//
// A line is a long thin box, not a line-list primitive: WebGPU has no
// line-width control, so a GPU line rasterises one physical pixel wide and all
// but vanishes on a hidpi display. A stretched box has caller-controlled
// thickness and keeps scene to one shader and one topology, batching with
// everything else. The cost is that thickness is a world-space size, so a
// distant line thins out on screen the way any object does; a line that must
// stay a fixed number of pixels wide is not this call.
//
// Scene builds the non-uniform matrix itself, so the scalar Scale on Transform
// is untouched at the API surface, and the packed instance carries
// SCENE_NONUNIFORM for the shader's inverse-transpose normal path.
func (q *opQueue) Line3D(layers LayerMask, start, end m.Vec3, thickness float32, color m.Color) {
	q.calls = append(q.calls, Op{Kind: OpLine3D, Layers: layers, Start: start, End: end, Thickness: thickness, Color: color})
	transform, length, ok := lineTransform(start, end)
	if !ok || thickness <= 0 {
		return
	}
	q.draw(drawRecord{
		shape: shapeBox, layers: layers, color: color, selfLit: true,
		transform: transform,
		stretch:   m.Vec3{X: length, Y: thickness, Z: thickness},
	})
}

// WireBox records the twelve edges of an axis-aligned box of the given size
// about center as self-lit lines, the way StrokeRect outlines a rectangle.
// Each edge is its own draw, culled on its own, so a pass reports a wire box as
// twelve recorded draws; Ops reports the one call. Edges extend half a
// thickness past each corner so the corners close.
func (q *opQueue) WireBox(layers LayerMask, center, size m.Vec3, thickness float32, color m.Color) {
	q.calls = append(q.calls, Op{Kind: OpWireBox, Layers: layers, Center: center, Size: size, Thickness: thickness, Color: color})
	if thickness <= 0 || size.X < 0 || size.Y < 0 || size.Z < 0 || size == (m.Vec3{}) {
		return
	}
	half := size.MulS(0.5)
	edge := func(position, stretch m.Vec3) {
		q.draw(drawRecord{
			shape: shapeBox, layers: layers, color: color, selfLit: true,
			transform: Transform{Position: center.Add(position)},
			stretch:   stretch,
		})
	}
	for _, sy := range [2]float32{-1, 1} {
		for _, sz := range [2]float32{-1, 1} {
			edge(m.Vec3{Y: sy * half.Y, Z: sz * half.Z}, m.Vec3{X: size.X + thickness, Y: thickness, Z: thickness})
		}
	}
	for _, sx := range [2]float32{-1, 1} {
		for _, sz := range [2]float32{-1, 1} {
			edge(m.Vec3{X: sx * half.X, Z: sz * half.Z}, m.Vec3{X: thickness, Y: size.Y + thickness, Z: thickness})
		}
	}
	for _, sx := range [2]float32{-1, 1} {
		for _, sy := range [2]float32{-1, 1} {
			edge(m.Vec3{X: sx * half.X, Y: sy * half.Y}, m.Vec3{X: thickness, Y: thickness, Z: size.Z + thickness})
		}
	}
}

// lineTransform places a unit box so that its local X axis runs from start to
// end: the midpoint, and the rotation that carries +X onto the line's
// direction. It also returns the line's length, which is the X stretch the
// box needs. A zero-length line has no direction and reports !ok.
func lineTransform(start, end m.Vec3) (Transform, float32, bool) {
	direction := end.Sub(start)
	length := direction.Length()
	if length == 0 {
		return Transform{}, 0, false
	}
	x := direction.DivS(length)
	// Any unit vector perpendicular to x serves as the box's local Y; the
	// helper axis only has to avoid being parallel to x.
	helper := m.Vec3{Y: 1}
	if abs32(x.Y) > 0.9 {
		helper = m.Vec3{X: 1}
	}
	y := helper.Cross(x).Normalize()
	z := x.Cross(y)
	basis := m.Mat3{x.X, x.Y, x.Z, y.X, y.Y, y.Z, z.X, z.Y, z.Z}
	return Transform{
		Position: start.Add(end).MulS(0.5),
		Rotation: m.QuatFromMat3(basis),
	}, length, true
}

func abs32(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}
