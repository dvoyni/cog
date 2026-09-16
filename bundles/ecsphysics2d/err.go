package ecsphysics2d

import (
	"fmt"

	"github.com/dvoyni/cog/bundles/ecsphysics2d/internal/types"
)

// ErrBadMass reports a mass that is zero, negative, NaN, or infinite, none of
// which a Dynamic body may have: the first three would store an infinite or a
// NaN inverse mass, and an infinite mass would be a Dynamic body nothing can
// push, which is said by having no Dynamic at all.
type ErrBadMass = types.ErrBadMass

// ErrBadMoment reports a Moment of inertia that is zero, negative or NaN. An
// infinite Moment is not among them: it is legal, and means a Body that does
// not turn.
type ErrBadMoment = types.ErrBadMoment

// ErrBadDamping reports a Damping rate that is negative, NaN or infinite, and
// says which of a Body's two rates it was. Damping is a rate per second, and a
// negative one would amplify velocity every tick until it was an infinity.
type ErrBadDamping = types.ErrBadDamping

// ErrInvalidConfig is returned by the plugin's Register when the config value
// handed to it is neither nil nor an ecsphysics2d.Config.
type ErrInvalidConfig struct {
	Got any
}

func (e ErrInvalidConfig) Error() string {
	return fmt.Sprintf("ecsphysics2d: invalid config: want %T, got %T", Config{}, e.Got)
}
