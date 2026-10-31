package internal

import (
	"math"
	"reflect"
	"sort"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app/appplugin"
)

// Sleeping, judged the way an app sees it: a pile that settles stops moving
// and carries the Sleeping Tag, stays bit for bit where it fell asleep, and
// wakes — all of it, with exactly one tick of gravity — when something
// disturbs it. Each test is a failing sequence: the ordering that goes wrong
// without the rule it names.

// napper is an app that turns sleeping on and reaches the Bodies the way an
// app does: a command writing Sleep, one reading whether a Body sleeps, and one
// writing a Body's Velocity.
type napper struct{}

func (*napper) Name() kernel.PluginName { return "physicstestnapper" }

func (*napper) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, Name}
}

type sleepCmd kernel.Command[sleepRequest, sleepResponse]

type sleepRequest struct{ Sleep Sleep }

type sleepResponse struct{}

type sleepingCmd kernel.Command[sleepingRequest, sleepingResponse]

type sleepingRequest struct{ Entity ecs.Entity }

type sleepingResponse struct{ Asleep bool }

type velocityCmd kernel.Command[velocityRequest, velocityResponse]

type velocityRequest struct {
	Entity   ecs.Entity
	Velocity Velocity
}

type velocityResponse struct{}

type forceCmd kernel.Command[forceRequest, forceResponse]

type forceRequest struct {
	Entity ecs.Entity
	Force  Force
}

type forceResponse struct{}

func (*napper) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[sleepCmd](ecs.ToExecute[sleepRequest, sleepResponse](registrar, func(
		request sleepRequest,
		sleep *ecs.Write[*Sleep],
		answer *ecs.Resp[sleepResponse],
	) {
		*sleep.Get() = request.Sleep
		answer.Set(sleepResponse{})
	}))
	registrar.HandleCommand[sleepingCmd](ecs.ToExecute[sleepingRequest, sleepingResponse](registrar, func(
		request sleepingRequest,
		sleeping *ecs.Get[Sleeping],
		answer *ecs.Resp[sleepingResponse],
	) {
		_, asleep := sleeping.Of(request.Entity)
		answer.Set(sleepingResponse{Asleep: asleep})
	}))
	registrar.HandleCommand[velocityCmd](ecs.ToExecute[velocityRequest, velocityResponse](registrar, func(
		request velocityRequest,
		velocities *ecs.Set[Velocity],
		answer *ecs.Resp[velocityResponse],
	) {
		velocities.UpdateFor(request.Entity, request.Velocity)
		answer.Set(velocityResponse{})
	}))
	registrar.HandleCommand[forceCmd](ecs.ToExecute[forceRequest, forceResponse](registrar, func(
		request forceRequest,
		forces *ecs.Set[Force],
		answer *ecs.Resp[forceResponse],
	) {
		if force, ok := forces.Ref(request.Entity); ok {
			force.Force = force.Force.Add(request.Force.Force)
			force.Torque += request.Force.Torque
		}
		answer.Set(forceResponse{})
	}))
	return nil
}

// newSleepHarness is the harness with sleeping reachable and gravity g
// written into Constants every tick; sleep is written before the first tick.
func newSleepHarness(t testing.TB, g m.Vec2d, sleep Sleep) (*harness, *weigher) {
	t.Helper()
	w := &weigher{gravity: g}
	h := newHarnessWithPlugins(t, nil, 1024, w, &napper{})
	h.setSleep(t, sleep)
	return h, w
}

func (h *harness) setSleep(t testing.TB, sleep Sleep) {
	t.Helper()
	h.kernel.ExecuteCommand[sleepCmd](sleepRequest{Sleep: sleep})
}

func (h *harness) asleep(t testing.TB, e ecs.Entity) bool {
	t.Helper()
	return h.kernel.ExecuteCommand[sleepingCmd](sleepingRequest{Entity: e}).Asleep
}

func (h *harness) setVelocity(t testing.TB, e ecs.Entity, velocity Velocity) {
	t.Helper()
	h.kernel.ExecuteCommand[velocityCmd](velocityRequest{Entity: e, Velocity: velocity})
}

func (h *harness) addForce(t testing.TB, e ecs.Entity, force Force) {
	t.Helper()
	h.kernel.ExecuteCommand[forceCmd](forceRequest{Entity: e, Force: force})
}

func (h *harness) wake(t testing.TB, e ecs.Entity) {
	t.Helper()
	h.kernel.ExecuteCommand[WakeCmd](WakeRequest{Entity: e})
}

// The scenes' gravity, the Sleep they turn on, and the box a pile is made of.
var napGravity = m.Vec2d{Y: -9.81}

// napTime is how long a pile must stay idle to fall asleep: half a second, 30
// ticks at the harness's 60 Hz.
const napTime = 0.5

// pileBox is the half-metre crate a pile is stacked from, 1 kg with a real
// box's moment and a Friction, so that a stack comes to rest without sliding.
func pileBox() Shape {
	box := NewBoxShape(0.5, 0.5, 0)
	box.Friction = 0.7
	return box
}

func pileFloor() Shape {
	floor := ground()
	floor.Friction = 0.7
	return floor
}

// spawnStack is a floor and a stack of count crates on it at x, each placed
// exactly where it rests, and answers the floor and the crates bottom first.
func spawnStack(t testing.TB, h *harness, x float64, count int) (ecs.Entity, []ecs.Entity) {
	t.Helper()
	floor := h.spawn(t, spawnRequest{
		Kind: kindShapedStatic, Place: Position{Current: m.Vec2d{X: x}}, Shape: pileFloor(),
	})
	return floor, spawnCrates(t, h, x, 0, count)
}

// spawnCrates stacks count crates at x from height y up.
func spawnCrates(t testing.TB, h *harness, x, y float64, count int) []ecs.Entity {
	t.Helper()
	var crates []ecs.Entity
	for i := range count {
		at := m.Vec2d{X: x, Y: y + 0.25 + 0.5*float64(i)}
		crates = append(crates, h.spawn(t, spawnRequest{
			Kind:  kindShapedBody,
			Place: Position{Current: at, Previous: at},
			Body:  dynamic(t, 1, MomentForBox(1, 0.5, 0.5), 0, 0),
			Shape: pileBox(),
		}))
	}
	return crates
}

// settle runs ticks until every crate sleeps, and fails if that takes longer
// than limit.
func settle(t testing.TB, h *harness, crates []ecs.Entity, limit int) int {
	t.Helper()
	for tick := 1; tick <= limit; tick++ {
		h.frame(t)
		all := true
		for _, e := range crates {
			if !h.asleep(t, e) {
				all = false
				break
			}
		}
		if all {
			return tick
		}
	}
	t.Fatalf("the pile is not asleep after %d ticks", limit)
	return 0
}

// A pile under the plugin's gravity falls asleep once every crate has been
// idle for Time, and then stays asleep, its Positions bit for bit what they
// were the tick it fell asleep. Without sleeping it would be integrated,
// detected and solved every tick, and the bias correction alone moves it in the
// last bits.
func TestAPileUnderGravityFallsAsleepAndStaysBitForBitWhereItSlept(t *testing.T) {
	h, _ := newSleepHarness(t, napGravity, Sleep{Time: napTime})
	_, crates := spawnStack(t, h, 0, 4)

	if ticks := settle(t, h, crates, 300); ticks < int(napTime/tick) {
		t.Fatalf("the pile fell asleep after %d ticks, before it could have been idle for %v s", ticks, napTime)
	}
	slept := make([]Position, len(crates))
	for i, e := range crates {
		slept[i] = h.read(t, e).Place
	}

	for range 120 {
		h.frame(t)
		for i, e := range crates {
			if !h.asleep(t, e) {
				t.Fatalf("crate %d woke with nothing disturbing it", i)
			}
			if got := h.read(t, e).Place; got != slept[i] {
				t.Fatalf("sleeping crate %d moved from %+v to %+v", i, slept[i], got)
			}
		}
	}
	// A sleeping Island's Contacts are quiet: neither Continuing nor Ended.
	if list := h.contacts(t); len(list) != 0 {
		t.Errorf("a sleeping pile reports %d Contacts, want none: its Contacts are quiet", len(list))
	}
}

// awakeCount is how many of crates are awake.
func awakeCount(t testing.TB, h *harness, crates []ecs.Entity) int {
	t.Helper()
	count := 0
	for _, e := range crates {
		if !h.asleep(t, e) {
			count++
		}
	}
	return count
}

// The same pile under gravity the app writes into Force as m·g, with the
// Constants' gravity left at zero and IdleSpeed named as the fallback would
// have derived it, falls asleep on the same tick and moves the same way every
// tick until then, at 1e-9 — and then stays asleep, because a Force repeated
// every tick disturbs nothing. Were a sleeper's Force compared without being
// cleared, the app's additions would pile up and wake it on the first tick.
func TestAPileUnderGravityWrittenIntoForceSleepsExactlyAsUnderConstantsGravity(t *testing.T) {
	constant, _ := newSleepHarness(t, napGravity, Sleep{Time: napTime})
	_, underConstant := spawnStack(t, constant, 0, 4)

	forced, _ := newSleepHarness(t, m.Vec2d{},
		Sleep{IdleSpeed: napGravity.Length() * tick, Time: napTime})
	_, underForce := spawnStack(t, forced, 0, 4)
	forced.game.push = napGravity.MulS(1)

	const tolerance = 1e-9
	sleptAt := -1
	for k := 1; k <= 200; k++ {
		constant.frame(t)
		forced.frame(t)
		for i := range underConstant {
			a, b := constant.read(t, underConstant[i]).Place, forced.read(t, underForce[i]).Place
			if math.Abs(a.Current.X-b.Current.X) > tolerance || math.Abs(a.Current.Y-b.Current.Y) > tolerance ||
				math.Abs(a.Angle-b.Angle) > tolerance {
				t.Fatalf("tick %d crate %d is at %v a %v under gravity in Force, want %v a %v as under Constants",
					k, i, b.Current, b.Angle, a.Current, a.Angle)
			}
			if constant.asleep(t, underConstant[i]) != forced.asleep(t, underForce[i]) {
				t.Fatalf("tick %d crate %d sleeps under one gravity and not the other", k, i)
			}
		}
		if sleptAt < 0 && awakeCount(t, forced, underForce) == 0 {
			sleptAt = k
		}
	}
	if sleptAt < 0 {
		t.Fatal("neither pile fell asleep in 200 ticks")
	}
	if awakeCount(t, forced, underForce) != 0 {
		t.Errorf("the pile under gravity in Force woke with nothing but the same Force disturbing it")
	}
}

// A pile asleep for 600 ticks and then woken, by the floor under it going, has
// on the waking tick the velocity one tick of gravity gives from where it
// slept: a sleeper is not velocity-integrated, so it gathers nothing while it
// sleeps. Were it still integrated behind its Tag, 600 ticks of gravity would
// land on it at once.
func TestAPileWokenAfterSixHundredTicksReceivesExactlyOneTickOfGravity(t *testing.T) {
	h, _ := newSleepHarness(t, napGravity, Sleep{Time: napTime})
	floor, crates := spawnStack(t, h, 0, 3)
	settle(t, h, crates, 300)
	h.frames(t, 600)

	before := make([]Velocity, len(crates))
	for i, e := range crates {
		if !h.asleep(t, e) {
			t.Fatalf("crate %d woke during the 600 ticks", i)
		}
		before[i] = h.read(t, e).Velocity
	}

	h.despawn(t, floor)
	h.frame(t)

	oneTick := napGravity.Y * tick
	total := 0.0
	for i, e := range crates {
		if h.asleep(t, e) {
			t.Fatalf("crate %d still sleeps with the floor under the pile gone", i)
		}
		got := h.read(t, e).Velocity.Linear.Y - before[i].Linear.Y
		// The Contacts between the crates come back warm-started and are
		// solved, so each crate's share moves by a few parts in a thousand;
		// what they pass between them sums to nothing.
		if math.Abs(got-oneTick) > 0.01*math.Abs(oneTick) {
			t.Errorf("crate %d gained %v m/s on the tick it woke, want one tick of gravity, %v", i, got, oneTick)
		}
		total += got
	}
	if want := oneTick * float64(len(crates)); math.Abs(total-want) > 1e-9 {
		t.Errorf("the pile gained %v m/s between its crates on the tick it woke, want %v: one tick of gravity each",
			total, want)
	}
}

// Writing a different gravity wakes a sleeping pile, as cp's SetGravity wakes
// every sleeping Body, and it moves the new way; writing the same value every
// tick, which is what the app's System does, wakes nothing. With nothing to
// hook, the sleep System compares the gravity it last saw.
func TestANewGravityWakesASleepingPileAndTheSameOneDoesNot(t *testing.T) {
	h, w := newSleepHarness(t, napGravity, Sleep{Time: napTime})
	_, crates := spawnStack(t, h, 0, 3)
	settle(t, h, crates, 300)

	h.frames(t, 30)
	if n := awakeCount(t, h, crates); n != 0 {
		t.Fatalf("%d crates woke under the same gravity written every tick", n)
	}

	top := h.read(t, crates[2]).Place.Current
	w.gravity = m.Vec2d{Y: 9.81}
	h.frame(t)
	if n := awakeCount(t, h, crates); n != len(crates) {
		t.Fatalf("%d of %d crates woke when the gravity turned round", n, len(crates))
	}
	h.frames(t, 20)
	if now := h.read(t, crates[2]).Place.Current; !(now.Y > top.Y+0.1) {
		t.Errorf("the top crate is at %v 20 ticks after gravity turned upwards, want it risen from %v", now, top)
	}
}

// Each disturbance a sleeper's own Components show wakes its whole Island on
// the next tick: a Velocity written, a Position written, a Force that differs
// from the one it fell asleep with, and a WakeCmd. The port has no setters to
// hook, so without the comparison a kick written straight into Velocity would
// be lost on a Body nothing integrates.
func TestAWriteOrAWakeCmdWakesTheWholeIsland(t *testing.T) {
	disturbances := []struct {
		name    string
		disturb func(h *harness, top ecs.Entity)
	}{
		{"a Velocity write", func(h *harness, top ecs.Entity) {
			h.setVelocity(t, top, Velocity{Linear: m.Vec2d{X: 0.5}})
		}},
		{"a Position write", func(h *harness, top ecs.Entity) {
			h.place(t, top, h.read(t, top).Place.Current.Add(m.Vec2d{Y: 0.001}))
		}},
		{"a changed Force", func(h *harness, top ecs.Entity) {
			h.addForce(t, top, Force{Force: m.Vec2d{X: 3}})
		}},
		{"a WakeCmd", func(h *harness, top ecs.Entity) {
			h.wake(t, top)
		}},
	}
	for _, d := range disturbances {
		t.Run(d.name, func(t *testing.T) {
			h, _ := newSleepHarness(t, napGravity, Sleep{Time: napTime})
			_, crates := spawnStack(t, h, 0, 3)
			settle(t, h, crates, 300)

			d.disturb(h, crates[2])
			h.frame(t)
			if n := awakeCount(t, h, crates); n != len(crates) {
				t.Errorf("%d of %d crates woke after %s on the top one, want the whole Island", n, len(crates), d.name)
			}
		})
	}
}

// A Body pressed against a wall by a Force that changes every tick never falls
// asleep, though it does not move: a changed Force resets its idle time, as
// every force write in cp wakes the Body. The same Body under a Force that
// stays the same falls asleep, which is the contrast that makes the first a
// test of the Force and not of the wall.
func TestABodyLeaningOnAWallUnderAChangingForceNeverFallsAsleep(t *testing.T) {
	lean := func(t *testing.T, changing bool) bool {
		h, _ := newSleepHarness(t, m.Vec2d{}, Sleep{IdleSpeed: 0.05, Time: napTime})
		h.spawn(t, spawnRequest{
			Kind:  kindShapedStatic,
			Place: Position{Current: m.Vec2d{X: 1}},
			Shape: NewBoxShapeFor(NewBB(0, -2, 1, 2), 0),
		})
		crate := spawnCrates(t, h, 0.75, -0.25, 1)[0]
		for k := range 240 {
			h.game.push = m.Vec2d{X: 10}
			if changing && k%2 == 1 {
				h.game.push = m.Vec2d{X: 11}
			}
			h.frame(t)
			if h.asleep(t, crate) {
				return true
			}
		}
		return false
	}
	if lean(t, true) {
		t.Error("a Body pressed into a wall by a Force changing every tick fell asleep")
	}
	if !lean(t, false) {
		t.Error("the same Body under an unchanging Force never fell asleep, so the test proves nothing")
	}
}

// A crate dropped onto a sleeping pile wakes it the tick it lands: an awake
// Body touching a sleeper wakes the sleeper's whole Island, which is cp's
// arbiter loop. Left asleep, the pile would not hold the crate up — it would
// never be solved against it.
func TestABodyDroppedOnASleepingPileWakesIt(t *testing.T) {
	h, _ := newSleepHarness(t, napGravity, Sleep{Time: napTime})
	_, crates := spawnStack(t, h, 0, 3)
	settle(t, h, crates, 300)

	dropped := spawnCrates(t, h, 0.1, 2, 1)[0]
	landed := false
	for range 60 {
		h.frame(t)
		for _, entry := range h.contacts(t) {
			if entry.Other(dropped) == crates[2] {
				landed = true
			}
		}
		if landed {
			break
		}
	}
	if !landed {
		t.Fatal("the dropped crate never reached the pile")
	}
	// The tick it lands, not the tick after: a sleeper left asleep would be
	// solved against the crate without being integrated.
	if n := awakeCount(t, h, crates); n != len(crates) {
		t.Fatalf("%d of %d crates woke on the tick a crate landed on the pile", n, len(crates))
	}
	h.frames(t, 60)
	if y := h.read(t, dropped).Place.Current.Y; y < 1.5 {
		t.Errorf("the dropped crate is at y = %v, want it resting on the pile near 1.75", y)
	}
}

// A Kinematic body touching a pile keeps it awake: it is never in an Island,
// and it can move at any moment without a write the sleep System could see.
// The pile here rests on a Kinematic floor standing still, where on a Static
// one it falls asleep.
func TestAKinematicBodyTouchingAPileKeepsItAwake(t *testing.T) {
	h, _ := newSleepHarness(t, napGravity, Sleep{Time: napTime})
	floor := h.spawn(t, spawnRequest{Kind: kindKinematic})
	h.setShape(t, floor, pileFloor())
	crates := spawnCrates(t, h, 0, 0, 3)

	for k := range 240 {
		h.frame(t)
		if n := awakeCount(t, h, crates); n != len(crates) {
			t.Fatalf("tick %d: %d crates fell asleep on a Kinematic floor", k, len(crates)-n)
		}
	}
}

// A Sensor overlapping a sleeper neither wakes it nor stops reporting the
// overlap: a Sensor Contact joins no Island and wakes none, and the sleeper is
// still in the Body index for the Sensor to find.
func TestASensorOverlappingASleeperDoesNotWakeItAndStillReportsIt(t *testing.T) {
	h, _ := newSleepHarness(t, napGravity, Sleep{Time: napTime})
	_, crates := spawnStack(t, h, 0, 3)
	settle(t, h, crates, 300)

	sensor := h.spawn(t, spawnRequest{
		Kind:  kindKinematic,
		Place: Position{Current: m.Vec2d{Y: 1.25}, Previous: m.Vec2d{Y: 1.25}},
	})
	h.setShape(t, sensor, sensorCircle(0.1))

	for k := range 60 {
		h.frame(t)
		if n := awakeCount(t, h, crates); n != 0 {
			t.Fatalf("tick %d: a Sensor overlapping the pile woke %d crates", k, n)
		}
		found := false
		for _, entry := range h.contacts(t) {
			if entry.Sensor && entry.Other(sensor) == crates[2] && entry.Phase != PhaseEnded {
				found = true
			}
		}
		if !found && k > 0 {
			t.Fatalf("tick %d: the Sensor's overlap with the sleeping crate is not reported", k)
		}
	}
}

// Removing a support wakes what rests on it. The bottom crate of a sleeping
// stack despawned, the rest falls; the floor under a sleeping pile despawned,
// the pile falls. Neither is a touch or a write on a sleeper, so without the
// rule both would hang in the air.
func TestRemovingASupportWakesWhatRestsOnIt(t *testing.T) {
	t.Run("the bottom crate", func(t *testing.T) {
		h, _ := newSleepHarness(t, napGravity, Sleep{Time: napTime})
		_, crates := spawnStack(t, h, 0, 3)
		settle(t, h, crates, 300)
		before := h.read(t, crates[2]).Place.Current.Y

		h.despawn(t, crates[0])
		h.frames(t, 30)
		if now := h.read(t, crates[2]).Place.Current.Y; !(now < before-0.3) {
			t.Errorf("the top crate is at y = %v 30 ticks after the bottom one went, want it fallen from %v", now, before)
		}
	})
	t.Run("the floor", func(t *testing.T) {
		h, _ := newSleepHarness(t, napGravity, Sleep{Time: napTime})
		floor, crates := spawnStack(t, h, 0, 3)
		settle(t, h, crates, 300)
		before := h.read(t, crates[0]).Place.Current.Y

		h.despawn(t, floor)
		h.frames(t, 30)
		if now := h.read(t, crates[0]).Place.Current.Y; !(now < before-0.3) {
			t.Errorf("the bottom crate is at y = %v 30 ticks after the floor went, want it fallen from %v", now, before)
		}
	})
}

// A sleeping crate is still in the Body index for every query: an Overlap on
// the Body index finds it, and one on the static index does not — cp moves a
// sleeper into its static tree, which the port's public indices cannot.
func TestAnOverlapOnTheBodyIndexFindsASleepingCrateAndTheStaticIndexDoesNot(t *testing.T) {
	h, _ := newSleepHarness(t, napGravity, Sleep{Time: napTime})
	_, crates := spawnStack(t, h, 0, 3)
	settle(t, h, crates, 300)
	h.frames(t, 5)

	got := h.indexed(t, m.Vec2d{Y: 0.75}, 0.05)
	if len(got.Bodies) != 1 || got.Bodies[0] != crates[1] {
		t.Errorf("an Overlap on the Body index at the middle crate finds %v, want the sleeping crate %v",
			got.Bodies, crates[1])
	}
	for _, e := range got.Statics {
		if e == crates[1] {
			t.Errorf("an Overlap on the static index finds the sleeping crate %v", e)
		}
	}
	if got.BodyLen != len(crates) {
		t.Errorf("the Body index holds %d Shapes, want the %d sleeping crates", got.BodyLen, len(crates))
	}
}

// A sleeping Island's Contacts go quiet — neither Continuing nor Ended while it
// sleeps — and come back Continuing when it wakes, with the Impulses they
// carried: were they left to the list, they would end the tick after the pile
// fell asleep and begin again, cold, the tick it woke.
func TestAWakingIslandsContactsComeBackContinuingWithTheirImpulses(t *testing.T) {
	h, _ := newSleepHarness(t, napGravity, Sleep{Time: napTime})
	_, crates := spawnStack(t, h, 0, 3)
	settle(t, h, crates, 300)
	h.frames(t, 10)
	if list := h.contacts(t); len(list) != 0 {
		t.Fatalf("a sleeping pile reports %d Contacts, want none", len(list))
	}

	h.wake(t, crates[0])
	h.frame(t)
	list := h.contacts(t)
	if len(list) != len(crates) {
		t.Fatalf("the woken pile reports %d Contacts, want the %d it slept with", len(list), len(crates))
	}
	for _, entry := range list {
		if entry.Phase != PhaseContinuing {
			t.Errorf("a Contact the pile slept with came back in phase %v, want Continuing", entry.Phase)
		}
		if entry.TotalImpulse().Length() < 0.5*tick*9.81 {
			t.Errorf("a Contact holding a crate up delivered %v, want at least half a crate's weight for a tick",
				entry.TotalImpulse())
		}
	}
}

// A sleeper the app teleports wakes its Island, and the Contacts it slept with
// come back Ended rather than Continuing: they name a place it has left. The
// rest of the Island's Contacts come back Continuing.
func TestATeleportedSleepersContactsComeBackEnded(t *testing.T) {
	h, _ := newSleepHarness(t, napGravity, Sleep{Time: napTime})
	_, crates := spawnStack(t, h, 0, 3)
	settle(t, h, crates, 300)

	h.place(t, crates[2], m.Vec2d{X: 3, Y: 3})
	h.frame(t)
	var ended, continuing int
	for _, entry := range h.contacts(t) {
		touchesTop := entry.A == crates[2] || entry.B == crates[2]
		switch {
		case touchesTop && entry.Phase == PhaseEnded:
			ended++
		case !touchesTop && entry.Phase == PhaseContinuing:
			continuing++
		default:
			t.Errorf("after the teleport a Contact of %v and %v is in phase %v", entry.A, entry.B, entry.Phase)
		}
	}
	if ended != 1 || continuing != 2 {
		t.Errorf("after the teleport %d Contacts ended and %d continued, want 1 and 2", ended, continuing)
	}
}

// Two crates held by a Joint are one Island: they fall asleep together and a
// kick to one wakes both, though nothing touches between them.
func TestAJointMakesTwoBodiesOneIsland(t *testing.T) {
	h, _ := newSleepHarness(t, napGravity, Sleep{Time: napTime})
	h.spawn(t, spawnRequest{Kind: kindShapedStatic, Shape: pileFloor()})
	left := spawnCrates(t, h, -1, 0, 1)[0]
	right := spawnCrates(t, h, 1, 0, 1)[0]
	h.spawn(t, spawnRequest{
		Kind:  kindJoint,
		Joint: NewPinJoint(left, right, m.Vec2d{}, m.Vec2d{}, 2),
	})
	pair := []ecs.Entity{left, right}
	settle(t, h, pair, 300)
	// One Island, so asleep together and staying so: two Islands a Joint
	// holds between them would wake each other every tick.
	for k := range 30 {
		h.frame(t)
		if n := awakeCount(t, h, pair); n != 0 {
			t.Fatalf("tick %d: %d of the two jointed crates woke with nothing disturbing them", k, n)
		}
	}

	h.setVelocity(t, left, Velocity{Linear: m.Vec2d{Y: 0.5}})
	h.frame(t)
	if n := awakeCount(t, h, pair); n != 2 {
		t.Errorf("%d of the two jointed crates woke when one was kicked, want both", n)
	}
}

// Turning sleeping off wakes every Island, rather than leaving it asleep with
// nothing left to wake it, and then nothing falls asleep again.
func TestTurningSleepingOffWakesEveryIsland(t *testing.T) {
	h, _ := newSleepHarness(t, napGravity, Sleep{Time: napTime})
	_, crates := spawnStack(t, h, 0, 3)
	settle(t, h, crates, 300)

	h.setSleep(t, Sleep{})
	for k := range 120 {
		h.frame(t)
		if n := awakeCount(t, h, crates); n != len(crates) {
			t.Fatalf("tick %d with sleeping off: %d crates sleep", k, len(crates)-n)
		}
	}
}

// Off is the default: a pile that would sleep under Sleep{Time} never does
// under the Resource the plugin registers.
func TestSleepingIsOffByDefault(t *testing.T) {
	h, _ := newSleepHarness(t, napGravity, Sleep{})
	_, crates := spawnStack(t, h, 0, 3)
	for k := range 240 {
		h.frame(t)
		if n := awakeCount(t, h, crates); n != len(crates) {
			t.Fatalf("tick %d with the default Sleep: %d crates sleep", k, len(crates)-n)
		}
	}
}

// The lock sets sleeping leaves, read off the engine's description, which is
// what the scheduler grants. Integrate, Index and Solve each gain one read, of
// the Sleeping Store, which only the sleep System writes; Detect gains
// nothing. The sleep System itself locks nothing Solve, the next link of the
// chain, does not already hold as strongly, beside the Stores and Resources
// sleeping adds — so whatever could run beside the chain before still can, and
// no System loses parallelism it had.
func TestSleepingCostsNoSystemParallelism(t *testing.T) {
	engine := kernel.New(nil).
		Handler(func(err error) error { t.Errorf("unexpected kernel error: %v", err); return err }).
		WithPlugins(appplugin.New(), mainLoopAdapter{}, ecsplugin.New(), New())

	type lockSet struct{ reads, writes map[reflect.Type]bool }
	sets := map[reflect.Type]lockSet{}
	writers := map[reflect.Type][]reflect.Type{}
	of := func(types []reflect.Type) map[reflect.Type]bool {
		set := map[reflect.Type]bool{}
		for _, each := range types {
			set[each] = true
		}
		return set
	}
	description := engine.Describe()
	for _, sub := range description.Subscriptions {
		sets[sub.Type] = lockSet{of(sub.Reads), of(sub.Writes)}
		for _, written := range sub.Writes {
			writers[written] = append(writers[written], sub.Type)
		}
	}
	for _, command := range description.Commands {
		for _, written := range command.Writes {
			writers[written] = append(writers[written], command.Type)
		}
	}

	entities := reflect.TypeFor[*ecs.Entities]()
	sleeping := reflect.TypeFor[*ecs.Store[Sleeping]]()
	rest := reflect.TypeFor[*ecs.Store[Rest]]()
	wakes := reflect.TypeFor[*Wakes]()
	settings := reflect.TypeFor[*Sleep]()

	for _, owned := range []reflect.Type{sleeping, rest} {
		if got := writers[owned]; len(got) != 1 || got[0] != reflect.TypeFor[SleepOnUpdate]() {
			t.Errorf("%v is written by %v, want the sleep System alone", owned, got)
		}
	}

	want := map[reflect.Type]lockSet{
		reflect.TypeFor[IntegrateOnUpdate](): {
			reads:  of([]reflect.Type{entities, reflect.TypeFor[*ecs.Store[Velocity]](), sleeping}),
			writes: of([]reflect.Type{reflect.TypeFor[*ecs.Store[Position]]()}),
		},
		reflect.TypeFor[IndexOnUpdate](): {
			reads: of([]reflect.Type{
				entities, sleeping,
				reflect.TypeFor[*ecs.Store[Joint]](),
				reflect.TypeFor[*ecs.Store[Polygon]](),
				reflect.TypeFor[*ecs.Store[Position]](),
				reflect.TypeFor[*ecs.Store[Shape]](),
				reflect.TypeFor[*ecs.Store[Static]](),
			}),
			writes: of([]reflect.Type{
				reflect.TypeFor[*BodyIndex](),
				reflect.TypeFor[*StaticIndex](),
				reflect.TypeFor[*JointedPairs](),
			}),
		},
		reflect.TypeFor[DetectOnUpdate](): {
			reads: of([]reflect.Type{
				entities,
				reflect.TypeFor[*BodyIndex](),
				reflect.TypeFor[*StaticIndex](),
				reflect.TypeFor[*JointedPairs](),
			}),
			writes: of([]reflect.Type{reflect.TypeFor[*Contacts]()}),
		},
		reflect.TypeFor[SolveOnUpdate](): {
			reads: of([]reflect.Type{
				entities, sleeping,
				reflect.TypeFor[*Constants](),
				reflect.TypeFor[*ecs.Store[Dynamic]](),
			}),
			writes: of([]reflect.Type{
				reflect.TypeFor[*Contacts](),
				reflect.TypeFor[*ecs.Store[Force]](),
				reflect.TypeFor[*ecs.Store[Joint]](),
				reflect.TypeFor[*ecs.Store[Position]](),
				reflect.TypeFor[*ecs.Store[Velocity]](),
			}),
		},
	}
	for system, expected := range want {
		got := sets[system]
		if !reflect.DeepEqual(got.reads, expected.reads) || !reflect.DeepEqual(got.writes, expected.writes) {
			t.Errorf("%v locks reads %v writes %v, want reads %v writes %v",
				system, keys(got.reads), keys(got.writes), keys(expected.reads), keys(expected.writes))
		}
	}

	solve := sets[reflect.TypeFor[SolveOnUpdate]()]
	sleep := sets[reflect.TypeFor[SleepOnUpdate]()]
	sleepings := map[reflect.Type]bool{sleeping: true, rest: true, wakes: true, settings: true}
	for read := range sleep.reads {
		if !solve.reads[read] && !solve.writes[read] && !sleepings[read] {
			t.Errorf("the sleep System reads %v, which Solve does not hold", read)
		}
	}
	for written := range sleep.writes {
		if !solve.writes[written] && !sleepings[written] {
			t.Errorf("the sleep System writes %v, which Solve does not write", written)
		}
	}
	t.Logf("the sleep System reads %v and writes %v", keys(sleep.reads), keys(sleep.writes))
}

func keys(set map[reflect.Type]bool) []string {
	var names []string
	for each := range set {
		names = append(names, each.String())
	}
	sort.Strings(names)
	return names
}
