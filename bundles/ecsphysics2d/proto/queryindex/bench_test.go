package queryindex

import (
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
)

// BenchmarkStatic/<layout>/<index>/<mix>/<op>: queries against static geometry.
func BenchmarkStatic(b *testing.B) {
	for _, l := range StaticLayouts {
		items := l.Make()
		b.Run(l.Name, func(b *testing.B) {
			for _, c := range staticCandidates() {
				idx := c.make()
				idx.Build(items)
				b.Run(c.name, func(b *testing.B) {
					for _, mix := range Mixes {
						if c.name == "linear" && mix.Name != "proj" && mix.Name != "losmap" {
							continue
						}
						benchQueries(b, idx, mix, QuerySet(mix, MapSize), nil)
					}
				})
			}
		})
	}
}

// BenchmarkBody/<layout>/<index>/<mix>/<op>: queries against bodies, each
// excluding one body as a projectile that is a body would.
func BenchmarkBody(b *testing.B) {
	for _, l := range BodyLayouts {
		items := Bodies(l.N, l.Side, 0)
		b.Run(l.Name, func(b *testing.B) {
			for _, c := range bodyCandidates() {
				idx := c.make()
				idx.Build(items)
				b.Run(c.name, func(b *testing.B) {
					for _, mix := range Mixes[:4] {
						benchQueries(b, idx, mix, QuerySet(mix, l.Side), func(i int) ecs.Entity { return bodyExclude(i, l.N) })
					}
				})
			}
		})
	}
}

func benchQueries(b *testing.B, idx Index, mix Mix, qs []Query, exclude func(int) ecs.Entity) {
	mask := len(qs) - 1
	ex := make([]ecs.Entity, len(qs))
	if exclude != nil {
		for i := range ex {
			ex[i] = exclude(i)
		}
	}
	b.Run(mix.Name, func(b *testing.B) {
		b.Run("Sweep", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				q := qs[i&mask]
				idx.Sweep(q.From, q.To, q.Radius, ex[i&mask])
			}
		})
		b.Run("Blocked", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				q := qs[i&mask]
				idx.Blocked(q.From, q.To, q.Radius, ex[i&mask])
			}
		})
		b.Run("SweepAll", func(b *testing.B) {
			dst := make([]Hit, 0, 1024)
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				q := qs[i&mask]
				dst = idx.SweepAll(dst[:0], q.From, q.To, q.Radius, ex[i&mask])
			}
		})
	})
}

// BenchmarkOverlap/<layout>/<index>/<radius>: Q5 and Q7 on bodies.
func BenchmarkOverlap(b *testing.B) {
	for _, l := range BodyLayouts {
		items := Bodies(l.N, l.Side, 0)
		qs := QuerySet(Mixes[0], l.Side)
		b.Run(l.Name, func(b *testing.B) {
			for _, c := range staticCandidates() {
				idx := c.make()
				idx.Build(items)
				b.Run(c.name, func(b *testing.B) {
					for _, r := range []struct {
						name string
						r    float32
					}{{"point", 0}, {"r3.1m", 3.1}, {"r36.9m", 36.9}} {
						b.Run(r.name, func(b *testing.B) {
							dst := make([]ecs.Entity, 0, 4096)
							b.ReportAllocs()
							for i := 0; b.Loop(); i++ {
								dst = idx.Overlap(dst[:0], qs[i&(len(qs)-1)].From, r.r, 0)
							}
						})
					}
				})
			}
		})
	}
}

// BenchmarkBodyUpdate/<layout>/<index>/<op>: keeping the body index current
// for one sub-step, alternating between two positions 0.11 m apart (a running
// character's sub-step, §6).
func BenchmarkBodyUpdate(b *testing.B) {
	for _, l := range BodyLayouts {
		frames := [2][]Placed{Bodies(l.N, l.Side, 0), nil}
		frames[1] = Moved(frames[0], 0.11, 1)
		b.Run(l.Name, func(b *testing.B) {
			for _, c := range staticCandidates()[1:] {
				b.Run(c.name+"/Rebuild", func(b *testing.B) {
					idx := c.make()
					idx.Build(frames[0])
					idx.Build(frames[1])
					b.ReportAllocs()
					for i := 0; b.Loop(); i++ {
						idx.Build(frames[i&1])
					}
				})
				if _, ok := c.make().(updater); ok {
					b.Run(c.name+"/Update", func(b *testing.B) {
						g := c.make().(updater)
						g.Build(frames[0])
						g.Update(frames[1])
						b.ReportAllocs()
						for i := 0; b.Loop(); i++ {
							g.Update(frames[i&1])
						}
					})
				}
				if c.name[:3] != "bvh" {
					continue
				}
				b.Run(c.name+"/Refit", func(b *testing.B) {
					idx := c.make().(*BVH)
					idx.Build(frames[0])
					b.ReportAllocs()
					for i := 0; b.Loop(); i++ {
						idx.Move(frames[i&1])
						idx.Refit()
					}
				})
			}
		})
	}
}

// BenchmarkStaticChange/<layout>/<index>/<op>: what a static Entity replaced
// at runtime costs: a grid replaces one slot; a BVH rebuilds.
func BenchmarkStaticChange(b *testing.B) {
	for _, l := range StaticLayouts {
		items := l.Make()
		door := items[len(items)/2]
		open := Placed{Segment(door.Shape.Half()), door.At}
		open.At.X += 0.5
		b.Run(l.Name, func(b *testing.B) {
			for _, c := range staticCandidates()[1:] {
				if g, ok := c.make().(interface {
					Index
					Replace(int32, Placed)
				}); ok {
					g.Build(items)
					b.Run(c.name+"/Replace", func(b *testing.B) {
						b.ReportAllocs()
						for i := 0; b.Loop(); i++ {
							if i&1 == 0 {
								g.Replace(int32(len(items)/2), open)
							} else {
								g.Replace(int32(len(items)/2), door)
							}
						}
					})
				}
				b.Run(c.name+"/Rebuild", func(b *testing.B) {
					idx := c.make()
					idx.Build(items)
					b.ReportAllocs()
					for b.Loop() {
						idx.Build(items)
					}
				})
			}
		})
	}
}
