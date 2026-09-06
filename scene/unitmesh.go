package scene

import (
	"math"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// unitShape names one of scene's own meshes, the ones the debug vocabulary is
// sugar over. The zero value is shapeNone, so a draw record that says nothing
// about its shape draws the mesh it names instead - and a draw that names no
// mesh either draws nothing, which is what a rejected mint has to yield.
type unitShape uint8

const (
	shapeNone unitShape = iota
	shapeBox
	shapeSphere
	shapePlane
	shapeCount
)

// The sphere's tessellation is fixed rather than configurable: 16 segments of
// longitude by 12 rings of latitude, 221 vertices and 352 triangles once the
// degenerate triangles at the two poles are dropped. It is a debug shape; a
// sphere that needs to be smoother or cheaper is a Mesh.
const (
	sphereSegments = 16
	sphereRings    = 12
)

// ensureUnit bakes one of scene's own meshes the first time something draws it
// and returns the same ref forever after. It is lazy because the backend may
// not be ready at startup, and a mesh baked then would either panic or silently
// not exist.
func (l *Lookup) ensureUnit(shape unitShape, bake bakeFunc) MeshRef {
	if l.unit[shape].source != meshNone {
		return l.unit[shape]
	}
	var vertices []Vertex
	var indices []uint32
	var bounds m.Sphere
	switch shape {
	case shapeSphere:
		vertices, indices = unitSphereGeometry()
		// The exact sphere, not the circumsphere of its box, which would be
		// sqrt(3) times too generous.
		bounds = m.Sphere{Radius: 1}
	case shapePlane:
		vertices, indices = unitPlaneGeometry()
		bounds = vertexBounds(vertices)
	default:
		vertices, indices = unitBoxGeometry()
		bounds = vertexBounds(vertices)
	}
	// The layout goes through the same cache a caller's bake uses, so scene's
	// own meshes and a caller's standard-layout mesh share one layout id rather
	// than two that happen to describe the same attributes.
	layoutID, layout, _ := l.layouts.resolve[Vertex]()
	l.unit[shape] = l.bakeMeshNow(meshInput{
		vertices: uploadBytes(vertices), indices: indexBytes(indices),
		vertexCount: len(vertices), indexCount: len(indices),
		topology: gfx.TopologyTriangleList, layout: layout,
		layoutID: layoutID, standard: true, bounds: bounds,
	}, bake)
	return l.unit[shape]
}

// quadFace is one flat square face: where it faces, which way its texture
// runs, and the two axes its corners are laid out along.
type quadFace struct {
	normal, tangent, right, up m.Vec3
}

// appendQuad appends one unit square centred on centre and facing face.normal,
// as four vertices and two triangles wound counter-clockwise when seen from
// the side the normal points to.
func appendQuad(vertices []Vertex, indices []uint32, face quadFace, centre m.Vec3) ([]Vertex, []uint32) {
	corners := [4]m.Vec2{{X: -1, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 1}, {X: -1, Y: 1}}
	base := uint32(len(vertices))
	for _, corner := range corners {
		position := centre.
			Add(face.right.MulS(corner.X * 0.5)).
			Add(face.up.MulS(corner.Y * 0.5))
		vertices = append(vertices, Vertex{
			Position: position,
			Normal:   face.normal,
			Tangent:  m.Vec4{X: face.tangent.X, Y: face.tangent.Y, Z: face.tangent.Z, W: 1},
			UV0:      m.Vec2{X: (corner.X + 1) / 2, Y: 1 - (corner.Y+1)/2},
			Color:    [4]uint8{255, 255, 255, 255},
		})
	}
	return vertices, append(indices, base, base+1, base+2, base, base+2, base+3)
}

// unitBoxGeometry builds the 1x1x1 cube centred on the origin: four vertices
// per face, so every face keeps its own flat normal, and 12 triangles wound
// counter-clockwise when seen from outside.
func unitBoxGeometry() ([]Vertex, []uint32) {
	faces := [6]quadFace{
		{normal: m.Vec3{X: 1}, tangent: m.Vec3{Z: -1}, right: m.Vec3{Z: -1}, up: m.Vec3{Y: 1}},
		{normal: m.Vec3{X: -1}, tangent: m.Vec3{Z: 1}, right: m.Vec3{Z: 1}, up: m.Vec3{Y: 1}},
		{normal: m.Vec3{Y: 1}, tangent: m.Vec3{X: 1}, right: m.Vec3{X: 1}, up: m.Vec3{Z: -1}},
		{normal: m.Vec3{Y: -1}, tangent: m.Vec3{X: 1}, right: m.Vec3{X: 1}, up: m.Vec3{Z: 1}},
		{normal: m.Vec3{Z: 1}, tangent: m.Vec3{X: 1}, right: m.Vec3{X: 1}, up: m.Vec3{Y: 1}},
		{normal: m.Vec3{Z: -1}, tangent: m.Vec3{X: -1}, right: m.Vec3{X: -1}, up: m.Vec3{Y: 1}},
	}
	vertices := make([]Vertex, 0, 24)
	indices := make([]uint32, 0, 36)
	for _, face := range faces {
		vertices, indices = appendQuad(vertices, indices, face, face.normal.MulS(0.5))
	}
	return vertices, indices
}

// unitPlaneGeometry builds the 1x1 square in the XZ plane centred on the
// origin, facing +Y, and its mirror facing -Y at the same place. Two faces
// rather than one because the bundled material culls back faces, and a
// one-sided debug plane seen from below would vanish; the two never fight for
// depth because only the one facing the camera survives the cull.
func unitPlaneGeometry() ([]Vertex, []uint32) {
	faces := [2]quadFace{
		{normal: m.Vec3{Y: 1}, tangent: m.Vec3{X: 1}, right: m.Vec3{X: 1}, up: m.Vec3{Z: -1}},
		{normal: m.Vec3{Y: -1}, tangent: m.Vec3{X: 1}, right: m.Vec3{X: 1}, up: m.Vec3{Z: 1}},
	}
	vertices := make([]Vertex, 0, 8)
	indices := make([]uint32, 0, 12)
	for _, face := range faces {
		vertices, indices = appendQuad(vertices, indices, face, m.Vec3{})
	}
	return vertices, indices
}

// unitSphereGeometry builds the radius-1 UV sphere: sphereRings+1 rows of
// sphereSegments+1 vertices from the north pole down, the extra column
// duplicating the seam so UVs wrap, with smooth normals equal to the position
// and tangents running eastward along each ring. The rows touching a pole
// contribute one triangle per segment instead of two, since the other would
// have two vertices at the pole and no area.
func unitSphereGeometry() ([]Vertex, []uint32) {
	const columns = sphereSegments + 1
	vertices := make([]Vertex, 0, columns*(sphereRings+1))
	for ring := 0; ring <= sphereRings; ring++ {
		v := float64(ring) / sphereRings
		polar := v * math.Pi
		y, radius := float32(math.Cos(polar)), float32(math.Sin(polar))
		for segment := 0; segment <= sphereSegments; segment++ {
			u := float64(segment) / sphereSegments
			azimuth := u * 2 * math.Pi
			sin, cos := float32(math.Sin(azimuth)), float32(math.Cos(azimuth))
			position := m.Vec3{X: radius * cos, Y: y, Z: radius * sin}
			vertices = append(vertices, Vertex{
				Position: position,
				Normal:   position,
				Tangent:  m.Vec4{X: -sin, Z: cos, W: 1},
				UV0:      m.Vec2{X: float32(u), Y: float32(v)},
				Color:    [4]uint8{255, 255, 255, 255},
			})
		}
	}
	indices := make([]uint32, 0, sphereSegments*(2*sphereRings-2)*3)
	for ring := 0; ring < sphereRings; ring++ {
		for segment := 0; segment < sphereSegments; segment++ {
			a := uint32(ring*columns + segment)
			b := a + 1
			c := a + columns
			d := c + 1
			// Rows run north to south and columns eastward (+Z at azimuth 0),
			// so seen from outside (a, b, d) and (a, d, c) run
			// counter-clockwise.
			if ring != 0 {
				indices = append(indices, a, b, d)
			}
			if ring != sphereRings-1 {
				indices = append(indices, a, d, c)
			}
		}
	}
	return vertices, indices
}
