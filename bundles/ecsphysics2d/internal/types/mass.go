package types

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// The moment and area helpers, ported from cp's everything.go.
//
// cp passes a vertex count beside the vertex slice, which is C's signature
// carried into Go; a Go slice says its own length, so the count is dropped. It
// changes no result: every cp call site passes the slice's own length.

// MomentForCircle is the moment of inertia of a circle of that mass, with r1
// and r2 the inner and outer radii. A solid circle has an inner radius of 0,
// and offset moves it off the centre of gravity.
func MomentForCircle(mass, r1, r2 float64, offset m.Vec2d) float64 {
	return mass * (0.5*(r1*r1+r2*r2) + offset.LengthSquared())
}

// MomentForSegment is the moment of inertia of a segment of that mass from a to
// b with that radius.
func MomentForSegment(mass float64, a, b m.Vec2d, radius float64) float64 {
	offset := a.Lerp(b, 0.5)
	length := b.Distance(a) + 2.0*radius
	return mass * ((length*length+4.0*radius*radius)/12.0 + offset.LengthSquared())
}

// MomentForBox is the moment of inertia of a solid box of that mass.
func MomentForBox(mass, width, height float64) float64 {
	return mass * (width*width + height*height) / 12.0
}

// MomentForPoly is the moment of inertia of a solid polygon of that mass,
// taking its centre of gravity to be its centroid, with offset added to each
// vertex.
//
// radius is accepted and ignored, as it is in cp and in C, whose comment reads
// "TODO account for radius". Keeping the parameter is what makes the day it is
// honoured a change inside this function rather than at every call site.
//
// A two-vertex outline is a segment, which is cp's own special case.
func MomentForPoly(mass float64, verts []m.Vec2d, offset m.Vec2d, radius float64) float64 {
	if len(verts) == 2 {
		return MomentForSegment(mass, verts[0], verts[1], 0)
	}

	var sum1, sum2 float64
	for i := range verts {
		v1 := verts[i].Add(offset)
		v2 := verts[(i+1)%len(verts)].Add(offset)

		a := v2.Cross(v1)
		b := v1.Dot(v1) + v1.Dot(v2) + v2.Dot(v2)

		sum1 += a * b
		sum2 += a
	}

	return (mass * sum1) / (6.0 * sum2)
}

// AreaForCircle is the area of a hollow circle, with r1 and r2 the inner and
// outer radii. A solid circle has an inner radius of 0.
func AreaForCircle(r1, r2 float64) float64 { return math.Pi * math.Abs(r1*r1-r2*r2) }

// AreaForSegment is the area of a segment from a to b fattened by that radius,
// which is a capsule.
func AreaForSegment(a, b m.Vec2d, radius float64) float64 {
	return radius * (math.Pi*radius + 2.0*a.Distance(b))
}

// AreaForPoly is the signed area of a polygon fattened by that radius.
// Clockwise winding gives a positive area, which is Chipmunk's winding for
// polygon Shapes and the reason the Shape constructor enforces it.
func AreaForPoly(verts []m.Vec2d, radius float64) float64 {
	var area, perimeter float64
	for i := range verts {
		v1 := verts[i]
		v2 := verts[(i+1)%len(verts)]

		area += v1.Cross(v2)
		perimeter += v1.Distance(v2)
	}

	return radius*(math.Pi*math.Abs(radius)+perimeter) + area/2.0
}

// CentroidForPoly is the natural centroid of a polygon outline, and reports
// false for a degenerate one.
//
// The sum it divides by is twice the outline's signed area, so a zero-area or
// coincident-vertex outline divides zero by zero. cp does the divide unguarded
// and hands back a NaN that reaches a Body's Position; here the caller learns
// instead, which is what lets the Shape constructor name the failure. The
// returned vector on a failure is the zero vector and not a centroid.
func CentroidForPoly(verts []m.Vec2d) (m.Vec2d, bool) {
	var sum float64
	var vsum m.Vec2d

	for i := range verts {
		v1 := verts[i]
		v2 := verts[(i+1)%len(verts)]
		cross := v1.Cross(v2)

		sum += cross
		vsum = vsum.Add(v1.Add(v2).MulS(cross))
	}

	if sum == 0 {
		return m.Vec2d{}, false
	}
	return vsum.MulS(1.0 / (3.0 * sum)), true
}
