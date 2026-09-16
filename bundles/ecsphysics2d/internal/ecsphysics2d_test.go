package internal

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
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
	if first.Force != (ecsphysics2d.Force{}) {
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
		Velocity: ecsphysics2d.Velocity{Linear: m.Vec2d{X: 6, Y: -3}, Angular: 2},
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
	e := h.spawn(t, spawnRequest{Kind: kindKinematic, Velocity: ecsphysics2d.Velocity{Linear: m.Vec2d{X: 6}}})

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
	place := ecsphysics2d.Position{Current: m.Vec2d{X: 3, Y: 4}, Angle: 1}
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
	if got.Velocity != (ecsphysics2d.Velocity{}) {
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
	if undeclared.Owner != ecsphysics2d.Name {
		t.Errorf("the refusal is %+v, want the Store owned by %q", undeclared, ecsphysics2d.Name)
	}
	if err := composeWithMover([]kernel.PluginName{ecs.Name, ecsphysics2d.Name}); err != nil {
		t.Fatalf("a System declaring ecsphysics2d did not compose: %v", err)
	}
}
