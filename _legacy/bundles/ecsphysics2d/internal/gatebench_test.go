package internal

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// BenchmarkTheGatedRebuild is continuous collision's nothing-fast price inside
// Index, the number the whole-step benchmarks cannot put on it: the Body index
// rebuilt through InsertMoving with Previous poses, as Index rebuilds it every
// tick, over a world where nothing moves fast. Every Body moves, a tenth of its
// minimum extent or less, which is the benchmark scenes' own (their largest
// ratio is 0.098×), so each one pays the gate's compare and the branch that
// leaves it on its end box, and none pays the path.
//
// Half the Bodies are circles, whose extent is their radius, and half boxes,
// whose extent is the face distance the constructor kept. It is read before and
// after the gate, interleaved over two built binaries, on the minimums, and the
// bar is 2 ns a Body (continuous-collision.md § Acceptance › The cost bar);
// ns/Body is ns/op divided by the rebuild's Bodies.
func BenchmarkTheGatedRebuild(b *testing.B) {
	idx := NewBodyIndex(2)
	circle := NewCircleShape(0.4, m.Vec2d{})
	box := NewBoxShape(0.8, 0.6, 0)
	type pose struct {
		at, previous         m.Vec2d
		angle, previousAngle float64
		shape                Shape
	}
	poses := make([]pose, rebuiltBodies)
	for i := range poses {
		at := m.Vec2d{X: float64(i%32) * 1.9, Y: float64(i/32) * 1.9}
		// A tenth of the smaller extent at most, in a direction that turns
		// with the Body, so no two are alike and none reaches the gate.
		step := m.Vec2d{X: 0.009 * float64(i%7-3), Y: 0.004 * float64(i%5-2)}
		poses[i] = pose{at, at.Sub(step), float64(i) * 0.013, float64(i)*0.013 - 0.001, circle}
		if i%2 == 1 {
			poses[i].shape = box
		}
	}
	rebuild := func() {
		idx.Clear()
		for i := range poses {
			p := &poses[i]
			idx.InsertMoving(testEntity(i), p.shape, p.at, p.previous, p.angle, p.previousAngle, nil)
		}
	}
	rebuild()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		rebuild()
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/rebuiltBodies, "ns/Body")
}
