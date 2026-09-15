package baseline

// PROTOTYPE (cog#345). Throwaway; never merges.

import (
	"testing"

	"github.com/dvoyni/cog/bundles/ecsphysics2d/proto/queryindex"
)

// static query sets span the whole map
const staticSide = queryindex.MapSize

var staticMixes = []string{"proj", "lospair", "sight", "losmap"}

var bodyLayout = "arena1024"

var bodyMixes = []string{"proj", "body"}

func mixNamed(name string) queryindex.Mix {
	for _, m := range queryindex.Mixes {
		if m.Name == name {
			return m
		}
	}
	panic("no mix " + name)
}

func bodyItems(name string) ([]queryindex.Placed, float32) {
	for _, l := range queryindex.BodyLayouts {
		if l.Name == name {
			return queryindex.Bodies(l.N, l.Side, 0), l.Side
		}
	}
	panic("no body layout " + name)
}

// sweeper is what both libraries offer the harness.
type sweeper interface {
	Sweep(q queryindex.Query) (Hit, bool)
	SweepAll(dst []Hit, q queryindex.Query) []Hit
}

var libs = []struct {
	name  string
	build func([]queryindex.Placed) sweeper
}{
	{"CP", func(items []queryindex.Placed) sweeper { return NewCP(items) }},
	{"Gox2d", func(items []queryindex.Placed) sweeper { return NewGox2d(items) }},
}

func BenchmarkCP(b *testing.B)    { benchLib(b, libs[0].build) }
func BenchmarkGox2d(b *testing.B) { benchLib(b, libs[1].build) }

var sink Hit
var sinkOK bool

func benchLib(b *testing.B, build func([]queryindex.Placed) sweeper) {
	for _, layout := range queryindex.StaticLayouts {
		b.Run(layout.Name, func(b *testing.B) {
			w := build(layout.Make())
			for _, mix := range staticMixes {
				qs := queryindex.QuerySet(mixNamed(mix), staticSide)
				b.Run(mix, func(b *testing.B) {
					b.Run("Sweep", func(b *testing.B) { benchSweep(b, w, qs) })
					b.Run("SweepAll", func(b *testing.B) { benchSweepAll(b, w, qs) })
				})
			}
		})
	}
	b.Run(bodyLayout, func(b *testing.B) {
		items, side := bodyItems(bodyLayout)
		w := build(items)
		for _, mix := range bodyMixes {
			qs := queryindex.QuerySet(mixNamed(mix), side)
			b.Run(mix, func(b *testing.B) {
				b.Run("Sweep", func(b *testing.B) { benchSweep(b, w, qs) })
			})
		}
	})
}

func benchSweep(b *testing.B, w sweeper, qs []queryindex.Query) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := qs[i&(len(qs)-1)]
		sink, sinkOK = w.Sweep(q)
	}
}

func benchSweepAll(b *testing.B, w sweeper, qs []queryindex.Query) {
	dst := make([]Hit, 0, 1024)
	for _, q := range qs { // warm the slice to its largest need
		dst = w.SweepAll(dst[:0], q)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := qs[i&(len(qs)-1)]
		dst = w.SweepAll(dst[:0], q)
	}
}

// TestAgreement compares each library's first hit against queryindex.Linear.
// It logs; it does not fail, because the libraries' edge semantics differ.
func TestAgreement(t *testing.T) {
	type set struct {
		layout string
		items  []queryindex.Placed
		mixes  []string
		side   float32
	}
	var sets []set
	for _, l := range queryindex.StaticLayouts {
		sets = append(sets, set{l.Name, l.Make(), staticMixes, staticSide})
	}
	items, side := bodyItems(bodyLayout)
	sets = append(sets, set{bodyLayout, items, bodyMixes, side})

	for _, s := range sets {
		lin := &queryindex.Linear{}
		lin.Build(s.items)
		built := make([]sweeper, len(libs))
		for i, lib := range libs {
			built[i] = lib.build(s.items)
		}
		for _, mix := range s.mixes {
			qs := queryindex.QuerySet(mixNamed(mix), s.side)
			for i, lib := range libs {
				w := built[i]
				var sameBool, bothHit, sameT, linHits, libOnly, linOnly, linOnlyStart int
				var worstDT float32
				for _, q := range qs {
					lh, lok := lin.Sweep(q.From, q.To, q.Radius, 0)
					h, ok := w.Sweep(q)
					if lok {
						linHits++
					}
					switch {
					case ok == lok:
						sameBool++
					case ok:
						libOnly++
					default:
						linOnly++
						if lh.T == 0 {
							linOnlyStart++ // Linear says the sweep starts overlapping
						}
					}
					if ok && lok {
						bothHit++
						dt := h.T - lh.T
						if dt < 0 {
							dt = -dt
						}
						if dt <= 1e-3 {
							sameT++
						}
						worstDT = max(worstDT, dt)
					}
				}
				n := float64(len(qs))
				tPct := 100.0
				if bothHit > 0 {
					tPct = 100 * float64(sameT) / float64(bothHit)
				}
				t.Logf("%-5s %-12s %-7s hit-bool %6.2f%% (linear hits %4d, lib-only %3d, linear-only %3d of which start-overlap %3d)  T within 1e-3 %6.2f%% of %4d both-hit (worst |dT| %.4f)",
					lib.name, s.layout, mix, 100*float64(sameBool)/n, linHits, libOnly, linOnly, linOnlyStart, tPct, bothHit, worstDT)
			}
		}
	}
}
