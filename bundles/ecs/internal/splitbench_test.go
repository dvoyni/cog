package internal

import "testing"

// The sweep: three entity counts, four work levels, three arms. Every point is a
// whole frame through benchmarkFrame - a real app.UpdateEvent publication, every
// lock acquired, the System run, the publication waited on - because a
// microbenchmark of the walk alone would hide the engine's own per-publication
// charge, which is what a split has to be worth paying beside.
//
// Each benchmark reports ns/entity beside ns/op: the measured per-Entity cost is
// what the go conditions are stated against, and the work level's name is only
// what it was calibrated to.
//
// How to run it is the ticket's protocol and not a detail: one prebuilt binary,
// A/A first for the noise band, then ten rounds of interleaved single-benchmark
// invocations, rotating arm order, medians. See scripts/split-sweep in the
// ticket's comment.

func benchmarkSplitSerial(b *testing.B, n int, system any) {
	benchmarkFrame(b, n, subscribeSplitSerial(system))
	reportPerEntity(b, n)
}

func benchmarkSplitPool(b *testing.B, n, workers, iterations int) {
	// The pool is built here, before benchmarkFrame resets the timer: its
	// goroutines and its per-worker buffers are setup, and a tick allocates
	// neither.
	pool := newSplitPool(b, workers, *splitK)
	benchmarkFrame(b, n, subscribeSplitPool(pool, iterations))
	reportPerEntity(b, n)
}

// reportPerEntity is the measured serial - or split - cost of one Entity, which
// is the number the sweep is read in and the go conditions are stated in.
func reportPerEntity(b *testing.B, n int) {
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/entity")
}

func BenchmarkSplit1000Work2Serial(b *testing.B)  { benchmarkSplitSerial(b, 1000, splitSerialWork2) }
func BenchmarkSplit1000Work2Split8(b *testing.B)  { benchmarkSplitPool(b, 1000, 8, splitWork2) }
func BenchmarkSplit1000Work2Split32(b *testing.B) { benchmarkSplitPool(b, 1000, 32, splitWork2) }

func BenchmarkSplit1000Work25Serial(b *testing.B)  { benchmarkSplitSerial(b, 1000, splitSerialWork25) }
func BenchmarkSplit1000Work25Split8(b *testing.B)  { benchmarkSplitPool(b, 1000, 8, splitWork25) }
func BenchmarkSplit1000Work25Split32(b *testing.B) { benchmarkSplitPool(b, 1000, 32, splitWork25) }

func BenchmarkSplit1000Work100Serial(b *testing.B)  { benchmarkSplitSerial(b, 1000, splitSerialWork100) }
func BenchmarkSplit1000Work100Split8(b *testing.B)  { benchmarkSplitPool(b, 1000, 8, splitWork100) }
func BenchmarkSplit1000Work100Split32(b *testing.B) { benchmarkSplitPool(b, 1000, 32, splitWork100) }

func BenchmarkSplit1000Work400Serial(b *testing.B)  { benchmarkSplitSerial(b, 1000, splitSerialWork400) }
func BenchmarkSplit1000Work400Split8(b *testing.B)  { benchmarkSplitPool(b, 1000, 8, splitWork400) }
func BenchmarkSplit1000Work400Split32(b *testing.B) { benchmarkSplitPool(b, 1000, 32, splitWork400) }

func BenchmarkSplit5000Work2Serial(b *testing.B)  { benchmarkSplitSerial(b, 5000, splitSerialWork2) }
func BenchmarkSplit5000Work2Split8(b *testing.B)  { benchmarkSplitPool(b, 5000, 8, splitWork2) }
func BenchmarkSplit5000Work2Split32(b *testing.B) { benchmarkSplitPool(b, 5000, 32, splitWork2) }

func BenchmarkSplit5000Work25Serial(b *testing.B)  { benchmarkSplitSerial(b, 5000, splitSerialWork25) }
func BenchmarkSplit5000Work25Split8(b *testing.B)  { benchmarkSplitPool(b, 5000, 8, splitWork25) }
func BenchmarkSplit5000Work25Split32(b *testing.B) { benchmarkSplitPool(b, 5000, 32, splitWork25) }

func BenchmarkSplit5000Work100Serial(b *testing.B)  { benchmarkSplitSerial(b, 5000, splitSerialWork100) }
func BenchmarkSplit5000Work100Split8(b *testing.B)  { benchmarkSplitPool(b, 5000, 8, splitWork100) }
func BenchmarkSplit5000Work100Split32(b *testing.B) { benchmarkSplitPool(b, 5000, 32, splitWork100) }

func BenchmarkSplit5000Work400Serial(b *testing.B)  { benchmarkSplitSerial(b, 5000, splitSerialWork400) }
func BenchmarkSplit5000Work400Split8(b *testing.B)  { benchmarkSplitPool(b, 5000, 8, splitWork400) }
func BenchmarkSplit5000Work400Split32(b *testing.B) { benchmarkSplitPool(b, 5000, 32, splitWork400) }

func BenchmarkSplit20000Work2Serial(b *testing.B)  { benchmarkSplitSerial(b, 20000, splitSerialWork2) }
func BenchmarkSplit20000Work2Split8(b *testing.B)  { benchmarkSplitPool(b, 20000, 8, splitWork2) }
func BenchmarkSplit20000Work2Split32(b *testing.B) { benchmarkSplitPool(b, 20000, 32, splitWork2) }

func BenchmarkSplit20000Work25Serial(b *testing.B)  { benchmarkSplitSerial(b, 20000, splitSerialWork25) }
func BenchmarkSplit20000Work25Split8(b *testing.B)  { benchmarkSplitPool(b, 20000, 8, splitWork25) }
func BenchmarkSplit20000Work25Split32(b *testing.B) { benchmarkSplitPool(b, 20000, 32, splitWork25) }

func BenchmarkSplit20000Work100Serial(b *testing.B) {
	benchmarkSplitSerial(b, 20000, splitSerialWork100)
}
func BenchmarkSplit20000Work100Split8(b *testing.B)  { benchmarkSplitPool(b, 20000, 8, splitWork100) }
func BenchmarkSplit20000Work100Split32(b *testing.B) { benchmarkSplitPool(b, 20000, 32, splitWork100) }

func BenchmarkSplit20000Work400Serial(b *testing.B) {
	benchmarkSplitSerial(b, 20000, splitSerialWork400)
}
func BenchmarkSplit20000Work400Split8(b *testing.B)  { benchmarkSplitPool(b, 20000, 8, splitWork400) }
func BenchmarkSplit20000Work400Split32(b *testing.B) { benchmarkSplitPool(b, 20000, 32, splitWork400) }

// The break-even probes: one thousand Entities at two work levels between the
// sweep's trivial and its ~25 ns one, so the point where the best split arm
// first beats serial can be stated in microseconds of serial walk a run rather
// than bracketed between two sweep points a factor of four apart.
func BenchmarkSplit1000Work8Serial(b *testing.B)   { benchmarkSplitSerial(b, 1000, splitSerialWork8) }
func BenchmarkSplit1000Work8Split8(b *testing.B)   { benchmarkSplitPool(b, 1000, 8, splitWork8) }
func BenchmarkSplit1000Work8Split32(b *testing.B)  { benchmarkSplitPool(b, 1000, 32, splitWork8) }
func BenchmarkSplit1000Work15Serial(b *testing.B)  { benchmarkSplitSerial(b, 1000, splitSerialWork15) }
func BenchmarkSplit1000Work15Split8(b *testing.B)  { benchmarkSplitPool(b, 1000, 8, splitWork15) }
func BenchmarkSplit1000Work15Split32(b *testing.B) { benchmarkSplitPool(b, 1000, 32, splitWork15) }

// The handler-driven arm: the framework owns the walk and the body is a func
// value called once an Entity. Run with -split.k=0 it measures whether the win
// survives the indirect call; run with a k above every row count it measures
// what the indirect call costs on its own, against the same walk with the body
// inlined.
func benchmarkSplitCall(b *testing.B, n, workers int, call splitBody, iterations int) {
	pool := newSplitPool(b, workers, *splitK)
	pool.call = call
	benchmarkFrame(b, n, subscribeSplitPool(pool, iterations))
	reportPerEntity(b, n)
}
func BenchmarkSplit1000Work2Call32(b *testing.B) {
	benchmarkSplitCall(b, 1000, 32, splitCallWork2, splitWork2)
}
func BenchmarkSplit1000Work25Call32(b *testing.B) {
	benchmarkSplitCall(b, 1000, 32, splitCallWork25, splitWork25)
}
func BenchmarkSplit1000Work100Call32(b *testing.B) {
	benchmarkSplitCall(b, 1000, 32, splitCallWork100, splitWork100)
}
func BenchmarkSplit1000Work400Call32(b *testing.B) {
	benchmarkSplitCall(b, 1000, 32, splitCallWork400, splitWork400)
}
func BenchmarkSplit5000Work2Call32(b *testing.B) {
	benchmarkSplitCall(b, 5000, 32, splitCallWork2, splitWork2)
}
func BenchmarkSplit5000Work25Call32(b *testing.B) {
	benchmarkSplitCall(b, 5000, 32, splitCallWork25, splitWork25)
}
func BenchmarkSplit5000Work100Call32(b *testing.B) {
	benchmarkSplitCall(b, 5000, 32, splitCallWork100, splitWork100)
}
func BenchmarkSplit5000Work400Call32(b *testing.B) {
	benchmarkSplitCall(b, 5000, 32, splitCallWork400, splitWork400)
}
func BenchmarkSplit20000Work2Call32(b *testing.B) {
	benchmarkSplitCall(b, 20000, 32, splitCallWork2, splitWork2)
}
func BenchmarkSplit20000Work25Call32(b *testing.B) {
	benchmarkSplitCall(b, 20000, 32, splitCallWork25, splitWork25)
}
func BenchmarkSplit20000Work100Call32(b *testing.B) {
	benchmarkSplitCall(b, 20000, 32, splitCallWork100, splitWork100)
}
func BenchmarkSplit20000Work400Call32(b *testing.B) {
	benchmarkSplitCall(b, 20000, 32, splitCallWork400, splitWork400)
}
