package types

import (
	"runtime"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// spikePeak is how many Entities a spike reaches, and spikeSurvivor picks the
// few left alive when it ends: one in a thousand, so the highest index held sits
// below the top of the index space and there are free indices above it to drop.
const spikePeak = 10_000

func spikeSurvivor(i int) bool { return i%1000 == 7 }

// spikedWorld is an engine after a spike has ended: ten thousand Entities were
// walked by a Query, and all but ten were despawned, so every area holds the
// spike's capacity and a fraction of its length.
type spikedWorld struct {
	entities   *Entities
	components *componentsPlugin
	engine     *kernel.Engine
	// query is the move System's own Query, caught from inside the System so the
	// test can read its walk.
	query     *Query[moveQuery]
	survivors []Entity
}

func spiked(t *testing.T) *spikedWorld {
	t.Helper()
	w := &spikedWorld{}
	w.entities, w.components, w.engine = newWorld(t, 16, func(registrar *kernel.Registrar) {
		registrar.Subscribe[moveSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[moveQuery]) {
			w.query = q
			move(q)
		}))
	})
	spike := make([]Entity, spikePeak)
	for i := range spike {
		spike[i] = w.entities.alloc()
		w.components.bodies.Set(spike[i], body{X: float32(i)})
		w.components.velocities.Set(spike[i], velocity{Y: 1})
	}
	frame(t, w.engine, 1)
	for i, e := range spike {
		if spikeSurvivor(i) {
			w.survivors = append(w.survivors, e)
			continue
		}
		w.entities.despawn(e)
	}
	// The frame after the spike ends walks the survivors, which is the steady
	// state an app would shrink from.
	frame(t, w.engine, 1)
	return w
}

func (w *spikedWorld) shrink(t *testing.T, request ShrinkRequest) ShrinkResponse {
	t.Helper()
	response := w.engine.Executioner().ExecuteCommand[shrinkCmd](request)
	return response
}

// storeCapacity is what one Store holds, as the three capacities a shrink cuts.
type storeCapacity struct{ sparse, owners, dense int }

func capacityOf[T any](store *Store[T]) storeCapacity {
	return storeCapacity{cap(store.sparse), cap(store.owners), cap(store.dense)}
}

func TestAShrinkCutsEveryStoreToItsPopulation(t *testing.T) {
	w := spiked(t)
	bodies := w.components.bodies

	released := w.shrink(t, ShrinkRequest{KeepEntities: true, KeepScratch: true})

	if released.Stores == 0 {
		t.Fatalf("a shrink after a spike released no Store bytes")
	}
	highest := int(w.survivors[len(w.survivors)-1].idx()) + 1
	want := storeCapacity{sparse: highest, owners: len(w.survivors), dense: len(w.survivors)}
	if got := capacityOf(bodies); got != want {
		t.Fatalf("the body Store holds %+v after a shrink, want %+v: its rows cut to its population and its sparse array to its highest index held", got, want)
	}
	if len(bodies.sparse) != highest {
		t.Fatalf("the sparse array is %d slots long, want %d", len(bodies.sparse), highest)
	}
	for _, e := range w.survivors {
		if value, ok := bodies.Get(e); !ok || value.Y != 2 {
			t.Fatalf("survivor %v reads back as %+v, %v after the shrink; want its value kept", e, value, ok)
		}
	}
}

func TestKeepStoresReleasesNothingFromAStore(t *testing.T) {
	w := spiked(t)
	before := capacityOf(w.components.bodies)

	released := w.shrink(t, ShrinkRequest{KeepStores: true})

	if released.Stores != 0 {
		t.Fatalf("KeepStores released %d Store bytes, want 0", released.Stores)
	}
	if after := capacityOf(w.components.bodies); after != before {
		t.Fatalf("KeepStores changed the body Store from %+v to %+v", before, after)
	}
}

func TestAShrinkDropsTheFreeIndicesAtTheTopOfTheIndexSpace(t *testing.T) {
	w := spiked(t)

	released := w.shrink(t, ShrinkRequest{KeepStores: true, KeepScratch: true})

	if released.Entities == 0 {
		t.Fatalf("a shrink after a spike released no Entities bytes")
	}
	highest := int(w.survivors[len(w.survivors)-1].idx()) + 1
	if len(w.entities.gens) != highest || cap(w.entities.gens) != highest {
		t.Fatalf("the index space is %d long with capacity %d after a shrink, want both %d: the highest index held",
			len(w.entities.gens), cap(w.entities.gens), highest)
	}
	if want := highest - len(w.survivors); len(w.entities.free) != want || cap(w.entities.free) != want {
		t.Fatalf("the free list is %d long with capacity %d after a shrink, want both %d",
			len(w.entities.free), cap(w.entities.free), want)
	}
	for _, index := range w.entities.free {
		if int(index) >= highest {
			t.Fatalf("the free list still holds index %d, above the index space of %d", index, highest)
		}
	}
	for _, e := range w.survivors {
		if !w.entities.Alive(e) {
			t.Fatalf("survivor %v is not Alive after the shrink", e)
		}
	}
}

func TestKeepEntitiesReleasesNothingFromTheAuthority(t *testing.T) {
	w := spiked(t)
	gens, free := cap(w.entities.gens), cap(w.entities.free)

	released := w.shrink(t, ShrinkRequest{KeepEntities: true})

	if released.Entities != 0 {
		t.Fatalf("KeepEntities released %d Entities bytes, want 0", released.Entities)
	}
	if cap(w.entities.gens) != gens || cap(w.entities.free) != free {
		t.Fatalf("KeepEntities changed the authority's capacities from %d and %d to %d and %d",
			gens, free, cap(w.entities.gens), cap(w.entities.free))
	}
}

// TestAHandleToADroppedIndexNeverMatchesItsNextEntity is the generation floor.
// Dropping an index forgets its generation, so without the floor the index
// would start again at generation 1 and the first handle ever issued for it
// would come back to life. Each round climbs the index's generation by
// recycling it before the shrink drops it, and every handle ever issued is
// checked against the one the index is allocated to next.
func TestAHandleToADroppedIndexNeverMatchesItsNextEntity(t *testing.T) {
	entities := newEntities(4)
	store := NewStore[position](entities, 4)
	var issued []Entity
	for round := range 4 {
		for range round + 1 {
			e := entities.alloc()
			issued = append(issued, e)
			entities.despawn(e)
		}
		entities.shrink(ShrinkRequest{})
		if len(entities.gens) != 0 {
			t.Fatalf("round %d: the shrink left an index space of %d with nothing alive, want 0", round, len(entities.gens))
		}

		next := entities.alloc()
		store.Set(next, position{X: float32(round)})
		for _, stale := range issued {
			if next == stale {
				t.Fatalf("round %d: the dropped index was allocated again as %v, a handle already issued", round, next)
			}
			if entities.Alive(stale) {
				t.Fatalf("round %d: the stale handle %v is Alive once its index is allocated again", round, stale)
			}
			if store.Has(stale) {
				t.Fatalf("round %d: the stale handle %v finds the row of %v", round, stale, next)
			}
		}
		issued = append(issued, next)
		entities.despawn(next)
	}
}

func TestAShrinkReleasesAQuerysWalk(t *testing.T) {
	w := spiked(t)
	if cap(w.query.walk) < spikePeak {
		t.Fatalf("the walk holds %d after the spike, want at least %d: the spike is not measuring the walk", cap(w.query.walk), spikePeak)
	}

	released := w.shrink(t, ShrinkRequest{KeepStores: true, KeepEntities: true})

	if released.Scratch == 0 {
		t.Fatalf("a shrink after a spike released no Scratch bytes")
	}
	if w.query.walk != nil {
		t.Fatalf("the walk still holds %d Entities of capacity after a shrink", cap(w.query.walk))
	}
	// The next run captures its walk afresh and finds every survivor.
	frame(t, w.engine, 1)
	for _, e := range w.survivors {
		if value, _ := w.components.bodies.Get(e); value.Y != 3 {
			t.Fatalf("survivor %v reads back as %+v after a frame following the shrink, want the Query to have moved it", e, value)
		}
	}
}

func TestKeepScratchReleasesNothingFromAQuery(t *testing.T) {
	w := spiked(t)
	walk := cap(w.query.walk)

	released := w.shrink(t, ShrinkRequest{KeepScratch: true})

	if released.Scratch != 0 {
		t.Fatalf("KeepScratch released %d Scratch bytes, want 0", released.Scratch)
	}
	if cap(w.query.walk) != walk {
		t.Fatalf("KeepScratch changed the walk's capacity from %d to %d", walk, cap(w.query.walk))
	}
}

// TestTheZeroRequestShrinksEveryArea is the request an app sends after a spike.
// No Hook reader watches this world, so Hooks has nothing to release and reports
// 0 whether or not it is kept. A second shrink finds every area already at its
// length.
func TestTheZeroRequestShrinksEveryArea(t *testing.T) {
	w := spiked(t)

	released := w.shrink(t, ShrinkRequest{})

	if released.Stores == 0 || released.Entities == 0 || released.Scratch == 0 {
		t.Fatalf("the zero request after a spike released %+v, want every area but Hooks above 0", released)
	}
	if released.Hooks != 0 {
		t.Fatalf("the zero request released %d Hooks bytes with no Hook reader to release", released.Hooks)
	}
	if again := w.shrink(t, ShrinkRequest{}); again != (ShrinkResponse{}) {
		t.Fatalf("a second shrink released %+v, want nothing: every area was already at its length", again)
	}
	for _, e := range w.survivors {
		if value, ok := w.components.bodies.Get(e); !ok || value.Y != 2 {
			t.Fatalf("survivor %v reads back as %+v, %v after the shrink; want its value kept", e, value, ok)
		}
	}
}

// TestTheSpikesArraysBecomeUnreachable asks the question a shrink is for, which
// is reachability, with a finaliser on the driver Store's owners array as the
// spike left it. The Query's walk aliases that array, so a shrink that cut the
// Store and kept the walk would report the bytes and give nothing back until the
// System next ran.
func TestTheSpikesArraysBecomeUnreachable(t *testing.T) {
	collectedAfter := func(request ShrinkRequest) bool {
		w := spiked(t)
		collected := make(chan struct{}, 1)
		runtime.SetFinalizer(&w.components.bodies.owners[:1][0], func(*Entity) { collected <- struct{}{} })
		w.shrink(t, request)
		defer runtime.KeepAlive(w)
		for range 5 {
			runtime.GC()
			select {
			case <-collected:
				return true
			default:
			}
		}
		return false
	}

	if !collectedAfter(ShrinkRequest{}) {
		t.Fatalf("the spike's owners array is still reachable after the zero request")
	}
	if collectedAfter(ShrinkRequest{KeepScratch: true}) {
		t.Fatalf("the spike's owners array was collected with KeepScratch, so the walk is not what holds it and this test measures nothing")
	}
}
