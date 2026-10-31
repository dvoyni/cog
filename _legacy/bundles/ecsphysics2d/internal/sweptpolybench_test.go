package internal

import (
	"fmt"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// The swept Sensor against a Polygon is an arm nothing measured, because the two
// tickets that built its halves each measured only its own: the Sensor scene is
// circles and segments, and the Polygon scene has no Sensor in it. probeWorld
// falls through to probePoly, and probePoly builds a world cache of every face
// plane on the caller's stack — which is exactly the shape of thing that
// allocates if the scratch is ever outgrown.
//
// So this is the same claim and the same method as the two measurements already
// on the line, over the one scene neither of them covers. Its emptiness guards
// are the point of it: a run with no Sensor Hits against a Polygon measures the
// walk past an index rather than the arm.
func TestTheSweptSensorAgainstPolygonsSitsOnTheEnginesAllocationLine(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	const ticks = 4_000

	measure := func(n int) (float64, int, int) {
		h := newHarnessWith(t, nil, uint32(4*max(n, 1)))
		populateSweptPolygons(t, h, n)
		h.frames(t, 100)

		touching, swept := 0, 0
		for _, entry := range h.contacts(t) {
			touching++
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
	full, touching, swept := measure(256)
	t.Logf("objects a step: %.3f with no Bodies, %.3f at N=256 over %d Contacts (%d of them "+
		"Sensor Hits against a Polygon)", empty, full, touching, swept)

	// Both guards, for the reason two earlier rounds of this measurement needed
	// them: a scene with nothing in it passes every assertion below.
	if touching == 0 {
		t.Fatal("the measured scene has no Contacts at all, so it measures neither Detect nor Solve")
	}
	if swept == 0 {
		t.Fatal("the measured scene has no Sensor Hits at all, so it does not measure the swept " +
			"Sensor against a Polygon, which is the one thing it is here for")
	}

	if full-empty > 0.05 {
		t.Errorf("the swept Sensor's Polygon step costs %.3f objects a tick, want none", full-empty)
	}
}

// populateSweptPolygons is a field of static Polygons with swept Sensors flying
// across them: a quarter boxes carrying four vertices inline, a quarter
// triangles, and half hexagons carrying a Polygon Component, so every polygon
// kind probePoly sorts a Probe into is inside the measurement.
//
// The Sensors are circles rather than Polygons, and each flies along its own row
// far enough from every other that the only pairs it can make are against a
// Polygon. A Sensor against a Sensor would be a circle against a circle, and
// that is the arm the other two measurements already cover.
func populateSweptPolygons(t testing.TB, h *harness, n int) {
	t.Helper()
	if n == 0 {
		return
	}

	hexagon, hexagonVerts := regularPolygon(t, 6, 0.35)
	triangle, _ := regularPolygon(t, 3, 0.4)
	box := NewBoxShape(0.6, 0.6, 0)

	for i := range n {
		shape, polygon := box, Polygon{}
		switch i % 4 {
		case 1:
			shape = triangle
		case 2, 3:
			shape, polygon = hexagon, hexagonVerts
		}
		h.spawn(t, spawnRequest{
			Kind:    kindPolygonStatic,
			Place:   Position{Current: sweptGridAt(i)},
			Shape:   shape,
			Polygon: polygon,
		})
	}

	// One Sensor per row of sixteen, each starting on a Polygon's own centre and
	// drifting along its row at a tenth of a metre a second. The speed is the
	// reference scene's and is chosen the same way: slow enough that ten thousand
	// ticks leave it inside the field it is measuring — a Sensor that flies off
	// the grid measures a walk past an empty index, which is exactly what the
	// guards below refuse — and the Probe between two ticks is short, which is
	// the shape a real projectile's is.
	//
	// Starting inside a Polygon puts the T = 0 arm in the measurement too, where
	// the Hit is an overlap at the start rather than a crossing.
	//
	// A mass of a tonne of tonnes keeps the game's Force off them without a
	// Component set of their own.
	for i := range max(n/16, 1) {
		h.spawn(t, spawnRequest{
			Kind:     kindShapedBody,
			Place:    Position{Current: sweptGridAt(i * 16)},
			Velocity: Velocity{Linear: m.Vec2d{X: 0.1}},
			Body:     dynamic(t, 1e6, 1e6, 0, 0),
			Shape:    sensorCircle(0.1),
		})
	}
}

// sweptGridAt is where the i-th Polygon goes: the same 1.7 m grid the reference
// scene uses, so a cell holds more than one and they are not all in the same one.
func sweptGridAt(i int) m.Vec2d {
	return m.Vec2d{X: float64(i%16) * 1.7, Y: float64(i/16) * 1.7}
}

// BenchmarkTheSweptSensorAgainstPolygons is the cost of one whole tick over that
// scene. Wall-clock is asserted nowhere, here as anywhere else.
func BenchmarkTheSweptSensorAgainstPolygons(b *testing.B) {
	for _, n := range []int{256, 1024} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			h := newHarnessWith(b, nil, uint32(4*n))
			populateSweptPolygons(b, h, n)
			h.frames(b, 100)

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				h.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
			}
		})
	}
}
