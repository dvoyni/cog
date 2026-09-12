package ecs

import (
	"strconv"
	"testing"
)

// What naming an engine-side thing costs. Two questions decide whether a hash
// is the right shape: what producing one costs in a System that changes what an
// Entity names, and what resolving one costs in the System that draws it. The
// first must allocate nothing, because it happens per Entity per tick; the
// second is one map probe against the alternatives, which is the table the spec
// quotes.
//
// These are microbenchmarks and say so. The whole-frame numbers in
// docs/specs/ecs.md §Naming an engine-side thing measure a System assigning a
// clip on a real engine and are not re-derived here.

// lookupBatch is how many lookups one measured iteration performs, so that the
// loop's own index arithmetic does not dominate a probe that costs well under a
// nanosecond.
const lookupBatch = 1000

var (
	hashSink  modelHash
	modelSink model
	textSink  string
	indexSink int
	foundSink bool
)

// manifest is the table a consumer actually holds, built three ways: by hash,
// by path, and as the dense index a consumer is free to use internally once it
// has resolved.
type manifest struct {
	names  Names[modelHash, model]
	byPath map[string]model
	dense  []model
	paths  []string
	hashes []modelHash
}

func newManifest(tb testing.TB, n int) *manifest {
	m := &manifest{
		byPath: make(map[string]model, n),
		dense:  make([]model, n),
		paths:  make([]string, n),
		hashes: make([]modelHash, n),
	}
	for i := range n {
		path := "models/characters/unit" + strconv.Itoa(i) + ".glb"
		value := model{path: path, mesh: i}
		if err := m.names.Register(path, value); err != nil {
			tb.Fatalf("Register(%q) = %v, want nil", path, err)
		}
		m.byPath[path] = value
		m.dense[i] = value
		m.paths[i] = path
		m.hashes[i] = HashOf[modelHash](path)
	}
	return m
}

// BenchmarkHashOfAShortName is the clip case: a name a System writes into a
// Component every tick it changes its mind.
func BenchmarkHashOfAShortName(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		hashSink = HashOf[modelHash]("Walk")
	}
}

// BenchmarkHashOfAPath is the model case, and the length that matters: FNV-1a
// is a byte at a time, so the cost is the name.
func BenchmarkHashOfAPath(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		hashSink = HashOf[modelHash]("models/characters/unit512.glb")
	}
}

// BenchmarkHashOfAPathNotConstant hashes a string the compiler cannot fold, so
// the loop measures the hash rather than a constant the optimiser precomputed.
func BenchmarkHashOfAPathNotConstant(b *testing.B) {
	m := newManifest(b, lookupBatch)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hashSink = HashOf[modelHash](m.paths[i%lookupBatch])
	}
}

// The four ways to get from a name to a thing, which is the table the spec
// quotes. The dense index is what a consumer may use internally; the stored
// hash is what a Component carries; hashing every lookup is what a consumer
// does if it forgets to store the hash; the path is what scene resolves today.
// Each loop is written out rather than passed to a shared helper as a closure.
// A func value is an indirect call of about a nanosecond, which is more than
// the difference these numbers are about.
func BenchmarkLookupByDenseIndex(b *testing.B) {
	m := newManifest(b, lookupBatch)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for index := range lookupBatch {
			modelSink = m.dense[index]
		}
	}
	perLookup(b)
}

func BenchmarkLookupByStoredHash(b *testing.B) {
	m := newManifest(b, lookupBatch)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for index := range lookupBatch {
			modelSink, foundSink = m.names.Lookup(m.hashes[index])
		}
	}
	perLookup(b)
}

func BenchmarkLookupByHashingEveryTime(b *testing.B) {
	m := newManifest(b, lookupBatch)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for index := range lookupBatch {
			modelSink, foundSink = m.names.Lookup(HashOf[modelHash](m.paths[index]))
		}
	}
	perLookup(b)
}

func BenchmarkLookupByPath(b *testing.B) {
	m := newManifest(b, lookupBatch)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for index := range lookupBatch {
			modelSink, foundSink = m.byPath[m.paths[index]]
		}
	}
	perLookup(b)
}

// BenchmarkLookupMisses is the procedural-name case: a hash nobody registered.
// It is the cost of the answer "no such thing", and it is what
// TestHashingAccumulatesNothing does a million times.
func BenchmarkLookupMisses(b *testing.B) {
	m := newManifest(b, lookupBatch)
	misses := make([]modelHash, lookupBatch)
	for i := range misses {
		misses[i] = HashOf[modelHash]("procedural/" + strconv.Itoa(i))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for index := range lookupBatch {
			modelSink, foundSink = m.names.Lookup(misses[index])
		}
	}
	perLookup(b)
}

// BenchmarkTextOf is what tooling pays to make a hash legible again.
func BenchmarkTextOf(b *testing.B) {
	m := newManifest(b, lookupBatch)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for index := range lookupBatch {
			textSink, foundSink = m.names.TextOf(m.hashes[index])
		}
	}
	perLookup(b)
}

// perLookup restates the iteration cost as the cost of one lookup, since an
// iteration is lookupBatch of them.
func perLookup(b *testing.B) {
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*lookupBatch), "ns/lookup")
}

// BenchmarkRegisterAManifest is a startup cost and is measured to be reported,
// not to be defended: a thousand names is what a game's manifest looks like and
// it happens once.
func BenchmarkRegisterAManifest(b *testing.B) {
	paths := make([]string, lookupBatch)
	for i := range paths {
		paths[i] = "models/characters/unit" + strconv.Itoa(i) + ".glb"
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var names Names[modelHash, model]
		for index, path := range paths {
			if err := names.Register(path, model{path: path, mesh: index}); err != nil {
				b.Fatalf("Register(%q) = %v, want nil", path, err)
			}
		}
		indexSink = len(names.byHash)
	}
}

// BenchmarkLookupByStoredHashBareMap is the control for the one structural
// choice Names makes: a row carries the text beside the value, so a row is
// wider than the value alone. This is the same probe into a map whose value is
// the model and nothing else.
func BenchmarkLookupByStoredHashBareMap(b *testing.B) {
	m := newManifest(b, lookupBatch)
	bare := make(map[modelHash]model, lookupBatch)
	for i, hash := range m.hashes {
		bare[hash] = m.dense[i]
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for index := range lookupBatch {
			modelSink, foundSink = bare[m.hashes[index]]
		}
	}
	perLookup(b)
}
