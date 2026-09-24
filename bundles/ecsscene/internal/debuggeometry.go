package internal

import (
	"math"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
)

// The debug shapes' geometry, in the shape's local space. Each builder appends
// to the backings it is handed and returns them, and appends nothing for a
// shape with nothing to draw, which is how the Systems tell a degenerate shape
// from a drawable one. The shading ignores normals, but every vertex still
// carries a true one, so a custom Material laid over a shape lights it.

// The sphere's tessellation, the same 16 by 12 model's unit sphere has.
const (
	debugSphereSegments = 16
	debugSphereRings    = 12
)

// appendDebugBox is DebugBox's geometry.
func appendDebugBox(vertices []model.Vertex, indices []uint32, shape DebugBox) ([]model.Vertex, []uint32) {
	size := shape.Size
	if size.X <= 0 || size.Y <= 0 || size.Z <= 0 {
		return vertices, indices
	}
	return appendCuboid(vertices, indices, m.Vec3{}, [3]m.Vec3{
		{X: size.X / 2}, {Y: size.Y / 2}, {Z: size.Z / 2},
	})
}

// appendDebugSphere is DebugSphere's geometry: debugSphereRings+1 rows of
// debugSphereSegments+1 vertices from the north pole down, the extra column
// duplicating the seam, with the triangles at the poles that would have no
// area left out.
func appendDebugSphere(vertices []model.Vertex, indices []uint32, shape DebugSphere) ([]model.Vertex, []uint32) {
	if shape.Radius <= 0 {
		return vertices, indices
	}
	const columns = debugSphereSegments + 1
	base := uint32(len(vertices))
	for ring := 0; ring <= debugSphereRings; ring++ {
		v := float64(ring) / debugSphereRings
		polar := v * math.Pi
		y, radius := float32(math.Cos(polar)), float32(math.Sin(polar))
		for segment := 0; segment <= debugSphereSegments; segment++ {
			u := float64(segment) / debugSphereSegments
			azimuth := u * 2 * math.Pi
			sin, cos := float32(math.Sin(azimuth)), float32(math.Cos(azimuth))
			normal := m.Vec3{X: radius * cos, Y: y, Z: radius * sin}
			vertices = append(vertices, model.Vertex{
				Position: normal.MulS(shape.Radius),
				Normal:   normal,
				Tangent:  m.Vec4{X: -sin, Z: cos, W: 1},
				UV0:      m.Vec2{X: float32(u), Y: float32(v)},
				Color:    m.White,
			})
		}
	}
	for ring := 0; ring < debugSphereRings; ring++ {
		for segment := 0; segment < debugSphereSegments; segment++ {
			a := base + uint32(ring*columns+segment)
			b := a + 1
			c := a + columns
			d := c + 1
			// Rows run north to south and columns eastward, so seen from
			// outside (a, b, d) and (a, d, c) run counter-clockwise.
			if ring != 0 {
				indices = append(indices, a, b, d)
			}
			if ring != debugSphereRings-1 {
				indices = append(indices, a, d, c)
			}
		}
	}
	return vertices, indices
}

// appendDebugPlane is DebugPlane's geometry: one quad facing along the
// normal, wound counter-clockwise seen from that side.
func appendDebugPlane(vertices []model.Vertex, indices []uint32, shape DebugPlane) ([]model.Vertex, []uint32) {
	if shape.Size.X <= 0 || shape.Size.Y <= 0 {
		return vertices, indices
	}
	right, up, normal, ok := debugPlaneAxes(shape.Normal)
	if !ok {
		return vertices, indices
	}
	centre := normal.MulS(-shape.D)
	return appendQuad(vertices, indices, centre, normal,
		right.MulS(shape.Size.X/2), up.MulS(shape.Size.Y/2))
}

// debugPlaneAxes are a plane's in-plane axes and its unit normal: right is
// world +X projected onto the plane, or +Z where the normal is along X, and up
// is normal × right, so right × up is the normal. A normal of no length has no
// plane.
func debugPlaneAxes(normal m.Vec3) (right, up, unit m.Vec3, ok bool) {
	length := normal.Length()
	if length == 0 || math.IsNaN(float64(length)) || math.IsInf(float64(length), 0) {
		return m.Vec3{}, m.Vec3{}, m.Vec3{}, false
	}
	unit = normal.DivS(length)
	reference := m.Vec3{X: 1}
	if abs(unit.X) > 0.999 {
		reference = m.Vec3{Z: 1}
	}
	right = reference.Sub(unit.MulS(reference.Dot(unit))).Normalize()
	return right, unit.Cross(right), unit, true
}

// appendDebugLine is DebugLine's geometry: a box from From to To with a Width
// by Width cross-section, ending exactly at the two points.
func appendDebugLine(vertices []model.Vertex, indices []uint32, shape DebugLine) ([]model.Vertex, []uint32) {
	span := shape.To.Sub(shape.From)
	length := span.Length()
	if length == 0 || shape.Width <= 0 {
		return vertices, indices
	}
	along := span.DivS(length)
	reference := m.Vec3{Y: 1}
	if abs(along.Y) > 0.999 {
		reference = m.Vec3{X: 1}
	}
	// (along, across, third) is right-handed, which appendCuboid's winding
	// assumes.
	across := along.Cross(reference).Normalize()
	third := along.Cross(across)
	half := shape.Width / 2
	return appendCuboid(vertices, indices, shape.From.Add(span.MulS(0.5)), [3]m.Vec3{
		along.MulS(length / 2), across.MulS(half), third.MulS(half),
	})
}

// appendDebugWireBox is DebugWireBox's geometry: twelve axis-aligned edge
// boxes in one mesh, each running half a Width past the corners it joins.
func appendDebugWireBox(vertices []model.Vertex, indices []uint32, shape DebugWireBox) ([]model.Vertex, []uint32) {
	size, width := shape.Size, shape.Width
	if width <= 0 || size.X < 0 || size.Y < 0 || size.Z < 0 || size == (m.Vec3{}) {
		return vertices, indices
	}
	half := size.MulS(0.5)
	extents := [3]float32{half.X, half.Y, half.Z}
	unit := [3]m.Vec3{{X: 1}, {Y: 1}, {Z: 1}}
	// An edge along axis k stands at the four sign combinations of the other
	// two. The axes are taken cyclically, k, k+1, k+2, so each edge's box is
	// right-handed.
	for k := range 3 {
		i, j := (k+1)%3, (k+2)%3
		for _, si := range [2]float32{-1, 1} {
			for _, sj := range [2]float32{-1, 1} {
				centre := unit[i].MulS(si * extents[i]).Add(unit[j].MulS(sj * extents[j]))
				vertices, indices = appendCuboid(vertices, indices, centre, [3]m.Vec3{
					unit[k].MulS(extents[k] + width/2),
					unit[i].MulS(width / 2),
					unit[j].MulS(width / 2),
				})
			}
		}
	}
	return vertices, indices
}

// appendCuboid appends a box centred on centre whose three half-extents are
// the vectors axes, which must be right-handed and at right angles: four
// vertices per face so each face keeps its own flat normal, and every face
// wound counter-clockwise seen from outside.
func appendCuboid(vertices []model.Vertex, indices []uint32, centre m.Vec3, axes [3]m.Vec3) ([]model.Vertex, []uint32) {
	for k := range 3 {
		a, b := axes[(k+1)%3], axes[(k+2)%3]
		normal := axes[k].Normalize()
		// For the + face, a × b runs along axes[k]; for the - face swapping
		// them turns it round.
		vertices, indices = appendQuad(vertices, indices, centre.Add(axes[k]), normal, a, b)
		vertices, indices = appendQuad(vertices, indices, centre.Sub(axes[k]), normal.Negate(), b, a)
	}
	return vertices, indices
}

// appendQuad appends the parallelogram centre ± right ± up as four vertices
// and two triangles, counter-clockwise seen from the side right × up points
// to, which is normal's side.
func appendQuad(vertices []model.Vertex, indices []uint32, centre, normal, right, up m.Vec3) ([]model.Vertex, []uint32) {
	base := uint32(len(vertices))
	tangent := right.Normalize()
	for _, corner := range [4]m.Vec2{{X: -1, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 1}, {X: -1, Y: 1}} {
		vertices = append(vertices, model.Vertex{
			Position: centre.Add(right.MulS(corner.X)).Add(up.MulS(corner.Y)),
			Normal:   normal,
			Tangent:  m.Vec4{X: tangent.X, Y: tangent.Y, Z: tangent.Z, W: 1},
			UV0:      m.Vec2{X: (corner.X + 1) / 2, Y: 1 - (corner.Y+1)/2},
			Color:    m.White,
		})
	}
	return vertices, append(indices, base, base+1, base+2, base, base+2, base+3)
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
