package internal

import (
	"fmt"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// A fast Body sliding over a tiled floor is not stopped at the seams
// (continuous-collision.md § A seam stops nothing, issue #588). A resting
// Body sits a few millimetres into the surface it slides on, so its path
// meets the next tile's leading corner or face, which it did not touch where
// the tick began. That meeting is one the discrete walk resolves on the right
// side, upwards, and it must stop nothing. The off-centre post is the case the
// rule that tells the two apart is weakest on, and the graze is the limit it
// names.

// seamFloor is one kind of tiled floor: tiles of one width laid edge to edge,
// 0.2 m thick, their top faces at y = 0.
type seamFloor struct {
	name string
	tile func(width float64) Shape
}

func seamFloors() []seamFloor {
	return []seamFloor{
		{"box tiles", func(width float64) Shape {
			return NewBoxShape(width, 0.2, 0)
		}},
		// A chain: each segment ends where the next begins, rounded to the
		// same 0.2 m thickness.
		{"a segment chain", func(width float64) Shape {
			return NewSegmentShape(m.Vec2d{X: -width / 2}, m.Vec2d{X: width / 2}, 0.1)
		}},
	}
}

// seamMovers are the ball and the box, each 0.4 m across, with no friction so
// that nothing but a stop can slow them.
func seamMovers() []tunnelMover {
	ball := NewCircleShape(0.2, m.Vec2d{})
	box := NewBoxShape(0.4, 0.4, 0)
	ball.Friction, box.Friction = 0, 0
	return []tunnelMover{{"the ball", ball}, {"the box", box}}
}

// seamSlop is how deep the mover starts in the floor, the engine's default
// Slop, which is where a resting Body settles.
const seamSlop = 0.005

// TestAFastBodySlidesOverTheSeamsOfATiledFloor is the seam scene: the ball and
// the box, on box tiles and on a segment chain, 0.4 m and 1.6 m wide, sliding
// at 1×, 2× and 5× their minimum extent a tick, from 4 starting phases, under
// gravity, for 60 ticks. Every tick covers its whole travel, and no tile
// Contact is a stopping one.
func TestAFastBodySlidesOverTheSeamsOfATiledFloor(t *testing.T) {
	const ticks, starts = 60, 4
	for _, floor := range seamFloors() {
		for _, width := range []float64{0.4, 1.6} {
			for _, mover := range seamMovers() {
				for _, factor := range []float64{1, 2, 5} {
					name := fmt.Sprintf("%s on %s %.1f m wide at %gx", mover.name, floor.name, width, factor)
					t.Run(name, func(t *testing.T) {
						for phase := range starts {
							slideOverSeams(t, floor, width, mover.shape, factor, phase, starts, ticks)
						}
					})
				}
			}
		}
	}
}

// slideOverSeams runs one phase of the seam scene in an engine of its own.
func slideOverSeams(
	t *testing.T, floor seamFloor, width float64, shape Shape,
	factor float64, phase, starts, ticks int,
) {
	t.Helper()
	h, _ := newGravityHarness(t, nil, m.Vec2d{Y: -9.8})
	body, shape, extent := solidFor(t, shape)
	step := factor * extent
	speed := step / tick

	// The phase shifts where the seams fall within a tick's travel.
	start := step * float64(phase) / float64(starts)
	tiles := map[ecs.Entity]bool{}
	for x := -1.0; x < start+step*float64(ticks+2)+2; x += width {
		tiles[h.spawn(t, spawnRequest{
			Kind:  kindShapedStatic,
			Place: Position{Current: m.Vec2d{X: x + width/2, Y: -0.1}},
			Shape: floor.tile(width),
		})] = true
	}
	mover := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Place:    Position{Current: m.Vec2d{X: start, Y: extent - seamSlop}},
		Velocity: Velocity{Linear: m.Vec2d{X: speed}},
		Body:     body,
		Shape:    shape,
	})

	for i := range ticks {
		before := h.read(t, mover)
		h.frame(t)
		after := h.read(t, mover).Place.Current
		stopped, failed := false, false
		var stop Contact
		for _, entry := range h.contacts(t) {
			other := entry.B
			if entry.B == mover {
				other = entry.A
			} else if entry.A != mover {
				continue
			}
			if entry.T < 1 && !stopped {
				stopped, stop = true, entry
			}
			if tiles[other] && entry.T < 1 {
				failed = true
				t.Errorf("phase %d tick %d: a tile Contact has T %.4f, normal %.3f, from %.4f",
					phase, i, entry.T, entry.Normal, before.Place.Current)
			}
		}

		// Integrate moves the mover by the tick's own velocity, and only a
		// stop moves it back. The discrete walk's push-out out of a seam moves
		// it too, along a normal that can lean back by a fraction of a
		// millimetre, which is no stop: a shortfall counts when the tick
		// stopped the mover, or when it is more than any push-out makes.
		want := before.Velocity.Linear.X * tick
		moved := after.X - before.Place.Current.X
		if short := want - moved; (stopped && short > 1e-6) || short > seamSlop {
			failed = true
			t.Errorf("phase %d tick %d: it slid %.6f m from %.4f, %.6f m short of its travel; stopped %v at T %.4f",
				phase, i, moved, before.Place.Current, short, stopped, stop.T)
		}
		if failed {
			// The first tick that goes wrong is the one worth reading: a
			// stop slows the mover too, and the ticks after it read the same.
			return
		}
	}
}

// A fast Body whose centre passes beside a target thinner than itself is still
// stopped where it first meets it. It is the case a rule that asks only
// whether the centre's path enters the target is weakest on: a post 5 cm
// wide, its top reaching halfway up the mover's lower half, and every mover
// thrown over it at projectile speed, from eight phases, so the post is fully
// inside one tick's path.
func TestAFastBodyIsStoppedByAPostItsCentrePassesBeside(t *testing.T) {
	for _, mover := range tunnelMovers() {
		t.Run(mover.name, func(t *testing.T) {
			body, shape, _ := solidFor(t, mover.shape)
			// How far the mover reaches below its centre, and so how high the
			// post stands: its top at half that reach.
			below := shape.Radius
			if shape.Kind != ShapeCircle {
				below = 0
				for _, v := range PolygonVerts(nil, shape, Polygon{}) {
					below = max(below, -v.Y)
				}
			}
			top := -below / 2
			for phase := range phases {
				h := newHarness(t)
				post := h.spawn(t, spawnRequest{
					Kind:  kindShapedStatic,
					Place: Position{Current: m.Vec2d{X: targetX, Y: top - 1}},
					Shape: NewBoxShape(0.05, 2, 0),
				})
				step := projectileSpeed * tick
				start := targetX - 2 - step*float64(phase)/phases
				thrown := h.spawn(t, spawnRequest{
					Kind:     kindShapedBody,
					Place:    Position{Current: m.Vec2d{X: start}},
					Velocity: Velocity{Linear: m.Vec2d{X: projectileSpeed}},
					Body:     body,
					Shape:    shape,
				})
				met := false
				for i := range 10 {
					h.frame(t)
					entry, found := between(h.contacts(t), thrown, post)
					if !found {
						continue
					}
					if !(entry.T < 1) || entry.Count != 1 || entry.Points[0].Depth != 0 {
						t.Errorf("phase %d tick %d: it first met the post with T %.4f, %d point(s), Depth %.4f, want a stop",
							phase, i, entry.T, entry.Count, entry.Points[0].Depth)
					}
					met = true
					break
				}
				if !met {
					at := h.read(t, thrown).Place.Current
					t.Errorf("phase %d: from x = %.3f it passed the post at %.3f without meeting it, ending at %.3f",
						phase, start, targetX, at.X)
				}
			}
		})
	}
}

// Pinned limit: a graze. A target that reaches into the band a fast Body
// sweeps no deeper than the Slop past the depth the Body already rests at, or
// that it closes on, along the normal it meets it by, by less than its own
// extent, is a seam to the path pass and is left to the discrete walk. A post
// whose top reaches 3 mm into a ball's path is passed without a stop, from
// every phase. This is the behaviour the seam rule chose, held so that a
// change to it is seen.
func TestPinnedAGrazeIsLeftToTheDiscreteWalk(t *testing.T) {
	body, shape, _ := solidFor(t, NewCircleShape(0.2, m.Vec2d{}))
	const reach = 0.003
	for phase := range phases {
		h := newHarness(t)
		post := h.spawn(t, spawnRequest{
			Kind:  kindShapedStatic,
			Place: Position{Current: m.Vec2d{X: targetX, Y: -0.2 + reach - 1}},
			Shape: NewBoxShape(0.05, 2, 0),
		})
		step := projectileSpeed * tick
		start := targetX - 2 - step*float64(phase)/phases
		ball := h.spawn(t, spawnRequest{
			Kind:     kindShapedBody,
			Place:    Position{Current: m.Vec2d{X: start}},
			Velocity: Velocity{Linear: m.Vec2d{X: projectileSpeed}},
			Body:     body,
			Shape:    shape,
		})
		for i := range 10 {
			h.frame(t)
			if entry, found := between(h.contacts(t), ball, post); found && entry.T < 1 {
				t.Errorf("phase %d tick %d: the post stopped the ball at T %.4f, want the graze left to the discrete walk",
					phase, i, entry.T)
			}
		}
		if at := h.read(t, ball).Place.Current.X; at <= targetX {
			t.Errorf("phase %d: the ball ended at x = %.3f, want past the post it grazed at %.3f", phase, at, targetX)
		}
	}
}
