package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/libs/m"
)

// gravity is the Force a 1 kg Body is pushed by in these scenes, which is what
// makes a stack settle rather than float. It is the app's own write, added
// Before[IntegrateOnUpdate] like any other.
const gravity = -9.8

// ground is the static floor every scene here rests on: a 10 m by 1 m box whose
// top face is exactly y = 0.
func ground() ecsphysics2d.Shape {
	return ecsphysics2d.NewBoxShapeFor(ecsphysics2d.NewBB(-5, -1, 5, 0), 0)
}

func TestBoxesStackAndSettle(t *testing.T) {
	h := newHarness(t)
	h.game.push = m.Vec2d{Y: gravity}

	h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: ecsphysics2d.Position{},
		Shape: ground(),
	})

	// Three boxes dropped a little above where they belong, each 1 m square.
	// The moment is a real box's, so a box landing off-centre turns rather than
	// sliding rigidly.
	box := ecsphysics2d.NewBoxShape(1, 1, 0)
	moment := ecsphysics2d.MomentForBox(1, 1, 1)
	var boxes []ecs.Entity
	for i := range 3 {
		boxes = append(boxes, h.spawn(t, spawnRequest{
			Kind:  kindShapedBody,
			Place: ecsphysics2d.Position{Current: m.Vec2d{Y: 0.6 + 1.05*float64(i)}},
			Body:  dynamic(t, 1, moment, 0, 0),
			Shape: box,
		}))
	}

	h.frames(t, 400)

	for i, e := range boxes {
		place := h.read(t, e).Place
		want := 0.5 + float64(i)
		// The stack rests within a Slop of where the geometry puts it, and the
		// Slop is 0.005 m: three boxes may sit at most three of them low.
		if math.Abs(place.Current.Y-want) > 0.02 {
			t.Errorf("box %d settled at y = %v, want %v within a few Slops", i, place.Current.Y, want)
		}
		// Sideways is a looser bound than up, and has to be: nothing here has
		// Friction — that is a later ticket — so the impulses that part the
		// stack shuffle it a few centimetres before it comes to rest. What is
		// asserted is that it comes to rest above where it was dropped rather
		// than sliding off.
		if math.Abs(place.Current.X) > 0.05 {
			t.Errorf("box %d drifted to x = %v, want it above where it was dropped", i, place.Current.X)
		}
		if math.Abs(place.Angle) > 0.02 {
			t.Errorf("box %d settled turned by %v rad, want it square", i, place.Angle)
		}

		velocity := h.read(t, e).Velocity
		if velocity.Linear.Length() > 0.01 || math.Abs(velocity.Angular) > 0.01 {
			t.Errorf("box %d is still moving at %v, %v rad/s after 400 ticks",
				i, velocity.Linear, velocity.Angular)
		}
	}

	// Every pair in the stack is reported, and Continuing rather than beginning
	// again every tick.
	contacts := h.contacts(t)
	if len(contacts) < 3 {
		t.Fatalf("a settled three-box stack on a floor reports %d Contacts, want at least 3",
			len(contacts))
	}
	for _, entry := range contacts {
		if entry.Phase != ecsphysics2d.PhaseContinuing {
			t.Errorf("a settled Contact is in phase %v, want Continuing", entry.Phase)
		}
		if entry.Count != 2 {
			t.Errorf("a box resting squarely on a box has %d points, want 2", entry.Count)
		}
	}
}

func TestAPolygonComponentReachesTheIndexAndCollides(t *testing.T) {
	h := newHarness(t)
	h.game.push = m.Vec2d{Y: gravity}

	// A hexagon, which is the smallest Shape that cannot carry its vertices
	// inline and so needs the second Component.
	var corners []m.Vec2d
	for i := range 6 {
		corners = append(corners, m.ForAngle(-2*math.Pi*float64(i)/6).MulS(0.5))
	}
	shape, polygon, err := ecsphysics2d.NewPolygonShape(corners, 0)
	if err != nil {
		t.Fatalf("hulling a hexagon: %v", err)
	}
	if shape.Kind != ecsphysics2d.ShapePoly {
		t.Fatalf("a hexagon is kind %v, want ShapePoly", shape.Kind)
	}

	h.spawn(t, spawnRequest{Kind: kindShapedStatic, Shape: ground()})
	hexagon := h.spawn(t, spawnRequest{
		Kind:    kindPolygonBody,
		Place:   ecsphysics2d.Position{Current: m.Vec2d{Y: 1.5}},
		Body:    dynamic(t, 1, ecsphysics2d.MomentForPoly(1, corners, m.Vec2d{}, 0), 0, 0),
		Shape:   shape,
		Polygon: polygon,
	})

	h.frames(t, 300)

	place := h.read(t, hexagon).Place
	// A regular hexagon of circumradius 0.5 built this way has a flat bottom at
	// −0.5·cos(30°) below its centre.
	want := 0.5 * math.Cos(math.Pi/6)
	if math.Abs(place.Current.Y-want) > 0.02 {
		t.Errorf("the hexagon settled at y = %v, want %v within a few Slops", place.Current.Y, want)
	}

	// It is in the Body index where it settled, which is the whole of "Index
	// probes the Polygon and copies it into the world cache".
	indexed := h.indexed(t, m.Vec2d{Y: want}, 0.1)
	if len(indexed.Bodies) != 1 || indexed.Bodies[0] != hexagon {
		t.Errorf("the Body index found %v under the hexagon, want just %v", indexed.Bodies, hexagon)
	}

	contacts := h.contacts(t)
	if len(contacts) != 1 {
		t.Fatalf("a hexagon resting on a floor reports %d Contacts, want 1", len(contacts))
	}
	if contacts[0].Count != 2 {
		t.Errorf("a hexagon resting on its flat bottom has %d points, want 2", contacts[0].Count)
	}
}

func TestAShapePolyWithNoPolygonBesideItIsInNoCell(t *testing.T) {
	// The package's stated-not-checked stance: a ShapePoly whose Polygon
	// Component never arrived caches nothing, so it is listed nowhere and costs
	// every query nothing. The way to avoid it is to spawn the two together.
	h := newHarness(t)

	var corners []m.Vec2d
	for i := range 5 {
		corners = append(corners, m.ForAngle(-2*math.Pi*float64(i)/5))
	}
	shape, _, err := ecsphysics2d.NewPolygonShape(corners, 0)
	if err != nil {
		t.Fatalf("hulling a pentagon: %v", err)
	}

	h.spawn(t, spawnRequest{Kind: kindShapedStatic, Shape: shape})
	h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Body:  dynamic(t, 1, 1, 0, 0),
		Shape: shape,
	})
	h.frames(t, 2)

	indexed := h.indexed(t, m.Vec2d{}, 0.1)
	if indexed.StaticLen != 1 || indexed.BodyLen != 1 {
		t.Fatalf("the indices hold %d statics and %d Bodies, want one each: both are held, "+
			"only not listed", indexed.StaticLen, indexed.BodyLen)
	}
	if len(indexed.Statics) != 0 || len(indexed.Bodies) != 0 {
		t.Errorf("an Overlap at the origin found %v and %v, want neither",
			indexed.Statics, indexed.Bodies)
	}
	if got := h.contacts(t); len(got) != 0 {
		t.Errorf("two vertexless ShapePolys made %d Contacts, want none", len(got))
	}
}

func TestACircleRollingAcrossAChainJointIsNotTurnedBack(t *testing.T) {
	// The acceptance the segment neighbours exist for, played out over ticks
	// rather than as a pair test: a circle pushed along a run of two segments
	// must cross the joint between them without losing speed to a cap it should
	// never have met.
	h := newHarness(t)

	left := m.Vec2d{X: -3}
	middle := m.Vec2d{}
	right := m.Vec2d{X: 3}
	for _, wall := range [][4]m.Vec2d{
		{left, left, middle, right},
		{left, middle, right, right},
	} {
		h.spawn(t, spawnRequest{
			Kind:  kindShapedStatic,
			Shape: ecsphysics2d.NewSegmentShapeWithNeighbours(wall[0], wall[1], wall[2], wall[3], 0),
		})
	}

	ball := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Place:    ecsphysics2d.Position{Current: m.Vec2d{X: -1, Y: 0.25}},
		Velocity: ecsphysics2d.Velocity{Linear: m.Vec2d{X: 2}},
		Body:     dynamic(t, 1, ecsphysics2d.MomentForCircle(1, 0, 0.25, m.Vec2d{}), 0, 0),
		Shape:    ecsphysics2d.NewCircleShape(0.25, m.Vec2d{}),
	})
	h.game.push = m.Vec2d{Y: gravity}

	slowest := math.Inf(1)
	for range 90 {
		h.frame(t)
		if speed := h.read(t, ball).Velocity.Linear.X; speed < slowest {
			slowest = speed
		}
	}

	place := h.read(t, ball).Place
	if place.Current.X < 1 {
		t.Errorf("the ball is at x = %v after 1.5 s at 2 m/s, want it well past the joint",
			place.Current.X)
	}
	if slowest < 1.9 {
		t.Errorf("the ball slowed to %v m/s crossing the joint, want it never checked", slowest)
	}
	if place.Current.Y < 0.2 {
		t.Errorf("the ball sank to y = %v, want it resting on the run at 0.25", place.Current.Y)
	}
}
