package types

import "testing"

// BenchmarkHookMarkChanged is Set.MarkChanged on 1% of a 10k-Entity Store,
// on a Store nothing watches and on one watched for Changed. ns/op is per mark,
// the run end's compare and its Changed record included.
func BenchmarkHookMarkChanged(b *testing.B) {
	const n, marked = 10_000, 100
	for _, arm := range []string{"unwatched", "watched"} {
		b.Run(arm, func(b *testing.B) {
			w := changedWriterWorld[body](b, n, arm == "watched")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i += marked {
				for j := range marked {
					w.set.MarkChanged(w.ids[(j*97+i)%n])
				}
				w.runEnd()
			}
		})
	}
}
