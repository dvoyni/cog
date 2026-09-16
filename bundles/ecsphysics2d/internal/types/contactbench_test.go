package types

import (
	"fmt"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// Wall-clock is asserted nowhere. These report the narrowphase's and the
// detection walk's cost so a change that moves them is visible; the whole
// step's cost is BenchmarkTheStep's, measured interleaved A/B against cp and
// recorded rather than asserted.

func BenchmarkCollideCircles(b *testing.B) {
	first := NewCircleShape(0.5, m.Vec2d{})
	second := NewCircleShape(0.7, m.Vec2d{})
	transformFirst := NewTransformRigid(m.Vec2d{}, 0)
	transformSecond := NewTransformRigid(m.Vec2d{X: 0.9, Y: 0.3}, 0)
	var worldFirst, worldSecond [worldScratchLen]m.Vec2d
	usedFirst, _ := cacheWorldAt(first, transformFirst, nil, worldFirst[:])
	usedSecond, _ := cacheWorldAt(second, transformSecond, nil, worldSecond[:])

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		touch, _ := collideWorld(
			first, transformFirst, worldFirst[:usedFirst],
			second, transformSecond, worldSecond[:usedSecond],
			m.Vec2d{X: 1}, 0,
		)
		sinkDepth = touch.points[0].depth
	}
}

func BenchmarkCollideCircleSegment(b *testing.B) {
	circle := NewCircleShape(0.5, m.Vec2d{})
	wall := NewSegmentShape(m.Vec2d{X: -2}, m.Vec2d{X: 2}, 0.1)
	transformCircle := NewTransformRigid(m.Vec2d{X: 0.25, Y: 0.4}, 0)
	transformWall := NewTransformRigid(m.Vec2d{}, 0)
	var worldCircle, worldWall [worldScratchLen]m.Vec2d
	usedCircle, _ := cacheWorldAt(circle, transformCircle, nil, worldCircle[:])
	usedWall, _ := cacheWorldAt(wall, transformWall, nil, worldWall[:])

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		touch, _ := collideWorld(
			circle, transformCircle, worldCircle[:usedCircle],
			wall, transformWall, worldWall[:usedWall],
			m.Vec2d{X: 1}, 0,
		)
		sinkDepth = touch.points[0].depth
	}
}

// BenchmarkCollidePolys and BenchmarkCollideSegments are the GJK path, where
// the two closed forms above are arithmetic: both pairs here overlap deeply
// enough to reach EPA, whose hull is the two ping-ponged stack buffers. A cold
// simplex is passed every iteration, which is the worst case — detection warm
// starts from the previous tick's.
func BenchmarkCollidePolys(b *testing.B) {
	box := NewBoxShape(1, 1, 0)
	transformFirst := NewTransformRigid(m.Vec2d{}, 0)
	transformSecond := NewTransformRigid(m.Vec2d{X: 0.1, Y: 0.9}, 0.3)
	var worldFirst, worldSecond [worldScratchLen]m.Vec2d
	usedFirst, _ := cacheWorldAt(box, transformFirst, nil, worldFirst[:])
	usedSecond, _ := cacheWorldAt(box, transformSecond, nil, worldSecond[:])

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		touch, _ := collideWorld(
			box, transformFirst, worldFirst[:usedFirst],
			box, transformSecond, worldSecond[:usedSecond],
			m.Vec2d{X: 1}, 0,
		)
		sinkDepth = touch.points[0].depth
	}
}

func BenchmarkCollideSegments(b *testing.B) {
	across := NewSegmentShape(m.Vec2d{Y: -1}, m.Vec2d{Y: 1}, 0.15)
	along := NewSegmentShape(m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0.1)
	transformFirst := NewTransformRigid(m.Vec2d{X: 0.3, Y: 0.2}, 0)
	transformSecond := NewTransformRigid(m.Vec2d{}, 0)
	var worldFirst, worldSecond [worldScratchLen]m.Vec2d
	usedFirst, _ := cacheWorldAt(across, transformFirst, nil, worldFirst[:])
	usedSecond, _ := cacheWorldAt(along, transformSecond, nil, worldSecond[:])

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		touch, _ := collideWorld(
			across, transformFirst, worldFirst[:usedFirst],
			along, transformSecond, worldSecond[:usedSecond],
			m.Vec2d{X: 1}, 0,
		)
		sinkDepth = touch.points[0].depth
	}
}

// BenchmarkDetect is the detection walk alone, over the same 1.7 m grid the
// step's own measurement runs on, with a Static 0.75 m along +X from a quarter
// of the Bodies so that a quarter of them touch one.
//
// The index rebuild is outside the timed loop on purpose. Detection reads the
// two indices and writes nothing back into them, so running it again over the
// same grid is the same walk — and the rebuild is Index's cost, measured by
// BenchmarkBodyIndexRebuild, not this System's.
func BenchmarkDetect(b *testing.B) {
	for _, n := range []int{256, 1024} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			contacts := NewContacts(7)
			bodies, statics := NewBodyIndex(0), NewStaticIndex(0)
			for i := range n / 4 {
				statics.Insert(ecs.Entity(1_000_000+i), NewCircleShape(0.4, m.Vec2d{}),
					benchGridAt(i).Add(m.Vec2d{X: 0.75}), 0, nil)
			}
			for i := range n {
				bodies.Insert(ecs.Entity(1+i), NewCircleShape(0.4, m.Vec2d{}), benchGridAt(i), 0, nil)
			}
			// Warm the two entry buffers and the two maps, so what is measured is
			// the steady state and not the tick that grew them.
			for range 8 {
				Collide(contacts, bodies, statics, 3)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				Collide(contacts, bodies, statics, 3)
			}
			b.StopTimer()
			b.ReportMetric(float64(contacts.Len()), "contacts")
		})
	}
}

// benchGridAt is stepbench's own 1.7 m grid, so the two measurements run over
// the same arrangement of Shapes.
func benchGridAt(i int) m.Vec2d {
	return m.Vec2d{X: float64(i%16) * 1.7, Y: float64(i/16) * 1.7}
}

var sinkDepth float64
