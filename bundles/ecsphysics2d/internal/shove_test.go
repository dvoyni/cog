package internal

import (
	"fmt"
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// A shoved box is not pushed through a neighboured wall (issue #592). nox's
// walls are Static 1 m segments of radius 0.2, each built with its neighbours,
// and a shove there sent its crate through one by three sequences: a corner
// past the core line at a joint, which the neighbour rule dropped every
// Contact of; an engaged stop off the centre, which turned the shove into spin
// and then met the first; and a surface touched at Previous and driven into,
// which the path test skipped and counted the whole shove of as resting depth.
// Every number here is nox's, and nox uses the default Config and no gravity.

// nox's walls and props.
const (
	shoveWallRadius  = 0.2
	shoveCrateLength = 48 / 16.26
	shoveCrateWidth  = 24 / 16.26
	shoveCrateMass   = 30.0
	shoveBlockSize   = 24 / 16.26
	shoveBlockMass   = 15.0
	shoveDamping     = 15.80
	// shovePower is the speed one power level of a shove gives the crate.
	shovePower = 40.0
	// shoveRunEnd is where the wall's run ends either side of x = 0.
	shoveRunEnd = 5
)

// shoveWall lays a run of neighboured 1 m segments of radius 0.2 along y = 0,
// from x = from to to, the joints at the whole metres, as nox builds a wall:
// each segment an Entity of its own, placed at its middle.
func shoveWall(t testing.TB, h *harness, from, to int) {
	t.Helper()
	for x := from; x < to; x++ {
		a, b := m.Vec2d{X: -0.5}, m.Vec2d{X: 0.5}
		before, after := a, b
		if x > from {
			before = a.Sub(m.Vec2d{X: 1})
		}
		if x+1 < to {
			after = b.Add(m.Vec2d{X: 1})
		}
		h.spawn(t, spawnRequest{
			Kind:  kindShapedStatic,
			Place: Position{Current: m.Vec2d{X: float64(x) + 0.5}},
			Shape: NewSegmentShapeWithNeighbours(before, a, b, after, shoveWallRadius),
		})
	}
}

// shoveProp is a box nox shoves: its size and mass. Both are dragged at the
// crate's rate.
type shoveProp struct {
	name          string
	length, width float64
	mass          float64
}

func shoveCrate() shoveProp {
	return shoveProp{"the crate", shoveCrateLength, shoveCrateWidth, shoveCrateMass}
}

func shoveBlock() shoveProp {
	return shoveProp{"the block", shoveBlockSize, shoveBlockSize, shoveBlockMass}
}

// corners are the prop's corners about its centre, turned by angle.
func (p shoveProp) corners(angle float64) [4]m.Vec2d {
	turn := m.Vec2d{X: math.Cos(angle), Y: math.Sin(angle)}
	x, y := p.length/2, p.width/2
	return [4]m.Vec2d{
		m.Vec2d{X: x, Y: y}.Rotate(turn), m.Vec2d{X: -x, Y: y}.Rotate(turn),
		m.Vec2d{X: -x, Y: -y}.Rotate(turn), m.Vec2d{X: x, Y: -y}.Rotate(turn),
	}
}

// highest is the prop's corner that reaches furthest up at that angle, about
// its centre.
func (p shoveProp) highest(angle float64) m.Vec2d {
	corners := p.corners(angle)
	best := corners[0]
	for _, v := range corners[1:] {
		if v.Y > best.Y {
			best = v
		}
	}
	return best
}

// shoveDepth is how deep a point stands in the wall, measured from its near
// face: 0.2 + y along the run, where the face is flat, and into the rounded
// end beyond it. A point that has passed beside the wall's end is not in it.
func shoveDepth(p m.Vec2d) float64 {
	if math.Abs(p.X) <= shoveRunEnd {
		return shoveWallRadius + p.Y
	}
	end := m.Vec2d{X: math.Copysign(shoveRunEnd, p.X)}
	return shoveWallRadius - p.Sub(end).Length()
}

// shoveRun is one shove of a prop at the wall from below: the prop turned by
// angle, its highest corner at x = at and gap short of the wall's face (below
// zero, into it), shoved straight up at each of speeds, one every so many
// ticks. A shove is nox's: a Force written for exactly one tick.
type shoveRun struct {
	prop   shoveProp
	angle  float64
	at     float64
	gap    float64
	speeds []float64
	every  int
}

func (r shoveRun) String() string {
	return fmt.Sprintf("%s at %.0f°, its corner at x = %g, %g m clear, shoved at %v m/s every %d ticks",
		r.prop.name, r.angle*180/math.Pi, r.at, r.gap, r.speeds, max(r.every, 1))
}

// shoveResult is what a shove did: whether the prop's centre ever crossed the
// wall's core line, the deepest any corner ever stood in the wall, and the
// deepest one stood when the second ended.
type shoveResult struct {
	crossed        bool
	deepest, ended float64
}

// shoveRestTolerance is how deep in the wall a corner may still stand a second
// after the shove: the push-out of a corner a spin drove deep into the wall is
// still closing its last 3 cm then, and a prop that went through stands a
// whole wall deep or more.
const shoveRestTolerance = 0.05

// through reports that the prop went through the wall: its centre crossed the
// core line, or a corner still stands deep in the wall at the end.
func (r shoveResult) through() bool {
	return r.crossed || r.ended > shoveRestTolerance
}

// shove runs the shove for a second.
func (r shoveRun) shove(t testing.TB) shoveResult {
	t.Helper()
	h := newHarness(t)
	shoveWall(t, h, -shoveRunEnd, shoveRunEnd)
	top := r.prop.highest(r.angle)
	body := dynamic(t, r.prop.mass, MomentForBox(r.prop.mass, r.prop.length, r.prop.width), shoveDamping, shoveDamping)
	prop := h.spawn(t, spawnRequest{
		Kind: kindShapedBody,
		Place: Position{
			Current: m.Vec2d{X: r.at - top.X, Y: -shoveWallRadius - r.gap - top.Y},
			Angle:   r.angle,
		},
		Body:  body,
		Shape: NewBoxShape(r.prop.length, r.prop.width, 0),
	})
	every := max(r.every, 1)
	result := shoveResult{deepest: math.Inf(-1)}
	for i := range 60 {
		h.game.push = m.Vec2d{}
		if n := i / every; i%every == 0 && n < len(r.speeds) {
			h.game.push = m.Vec2d{Y: r.speeds[n] * r.prop.mass / tick}
		}
		h.frame(t)
		place := h.read(t, prop).Place
		if place.Current.Y >= 0 && math.Abs(place.Current.X) <= shoveRunEnd {
			result.crossed = true
		}
		result.ended = math.Inf(-1)
		for _, v := range r.prop.corners(place.Angle) {
			result.ended = max(result.ended, shoveDepth(place.Current.Add(v)))
		}
		result.deepest = max(result.deepest, result.ended)
	}
	return result
}

// TestAShovedCrateIsNotPushedThroughANeighbouredWall is the three sequences
// nox's crate went through its wall by, each at nox's numbers.
//
//   - A: the crate at 45°, 0.3 m clear, a power-1 shove: 0.666 m in the tick,
//     under the gate, its corner ending 0.166 m past the core line 2 cm from
//     a joint. GJK/EPA puts the witness on the joint for both segments, and
//     the neighbour rule rejected both, so it had no Contact at all.
//   - B: the crate square to the wall, 0.1 m clear, a power-2 shove, stopped
//     by one point well off its centre, which Solve turns into spin; the next
//     tick finds its corner past the core lines, and the neighbour rule
//     rejected them all.
//   - C: the crate at 15°, touching the wall, a power-3 shove. The two
//     segments it touches were skipped as touched at Previous, and its drift
//     into them was counted as resting depth, so the next two read as seams.
func TestAShovedCrateIsNotPushedThroughANeighbouredWall(t *testing.T) {
	for _, run := range []struct {
		name string
		run  shoveRun
	}{
		{"A: a corner past the core line at a joint", shoveRun{
			prop: shoveCrate(), angle: math.Pi / 4, at: 1.02, gap: 0.3, speeds: []float64{shovePower},
		}},
		{"B: a stop off the centre, then A", shoveRun{
			prop: shoveCrate(), at: 0.55, gap: 0.1, speeds: []float64{2 * shovePower},
		}},
		{"C: a surface touched at Previous and driven into", shoveRun{
			prop: shoveCrate(), angle: 15 * math.Pi / 180, at: 0.5, speeds: []float64{3 * shovePower},
		}},
	} {
		t.Run(run.name, func(t *testing.T) {
			if got := run.run.shove(t); got.through() {
				t.Errorf("%v: went through: centre crossed the core line %v, deepest corner %.3f m into the wall, %.3f m at the end",
					run.run, got.crossed, got.deepest, got.ended)
			}
		})
	}
}

// shoveSweep is the crate and the block at several angles, the highest corner
// at a segment's middle, at a joint, 2 cm off it and 0.1 m short of the run's
// end, from clear of the wall to slightly into it, each shoved once at power
// 1, 2 and 3, or at power 1 four times, 5 ticks apart. A shove is nox's Force,
// so the block, half the crate's mass, leaves at twice the crate's speed. The
// end is the one the highest corner leads towards, with the rest of the prop
// under the wall. A shove every tick is outside it: that is the named limit
// for a turning Body.
func shoveSweep() []shoveRun {
	var runs []shoveRun
	for _, prop := range []shoveProp{shoveCrate(), shoveBlock()} {
		power := shovePower * shoveCrateMass / prop.mass
		for _, degrees := range []float64{0, 5, 15, 30, 45, 60, 75} {
			angle := degrees * math.Pi / 180
			end := shoveRunEnd - 0.1
			if prop.highest(angle).X < 0 {
				end = -end
			}
			for _, at := range []float64{0.5, 1, 1.02, end} {
				for _, gap := range []float64{0.3, 0.05, 0, -0.005} {
					for _, schedule := range []struct {
						speeds []float64
						every  int
					}{
						{[]float64{power}, 1},
						{[]float64{2 * power}, 1},
						{[]float64{3 * power}, 1},
						{[]float64{power, power, power, power}, 5},
					} {
						runs = append(runs, shoveRun{
							prop: prop, angle: angle, at: at, gap: gap,
							speeds: schedule.speeds, every: schedule.every,
						})
					}
				}
			}
		}
	}
	return runs
}

// TestNoShoveGoesThroughANeighbouredWall is the sweep: 896 shoves, and none
// goes through.
func TestNoShoveGoesThroughANeighbouredWall(t *testing.T) {
	runs := shoveSweep()
	through := 0
	for _, run := range runs {
		if got := run.shove(t); got.through() {
			through++
			t.Errorf("%v: went through: centre crossed the core line %v, deepest corner %.3f m into the wall, %.3f m at the end",
				run, got.crossed, got.deepest, got.ended)
		}
	}
	if through > 0 {
		t.Logf("%d of %d shoves went through", through, len(runs))
	}
}

// TestABoxSlidesAlongANeighbouredRunAtFullSpeed is the neighbour rule's own
// case, which the core line leaves standing: the crate sliding 8 m along a run
// of neighboured segments under gravity, square to it and tipped 5° and 15°
// onto a corner, at 2, 4 and 8 m/s, from nine places across a segment, keeps
// its speed through every joint within 1%. It has no friction, and a moment
// large enough that it keeps its tip, so nothing but catching at a joint can
// slow it.
func TestABoxSlidesAlongANeighbouredRunAtFullSpeed(t *testing.T) {
	const distance = 8.0
	crate := shoveCrate()
	for _, degrees := range []float64{0, 5, 15} {
		angle := degrees * math.Pi / 180
		// The lowest corner, which rests a Slop into the run's top face.
		corners := crate.corners(angle)
		lowest := corners[0]
		for _, v := range corners[1:] {
			if v.Y < lowest.Y {
				lowest = v
			}
		}
		for _, speed := range []float64{2, 4, 8} {
			t.Run(fmt.Sprintf("tipped %g° at %g m/s", degrees, speed), func(t *testing.T) {
				for tenth := 1; tenth < 10; tenth++ {
					slideAlongARun(t, crate, angle, lowest, speed, float64(tenth)/10, distance)
				}
			})
		}
	}
}

// slideAlongARun is one slide of the joint test, its lowest corner starting at
// x = start, in an engine of its own.
func slideAlongARun(t *testing.T, crate shoveProp, angle float64, lowest m.Vec2d, speed, start, distance float64) {
	t.Helper()
	h, _ := newGravityHarness(t, nil, m.Vec2d{Y: -9.8})
	shoveWall(t, h, -3, 12)
	shape := NewBoxShape(crate.length, crate.width, 0)
	shape.Friction = 0
	from := m.Vec2d{X: start - lowest.X, Y: shoveWallRadius - seamSlop - lowest.Y}
	box := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Place:    Position{Current: from, Angle: angle},
		Velocity: Velocity{Linear: m.Vec2d{X: speed}},
		Body:     dynamic(t, crate.mass, 1e9, 0, 0),
		Shape:    shape,
	})
	for i := range int(math.Ceil(distance / speed / tick)) {
		h.frame(t)
		got := h.read(t, box)
		if v := got.Velocity.Linear.X; v < 0.99*speed {
			t.Errorf("from %.1f, tick %d, its corner at x = %.3f: it slides at %.4f m/s, want %g within 1%%",
				start, i, got.Place.Current.X+lowest.X, v, speed)
			return
		}
	}
	if slid := h.read(t, box).Place.Current.X - from.X; slid < distance-0.01 {
		t.Errorf("from %.1f it slid %.3f m, want %g", start, slid, distance)
	}
}
