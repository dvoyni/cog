package ecsphysics2d

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// Name is the physics plugin's kernel name, the owner of every Component Store
// it registers, and the name a plugin whose Systems lock one of those Stores
// declares a dependency on. It is also the key its Config arrives under.
const Name kernel.PluginName = "ecsphysics2d"

// The five Systems the step is, in cp's own order —
// Integrate → Index → Detect → Sleep → Solve — each chained After the one
// before it by the plugin itself, so an app never has to know the order to stay
// out of it. Positions integrate first, which is cp's order and not Box2D's,
// and is why a Force written this tick moves the Body next tick.
//
// They are exported so an app orders its own Systems against them. There is no
// physics group and no phase of its own: the five sit in the ordinary phase,
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

	// SleepOnUpdate is cp's ProcessComponents, between Detect and Solve: it
	// keeps each Dynamic body's idle time, wakes the Islands something
	// disturbed, and puts to sleep the Islands that stayed idle for Sleep.Time.
	// It does nothing but wake what still sleeps while sleeping is off.
	//
	// A filter System ordered only After[DetectOnUpdate]().Before[SolveOnUpdate]()
	// may run either side of it. One that wants cp's order — PreSolve before the
	// Islands are built, so a Contact it drops neither joins nor wakes one —
	// adds Before[SleepOnUpdate](); one that must also see the Contacts a waking
	// Island hands back runs After[SleepOnUpdate]() instead. Ordering it either
	// way also spares the kernel's dispatch the map it builds, on the ticks the
	// two race, for whichever of them is kept waiting.
	SleepOnUpdate kernel.Subscription[app.UpdateEvent]

	// SolveOnUpdate is the indivisible half of the step: it integrates
	// velocities and, once there is one, runs the impulse solver around that.
	// An app's reaction Systems — cp's PostSolve — run after it.
	SolveOnUpdate kernel.Subscription[app.UpdateEvent]
)
