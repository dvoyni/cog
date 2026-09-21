package types

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// The sphere is a 16 x 12 UV sphere of 352 triangles, every vertex on the unit
// sphere with its normal pointing out through it, wound outwards.
func TestUnitSphereIsTheDocumentedTessellation(t *testing.T) {
	vertices, indices := UnitSphereGeometry()
	if len(vertices) != 17*13 {
		t.Fatalf("the unit sphere has %d vertices, want 17 x 13 with the seam duplicated", len(vertices))
	}
	if len(indices) != 352*3 {
		t.Fatalf("the unit sphere has %d triangles, want 352", len(indices)/3)
	}
	for i, vertex := range vertices {
		if length := vertex.Position.Length(); !near(length, 1) {
			t.Fatalf("vertex %d is at distance %v, want 1", i, length)
		}
		if vertex.Normal != vertex.Position {
			t.Fatalf("vertex %d has normal %v at position %v, want them equal", i, vertex.Normal, vertex.Position)
		}
		tangent := m.Vec3{X: vertex.Tangent.X, Y: vertex.Tangent.Y, Z: vertex.Tangent.Z}
		if !near(tangent.Dot(vertex.Normal), 0) {
			t.Fatalf("vertex %d has tangent %v not perpendicular to normal %v", i, tangent, vertex.Normal)
		}
	}
	assertWindsOutwards(t, vertices, indices)
}

// The plane is two 1x1 faces at y = 0, one facing up and one facing down, so
// that a back-face-culled plane renders from either side.
func TestUnitPlaneIsTwoSided(t *testing.T) {
	vertices, indices := UnitPlaneGeometry()
	if len(vertices) != 8 || len(indices) != 12 {
		t.Fatalf("the unit plane has %d vertices and %d indices, want 8 and 12", len(vertices), len(indices))
	}
	up, down := 0, 0
	for i, vertex := range vertices {
		if vertex.Position.Y != 0 || abs32(vertex.Position.X) != 0.5 || abs32(vertex.Position.Z) != 0.5 {
			t.Fatalf("vertex %d is at %v, want a corner of the unit square at y = 0", i, vertex.Position)
		}
		switch vertex.Normal {
		case m.Vec3{Y: 1}:
			up++
		case m.Vec3{Y: -1}:
			down++
		default:
			t.Fatalf("vertex %d has normal %v, want +Y or -Y", i, vertex.Normal)
		}
	}
	if up != 4 || down != 4 {
		t.Fatalf("%d vertices face up and %d down, want 4 and 4", up, down)
	}
	for i := 0; i < len(indices); i += 3 {
		a, b, c := vertices[indices[i]], vertices[indices[i+1]], vertices[indices[i+2]]
		face := b.Position.Sub(a.Position).Cross(c.Position.Sub(a.Position))
		if face.Dot(a.Normal) <= 0 {
			t.Fatalf("triangle %d winds against its normal %v", i/3, a.Normal)
		}
	}
}

// assertWindsOutwards checks every triangle of a convex hull about the origin
// faces away from it and agrees with its own vertex normal.
func assertWindsOutwards(t *testing.T, vertices []Vertex, indices []uint32) {
	t.Helper()
	for i := 0; i < len(indices); i += 3 {
		a := vertices[indices[i]].Position
		b := vertices[indices[i+1]].Position
		c := vertices[indices[i+2]].Position
		face := b.Sub(a).Cross(c.Sub(a))
		if face.LengthSquared() == 0 {
			t.Fatalf("triangle %d has no area", i/3)
		}
		centroid := a.Add(b).Add(c).MulS(1.0 / 3)
		if face.Dot(centroid) <= 0 {
			t.Fatalf("triangle %d faces inwards: normal %v against centroid %v", i/3, face, centroid)
		}
		if face.Dot(vertices[indices[i]].Normal) <= 0 {
			t.Fatalf("triangle %d disagrees with its own vertex normal %v", i/3, vertices[indices[i]].Normal)
		}
	}
}
