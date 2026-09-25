package internal

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
)

// The step runs in cp's own order — positions first, then the index, then
// detection, then everything else inside Solve — and the plugin chains it
// itself, so an app never has to know the order to stay out of it. Four
// recorders sandwiched between the four identities read the order back.
func TestTheFourSystemsRunInChipmunksOrder(t *testing.T) {
	h := newHarness(t)

	h.frames(t, 2)

	want := []string{"integrate", "index", "detect", "solve", "integrate", "index", "detect", "solve"}
	if got := h.game.ran(); !slices.Equal(got, want) {
		t.Fatalf("the step ran as %v, want %v", got, want)
	}
}

// The one-tick offset, pinned. Position integrates first, so the Force this
// tick's gameplay wrote is turned into velocity at the end of this tick and
// spent by the next tick's Integrate. Only Force pays it — Box2D's order
// removes it, and is a different engine's step.
func TestAForceWrittenThisTickMovesTheBodyNextTick(t *testing.T) {
	h := newHarness(t)
	e := h.spawn(t, spawnRequest{Kind: kindDynamic, Body: dynamic(t, 2, 8, 0, 0)})
	h.game.push = m.Vec2d{X: 12}

	h.frame(t)

	first := h.read(t, e)
	if first.Place.Current != (m.Vec2d{}) {
		t.Errorf("the Body moved to %v on the tick the Force was written, want it still at the origin",
			first.Place.Current)
	}
	if want := 12 * tick / 2; !near(first.Velocity.Linear.X, want) {
		t.Errorf("velocity after one tick = %v, want %v", first.Velocity.Linear.X, want)
	}
	if first.Force != (Force{}) {
		t.Errorf("the Force was left at %+v, want Solve to have cleared it", first.Force)
	}

	h.game.push = m.Vec2d{}
	h.frame(t)

	second := h.read(t, e)
	if want := 12 * tick / 2 * tick; !near(second.Place.Current.X, want) {
		t.Errorf("the Body is at %v after the second tick, want %v — one tick's velocity spent once",
			second.Place.Current.X, want)
	}
}

// A Velocity written directly is not delayed: only Force pays the offset,
// because Integrate reads the Velocity a Before[IntegrateOnUpdate] System has
// just written.
func TestAVelocityWrittenDirectlyMovesTheBodyTheSameTick(t *testing.T) {
	h := newHarness(t)
	e := h.spawn(t, spawnRequest{
		Kind:     kindKinematic,
		Velocity: Velocity{Linear: m.Vec2d{X: 6, Y: -3}, Angular: 2},
	})

	h.frame(t)

	got := h.read(t, e)
	if want := (m.Vec2d{X: 6 * tick, Y: -3 * tick}); !near(got.Place.Current.X, want.X) || !near(got.Place.Current.Y, want.Y) {
		t.Errorf("the Kinematic body is at %v, want %v", got.Place.Current, want)
	}
	if want := 2 * tick; !near(got.Place.Angle, want) {
		t.Errorf("the Kinematic body's Angle is %v, want %v", got.Place.Angle, want)
	}
	if got.Place.Previous != (m.Vec2d{}) {
		t.Errorf("Previous is %v, want the origin the tick began at", got.Place.Previous)
	}
}

// A Kinematic body is a Velocity with no Dynamic: the app sets it and the
// integrator moves it, and nothing damps it, because Damping is a Dynamic's.
func TestAKinematicBodyIsMovedButNeverDamped(t *testing.T) {
	h := newHarness(t)
	e := h.spawn(t, spawnRequest{Kind: kindKinematic, Velocity: Velocity{Linear: m.Vec2d{X: 6}}})

	h.frames(t, 60)

	got := h.read(t, e)
	if want := 6.0; !near(got.Velocity.Linear.X, want) {
		t.Errorf("the Kinematic body's speed is %v after a second, want an undamped %v",
			got.Velocity.Linear.X, want)
	}
	if want := 6.0 * 60 * tick; math.Abs(got.Place.Current.X-want) > 1e-9 {
		t.Errorf("the Kinematic body is at %v after a second, want %v", got.Place.Current.X, want)
	}
	if got.HasForce {
		t.Errorf("the Kinematic body carries a Force: %+v", got.Force)
	}
}

// A Static body has no Velocity, so it falls out of the integrator's Query and
// nothing the plugin does can move it. Moving one means replacing the Entity.
func TestAStaticBodyNeverMoves(t *testing.T) {
	h := newHarness(t)
	place := Position{Current: m.Vec2d{X: 3, Y: 4}, Angle: 1}
	e := h.spawn(t, spawnRequest{Kind: kindStatic, Place: place})
	h.game.push, h.game.torque = m.Vec2d{X: 1000}, 1000

	h.frames(t, 10)

	if got := h.read(t, e); got.Place != place {
		t.Fatalf("the Static body is at %+v, want the %+v it was spawned at", got.Place, place)
	}
}

// The one trap in the Component set, stated rather than checked: a Dynamic body
// with no Force falls out of the velocity integrator's Query and silently never
// moves. The package has no validity checks and does not borrow the ECS's
// Validation mode for them.
func TestADynamicBodyWithNoForceNeverMoves(t *testing.T) {
	h := newHarness(t)
	e := h.spawn(t, spawnRequest{Kind: kindForceless, Body: dynamic(t, 2, 8, 0, 0)})
	h.game.push = m.Vec2d{X: 1000}

	h.frames(t, 10)

	got := h.read(t, e)
	if got.Velocity != (Velocity{}) {
		t.Errorf("a Dynamic body with no Force reached %+v, want it never to have moved", got.Velocity)
	}
	if got.Place.Current != (m.Vec2d{}) {
		t.Errorf("a Dynamic body with no Force is at %v, want the origin", got.Place.Current)
	}
}

// The whole of the tracer bullet, end to end: register the plugin, spawn a
// Dynamic body, write Force into it from the app's own System, and watch it
// accelerate and settle at the terminal speed the integrator's Euler Force step
// gives — +13.0% over the F/(mλ) asked for, at 60 Hz.
func TestABodyAcceleratesUnderForceAndSettles(t *testing.T) {
	const (
		rate = 15.0
		mass = 2.0
		push = 10.0
	)
	h := newHarness(t)
	e := h.spawn(t, spawnRequest{Kind: kindDynamic, Body: dynamic(t, mass, 8, rate, 0)})
	h.game.push = m.Vec2d{X: push}

	h.frames(t, 600)

	asked := push / (mass * rate)
	terminal := asked * rate * tick / (1 - math.Exp(-rate*tick))
	got := h.read(t, e)
	t.Logf("settled at %.6f m/s against the %.6f m/s asked for", got.Velocity.Linear.X, asked)
	if !near(got.Velocity.Linear.X, terminal) {
		t.Errorf("the Body settled at %v m/s, want %v m/s", got.Velocity.Linear.X, terminal)
	}
	if got.Place.Current.X <= got.Place.Previous.X {
		t.Errorf("the Body is not still moving: Current %v, Previous %v",
			got.Place.Current.X, got.Place.Previous.X)
	}
}

// A Torque turns a Dynamic body about its Position, which is its centre of
// gravity, and moves it nowhere. The Angle it reaches is the one-tick offset
// again, in its rotational half: after n ticks the Angular velocity is n·α·h
// but the Angle is only α·h²·n(n−1)/2, because the tick that raised the
// velocity had already integrated the Angle.
func TestATorqueTurnsTheBodyAboutItsCentreOfGravity(t *testing.T) {
	const (
		torque = 8.0
		moment = 4.0
		ticks  = 120
	)
	h := newHarness(t)
	e := h.spawn(t, spawnRequest{Kind: kindDynamic, Body: dynamic(t, 2, moment, 0, 0)})
	h.game.torque = torque

	h.frames(t, ticks)

	alpha := torque / moment
	got := h.read(t, e)
	if want := ticks * alpha * tick; !near(got.Velocity.Angular, want) {
		t.Errorf("the Body's Angular velocity is %v rad/s, want %v rad/s", got.Velocity.Angular, want)
	}
	if want := alpha * tick * tick * ticks * (ticks - 1) / 2; math.Abs(got.Place.Angle-want) > 1e-9 {
		t.Errorf("the Body's Angle is %v rad, want %v rad", got.Place.Angle, want)
	}
	if got.Place.Current != (m.Vec2d{}) {
		t.Errorf("a Torque moved the Body to %v, want it turning in place", got.Place.Current)
	}
}

// Every Component the plugin registers is owned by it, so an app System that
// locks one must declare ecsphysics2d — the coupling check holding across the
// root-and-internal split, exactly as it does for every other Bundle's
// Components.
func TestTheCouplingCheckHoldsOnThePluginsComponents(t *testing.T) {
	err := composeWithMover([]kernel.PluginName{ecs.Name})
	var undeclared kernel.ErrUndeclaredDependency
	if !errors.As(err, &undeclared) {
		t.Fatalf("a System locking Position without declaring ecsphysics2d composed with %v", err)
	}
	if undeclared.Owner != Name {
		t.Errorf("the refusal is %+v, want the Store owned by %q", undeclared, Name)
	}
	if err := composeWithMover([]kernel.PluginName{ecs.Name, Name}); err != nil {
		t.Fatalf("a System declaring ecsphysics2d did not compose: %v", err)
	}
}

// The two halves of Index, through a real Engine. The static index is
// maintained incrementally from the Shape hooks — an added Shape inserted and
// world-cached once, a removed one taken out — while the Body index is Cleared
// and refilled whole every tick from wherever Integrate has just left the
// Bodies.
func TestTheStaticDrainAndTheBodyRebuildAreTheTwoShapesIndexTakes(t *testing.T) {
	h := newHarness(t)
	wall := h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{X: 5}},
		Shape: circle(1),
	})
	// 60 m/s at a 1/60 s step is one metre a tick, so where the mover should be
	// after n ticks needs no arithmetic.
	mover := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Velocity: Velocity{Linear: m.Vec2d{X: 60}},
		Body:     dynamic(t, 2, 8, 0, 0),
		Shape:    circle(0.5),
	})

	h.frame(t)

	got := h.indexed(t, m.Vec2d{X: 1}, 0.1)
	if got.StaticLen != 1 || got.BodyLen != 1 {
		t.Fatalf("the indices hold %d statics and %d Bodies, want one of each", got.StaticLen, got.BodyLen)
	}
	if !slices.Equal(got.Bodies, []ecs.Entity{mover}) {
		t.Errorf("the Body index has %v a metre along, want the mover Index put there after Integrate", got.Bodies)
	}
	if statics := h.indexed(t, m.Vec2d{X: 5}, 0.1).Statics; !slices.Equal(statics, []ecs.Entity{wall}) {
		t.Errorf("the static index has %v where the wall was spawned, want the wall", statics)
	}

	// Rebuilt whole: the mover is where this tick put it and nowhere it has
	// been, which a Clear-less index could not say.
	h.frame(t)

	if bodies := h.indexed(t, m.Vec2d{X: 2}, 0.1).Bodies; !slices.Equal(bodies, []ecs.Entity{mover}) {
		t.Errorf("after the second tick the Body index has %v two metres along, want the mover", bodies)
	}
	stale := h.indexed(t, m.Vec2d{X: 1}, 0.1)
	if len(stale.Bodies) != 0 {
		t.Errorf("the Body index still has %v where the mover was last tick, want the rebuild to have dropped it",
			stale.Bodies)
	}
	if stale.BodyLen != 1 {
		t.Errorf("the Body index holds %d after a rebuild, want the one mover", stale.BodyLen)
	}

	// A Shape removed is drained out of the static index on the next tick.
	h.dropShape(t, wall)
	h.frame(t)

	if got := h.indexed(t, m.Vec2d{X: 5}, 0.1); got.StaticLen != 0 || len(got.Statics) != 0 {
		t.Errorf("the static index holds %d and found %v after the wall's Shape was removed, want it empty",
			got.StaticLen, got.Statics)
	}
}

// A Shape given to an Entity that already exists is an addition like any other:
// the drain hears it on its next run and inserts it. This is the path a spawn
// does not cover, and the one an app takes when it builds geometry in pieces.
func TestAShapeGivenToALiveStaticIsDrainedInOnTheNextTick(t *testing.T) {
	h := newHarness(t)
	wall := h.spawn(t, spawnRequest{
		Kind:  kindStatic,
		Place: Position{Current: m.Vec2d{X: 3, Y: 4}},
	})

	h.frame(t)
	if got := h.indexed(t, m.Vec2d{X: 3, Y: 4}, 0.1); got.StaticLen != 0 {
		t.Fatalf("a Static with no Shape is in the index %d times, want none — shapeless Bodies are in neither",
			got.StaticLen)
	}

	h.setShape(t, wall, circle(1))
	h.frame(t)

	if statics := h.indexed(t, m.Vec2d{X: 3, Y: 4}, 0.1).Statics; !slices.Equal(statics, []ecs.Entity{wall}) {
		t.Errorf("the static index has %v where the wall is, want the wall the drain inserted", statics)
	}
}

// Statics are world-cached at insert and never again, so writing a Static's
// Position does not move it: the app replaces the Entity instead. Nothing here
// is a check — the index simply never looks at the Position again.
func TestAStaticIsWorldCachedAtInsertAndMovingItsPositionDoesNothing(t *testing.T) {
	h := newHarness(t)
	wall := h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{X: 5}},
		Shape: circle(1),
	})

	h.frame(t)
	h.place(t, wall, m.Vec2d{X: 50})
	h.frames(t, 3)

	if statics := h.indexed(t, m.Vec2d{X: 5}, 0.1).Statics; !slices.Equal(statics, []ecs.Entity{wall}) {
		t.Errorf("the static index has %v where the wall was inserted, want it still cached there", statics)
	}
	if moved := h.indexed(t, m.Vec2d{X: 50}, 0.1).Statics; len(moved) != 0 {
		t.Errorf("the static index followed the wall to %v, want a Static cached once and never again", moved)
	}
}

// Which index a Shape goes into is the Static Tag and nothing else. A Body's
// Shape must never reach the static index: the static index is never Cleared,
// so one inserted there would stay frozen at its spawn place for ever while the
// Body itself moved on in BodyIndex.
func TestAShapedBodyIsRebuiltIntoTheBodyIndexAndNeverTheStaticOne(t *testing.T) {
	h := newHarness(t)
	mover := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Velocity: Velocity{Linear: m.Vec2d{X: 60}},
		Body:     dynamic(t, 2, 8, 0, 0),
		Shape:    circle(0.5),
	})

	h.frames(t, 3)

	got := h.indexed(t, m.Vec2d{X: 3}, 0.1)
	if got.StaticLen != 0 {
		t.Errorf("the static index holds %d after three ticks of one Body, want none", got.StaticLen)
	}
	if !slices.Equal(got.Bodies, []ecs.Entity{mover}) {
		t.Errorf("the Body index has %v three metres along, want the mover", got.Bodies)
	}
	if ghost := h.indexed(t, m.Vec2d{}, 0.1).Statics; len(ghost) != 0 {
		t.Errorf("the static index has %v at the mover's spawn place, want nothing left behind", ghost)
	}
}

// The one trap in the drain, stated rather than checked: a Static enters the
// index on its Shape hook and nowhere else, so a Shape that arrives before the
// Position is skipped, and giving it a Position afterwards is no second chance.
// Spawning the Shape, the Position and the Tag together is what avoids it.
func TestAStaticWhoseShapeArrivesWithoutAPositionIsSkippedForGood(t *testing.T) {
	h := newHarness(t)
	ghost := h.spawn(t, spawnRequest{Kind: kindPlacelessStatic, Shape: circle(1)})

	h.frame(t)
	if got := h.indexed(t, m.Vec2d{}, 2); got.StaticLen != 0 {
		t.Fatalf("a Shape with no Position was indexed %d times, want it skipped", got.StaticLen)
	}

	// The Position arrives late. The hook has already been drained, so nothing
	// brings the Entity back.
	h.place(t, ghost, m.Vec2d{})
	h.frames(t, 3)

	got := h.indexed(t, m.Vec2d{}, 2)
	if got.StaticLen != 0 {
		t.Errorf("a late Position put the Entity in the static index after all: %d held, %v found",
			got.StaticLen, got.Statics)
	}
	if got.BodyLen != 0 {
		t.Errorf("a Static reached the Body index: %d held", got.BodyLen)
	}
}
