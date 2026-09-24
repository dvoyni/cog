package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// These report and never fail, which is the point: an assertion is what keeps a
// query at zero allocations, and TestTheQueriesAllocateNothing is where it
// lives. A benchmark alone reports and never fails, which is how cp's two
// allocations a query survived — so ReportAllocs here is a reading beside the
// cost, not the guard.
//
// Every number these print is in float64. The index prototype that measured a
// short Probe at 30 to 50 nanoseconds measured it in float32, so nothing here
// is comparable with it until the float64 re-measurement is taken; the numbers
// below are this port's own, on whatever hardware the run names.

// benchIndex is a scatter of small circles among a few long walls, which is the
// shape of a scene an index is asked about.
func benchIndex(entities int) *StaticIndex {
	idx := NewStaticIndex(2)
	for i := range entities {
		idx.Insert(testEntity(i), NewCircleShape(0.4, m.Vec2d{}),
			m.Vec2d{X: float64(i%32) * 1.9, Y: float64(i/32) * 1.9}, 0, nil)
	}
	for i := range 16 {
		idx.Insert(testEntity(entities+i), NewSegmentShape(m.Vec2d{Y: -20}, m.Vec2d{Y: 20}, 0.1),
			m.Vec2d{X: float64(i) * 4}, 0, nil)
	}
	return idx
}

func BenchmarkProbeShort(b *testing.B) {
	idx := benchIndex(1024)
	from, to := m.Vec2d{X: 10, Y: 10}, m.Vec2d{X: 12, Y: 11}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		hitSink, boolSink = idx.Probe(from, to, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	}
}

func BenchmarkProbeShortWithARadius(b *testing.B) {
	idx := benchIndex(1024)
	from, to := m.Vec2d{X: 10, Y: 10}, m.Vec2d{X: 12, Y: 11}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		hitSink, boolSink = idx.Probe(from, to, 0.3, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	}
}

func BenchmarkProbeAcrossTheScene(b *testing.B) {
	idx := benchIndex(1024)
	from, to := m.Vec2d{X: -2, Y: -2}, m.Vec2d{X: 62, Y: 62}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		hitSink, boolSink = idx.Probe(from, to, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	}
}

func BenchmarkProbeAll(b *testing.B) {
	idx := benchIndex(1024)
	from, to := m.Vec2d{X: -2, Y: -2}, m.Vec2d{X: 62, Y: 62}
	dst := make([]Hit, 0, 64)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		dst = idx.ProbeAll(dst[:0], from, to, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	}
}

func BenchmarkOverlap(b *testing.B) {
	idx := benchIndex(1024)
	query := NewCircleShape(1, m.Vec2d{})
	at := m.Vec2d{X: 20, Y: 20}
	dst := make([]ecs.Entity, 0, 64)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		dst = idx.Overlap(dst[:0], query, at, 0, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	}
}

func BenchmarkOverlapLarge(b *testing.B) {
	// The case the numbers record a BVH doing better on: a wide Overlap over a
	// hashed grid scans every cell it covers.
	idx := benchIndex(1024)
	query := NewCircleShape(18, m.Vec2d{})
	at := m.Vec2d{X: 30, Y: 30}
	dst := make([]ecs.Entity, 0, 1024)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		dst = idx.Overlap(dst[:0], query, at, 0, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	}
}

func BenchmarkOneStaticEntityReplaced(b *testing.B) {
	idx := benchIndex(1024)
	shape := NewCircleShape(0.4, m.Vec2d{})

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		idx.Insert(testEntity(0), shape, m.Vec2d{X: float64(i%32) * 1.9}, 0, nil)
	}
}

func BenchmarkBodyIndexRebuild(b *testing.B) {
	idx := NewBodyIndex(2)
	shape := NewCircleShape(0.4, m.Vec2d{})
	rebuild := func() {
		idx.Clear()
		for i := range 1024 {
			idx.Insert(testEntity(i), shape, m.Vec2d{X: float64(i%32) * 1.9, Y: float64(i/32) * 1.9}, 0, nil)
		}
	}
	rebuild()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		rebuild()
	}
}

func BenchmarkProbeShapeAgainstOneCircle(b *testing.B) {
	from, to, at := origin, m.Vec2d{X: 10}, m.Vec2d{X: 5}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		hitSink, boolSink = ProbeShape(from, to, 0, unitCircle, at, 0, nil)
	}
}

func BenchmarkProbeShapeAgainstOneSegment(b *testing.B) {
	from, to, at := origin, m.Vec2d{X: 10}, m.Vec2d{X: 5}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		hitSink, boolSink = ProbeShape(from, to, 0.3, upright, at, 0, nil)
	}
}

func BenchmarkPenetration(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		vecSink, floatSink, boolSink = Penetration(
			unitCircle, origin, 0, nil, upright, m.Vec2d{X: 0.5}, 0, nil)
	}
}
