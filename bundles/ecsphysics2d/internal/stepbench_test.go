package internal

import (
	"fmt"
	"math"
	"runtime"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// TestTheStepSitsOnTheEnginesAllocationLine measures a steady state rather than
// an average over b.N, because an average can hide amortised growth: ten
// thousand ticks counted through a MemStats delta, at three workload sizes an
// order of magnitude apart.
//
// Zero heap allocation on the hot path is one of the four requirements the
// design is bound by, and it is measured rather than asserted. What is measured
// is the *slope*: an engine with no Bodies at all still pays the kernel's own
// per-tick dispatch, once per subscription, and that is the engine's line
// rather than physics'. An allocation inside the step would scale with the Body
// count, so the claim is that the empty engine, N=256 and N=1024 all cost the
// same. cp allocates 164 objects and 12.5 KB a step at N=256 and 656 and 49.8
// KB at N=1024; the port allocates none of its own at either size.
//
// The measured scene's Shapes carry a Friction and a Restitution, so the
// material is inside the claim rather than beside it, and the scene is refused
// outright below if it finds no Contacts at all — a measurement over an empty
// Contact list would pass without measuring either Detect or Solve.
func TestTheStepSitsOnTheEnginesAllocationLine(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	const ticks = 10_000

	measure := func(n int) (float64, int, int) {
		h := newHarnessWith(t, nil, uint32(2*max(n, 1)))
		populate(t, h, n)
		h.game.push = m.Vec2d{X: 10}
		// Warm every pool the first ticks fill, so what is measured is the
		// steady state and not the first tick.
		h.frames(t, 100)
		list := h.contacts(t)
		touching, swept := len(list), 0
		for _, entry := range list {
			if entry.Sensor {
				swept++
			}
		}
		mallocs := allocationsDuring(func() {
			for range ticks {
				h.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
			}
		})
		return float64(mallocs) / ticks, touching, swept
	}

	empty, _, _ := measure(0)
	small, smallTouching, smallSwept := measure(256)
	large, largeTouching, largeSwept := measure(1024)
	t.Logf("objects a step: %.3f with no Bodies, %.3f at N=256 over %d Contacts (%d Sensor Hits), "+
		"%.3f at N=1024 over %d Contacts (%d Sensor Hits) — the first is the kernel's own dispatch over "+
		"the ten subscriptions this engine composes",
		empty, small, smallTouching, smallSwept, large, largeTouching, largeSwept)
	if smallTouching == 0 || largeTouching == 0 {
		t.Fatal("the measured scene has no Contacts at all, so it measures neither Detect nor Solve")
	}
	// Two earlier rounds measured a scene with nothing in it without noticing,
	// which is why the emptiness guards are assertions rather than a comment.
	// The swept Sensors have one of their own: the Probe half of detection is
	// not on the line unless the scene is Probing something.
	if smallSwept == 0 || largeSwept == 0 {
		t.Fatal("the measured scene has no Sensor Hits at all, so it does not measure the Probe half of Detect")
	}

	if small > empty+0.05 {
		t.Errorf("256 Bodies cost %.3f objects a step against %.3f with none", small, empty)
	}
	if large > small+0.05 {
		t.Errorf("allocation scales with the Body count: %.3f a step at N=256, %.3f at N=1024", small, large)
	}
	if large-empty > 0.05 {
		t.Errorf("the step's own cost is %.3f objects a tick, want none", large-empty)
	}
}

// TestThePolygonStepSitsOnTheEnginesAllocationLineToo measures the same slope
// over a scene of Polygons rather than circles, which is what puts GJK, EPA and
// the Polygon Component's copy into the index inside the measurement.
//
// It is the same claim and the same method as the circle measurement above, at
// two sizes rather than three and over fewer ticks, because what it adds is one
// question: whether the narrowphase's own path allocates. cp's does — a fresh
// hull slice on every one of up to thirty EPA iterations, plus the lookup key
// per touching pair — and this is where the port's two ping-ponged stack
// buffers are worth their comment.
func TestThePolygonStepSitsOnTheEnginesAllocationLineToo(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	const ticks = 4_000

	measure := func(n int) (float64, int) {
		h := newHarnessWith(t, nil, uint32(4*max(n, 1)))
		populatePolygons(t, h, n)
		h.game.push = m.Vec2d{Y: -9.8}
		h.frames(t, 100)
		touching := len(h.contacts(t))
		mallocs := allocationsDuring(func() {
			for range ticks {
				h.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
			}
		})
		return float64(mallocs) / ticks, touching
	}

	empty, _ := measure(0)
	full, touching := measure(256)
	t.Logf("objects a step: %.3f with no Bodies, %.3f at N=256 over %d Contacts", empty, full, touching)
	if touching == 0 {
		t.Fatal("the measured scene has no Contacts at all, so it measures neither Detect nor Solve")
	}
	if full-empty > 0.05 {
		t.Errorf("the Polygon step's own cost is %.3f objects a tick, want none", full-empty)
	}
}

// TestTheStepUnderGravitySitsOnTheEnginesAllocationLine is the Polygon scene
// with its gravity read from Constants rather than written into Force, and an
// app System writing Constants every tick, so that the Resource read in Solve
// and the app's write are both inside the measurement. The empty engine carries
// the same writer, so the line it sets includes that subscription's dispatch.
func TestTheStepUnderGravitySitsOnTheEnginesAllocationLine(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	const ticks = 4_000

	measure := func(n int) (float64, int) {
		h, _ := newGravityHarness(t, nil, m.Vec2d{Y: -9.8})
		populatePolygons(t, h, n)
		h.frames(t, 100)
		touching := len(h.contacts(t))
		mallocs := allocationsDuring(func() {
			for range ticks {
				h.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
			}
		})
		return float64(mallocs) / ticks, touching
	}

	empty, _ := measure(0)
	full, touching := measure(256)
	t.Logf("objects a step under gravity: %.3f with no Bodies, %.3f at N=256 over %d Contacts",
		empty, full, touching)
	if touching == 0 {
		t.Fatal("the measured scene has no Contacts at all, so it measures neither Detect nor Solve")
	}
	if full-empty > 0.05 {
		t.Errorf("the step's own cost under gravity is %.3f objects a tick, want none", full-empty)
	}
}

// populatePolygons spawns n Polygon Bodies over a grid of static boxes: a
// quarter of them boxes carrying their four vertices inline, a quarter
// triangles, and half hexagons carrying a Polygon Component, so that all three
// polygon kinds and every pair the switch sorts them into are inside the
// measurement.
func populatePolygons(t testing.TB, h *harness, n int) {
	t.Helper()
	if n == 0 {
		return
	}

	hexagon, hexagonVerts := regularPolygon(t, 6, 0.3)
	triangle, _ := regularPolygon(t, 3, 0.35)
	box := NewBoxShape(0.5, 0.5, 0)

	for i := range n {
		shape, polygon := box, Polygon{}
		switch i % 4 {
		case 1:
			shape = triangle
		case 2, 3:
			shape, polygon = hexagon, hexagonVerts
		}
		h.spawn(t, spawnRequest{
			Kind:    kindPolygonBody,
			Place:   Position{Current: gridAt(i).Add(m.Vec2d{Y: 0.45})},
			Body:    dynamic(t, 1, 0.1, 0, 0),
			Shape:   shape,
			Polygon: polygon,
		})
	}
	// A static floor tile under each of them, close enough that every Body
	// rests on one and the pair Continues every tick.
	for i := range n {
		h.spawn(t, spawnRequest{
			Kind:  kindShapedStatic,
			Place: Position{Current: gridAt(i)},
			Shape: NewBoxShapeFor(NewBB(-0.8, -0.4, 0.8, 0.2), 0),
		})
	}
}

// regularPolygon is the n-gon of that circumradius, built the one way in.
func regularPolygon(t testing.TB, count int, radius float64) (Shape, Polygon) {
	t.Helper()
	corners := make([]m.Vec2d, count)
	for i := range count {
		corners[i] = m.ForAngle(-2 * math.Pi * float64(i) / float64(count)).MulS(radius)
	}
	shape, polygon, err := NewPolygonShape(corners, 0)
	if err != nil {
		t.Fatalf("hulling a %d-gon: %v", count, err)
	}
	return shape, polygon
}

// BenchmarkThePolygonStep is BenchmarkTheStep's scene made of Polygons, which
// is the narrowphase's expensive half: GJK and, wherever two of them overlap,
// EPA.
func BenchmarkThePolygonStep(b *testing.B) {
	for _, n := range []int{256, 1024} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			h := newHarnessWith(b, nil, uint32(4*n))
			populatePolygons(b, h, n)
			h.game.push = m.Vec2d{Y: -9.8}
			h.frames(b, 100)

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				h.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
			}
		})
	}
}

// BenchmarkTheStep is the cost of one whole tick over a scene of moving Bodies,
// half of them Dynamic under a Force and half Kinematic. Wall-clock is asserted
// nowhere: a whole-frame benchmark here swings about ±10% by run order, so cp's
// numbers are compared against interleaved A/B and recorded, never asserted.
func BenchmarkTheStep(b *testing.B) {
	for _, n := range []int{256, 1024} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			h := newHarnessWith(b, nil, uint32(2*n))
			populate(b, h, n)
			h.game.push = m.Vec2d{X: 10}
			h.frames(b, 100)

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				h.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
			}
		})
	}
}

// populate spawns n Bodies, half Dynamic under a Force and half Kinematic, plus
// a quarter as many Static ones for the walk to step over. A count of zero
// spawns nothing, which is the engine's own line to measure the rest against.
//
// The Dynamic half and the statics carry Shapes, and are spread over a grid
// rather than stacked in one cell, so that the per-tick Body rebuild — Clear
// then one Insert per Body, which is squarely on the hot path — is inside what
// is measured, and doing the work a real scene gives it. The Kinematic half
// stays shapeless, which is the other thing worth pinning: a shapeless Body is
// in neither index and costs the rebuild nothing.
//
// Each Static sits 0.75 m along +X from one of the Dynamic Bodies, which are
// pushed into them: a summed radius of 0.8 m, so the pair overlaps and the
// solver holds it at the Slop for ever. That gives n/4 Contacts that Began once
// and Continue every tick after, so detection, the pair map, the gather, the
// warm start and ten iterations are all inside the measurement rather than
// walking an empty list. The other quarter of the Dynamic Bodies drift free,
// which keeps the no-Contact path in it too.
func populate(t testing.TB, h *harness, n int) {
	t.Helper()
	if n == 0 {
		return
	}
	for i := range n / 2 {
		h.spawn(t, spawnRequest{
			Kind:  kindShapedBody,
			Place: Position{Current: gridAt(i)},
			Body:  dynamic(t, 2, 8, 15, 0.3),
			Shape: measured(0.4),
		})
	}
	h.spawn(t, spawnRequest{
		Kind:     kindKinematic,
		Count:    n / 2,
		Velocity: Velocity{Linear: m.Vec2d{X: 1}},
	})
	for i := range n / 4 {
		h.spawn(t, spawnRequest{
			Kind:  kindShapedStatic,
			Place: Position{Current: gridAt(i).Add(m.Vec2d{X: 0.75})},
			Shape: measured(0.4),
		})
	}
	// A sixteenth of the scene again as swept Sensors, so the Probe half of
	// Detect is inside the measurement rather than walking past an index with
	// no Sensor in it.
	//
	// Each starts on a Static's own centre and drifts along +X at a tenth of a
	// metre a second, which keeps it inside the band of Statics for the whole
	// run and touching one for sixteen seventeenths of it: the Probe is short,
	// which is the shape a real projectile's is between two ticks, and it
	// starts inside something often enough that the T = 0 arm is measured too.
	// They are stacked a couple of centimetres apart in Y within eight columns,
	// so they Probe each other as well and the pair each of them finds twice is
	// resolved every tick.
	//
	// A mass of a tonne of tonnes is what keeps the game's Force off them
	// without a Component set of their own: at a tenth of a newton-second per
	// tonne the whole run adds under two millimetres a second.
	for i := range n / 16 {
		h.spawn(t, spawnRequest{
			Kind:     kindShapedBody,
			Place:    Position{Current: gridAt(i % 8).Add(m.Vec2d{X: 0.75, Y: 0.02 * float64(i/8)})},
			Velocity: Velocity{Linear: m.Vec2d{X: 0.1}},
			Body:     dynamic(t, 1e6, 1e6, 0, 0),
			Shape:    sensorCircle(0.4),
		})
	}
}

// measured is the Shape the measured scene is made of: a circle carrying a
// material, so that the Coulomb clamp and the bounce are inside the measurement
// rather than beside it. Neither changes what the scene settles to — the pairs
// are pressed straight along their normals, so the tangent stays at zero and a
// settled pair has no approach left to give back — which is what keeps the
// Contacts the measurement needs from going away.
func measured(radius float64) Shape {
	shape := circle(radius)
	shape.Friction, shape.Restitution = 0.7, 0.3
	return shape
}

// gridAt is where the i-th Shape goes: a 1.7 m grid, the spacing the index's
// own measurements use — close enough that a 2 m cell holds more than one, far
// enough apart that they are not all in the same one.
func gridAt(i int) m.Vec2d {
	return m.Vec2d{X: float64(i%16) * 1.7, Y: float64(i/16) * 1.7}
}

// TestTheJointedStepSitsOnTheEnginesAllocationLine is the same measurement over
// a scene that also has Joints in it, because the Joint half of Solve carries
// three buffers of its own — the dense Joint rows, the Entity table that
// answers for a jointed Body detection never saw, and the pair set Index
// rebuilds — and a buffer that is rebuilt rather than refilled would scale with
// the Joint count.
//
// It fails if the scene finds no Contacts and it fails if the scene's Joints
// delivered no Impulse, for the same reason: a measurement over an empty list
// measures the walk and not the work.
func TestTheJointedStepSitsOnTheEnginesAllocationLine(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	const ticks = 10_000

	measure := func(n int) (float64, int, int, float64) {
		h := newHarnessWith(t, nil, uint32(3*max(n, 1)))
		joints, sample := populateJointed(t, h, n)
		h.game.push = m.Vec2d{X: 10}
		h.frames(t, 100)
		touching := len(h.contacts(t))
		var impulse float64
		if sample != ecs.NoEntity {
			impulse = h.joint(t, sample).Joint.Impulse()
		}
		mallocs := allocationsDuring(func() {
			for range ticks {
				h.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
			}
		})
		return float64(mallocs) / ticks, touching, joints, impulse
	}

	empty, _, _, _ := measure(0)
	small, smallTouching, smallJoints, smallImpulse := measure(256)
	large, largeTouching, largeJoints, largeImpulse := measure(1024)
	t.Logf("objects a step: %.3f with nothing, %.3f at N=256 over %d Contacts and %d Joints, "+
		"%.3f at N=1024 over %d Contacts and %d Joints",
		empty, small, smallTouching, smallJoints, large, largeTouching, largeJoints)
	if smallTouching == 0 || largeTouching == 0 {
		t.Fatal("the measured scene has no Contacts at all, so it measures neither Detect nor Solve")
	}
	if smallJoints == 0 || largeJoints == 0 {
		t.Fatal("the measured scene has no Joints at all, so it measures none of the Joint pass")
	}
	if smallImpulse == 0 || largeImpulse == 0 {
		t.Fatal("the measured scene's Joints delivered no Impulse, so the Joint pass did no work")
	}

	if small > empty+0.05 {
		t.Errorf("256 Bodies cost %.3f objects a step against %.3f with none", small, empty)
	}
	if large > small+0.05 {
		t.Errorf("allocation scales with the Body count: %.3f a step at N=256, %.3f at N=1024", small, large)
	}
	if large-empty > 0.05 {
		t.Errorf("the step's own cost is %.3f objects a tick, want none", large-empty)
	}
}

// BenchmarkTheJointedStep is the cost of one whole tick over the same scene
// with Joints in it. Wall-clock is asserted nowhere, here as anywhere else.
func BenchmarkTheJointedStep(b *testing.B) {
	for _, n := range []int{256, 1024} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			h := newHarnessWith(b, nil, uint32(3*n))
			populateJointed(b, h, n)
			h.game.push = m.Vec2d{X: 10}
			h.frames(b, 100)

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				h.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
			}
		})
	}
}

// populateJointed is the reference scene with a quarter as many pin Joints laid
// over it, and reports how many it spawned and one of them to read an Impulse
// off.
//
// Each Joint holds two shapeless Bodies of different masses under the same
// Force, so the pin is really loaded every tick rather than merely walked: two
// Bodies of the same mass under the same Force accelerate alike and a pin
// between them does nothing. They carry no Shape, so they are in no index and
// the Contact half of the measurement is exactly what populate already makes —
// which is also what puts the jointed Bodies through the Entity table rather
// than through the slot table, the path a jointed Body that touches nothing
// takes.
//
// Every one of them says its two Bodies do not collide, so JointedPairs is
// filled and Detect's check is inside the measurement too.
func populateJointed(t testing.TB, h *harness, n int) (int, ecs.Entity) {
	t.Helper()
	if n == 0 {
		return 0, ecs.NoEntity
	}
	populate(t, h, n)

	var first ecs.Entity
	count := 0
	for i := range n / 4 {
		at := gridAt(i)
		a := h.spawn(t, spawnRequest{
			Kind:  kindDynamic,
			Place: Position{Current: at},
			Body:  dynamic(t, 2, 8, 15, 0.3),
		})
		b := h.spawn(t, spawnRequest{
			Kind:  kindDynamic,
			Place: Position{Current: at.Add(m.Vec2d{Y: 0.6})},
			Body:  dynamic(t, 5, 3, 15, 0.3),
		})
		joint := NewPinJoint(a, b, m.Vec2d{}, m.Vec2d{}, 0.6)
		joint.CollideBodies = false
		e := h.spawn(t, spawnRequest{Kind: kindJoint, Joint: joint})
		if count == 0 {
			first = e
		}
		count++
	}
	return count, first
}

// allocationsDuring counts the objects f allocates, the way ecs's own frame
// measurement does.
func allocationsDuring(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.Mallocs - before.Mallocs
}
