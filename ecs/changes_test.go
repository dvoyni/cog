package ecs

import (
	"math/rand/v2"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

type health struct{ Points int32 }

// observedWorld is a world with an observed health store and its handles.
type observedWorld struct {
	world   World[game]
	store   Store[game, health]
	lookup  Lookup[game, health]
	query   Query1[game, *health]
	changes Changes[game, health]
	reset   *kernel.Task[kernel.NoArg, kernel.NoResult]
	harness *harness
}

// spawnUnobserved spawns the world with observation off: no reset has run.
func spawnUnobserved(t *testing.T) *observedWorld {
	o := &observedWorld{}
	o.harness = spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, health](c)
		o.world = declare[World[game]](c)
		o.store = declare[Store[game, health]](c)
		o.lookup = declare[Lookup[game, health]](c)
		o.query = declare[Query1[game, *health]](c)
		o.changes = declare[Changes[game, health]](c)
		o.reset = c.RegisterTask1(ObservationReset[game])
	})
	return o
}

// spawnObserved spawns the world and runs its first observation reset, so
// it observes.
func spawnObserved(t *testing.T) *observedWorld {
	o := spawnUnobserved(t)
	ObservationReset(nil, kernel.NoArg{}, o.world)
	return o
}

// collect answers the window's changes by entity.
func (o *observedWorld) collect() map[Entity]Change[health] {
	got := map[Entity]Change[health]{}
	for e, change := range o.changes.All() {
		got[e] = change
	}
	return got
}

// changeModel is the reference: what each entity held at the last reset and
// holds now, and the last value of each removed since.
type changeModel struct {
	atReset, now, last map[Entity]health
}

func (m *changeModel) expected() map[Entity]Change[health] {
	want := map[Entity]Change[health]{}
	for e, value := range m.now {
		previous, existed := m.atReset[e]
		switch {
		case !existed:
			want[e] = Change[health]{Kind: ChangeAdded, Value: value}
		case previous != value:
			want[e] = Change[health]{Kind: ChangeChanged, Value: value, Previous: previous}
		}
	}
	for e, previous := range m.atReset {
		if _, ok := m.now[e]; !ok {
			want[e] = Change[health]{Kind: ChangeRemoved, Value: m.last[e], Previous: previous}
		}
	}
	return want
}

func (m *changeModel) remove(e Entity) {
	if value, ok := m.now[e]; ok {
		m.last[e] = value
		delete(m.now, e)
	}
}

func TestChangesMatchAReferenceModel(t *testing.T) {
	o := spawnObserved(t)
	m := &changeModel{atReset: map[Entity]health{}, now: map[Entity]health{}, last: map[Entity]health{}}
	random := rand.New(rand.NewPCG(1, 2))
	var alive []Entity
	pick := func() Entity { return alive[random.IntN(len(alive))] }
	for step := range 20000 {
		switch op := random.IntN(100); {
		case op < 15 || len(alive) == 0:
			alive = append(alive, o.world.Spawn())
		case op < 22:
			i := random.IntN(len(alive))
			o.world.Despawn(alive[i])
			m.remove(alive[i])
			alive[i] = alive[len(alive)-1]
			alive = alive[:len(alive)-1]
		case op < 45:
			e := pick()
			value := health{Points: random.Int32N(4)}
			o.store.Add(e, value)
			m.now[e] = value
		case op < 60:
			e := pick()
			o.store.Remove(e)
			m.remove(e)
		case op < 75:
			e := pick()
			if p := o.store.Write(e); p != nil {
				p.Points = random.Int32N(4) // often back to what it was
				m.now[e] = *p
			}
		case op < 80:
			for e, row := range o.query.All() {
				p := row.Get()
				p.Points = (p.Points + 1) % 4
				m.now[e] = *p
			}
		case op < 85:
			ObservationReset(nil, kernel.NoArg{}, o.world)
			m.atReset = map[Entity]health{}
			for e, value := range m.now {
				m.atReset[e] = value
			}
			m.last = map[Entity]health{}
		}
		got := o.collect()
		want := m.expected()
		if len(got) != len(want) {
			t.Fatalf("step %d: %d changes, want %d:\n got %v\nwant %v", step, len(got), len(want), got, want)
		}
		for e, change := range want {
			if got[e] != change {
				t.Fatalf("step %d: entity %#x changed %+v, want %+v", step, uint64(e), got[e], change)
			}
		}
		for e, value := range m.now {
			if got, ok := o.lookup.Get(e); !ok || got != value {
				t.Fatalf("step %d: entity %#x holds %v, %v, want %v", step, uint64(e), got, ok, value)
			}
		}
		if o.store.store.len() != len(m.now) {
			t.Fatalf("step %d: %d live rows, want %d", step, o.store.store.len(), len(m.now))
		}
	}
}

func TestWritesThroughGetGoUnseen(t *testing.T) {
	o := spawnObserved(t)
	e := o.world.Spawn()
	o.store.Add(e, health{Points: 1})
	ObservationReset(nil, kernel.NoArg{}, o.world)
	o.store.Get(e).Points = 2
	if len(o.collect()) != 0 {
		t.Fatal("a write through Get was seen")
	}
	o.store.Write(e).Points = 3
	if got := o.collect()[e]; got != (Change[health]{Kind: ChangeChanged, Value: health{3}, Previous: health{1}}) {
		t.Fatalf("a write through Write reported %+v", got)
	}
}

func TestDespawnedIndicesAreReusedOnlyAfterTheReset(t *testing.T) {
	o := spawnObserved(t)
	a := o.world.Spawn()
	b := o.world.Spawn()
	o.world.Despawn(b)
	o.world.Despawn(a)
	if c := o.world.Spawn(); c.index() == a.index() || c.index() == b.index() {
		t.Fatal("an index was reused within the window it died in")
	}
	o.harness.run(func(ex *kernel.Executor) kernel.JobHandle {
		return ex.Schedule(o.reset, kernel.NoArg{}).Handle()
	})
	if got := o.world.Spawn(); got != makeEntity(b.index(), 2) {
		t.Fatalf("Spawn after the reset answered %#x, want b's index at generation 2", uint64(got))
	}
	if got := o.world.Spawn(); got != makeEntity(a.index(), 2) {
		t.Fatalf("Spawn after the reset answered %#x, want a's index at generation 2", uint64(got))
	}
}

func TestAbandonedReservationsComeBackBurned(t *testing.T) {
	o := spawnObserved(t)
	abandoned := o.world.world.reserve()
	spawned := o.world.Spawn()
	ObservationReset(nil, kernel.NoArg{}, o.world)
	if !o.world.Alive(spawned) || o.world.Alive(abandoned) {
		t.Fatal("the reset disturbed a spawned entity or revived an abandoned one")
	}
	if got := o.world.Spawn(); got.index() != abandoned.index() || got.generation() == abandoned.generation() {
		t.Fatalf("Spawn answered %#x, want the abandoned index at a new generation", uint64(got))
	}
}

func TestDespawnedIndicesOutsideTheRangeAreNotFreed(t *testing.T) {
	o := spawnObserved(t)
	mirrored := makeEntity(1<<30, 1)
	o.world.CreateAt(mirrored)
	o.world.Despawn(mirrored)
	ObservationReset(nil, kernel.NoArg{}, o.world)
	if o.world.world.freeEntities.Len() != 0 {
		t.Fatal("a mirrored index went onto the free list")
	}
}

func TestChangesAndResetAllocateNothingOnceGrown(t *testing.T) {
	o := spawnObserved(t)
	entities := make([]Entity, 1000)
	for i := range entities {
		entities[i] = o.world.Spawn()
		o.store.Add(entities[i], health{})
	}
	window := func() {
		for i, e := range entities {
			switch i % 3 {
			case 0:
				o.store.Write(e).Points++
			case 1:
				o.store.Remove(e)
				o.store.Add(e, health{Points: 1})
			}
		}
		for range o.changes.All() {
		}
		ObservationReset(nil, kernel.NoArg{}, o.world)
	}
	window()
	if got := testing.AllocsPerRun(20, window); got != 0 {
		t.Fatalf("a window of writes, changes and a reset allocated %v times", got)
	}
}

func TestUnobservedWorldKeepsNothing(t *testing.T) {
	o := spawnUnobserved(t)
	a := o.world.Spawn()
	b := o.world.Spawn()
	o.store.Add(a, health{Points: 1})
	o.store.Add(b, health{Points: 2})
	o.store.Remove(a)
	o.world.Despawn(b)
	if o.store.store.owners.Len() != 0 || len(o.collect()) != 0 {
		t.Fatal("an unobserved store kept removed rows or reported changes")
	}
	if c := o.world.Spawn(); c != makeEntity(b.index(), 2) {
		t.Fatalf("Spawn answered %#x, want b's index reused at once", uint64(c))
	}
}

func TestFirstResetReportsEveryRowAsAdded(t *testing.T) {
	o := spawnUnobserved(t)
	a := o.world.Spawn()
	o.store.Add(a, health{Points: 7})
	ObservationReset(nil, kernel.NoArg{}, o.world)
	if got := o.collect()[a]; got != (Change[health]{Kind: ChangeAdded, Value: health{7}}) {
		t.Fatalf("the first window reported %+v for a row present before it", got)
	}
	ObservationReset(nil, kernel.NoArg{}, o.world)
	if len(o.collect()) != 0 {
		t.Fatal("the second window reported a row nobody touched")
	}
}

func TestUnobservedReservationsNeverRepeat(t *testing.T) {
	o := spawnUnobserved(t)
	random := rand.New(rand.NewPCG(3, 4))
	handedOut := map[Entity]bool{}
	var alive []Entity
	take := func(e Entity) {
		if e == NoEntity || handedOut[e] {
			t.Fatalf("entity %#x handed out twice or NoEntity", uint64(e))
		}
		handedOut[e] = true
	}
	for range 5000 {
		switch op := random.IntN(10); {
		case op < 2:
			take(o.world.world.reserve()) // abandoned: its index is lost
		case op < 6 || len(alive) == 0:
			e := o.world.Spawn()
			take(e)
			alive = append(alive, e)
		default:
			i := random.IntN(len(alive))
			o.world.Despawn(alive[i])
			alive[i] = alive[len(alive)-1]
			alive = alive[:len(alive)-1]
		}
	}
	indices := map[uint32]bool{}
	for _, e := range alive {
		if indices[e.index()] || !o.world.Alive(e) {
			t.Fatalf("entity %#x is dead or shares its index", uint64(e))
		}
		indices[e.index()] = true
	}
}
