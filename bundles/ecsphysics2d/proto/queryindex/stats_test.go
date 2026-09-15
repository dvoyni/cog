//go:build protostats

package queryindex

import (
	"fmt"
	"os"
	"testing"
)

// TestStats writes, per layout, index and mix: the fraction of Sweeps that
// hit, mean hits per SweepAll, and mean shape tests per Sweep, Blocked and
// SweepAll. Run with -tags protostats; the path is $STATS_OUT.
func TestStats(t *testing.T) {
	out, err := os.Create(os.Getenv("STATS_OUT"))
	if err != nil {
		t.Skip("set STATS_OUT")
	}
	defer out.Close()
	fmt.Fprintln(out, "set\tlayout\tindex\tmix\thit\tallHits\ttestsSweep\ttestsBlocked\ttestsAll")
	run := func(set, layout string, items []Placed, cands []candidate, mixes []Mix, side float32, n int) {
		for _, c := range cands {
			idx := c.make()
			idx.Build(items)
			for _, mix := range mixes {
				qs := QuerySet(mix, side)
				var hits, all int
				var ts, tb, ta uint64
				var dst []Hit
				for i, q := range qs {
					ex := entityOf(-1)
					if set == "body" {
						ex = bodyExclude(i, n)
					}
					statTests = 0
					if _, ok := idx.Sweep(q.From, q.To, q.Radius, ex); ok {
						hits++
					}
					ts += statTests
					statTests = 0
					idx.Blocked(q.From, q.To, q.Radius, ex)
					tb += statTests
					statTests = 0
					dst = idx.SweepAll(dst[:0], q.From, q.To, q.Radius, ex)
					ta += statTests
					all += len(dst)
				}
				k := float64(len(qs))
				fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%.3f\t%.2f\t%.1f\t%.1f\t%.1f\n", set, layout, c.name, mix.Name,
					float64(hits)/k, float64(all)/k, float64(ts)/k, float64(tb)/k, float64(ta)/k)
			}
		}
	}
	for _, l := range StaticLayouts {
		run("static", l.Name, l.Make(), staticCandidates(), Mixes, MapSize, 0)
	}
	for _, l := range BodyLayouts {
		run("body", l.Name, Bodies(l.N, l.Side, 0), bodyCandidates(), Mixes[:4], l.Side, l.N)
	}
}
