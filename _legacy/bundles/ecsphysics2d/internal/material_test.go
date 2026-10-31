package internal

import (
	"fmt"
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"

	"github.com/dvoyni/cog/libs/m"
)

// Friction and Restitution, turned on. The two acceptance scenes here are the
// specification's layer C and name no cp number at all: a block on a ramp slides
// if and only if tan θ > u, and a bounce returns e² of the height it arrived
// with. A closed form is right where cp is merely the reference, so cp is not
// the oracle for either — it is only the oracle for the one layer-B trajectory
// at the end of this file.
//
// Every scene writes its own gravity into Force from an ordinary System and
// leaves Constants.Gravity at zero, which is the same fall and is also what
// checks that gravity really is optional rather than assumed.

// gravity is the acceleration the scenes here write, and rampAngle the slope
// they write it down.
const (
	gravity   = 9.81
	rampAngle = math.Pi / 6
)

// restingDepth is how far into its wall each scene starts its Body: less than
// the Slop, so the de-penetration bias is exactly zero and the closed forms are
// the solver's own arithmetic rather than the solver's plus a nudge.
const restingDepth = 0.002

// blockMoment is what makes a circle a block: an infinite Moment of inertia
// stores invInertia = 0, so the friction impulse at the Contact spins nothing
// and the Body slides the way a box would. Boxes are Polygons and Polygons are
// a later ticket, so the ramp is run with a circle that cannot turn — which is
// the same free body, and is stated rather than hidden.
var blockMoment = math.Inf(1)

// ramp is the static slope: a segment through the origin at that angle, ten
// metres of it, which is longer than any scene here slides along.
func ramp(angle float64) Shape {
	along := m.Vec2d{X: math.Cos(angle), Y: math.Sin(angle)}
	return NewSegmentShape(along.MulS(-5), along.MulS(5), 0)
}

// rampFrame is the slope's own two directions: down the slope, and out of it.
func rampFrame(angle float64) (down, out m.Vec2d) {
	return m.Vec2d{X: -math.Cos(angle), Y: -math.Sin(angle)},
		m.Vec2d{X: -math.Sin(angle), Y: math.Cos(angle)}
}

// withMaterial is a circle carrying one Friction and one Restitution, which is
// how an app writes a material: plain fields on the Shape, no constructor.
func withMaterial(radius, friction, restitution float64) Shape {
	shape := NewCircleShape(radius, m.Vec2d{})
	shape.Friction, shape.Restitution = friction, restitution
	return shape
}

// onRamp builds the ramp scene: a slope of that Friction, a block of that one
// resting on it just inside the Slop, and gravity written as a Force.
func onRamp(t testing.TB, angle, slopeFriction, blockFriction float64) (*harness, ecs.Entity, m.Vec2d) {
	t.Helper()
	h := newHarnessWith(t, nil, 64)
	slope := ramp(angle)
	slope.Friction = slopeFriction
	h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{}},
		Shape: slope,
	})

	const mass, radius = 2, 0.5
	_, out := rampFrame(angle)
	at := out.MulS(radius - restingDepth)
	body := h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Place: Position{Current: at},
		Body:  dynamic(t, mass, blockMoment, 0, 0),
		Shape: withMaterial(radius, blockFriction, 0),
	})
	h.game.push = m.Vec2d{Y: -gravity * mass}
	return h, body, at
}

func TestABlockOnARampSlidesAtTheClosedFormWhenTheSlopeBeatsItsFriction(t *testing.T) {
	// Half of the iff. tan 30° is 0.57735, and every Friction below it slides at
	// g(sin θ − u cos θ): gravity gives the block g sin θ · h of down-slope
	// velocity a tick and the Coulomb clamp takes exactly u · jn back, jn being
	// the m g cos θ · h that cancels the approach. The third rung is a thousandth
	// under the limit, which is what says the rule is the limit and not the
	// neighbourhood of it.
	//
	// The one-tick offset from gravity-as-Force is in the closed form rather than
	// in the tolerance: the Force an app writes on the first tick is turned into
	// velocity by that same tick's Solve, so after N ticks the block has taken N
	// increments and not N − 1.
	limit := math.Tan(rampAngle)
	for _, u := range []float64{0, 0.3, limit - 1e-3} {
		t.Run(fmt.Sprintf("u=%.5f", u), func(t *testing.T) {
			h, block, _ := onRamp(t, rampAngle, 1, u)
			down, out := rampFrame(rampAngle)

			for n := 1; n <= 60; n++ {
				h.frames(t, 1)
				got := h.read(t, block).Velocity.Linear
				want := float64(n) * tick * gravity * (math.Sin(rampAngle) - u*math.Cos(rampAngle))
				if slid := got.Dot(down); math.Abs(slid-want) > 1e-9 {
					t.Fatalf("after %d ticks the block slides at %.17g m/s, want the closed form's %.17g",
						n, slid, want)
				}
				if sunk := got.Dot(out); math.Abs(sunk) > 1e-9 {
					t.Fatalf("after %d ticks the block moves %v m/s into the ramp, want none", n, sunk)
				}
			}
		})
	}
}

func TestABlockOnARampDoesNotSlideAtAllWhenItsFrictionBeatsTheSlope(t *testing.T) {
	// The other half. At and above tan 30° the friction impulse the block needs
	// is inside the Coulomb clamp, so the tangent is driven to zero instead of to
	// the limit and nothing moves. Nothing may creep either — a block that slides
	// a millimetre a tick is a block that slides — so the position is held to the
	// same tolerance as the velocity.
	limit := math.Tan(rampAngle)
	for _, u := range []float64{limit, limit + 1e-3, 0.8} {
		t.Run(fmt.Sprintf("u=%.5f", u), func(t *testing.T) {
			h, block, at := onRamp(t, rampAngle, 1, u)

			h.frames(t, 240)
			got := h.read(t, block)
			if speed := got.Velocity.Linear.Length(); speed > 1e-12 {
				t.Errorf("a block whose Friction beats the slope moves at %v m/s, want none", speed)
			}
			if crept := got.Place.Current.Sub(at).Length(); crept > 1e-12 {
				t.Errorf("the block crept %v m down a ramp it grips, want none", crept)
			}
		})
	}
}

func TestTheRestitutionLadderGivesBounceHeightsOfESquared(t *testing.T) {
	// The bounce is taken in PreStep from the velocity before integration, so the
	// rebound is exactly e times the approach whatever gravity added this tick —
	// which is what stops gravity-fed jitter from eating Restitution, and is half
	// of why Solve is indivisible.
	//
	// The ladder is run on the entry's own e, which is the product of the wall's
	// 0.8 and the ball's, so the three rungs are three ball Restitutions and one
	// wall. The third is above 1, which is legal and unvalidated, and lands the
	// pair at exactly 1.
	for _, rung := range []struct{ ball, want float64 }{
		{0, 0}, {0.625, 0.5}, {1.25, 1},
	} {
		t.Run(fmt.Sprintf("e=%v", rung.want), func(t *testing.T) {
			wantBounceHeightRatio(t, rung.ball, rung.want)
		})
	}
}

// wantBounceHeightRatio drops a ball onto a wall at that Restitution and checks
// the height it comes back to against e² of the height its approach came from.
//
// The scene starts the ball one tick above the wall rather than up in the air,
// so that the tick it is found on has it exactly restingDepth deep and exactly
// the approach speed named here. A ball dropped from a height is found at
// whatever depth the tick boundary left it at, which is up to v·h of geometry
// nobody chose, and the closed form would then be measuring the sampling.
func wantBounceHeightRatio(t *testing.T, ballRestitution, wantE float64) {
	t.Helper()
	const mass, radius, approach = 2, 0.5, 2.0

	h := newHarnessWith(t, nil, 64)
	wall := NewSegmentShape(m.Vec2d{X: -5}, m.Vec2d{X: 5}, 0)
	wall.Restitution = 0.8
	h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{}},
		Shape: wall,
	})
	// One tick of the approach above the resting depth, so the first tick's
	// position integration lands the ball exactly restingDepth into the wall.
	start := radius + approach*tick - restingDepth
	placed := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Place:    Position{Current: m.Vec2d{Y: start}},
		Velocity: Velocity{Linear: m.Vec2d{Y: -approach}},
		Body:     dynamic(t, mass, 8, 0, 0),
		Shape:    withMaterial(radius, 0, ballRestitution),
	})
	h.game.push = m.Vec2d{Y: -gravity * mass}

	h.frames(t, 1)
	bounced := h.read(t, placed)
	rebound, floor := bounced.Velocity.Linear.Y, bounced.Place.Current.Y
	if want := wantE * approach; math.Abs(rebound-want) > 1e-12 {
		t.Fatalf("a pair at e = %v turned a %v m/s approach into %.17g m/s, want %.17g",
			wantE, approach, rebound, want)
	}

	// The apex, taken as the last height before the ball starts coming down. A
	// ball that never rises has its apex where it bounced, which is the e = 0
	// rung.
	apex := floor
	for range 120 {
		h.frames(t, 1)
		now := h.read(t, placed).Place.Current.Y
		if now < apex {
			break
		}
		apex = now
	}

	// Free flight is explicit Euler — the step integrates positions from the
	// velocity the previous tick ended with — which climbs exactly v·h/2 past the
	// continuous v²/2g, and the one-tick offset of gravity-as-Force is already in
	// that v. Both are subtracted here rather than covered by a tolerance.
	//
	// What is left is the tick boundaries: the apex is read at one of them, and
	// the parabola through them has a curvature of g·h² a tick, so the sampled
	// apex sits between the continuous one and g·h²/8 below it. That is the whole
	// of the band, and it is a closed form too.
	sampling := gravity * tick * tick / 8
	rose := apex - floor - rebound*tick/2
	want := rebound * rebound / (2 * gravity)
	if rose < want-1e-9 || rose > want+sampling+1e-9 {
		t.Fatalf("the ball rose %.17g m, want the closed form's %.17g and at most %.17g of sampling over it",
			rose, want, sampling)
	}

	// And the ticket's own acceptance, said as the ratio: the height a bounce
	// comes back to is e² of the height its approach came from, to within the
	// same band read as a share of that height.
	from := approach * approach / (2 * gravity)
	ratio := rose / from
	if ratio < wantE*wantE-1e-9 || ratio > wantE*wantE+sampling/from+1e-9 {
		t.Errorf("a bounce at e = %v came back to %.6f of the height it fell from, want e² = %v",
			wantE, ratio, wantE*wantE)
	}
}

func TestAFilterWritesTheMaterialForExactlyOneTick(t *testing.T) {
	// cp's PreSolve edits, landing where the specification promised them: a
	// filter System between Detect and Solve makes one pair rubber on ice without
	// writing a Shape, which is permanent and shared. The material never carries
	// across a tick, being recomputed from the two Shapes every tick as cp does,
	// so the filter's edit lasts exactly the tick it was made on.
	const mass, radius, along = 2, 0.5, 3.0

	h := newHarnessWith(t, nil, 64)
	h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{}},
		Shape: NewSegmentShape(m.Vec2d{X: -50}, m.Vec2d{X: 50}, 0),
	})
	// Both Shapes ship frictionless, so every grip below is the filter's alone.
	placed := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Place:    Position{Current: m.Vec2d{Y: radius - restingDepth}},
		Velocity: Velocity{Linear: m.Vec2d{X: along}},
		Body:     dynamic(t, mass, blockMoment, 0, 0),
		Shape:    NewCircleShape(radius, m.Vec2d{}),
	})
	h.game.push = m.Vec2d{Y: -gravity * mass}

	h.frames(t, 1)
	if got := h.read(t, placed).Velocity.Linear.X; math.Abs(got-along) > 1e-12 {
		t.Fatalf("two frictionless Shapes slowed the slide to %v, want the whole %v", got, along)
	}

	// One tick of grip. The Coulomb clamp binds at u·jn, and jn is the m·g·h that
	// holds the block up, so the tick takes exactly u·g·h off the slide.
	const filtered = 1.0
	h.game.filter = func(entry *Contact) { entry.Friction = filtered }
	h.frames(t, 1)
	h.game.filter = nil

	want := along - filtered*gravity*tick
	if got := h.read(t, placed).Velocity.Linear.X; math.Abs(got-want) > 1e-12 {
		t.Errorf("one tick of filtered Friction left the slide at %.17g, want %.17g", got, want)
	}
	if entry := h.contacts(t); len(entry) != 1 || entry[0].Friction != filtered {
		t.Fatalf("the filtered tick's entry reports %+v, want one at Friction %v", entry, filtered)
	}

	// And it is gone. Nothing was written to either Shape, so the next tick's
	// Detect recomputes the pair's material from the two of them and finds zero.
	h.frames(t, 1)
	if entry := h.contacts(t); len(entry) != 1 || entry[0].Friction != 0 {
		t.Fatalf("the filter's edit outlived its tick: %+v", entry)
	}
	if got := h.read(t, placed).Velocity.Linear.X; math.Abs(got-want) > 1e-12 {
		t.Errorf("the tick after the filter slowed the slide to %.17g, want the unchanged %.17g",
			got, want)
	}

	// A Shape write is the permanent one, and it reaches the entry as the product
	// with the wall's — which the wall ships at zero, so one slippery Shape is
	// still enough to make the pair slide.
	h.setShape(t, placed, withMaterial(radius, filtered, 0))
	h.frames(t, 2)
	if entry := h.contacts(t); len(entry) != 1 || entry[0].Friction != 0 {
		t.Fatalf("a gripping Shape against a frictionless one is %+v, want a Friction of 0", entry)
	}
}

func TestAFilterWritesASurfaceVelocityAndTheContactCarriesTheBodyAlongIt(t *testing.T) {
	// The package's Conveyors recipe, and its proof. Surface velocity is not a
	// Shape field: a belt is a Component of the app's own on the belt's Entity,
	// and a filter between Detect and Solve writes each touching entry's
	// SurfaceVelocity from it. The filter below is that recipe, for a belt that
	// is the wall.
	//
	// The sign is cp's surface_vr in the port's A/B convention. cp's Update
	// computes b.surfaceV − a.surfaceV, and this port's A plays the part cp's b
	// plays — the Normal is B's surface facing A — so the entry's value is A's
	// surface velocity less B's. The observable consequence is here: the solver
	// drives the relative tangential velocity plus the entry to zero, so a Body
	// resting on a belt ends up moving with the belt.
	//
	// The belt also runs a little into the wall, which the recipe removes along
	// with the rest of the normal component: Solve adds the entry to the
	// relative velocity before splitting it, so a normal component left in
	// would drive the pair apart or together instead of carrying anything.
	const mass, radius = 2, 0.5
	beltVelocity := m.Vec2d{X: 1, Y: -0.5}

	h := newHarnessWith(t, nil, 64)
	belt := h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{}},
		Shape: NewSegmentShape(m.Vec2d{X: -50}, m.Vec2d{X: 50}, 0),
	})
	placed := h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Place: Position{Current: m.Vec2d{Y: radius - restingDepth}},
		Body:  dynamic(t, mass, blockMoment, 0, 0),
		Shape: NewCircleShape(radius, m.Vec2d{}),
	})
	h.game.push = m.Vec2d{Y: -gravity * mass}

	// surfaceOf is the app's belt Component, looked up per party.
	surfaceOf := func(e ecs.Entity) m.Vec2d {
		if e == belt {
			return beltVelocity
		}
		return m.Vec2d{}
	}
	conveyor := func(entry *Contact) {
		if entry.A != belt && entry.B != belt {
			return
		}
		surface := surfaceOf(entry.A).Sub(surfaceOf(entry.B))
		entry.SurfaceVelocity = surface.Sub(entry.Normal.MulS(surface.Dot(entry.Normal)))
	}

	// Both Shapes ship at Friction 0, so the entry's product is 0 and the filter
	// has to write one: the drag stays bounded by the Coulomb clamp.
	h.game.filter = func(entry *Contact) {
		conveyor(entry)
		entry.Friction = 1
	}
	h.frames(t, 180)

	if got := h.read(t, placed).Velocity.Linear; math.Abs(got.X-beltVelocity.X) > 1e-12 || math.Abs(got.Y) > 1e-12 {
		t.Errorf("a belt at %v carried the Body to %v, want (%v, 0)", beltVelocity, got, beltVelocity.X)
	}

	// A belt with no Friction carries nothing: the drag is the same Coulomb
	// clamp, and at u = 0 the clamp is ±0.
	h.game.filter = conveyor
	before := h.read(t, placed).Velocity.Linear.X
	h.frames(t, 60)
	if got := h.read(t, placed).Velocity.Linear.X; math.Abs(got-before) > 1e-12 {
		t.Errorf("a frictionless belt changed the Body's slide from %v to %v", before, got)
	}
}

func TestACircleSlidingOnAWallWithMaterialSolvesExactlyAsChipmunkDoes(t *testing.T) {
	// The specification's layer B, and the layer that actually pins Friction and
	// Restitution: one solve from a fixed state over a short horizon, against cp,
	// with a material on both Shapes. The pair's u is 0.8 · 0.5 and its e is
	// 0.25 · 0.8, so both products are live, the bounce reverses a 2 m/s approach
	// into 0.4 m/s and the Coulomb clamp takes the slide from 3 m/s and spins the
	// ball up — which is the whole of this ticket in one trajectory.
	h := layerB(t)
	wall := NewSegmentShape(m.Vec2d{X: -5}, m.Vec2d{X: 5}, 0)
	wall.Friction, wall.Restitution = 0.8, 0.25
	h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{}},
		Shape: wall,
	})
	placed := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Place:    Position{Current: m.Vec2d{Y: 0.48}},
		Velocity: Velocity{Linear: m.Vec2d{X: 3, Y: -2}},
		Body:     dynamic(t, 2, 8, 0, 0),
		Shape:    withMaterial(0.5, 0.5, 0.8),
	})
	h.game.push = m.Vec2d{Y: -gravity * 2}

	wantTrajectory(t, h, placed, []cpStep{
		{0.050000000000000003, 0.44666666666666666, 0, 1.9746000000000017, 0.39999999999999902, -0.12817499999999993},
		{0.082910000000000039, 0.45816666666666667, -0.0021362499999999988, 1.9746000000000017, 0.23649999999999904, -0.12817499999999993},
		{0.11582000000000008, 0.46579166666666666, -0.0042724999999999977, 1.9746000000000017, 0.072999999999999038, -0.12817499999999993},
		{0.14873000000000011, 0.46992916666666662, -0.006408749999999996, 1.9442400000000004, -0.014599999999998003, -0.13197000000000009},
		{0.18113400000000013, 0.47219291666666668, -0.0086082499999999978, 1.8718320000000013, 0.0029199999999995896, -0.14102099999999998},
		{0.21233120000000016, 0.47452229166666665, -0.010958599999999997, 1.8078335999999986, -0.00058399999999481783, -0.14902080000000029},
		{0.24246176000000014, 0.47656032916666674, -0.013442280000000001, 1.7421532800000006, 0.00011679999999969406, -0.15723084000000001},
		{0.27149764800000015, 0.47840624291666672, -0.016062794000000002, 1.6768093439999998, -2.3359999998778477e-05, -0.16539883200000008},
		{0.29944447040000016, 0.48006522929166673, -0.018819441200000002, 1.6113981312000005, 4.6719999995392669e-06, -0.17357523359999999},
		{0.32630110592000017, 0.4815587842291667, -0.02171236176, 1.5460003737600028, -9.3440000464853958e-07, -0.18174995327999977},
	})
}
