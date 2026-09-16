package ecsphysics2d

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// Name is the physics plugin's kernel name, the owner of every Component Store
// it registers, and the name a plugin whose Systems lock one of those Stores
// declares a dependency on. It is also the key its Config arrives under.
const Name kernel.PluginName = "ecsphysics2d"

// The four Systems the step is, in cp's own order —
// Integrate → Index → Detect → Solve — each chained After the one before it by
// the plugin itself, so an app never has to know the order to stay out of it.
// Positions integrate first, which is cp's order and not Box2D's, and is why a
// Force written this tick moves the Body next tick.
//
// They are exported so an app orders its own Systems against them. There is no
// physics group and no phase of its own: the four sit in the ordinary phase,
// already after input's First.
type (
	// IntegrateOnUpdate moves every Body with a Velocity, Kinematic ones
	// included, by that Velocity over one step. Gameplay that adds Force or
	// writes a Body's Position or Velocity orders itself
	// Before[IntegrateOnUpdate].
	IntegrateOnUpdate kernel.Subscription[app.UpdateEvent]

	// IndexOnUpdate rebuilds the two spatial indices from the positions
	// Integrate has just written. A query wanting exactly this tick's positions
	// runs Before[IntegrateOnUpdate]; one wanting the indices runs after this.
	IndexOnUpdate kernel.Subscription[app.UpdateEvent]

	// DetectOnUpdate finds the tick's Contacts through the indices. An app's
	// filter Systems — cp's Begin and PreSolve — run
	// After[DetectOnUpdate]().Before[SolveOnUpdate]().
	DetectOnUpdate kernel.Subscription[app.UpdateEvent]

	// SolveOnUpdate is the indivisible half of the step: it integrates
	// velocities and, once there is one, runs the impulse solver around that.
	// An app's reaction Systems — cp's PostSolve — run after it.
	SolveOnUpdate kernel.Subscription[app.UpdateEvent]
)
