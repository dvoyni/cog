package ecs

import (
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// solid is the app-maintained Tag of the Driver's remedy: the app puts it on
// exactly the Entities that have both Components, and maintains it itself.
// There is deliberately no ECS mechanism for this — that road is EnTT groups
// and shipyard packs, and exclusivity is what killed both — so it is an
// ordinary Tag, an ordinary Store, and a very good Driver.
type solid struct{}

// badCaseQuery is the Driver's known bad case: two large, mostly disjoint
// Stores. The shortest-Store heuristic can only pick one of the two five
// thousands, so it yields five thousand candidates and throws away all but the
// hundred in the intersection.
type badCaseQuery struct {
	Body     *body
	Collider collider
}

// remedyQuery is the same Query with the Tag named. The Tag's Store is a
// hundred long, so the scan picks it and the walk is a hundred steps: the Tag
// is named as an ordinary field rather than as a With, because a filter can
// never drive.
type remedyQuery struct {
	Body     *body
	Collider collider
	Solid    solid
}

type badCaseSystem kernel.Subscription[app.UpdateEvent]

type remedySystem kernel.Subscription[app.UpdateEvent]

// disjointStores builds the shape the spec records: n with Body, n with
// Collider, both with an intersection of overlap, and the Tag on exactly that
// intersection. The entities are interleaved rather than laid out in blocks, so
// the probe hits the other Store's dense array at a realistic distance.
func disjointStores(tb testing.TB, n, overlap int, subscribe func(*kernel.Registrar, *Entities)) (
	*Entities, *componentsPlugin, *kernel.Engine,
) {
	tb.Helper()
	entities, components, engine := newWorld(tb, uint32(2*n), subscribe)
	for i := range n - overlap {
		bodied := entities.alloc()
		components.bodies.Set(bodied, body{X: float32(i)})
		collided := entities.alloc()
		components.colliders.Set(collided, collider{Radius: 1})
	}
	for range overlap {
		both := entities.alloc()
		components.bodies.Set(both, body{})
		components.colliders.Set(both, collider{Radius: 1})
		components.solids.Set(both, solid{})
	}
	return entities, components, engine
}

// benchmarkDriver measures the walk itself rather than a whole frame: the
// engine's own per-publication charge is around eighty nanoseconds, which would
// swamp the remedy's number and flatter the bad case's. walk is a closure
// because the Query type differs between the two arms; it is called once per
// op, never per Entity, so the range statement inside it keeps every call in
// its own chain direct.
func benchmarkDriver(b *testing.B, n, overlap int, subscribe func(*kernel.Registrar, *Entities), walk func() int) {
	_, _, engine := disjointStores(b, n, overlap, subscribe)
	// One frame, so the System has run and the Query is the one the kernel
	// bound rather than a Query assembled by the test.
	frame(b, engine, 1)

	visited := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		visited += walk()
	}
	b.StopTimer()
	if want := b.N * overlap; visited != want {
		b.Fatalf("the walk yielded %d entities over %d runs, want %d", visited, b.N, want)
	}
}

// BenchmarkDriverBadCase drives off a five-thousand-entity Store to find a
// hundred matches.
func BenchmarkDriverBadCase(b *testing.B) {
	var query *Query[badCaseQuery]
	benchmarkDriver(b, 5000, 100,
		func(registrar *kernel.Registrar, world *Entities) {
			registrar.Subscribe[badCaseSystem](ToHandler[app.UpdateEvent](world,
				func(q *Query[badCaseQuery]) { query = q }))
		},
		func() int {
			visited := 0
			for _, it := range query.All() {
				it.Body.X += it.Collider.Radius
				visited++
			}
			return visited
		})
}

// BenchmarkDriverTagRemedy drives off the hundred-entity Tag on the
// intersection. Same matches, same probes, one Store scanned instead of five
// thousand.
func BenchmarkDriverTagRemedy(b *testing.B) {
	var query *Query[remedyQuery]
	benchmarkDriver(b, 5000, 100,
		func(registrar *kernel.Registrar, world *Entities) {
			registrar.Subscribe[remedySystem](ToHandler[app.UpdateEvent](world,
				func(q *Query[remedyQuery]) { query = q }))
		},
		func() int {
			visited := 0
			for _, it := range query.All() {
				it.Body.X += it.Collider.Radius
				visited++
			}
			return visited
		})
}

// TestTheRemedyTagYieldsTheSameEntities is what makes the two benchmarks
// comparable rather than merely different: the Tag is a Driver, not a filter,
// so it must change what the Query costs and nothing about what it matches.
func TestTheRemedyTagYieldsTheSameEntities(t *testing.T) {
	const n, overlap = 500, 20
	var bad *Query[badCaseQuery]
	var remedy *Query[remedyQuery]
	_, _, engine := disjointStores(t, n, overlap, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[badCaseSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[badCaseQuery]) { bad = q }))
		registrar.Subscribe[remedySystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[remedyQuery]) { remedy = q }))
	})
	frame(t, engine, 1)

	seen := map[Entity]bool{}
	for e := range bad.All() {
		seen[e] = true
	}
	if len(seen) != overlap {
		t.Fatalf("the unaided Query matched %d entities, want %d", len(seen), overlap)
	}
	matched := 0
	for e := range remedy.All() {
		if !seen[e] {
			t.Fatalf("%v matched with the Tag and not without it", e)
		}
		matched++
	}
	if matched != overlap {
		t.Fatalf("the Tag-driven Query matched %d entities, want %d", matched, overlap)
	}
	if len(bad.walk) != n || len(remedy.walk) != overlap {
		t.Fatalf("the walks are %d and %d long, want %d and %d",
			len(bad.walk), len(remedy.walk), n, overlap)
	}
}
