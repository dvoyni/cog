package types

import (
	"errors"
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
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

// ErrNoArea reports a Shape that encloses nothing, so that no density gives it
// a mass: a circle or a segment of radius 0, a Poly whose Polygon is empty, or
// a Polygon written by hand that is degenerate or wound against Chipmunk's
// winding. It is a sentinel a caller compares with errors.Is.
var ErrNoArea = errors.New("ecsphysics2d: the Shape has no area, so no density gives it a mass")

// NewDynamicForShape is the Dynamic body a Shape of that density makes, in
// kg/m², with the two Damping rates in 1/s, and the Shape moved so that its
// centre of gravity is its local origin — which is what Position is.
//
// It is cp's AccumulateMassFromShapes in its one-shape case, one Shape per Body
// being the port's rule. The mass is density times the area, and the Moment of
// inertia is about the centroid, from the same helpers cp's shapes use:
// AreaForCircle and MomentForCircle; AreaForSegment and MomentForBox over the
// capsule's length and width, which is cp's segment and is MomentForSegment's
// formula; AreaForPoly, MomentForPoly and CentroidForPoly for the three polygon
// kinds. Every kind's area takes its radius. MomentForPoly does not, in cp or
// in C, so a rounded polygon's Moment is its sharp outline's scaled up to the
// rounded mass. The Body is built through NewDynamic and its checks apply.
//
// The Shape and the Polygon go in and come back as NewPolygonShape returns them
// and PolygonVerts takes them. What comes back is recentred: a circle's offset
// becomes zero, and a segment's endpoints and a polygon's vertices are shifted
// by the centroid. A segment's neighbour tangents are relative to its endpoints
// and stay as they are, and the material, the two collision fields and Sensor
// are carried over. An inline kind ignores the Polygon and returns the zero
// Polygon.
//
// The caller's Polygon is never written. A Poly comes back with a new vertex
// List, which allocates; this is a constructor, called at spawn and never on
// the hot path. The inline kinds allocate nothing.
//
// Recentring moves the geometry in the Body's frame, so to leave it where it
// was the app places the Body at the old origin plus the centroid. For a Body
// spawned at origin with an Angle of 0, that is the one line
//
//	place := Position{Current: origin.Add(centroid), Previous: origin.Add(centroid)}
//
// where centroid is the circle's offset, the midpoint of the segment's two
// endpoints, or CentroidForPoly of the outline the polygon was built from. A
// Body spawned turned adds centroid.Rotate(m.ForAngle(angle)) instead.
//
// A Shape with no area is refused with ErrNoArea, a density that is not
// positive and finite with ErrBadDensity, and a mass or a Damping rate
// NewDynamic refuses with that error. Every refusal returns the zero Dynamic —
// which moves under nothing — and the Shape and the Polygon exactly as given.
func NewDynamicForShape(shape Shape, polygon Polygon, density, damping, angularDamping float64) (Dynamic, Shape, Polygon, error) {
	if !(density > 0) || math.IsInf(density, 1) {
		return Dynamic{}, shape, polygon, ErrBadDensity{Density: density}
	}

	var (
		area, unitMoment float64
		centroid         m.Vec2d
		verts            []m.Vec2d
	)
	switch shape.Kind {
	case ShapeCircle:
		centroid = shape.verts[0]
		area = AreaForCircle(0, shape.Radius)
		unitMoment = MomentForCircle(1, 0, shape.Radius, m.Vec2d{})
	case ShapeSegment:
		a, b := shape.verts[0], shape.verts[1]
		centroid = a.Lerp(b, 0.5)
		area = AreaForSegment(a, b, shape.Radius)
		unitMoment = MomentForBox(1, a.Distance(b)+2*shape.Radius, 2*shape.Radius)
	case ShapeTri, ShapeQuad, ShapePoly:
		if shape.Kind == ShapePoly {
			verts = PolygonVerts(make([]m.Vec2d, 0, polygon.Verts.Len()), shape, polygon)
		} else {
			verts = shape.verts[:polyCount(shape, nil)]
		}
		var ok bool
		if centroid, ok = CentroidForPoly(verts); !ok {
			return Dynamic{}, shape, polygon, ErrNoArea
		}
		area = AreaForPoly(verts, shape.Radius)
		unitMoment = MomentForPoly(1, verts, centroid.Negate(), shape.Radius)
	}
	// NaN fails the first test, so a NaN radius is refused here too.
	if !(area > 0) || math.IsInf(area, 1) {
		return Dynamic{}, shape, polygon, ErrNoArea
	}

	mass := density * area
	body, err := NewDynamic(mass, mass*unitMoment, damping, angularDamping)
	if err != nil {
		return Dynamic{}, shape, polygon, err
	}

	recentred := shape
	switch shape.Kind {
	case ShapeCircle:
		recentred.verts[0] = m.Vec2d{}
	case ShapeSegment:
		recentred.verts[0] = shape.verts[0].Sub(centroid)
		recentred.verts[1] = shape.verts[1].Sub(centroid)
	case ShapeTri, ShapeQuad:
		for i := range polyCount(shape, nil) {
			recentred.verts[i] = shape.verts[i].Sub(centroid)
		}
	case ShapePoly:
		// verts is this call's own copy, so shifting it writes nothing the
		// caller holds, and ListOf copies it once more into the new List.
		for i := range verts {
			verts[i] = verts[i].Sub(centroid)
		}
		return body, recentred, Polygon{Verts: ecs.ListOf(verts)}, nil
	}
	return body, recentred, Polygon{}, nil
}
