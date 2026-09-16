package internal

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecsphysics2d"
)

// The defaults a zero Config field takes. Every one of them is documented on
// the Config field it fills, with what it is in cp and what changed.
const (
	defaultIterations     = 10
	defaultSlop           = 0.005
	defaultBias           = 6.32
	defaultPersistence    = 0.05
	defaultStaticCellSize = 2.0
	defaultBodyCellSize   = 2.0
)

// settings is the Config with every zero field filled in, resolved once at
// registration and never written again. It is not a kernel resource and never
// becomes one: a resource an app System declared write on would conflict with
// Solve for the whole frame, so what the Systems close over is this value.
type settings struct {
	iterations     int
	slop           float64
	bias           float64
	persistence    float64
	staticCellSize float64
	bodyCellSize   float64
	seed           uint64
}

// persistenceTicks is how many ticks of a step of h seconds the persistence
// window is, which is what cp counts directly.
//
// It is stored in seconds and converted here because 3 ticks is 0.05 s at 60 Hz
// and 0.1 s at 30, and the second is not what cp intends: it is a hysteresis
// window, not a frame count. At least one tick, so that a window shorter than a
// step still reports the end of a Contact.
func (s settings) persistenceTicks(h float64) int {
	if !(h > 0) {
		return 1
	}
	return max(1, int(math.Round(s.persistence/h)))
}

// resolveConfig reads the plugin's configuration value and fills every zero
// field from the defaults. A value of another type is refused rather than
// ignored: a caller that wrote one meant to configure this plugin.
func resolveConfig(value any) (settings, error) {
	resolved := settings{
		iterations:     defaultIterations,
		slop:           defaultSlop,
		bias:           defaultBias,
		persistence:    defaultPersistence,
		staticCellSize: defaultStaticCellSize,
		bodyCellSize:   defaultBodyCellSize,
	}
	if value == nil {
		return resolved, nil
	}
	given, ok := value.(ecsphysics2d.Config)
	if !ok {
		return settings{}, ecsphysics2d.ErrInvalidConfig{Got: value}
	}
	if given.Iterations != 0 {
		resolved.iterations = given.Iterations
	}
	if given.Slop != 0 {
		resolved.slop = given.Slop
	}
	if given.Bias != 0 {
		resolved.bias = given.Bias
	}
	if given.Persistence != 0 {
		resolved.persistence = given.Persistence
	}
	if given.StaticCellSize != 0 {
		resolved.staticCellSize = given.StaticCellSize
	}
	if given.BodyCellSize != 0 {
		resolved.bodyCellSize = given.BodyCellSize
	}
	// The seed is taken as given, zero included: zero is an ordinary seed and
	// the caller naming none is not the caller asking for an arbitrary one.
	resolved.seed = given.Seed
	return resolved, nil
}
