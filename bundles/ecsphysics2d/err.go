package ecsphysics2d

import (
	"github.com/dvoyni/cog/bundles/ecsphysics2d/internal"
)

// ErrBadMass reports a mass that is zero, negative, NaN, or infinite, none of
// which a Dynamic body may have: the first three would store an infinite or a
// NaN inverse mass, and an infinite mass would be a Dynamic body nothing can
// push, which is said by having no Dynamic at all.
type ErrBadMass = internal.ErrBadMass

// ErrBadMoment reports a Moment of inertia that is zero, negative or NaN. An
// infinite Moment is not among them: it is legal, and means a Body that does
// not turn.
type ErrBadMoment = internal.ErrBadMoment

// ErrBadDamping reports a Damping rate that is negative, NaN or infinite, and
// says which of a Body's two rates it was. Damping is a rate per second, and a
// negative one would amplify velocity every tick until it was an infinity.
type ErrBadDamping = internal.ErrBadDamping

// ErrBadDensity reports a density, in kg/m², that is zero, negative, NaN or
// infinite, which NewDynamicForShape refuses before it computes a mass.
type ErrBadDensity = internal.ErrBadDensity

// ErrNoArea reports a Shape NewDynamicForShape cannot give a mass because it
// encloses nothing: a circle or a segment of radius 0, a Poly whose Polygon is
// empty, or a Polygon written by hand that is degenerate or wound the wrong
// way. It is a sentinel a caller compares with errors.Is.
var ErrNoArea = internal.ErrNoArea

// The three ways NewPolygonShape refuses an outline, each a sentinel a caller
// compares with errors.Is so that the failure path allocates nothing. A refused
// outline comes back as a point, which collides with almost nothing and cannot
// be mistaken for what was asked for.
//
// ErrTooFewVertices is an outline of fewer than three vertices.
// ErrDegenerateOutline is one with no area — coincident or collinear vertices —
// which is what makes Chipmunk's CentroidForPoly divide by zero.
// ErrConcaveOutline is one the hull changed, which is the concave case and also
// the redundant one.
var (
	ErrTooFewVertices    = internal.ErrTooFewVertices
	ErrDegenerateOutline = internal.ErrDegenerateOutline
	ErrConcaveOutline    = internal.ErrConcaveOutline
)

// ErrInvalidConfig is returned by the plugin's Register when the config value
// handed to it is neither nil nor an ecsphysics2d.Config.
type ErrInvalidConfig = internal.ErrInvalidConfig
