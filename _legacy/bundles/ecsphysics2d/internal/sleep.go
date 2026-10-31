package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/kernel"
)

// What sleeping keeps per Body, and the command that wakes one. The Islands
// themselves, and the tick that builds them, are islands.go.

// Rest is what the sleep System keeps for one Dynamic body across ticks: cp's
// sleepingIdleTime, the Force it compares against, and — while the Body sleeps
// — the Position and Velocity it left in it and the Island it sleeps in.
//
// It is a Component the plugin adds to a Dynamic body the first tick sleeping
// is on and nothing else reads or writes; an app cannot name it. It is a
// Component rather than a table in a Resource because the sleep System reaches
// it from the Entity a Contact or a Joint names, which is one load of a Store's
// sparse array where a table keyed by Entity is a hash, and because a despawn
// empties it with every other Store, so nothing has to notice a Body going.
//
// Nothing here mirrors a Component. What a Sleeping body's Position and
// Velocity were when it fell asleep is what they are compared against, and
// they stay the source of truth: a write to either wakes the Body, and the
// Body moves from what the app wrote.
type Rest struct {
	// idle is how long the Body has been idle, in seconds: cp's
	// sleepingIdleTime.
	Idle float64
	// force is the Force the Body carried on the last tick the System saw it
	// awake, and while it sleeps the one it last carried before it fell
	// asleep.
	Force Force
	// velocity and position are what the System left in a Sleeping body.
	Velocity Velocity
	Position Position
	// island is the Island the Body sleeps in, and −1 while it is awake.
	Island int32
	// node is the Body's node in this tick's Islands, meaningful when stamp is
	// this tick.
	Node int32
	// stamp is the last tick the System walked the Body awake, cleared the
	// tick the Force of a sleeper was cleared on, and woke the tick it woke.
	Stamp, Cleared, Woke uint32
}

// Wakes is the WakeCmd's queue: the Entities whose Islands the sleep System
// wakes on its next run. It is a Resource of its own so that the command's lock
// is a write on it and nothing wider.
type Wakes struct{ entities []ecs.Entity }

// NewWakes is an empty queue.
func NewWakes() *Wakes { return &Wakes{} }

// WakeRequest names the Entity whose Island a WakeCmd wakes.
type WakeRequest struct{ Entity ecs.Entity }

// WakeResponse is what a WakeCmd answers, which is nothing: the Island wakes
// on the sleep System's next run, and whether a Body sleeps is read off the
// Sleeping Tag.
type WakeResponse struct{}

// WakeCommand is the WakeCmd factory the physics plugin registers.
//
// Its lock is write on the Wakes queue and nothing besides, so a System that
// dispatches it serialises with the sleep System and with nothing else. It
// wakes nothing itself: waking takes the Sleeping Tag off every Body in the
// Island and hands its quiet Contacts back to the tick's list, which is the
// sleep System's to do, so the command queues the Entity and the System wakes
// its Island on its next run — before Solve, so a Body woken from a System
// ordered before Integrate is solved on the same tick.
//
// Like ShrinkCmd it is not an ecs System and declares itself exclusive, the
// queue it writes living in this closure's handle.
func WakeCommand() (kernel.Lock, kernel.Execute[WakeRequest, WakeResponse]) {
	var wakes kernel.Write[*Wakes]
	return func(access kernel.ResourceAccess) {
			access.Exclusive()
			wakes = access.GetWrite[*Wakes]()
		}, func(_ kernel.Kernel, request WakeRequest) WakeResponse {
			if request.Entity != ecs.NoEntity {
				queue := wakes.Get()
				queue.entities = append(queue.entities, request.Entity)
			}
			return WakeResponse{}
		}
}
