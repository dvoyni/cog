package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"

	"github.com/dvoyni/cog/libs/m"
)

// The app-facing acceptance for the swept Sensor, run against a real engine:
// what the plugin does over the ticks a Body is published, never what its
// Systems look like. The numbers are closed forms of the geometry, at the
// specification's own 1e-9 m.

// wallFace is where a 0.4 m wall standing at x = 1 begins: a segment of radius
// 0.2 occupies x from 0.8 to 1.2.
const wallFace = 0.8

func TestNoProjectilePassesAFortyCentimetreWallAtFortyMetresASecond(t *testing.T) {
	// 40.4 m/s is 0.673 m a tick at 60 Hz, which clears a 0.4 m wall whole. The
	// projectile is an ordinary Dynamic body with a circle Shape marked a
	// Sensor: the package has no projectile concept, and the radius is 0,
	// because a point is what tunnels.
	h := newHarness(t)
	wall := h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{X: 1}},
		Shape: NewSegmentShape(m.Vec2d{Y: -2}, m.Vec2d{Y: 2}, 0.2),
	})
	arrow := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Velocity: Velocity{Linear: m.Vec2d{X: 40.4}},
		Body:     dynamic(t, 1, 1, 0, 0),
		Shape:    sensorCircle(0),
	})

	// The first tick lands short of the wall and the second clears it whole,
	// which is the tick a discrete test would have found nothing on.
	h.frame(t)
	if list := h.contacts(t); len(list) != 0 {
		t.Fatalf("the first tick, which ends short of the wall, reported %d Contacts", len(list))
	}
	before := h.read(t, arrow).Place.Current
	h.frame(t)
	after := h.read(t, arrow).Place.Current

	if before.X >= wallFace || after.X <= 1.2 {
		t.Fatalf("the tick ran from %v to %v; it is meant to clear the whole wall in one step",
			before.X, after.X)
	}

	list := h.contacts(t)
	if len(list) != 1 {
		t.Fatalf("a projectile that crossed the wall reported %d Contacts, want 1", len(list))
	}
	entry := list[0]
	if entry.A != arrow || entry.B != wall {
		t.Errorf("the entry names %v and %v, want the projectile and the wall", entry.A, entry.B)
	}
	if !entry.Sensor || entry.Phase != PhaseBegan {
		t.Errorf("the entry is Sensor %v and %v, want a Sensor's that Began", entry.Sensor, entry.Phase)
	}
	if !(entry.T > 0 && entry.T < 1) {
		t.Errorf("T is %v, want the fraction of the tick the wall was met at", entry.T)
	}

	// The Probe met the wall's near face, and snap-back — which is the app's
	// own write — puts the projectile exactly on it.
	wantAt(t, "the Point", entry.Points[0].Point, m.Vec2d{X: wallFace})
	wantAt(t, "the Normal", entry.Normal, m.Vec2d{X: -1})
	wantAt(t, "the snap-back", before.Lerp(after, entry.T), m.Vec2d{X: wallFace})
	if got := entry.Points[0].Depth; got != 0 {
		t.Errorf("Depth is %v, want 0: a Probed Sensor met the Shape rather than overlapping it", got)
	}
}

func TestASensorReportsWhatItTouchedOnTheWayThroughNotOnlyWhereItEnded(t *testing.T) {
	// Three pebbles inside one tick's flight, and a projectile fast enough to
	// end past all of them. A discrete test finds none.
	h := newHarness(t)
	var pebbles []ecs.Entity
	for _, at := range []float64{0.2, 0.4, 0.6} {
		pebbles = append(pebbles, h.spawn(t, spawnRequest{
			Kind:  kindShapedStatic,
			Place: Position{Current: m.Vec2d{X: at}},
			Shape: circle(0.05),
		}))
	}
	arrow := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Velocity: Velocity{Linear: m.Vec2d{X: 60}},
		Body:     dynamic(t, 1, 1, 0, 0),
		Shape:    sensorCircle(0),
	})

	h.frame(t)
	if got := h.read(t, arrow).Place.Current.X; !near(got, 1) {
		t.Fatalf("the projectile ended the tick at %v, want a metre along", got)
	}

	list := h.contacts(t)
	if len(list) != 3 {
		t.Fatalf("a projectile that flew past three pebbles reported %d Contacts, want 3", len(list))
	}
	// The entries sit together and in order of T, so the app takes the first
	// and stops at whatever its own rule says stops it.
	for i, pebble := range pebbles {
		if list[i].A != arrow || list[i].B != pebble {
			t.Errorf("entry %d names %v and %v, want the projectile and pebble %d",
				i, list[i].A, list[i].B, i)
		}
		if got, want := list[i].T, 0.2*float64(i+1)-0.05; !near(got, want) {
			t.Errorf("entry %d met its pebble at T %v, want %v", i, got, want)
		}
	}
}

func TestThePluginNeverMovesASensorBackAndNeverStopsOne(t *testing.T) {
	h := newHarness(t)
	h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{X: 1}},
		Shape: NewSegmentShape(m.Vec2d{Y: -2}, m.Vec2d{Y: 2}, 0.2),
	})
	arrow := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Velocity: Velocity{Linear: m.Vec2d{X: 40.4}},
		Body:     dynamic(t, 1, 1, 0, 0),
		Shape:    sensorCircle(0),
	})

	// Snap-back, reflection, an explosion and an expiry are all app writes. The
	// plugin's own part is to report and to leave the Body alone, so the
	// projectile's flight is exactly v·h a tick for as long as it is published,
	// through the tick it met the wall and past it.
	reported := 0
	for step := 1; step <= 6; step++ {
		h.frame(t)
		body := h.read(t, arrow)
		if got, want := body.Velocity.Linear.X, 40.4; got != want {
			t.Fatalf("tick %d left the projectile at %v m/s, want %v: nothing here stops one",
				step, got, want)
		}
		if got, want := body.Place.Current.X, 40.4*tick*float64(step); !near(got, want) {
			t.Fatalf("tick %d left the projectile at %v m, want %v: nothing here moves one back",
				step, got, want)
		}
		for _, entry := range h.contacts(t) {
			if entry.Sensor && entry.Phase != PhaseEnded {
				reported++
			}
		}
	}
	if reported == 0 {
		t.Fatal("the projectile flew through the wall without the plugin reporting anything")
	}
}

func TestABodyChangesWhatItCollidesWithByWritingItsOwnShape(t *testing.T) {
	const (
		plates uint32 = 1 << 0
		crates uint32 = 1 << 1
	)
	// A stationary Sensor overlapping a crate: a pressure plate as a Body, so
	// that the pair is reported and never solved and neither Body moves,
	// leaving the groups the only thing that can change the answer.
	h := newHarness(t)
	plate := sensorCircle(0.5)
	plate.CollisionBits, plate.CollidesWith = plates, crates
	h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Body:  dynamic(t, 1, 1, 0, 0),
		Shape: plate,
	})

	crate := circle(0.5)
	crate.CollisionBits, crate.CollidesWith = crates, plates
	box := h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Place: Position{Current: m.Vec2d{X: 0.5}},
		Body:  dynamic(t, 1, 1, 0, 0),
		Shape: crate,
	})

	h.frame(t)
	if list := h.contacts(t); len(list) != 1 || !list[0].Sensor {
		t.Fatalf("a plate overlapping a crate reported %d Contacts, want one Sensor's", len(list))
	}

	// The plugin has no collision configuration at all: there is no matrix and
	// no rule list, and a Body changes what it collides with by writing its own
	// Shape, which Index picks up on the next tick.
	crate.CollidesWith = CollisionBitsNone
	h.setShape(t, box, crate)

	h.frame(t)
	for _, entry := range h.contacts(t) {
		if entry.Phase != PhaseEnded {
			t.Errorf("the crate still reports %v after it stopped looking for the plate", entry.Phase)
		}
	}
	h.frame(t)
	if list := h.contacts(t); len(list) != 0 {
		t.Errorf("the pair still reports %d Contacts two ticks after the Shape was rewritten", len(list))
	}
}

// sensorCircle is a circle Shape marked a Sensor, which is the whole of what a
// projectile is: the package has no projectile concept of its own, and drag,
// homing Force and inherited velocity all come from the integrator.
func sensorCircle(radius float64) Shape {
	shape := NewCircleShape(radius, m.Vec2d{})
	shape.Sensor = true
	return shape
}

// wantAt compares a vector against the closed form at the specification's own
// 1e-9 m.
func wantAt(t testing.TB, name string, got, want m.Vec2d) {
	t.Helper()
	if abs(got.X-want.X) > 1e-9 || abs(got.Y-want.Y) > 1e-9 {
		t.Errorf("%s is (%.17g, %.17g), want (%.17g, %.17g)", name, got.X, got.Y, want.X, want.Y)
	}
}
