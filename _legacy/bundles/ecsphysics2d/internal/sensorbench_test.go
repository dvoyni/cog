package internal

import (
	"fmt"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// Wall-clock is asserted nowhere. This reports what the Probe half of Detect
// costs by running the same scene twice — once with the Sensors swept and once
// with the same Shapes standing still, which is the discrete pass alone — so
// the difference between the two arms is the sweep and nothing else.
//
// The two arms are sub-benchmarks of one run for that reason: a whole-frame
// measurement swings about a tenth by run order, and only an interleaved A/B
// over two built binaries settles that. Here both arms are the same binary and
// the same Shapes, so the comparison is honest without one.
func BenchmarkDetectWithSweptSensors(b *testing.B) {
	for _, n := range []int{256, 1024} {
		for _, swept := range []bool{false, true} {
			name := fmt.Sprintf("N=%d/swept=%v", n, swept)
			b.Run(name, func(b *testing.B) {
				contacts := NewContacts(7)
				bodies, statics := NewBodyIndex(0), NewStaticIndex(0)
				for i := range n / 4 {
					statics.Insert(ecs.Entity(1_000_000+i), NewCircleShape(0.4, m.Vec2d{}),
						benchGridAt(i).Add(m.Vec2d{X: 0.75}), 0, nil)
				}

				fill := func() {
					bodies.Clear()
					for i := range n {
						bodies.Insert(ecs.Entity(1+i), NewCircleShape(0.4, m.Vec2d{}), benchGridAt(i), 0, nil)
					}
					// A sixteenth of the scene again as Sensors, each on a
					// Static's own centre: the swept arm gives them a tick's
					// worth of drift and the discrete arm leaves them where
					// they are, which is the same Shapes in the same cells.
					for i := range n / 16 {
						at := benchGridAt(i % 8).Add(m.Vec2d{X: 0.75, Y: 0.02 * float64(i/8)})
						from := at
						if swept {
							from = at.Sub(m.Vec2d{X: 0.1 / 60})
						}
						bodies.InsertMoving(ecs.Entity(2_000_000+i), solverSensorCircle(0.4), at, from, 0, 0, nil)
					}
				}

				// Warm the two entry buffers, the two maps and the Probe
				// scratch, so what is measured is the steady state.
				for range 8 {
					fill()
					Collide(contacts, bodies, statics, noJoints, 3, testSlop)
				}

				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					Collide(contacts, bodies, statics, noJoints, 3, testSlop)
				}
				b.StopTimer()
				b.ReportMetric(float64(contacts.Len()), "contacts")
			})
		}
	}
}
