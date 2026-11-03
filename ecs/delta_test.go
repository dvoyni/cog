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
	deltas  DeltaQuery1[game, health]
	reset   *kernel.Task[kernel.NoArg, kernel.NoResult]
	harness *harness
}

// spawnUnobserved spawns the world with observation off: no reset has run.
func spawnUnobserved(t *testing.T) *observedWorld {
	o := &observedWorld{}
	o.harness = spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, health](c, "health")
		o.world = declare[World[game]](c)
		o.store = declare[Store[game, health]](c)
		o.lookup = declare[Lookup[game, health]](c)
		o.query = declare[Query1[game, *health]](c)
		o.deltas = declare[DeltaQuery1[game, health]](c)
		o.reset = c.RegisterTask1(ObservationResetSystem[game])
	})
	return o
}

// spawnObserved spawns the world and runs its first observation reset, so
// it observes.
func spawnObserved(t *testing.T) *observedWorld {
	o := spawnUnobserved(t)
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	return o
}

// collect answers the window's deltas by entity.
func (o *observedWorld) collect() map[Entity]observedDelta[health] {
	return collectDeltas(o.deltas)
}

// observedDelta is one entity's row of a one-term delta query, as the tests
// compare it.
type observedDelta[T any] struct {
	Delta           Delta
	Value, Previous T
}

// collectDeltas answers what q yields, by entity. It fails nothing itself:
// an entity yielded twice shows as fewer entries than the model expects.
func collectDeltas[T any](q DeltaQuery1[game, T]) map[Entity]observedDelta[T] {
	got := map[Entity]observedDelta[T]{}
	for e, row := range q.All() {
		got[e] = observedDelta[T]{Delta: row.Delta(), Value: row.Get(), Previous: row.Previous()}
	}
	return got
}

// changeModel is the reference: what each entity held at the last reset and
// holds now, and the last value of each removed since.
type changeModel struct {
	atReset, now, last map[Entity]health
}

func (m *changeModel) expected() map[Entity]observedDelta[health] {
	want := map[Entity]observedDelta[health]{}
	for e, value := range m.now {
		previous, existed := m.atReset[e]
		switch {
		case !existed:
			want[e] = observedDelta[health]{Delta: DeltaAdded, Value: value}
		case previous != value:
			want[e] = observedDelta[health]{Delta: DeltaChanged, Value: value, Previous: previous}
		}
	}
	for e, previous := range m.atReset {
		if _, ok := m.now[e]; !ok {
			want[e] = observedDelta[health]{Delta: DeltaRemoved, Value: m.last[e], Previous: previous}
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

func TestDeltasMatchAReferenceModel(t *testing.T) {
	o := spawnObserved(t)
	m := &changeModel{atReset: map[Entity]health{}, now: map[Entity]health{}, last: map[Entity]health{}}
	random := rand.New(rand.NewPCG(1, 2))
	var alive []Entity
	pick := func() Entity { return alive[random.IntN(len(alive))] }
	// listedSteps and overflowedSteps count the steps whose deltas came from
	// the listing of changed entities and from a walk of every row.
	listedSteps := 0
	overflowedSteps := 0
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
			ObservationResetSystem(nil, kernel.NoArg{}, o.world)
			m.atReset = map[Entity]health{}
			for e, value := range m.now {
				m.atReset[e] = value
			}
			m.last = map[Entity]health{}
		}
		if s := o.store.store; s.changedEntitiesOverflowed {
			overflowedSteps++
		} else if len(s.changedEntities) != 0 {
			listedSteps++
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
	if listedSteps < 1000 || overflowedSteps < 1000 {
		t.Fatalf("%d steps listed and %d walked every row", listedSteps, overflowedSteps)
	}
}

func TestWritesThroughGetGoUnseen(t *testing.T) {
	o := spawnObserved(t)
	e := o.world.Spawn()
	o.store.Add(e, health{Points: 1})
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	o.store.Get(e).Points = 2
	if len(o.collect()) != 0 {
		t.Fatal("a write through Get was seen")
	}
	o.store.Write(e).Points = 3
	if got := o.collect()[e]; got != (observedDelta[health]{Delta: DeltaChanged, Value: health{3}, Previous: health{1}}) {
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
	if got := o.world.Spawn(); got != makeEntity(a.index(), 2) {
		t.Fatalf("Spawn after the reset answered %#x, want a's index, despawned last, at generation 2", uint64(got))
	}
	if got := o.world.Spawn(); got != makeEntity(b.index(), 2) {
		t.Fatalf("Spawn after the reset answered %#x, want b's index at generation 2", uint64(got))
	}
}

func TestAbandonedReservationsComeBackBurned(t *testing.T) {
	o := spawnObserved(t)
	abandoned := o.world.world.entities.reserve()
	spawned := o.world.Spawn()
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
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
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	if o.world.world.entities.freeEntities.Len() != 0 {
		t.Fatal("a mirrored index went onto the free list")
	}
}

// TestIndexReusedInTheWindowReportsBoth despawns a mirrored entity and
// places the next generation at its index within one window, in a store few
// enough changes leave listing: the removed row in the tail loses its link to
// the index, and is still reported.
func TestIndexReusedInTheWindowReportsBoth(t *testing.T) {
	o := spawnObserved(t)
	for range 100 {
		o.store.Add(o.world.Spawn(), health{})
	}
	mirrored := makeEntity(1<<30, 1)
	o.world.CreateAt(mirrored)
	o.store.Add(mirrored, health{Points: 1})
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	o.world.Despawn(mirrored)
	if o.store.store.changedEntitiesOverflowed {
		t.Fatal("one removal stopped the listing")
	}
	replaced := makeEntity(1<<30, 2)
	o.world.CreateAt(replaced)
	o.store.Add(replaced, health{Points: 2})
	got := o.collect()
	if len(got) != 2 || got[mirrored] != (observedDelta[health]{Delta: DeltaRemoved, Value: health{1}, Previous: health{1}}) || got[replaced] != (observedDelta[health]{Delta: DeltaAdded, Value: health{2}}) {
		t.Fatalf("the window reported %+v, want the mirrored entity removed and its successor added", got)
	}
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	if got := o.collect(); len(got) != 0 {
		t.Fatalf("the next window reported %+v", got)
	}
}

// TestRowsReaddedInTheWindowAreYieldedOnce removes and adds back, within one
// window and while the store lists its changes, a row added in the window
// and a row it began with: each is yielded once, with what it holds last.
func TestRowsReaddedInTheWindowAreYieldedOnce(t *testing.T) {
	o := spawnObserved(t)
	for range 100 {
		o.store.Add(o.world.Spawn(), health{})
	}
	kept := o.world.Spawn()
	o.store.Add(kept, health{Points: 1})
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	added := o.world.Spawn()
	for points := range int32(3) {
		o.store.Add(added, health{Points: points})
		o.store.Remove(added)
		o.store.Remove(kept)
		o.store.Add(kept, health{Points: points + 2})
	}
	o.store.Add(added, health{Points: 5})
	if o.store.store.changedEntitiesOverflowed {
		t.Fatal("two entities changed stopped the listing")
	}
	yields := map[Entity]int{}
	for e := range o.deltas.All() {
		yields[e]++
	}
	got := o.collect()
	if len(yields) != 2 || yields[added] != 1 || yields[kept] != 1 {
		t.Fatalf("the window yielded %v, want each of two entities once", yields)
	}
	if got[added] != (observedDelta[health]{Delta: DeltaAdded, Value: health{5}}) || got[kept] != (observedDelta[health]{Delta: DeltaChanged, Value: health{4}, Previous: health{1}}) {
		t.Fatalf("the window reported %+v", got)
	}
}

func TestDeltasAndResetAllocateNothingOnceGrown(t *testing.T) {
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
		for range o.deltas.All() {
		}
		ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	}
	window()
	if got := testing.AllocsPerRun(20, window); got != 0 {
		t.Fatalf("a window of writes, deltas and a reset allocated %v times", got)
	}
	listed := func() {
		for i := 0; i < len(entities); i += 20 {
			o.store.Write(entities[i]).Points++
			o.store.Remove(entities[i+1])
			o.store.Add(entities[i+1], health{Points: 1})
		}
		if o.store.store.changedEntitiesOverflowed {
			t.Fatal("a tenth of the rows changed stopped the listing")
		}
		for range o.deltas.All() {
		}
		ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	}
	listed()
	if got := testing.AllocsPerRun(20, listed); got != 0 {
		t.Fatalf("a window of listed changes, deltas and a reset allocated %v times", got)
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

// TestWorldWithoutResetObservesNothing goes down every write path of a world
// whose store has an observer but which is never reset: the store keeps no
// shadow, state or tail, reports nothing, and a despawned index is reused at
// once.
func TestWorldWithoutResetObservesNothing(t *testing.T) {
	var w World[game]
	var healths Store[game, health]
	var deltas DeltaQuery1[game, health]
	var query Query1[game, *health]
	var commands CommandBuffer[game, aiCommands]
	var sink CommandSink[game, aiCommands]
	var healthID ComponentID
	spawn(t, func(c *kernel.Configurator) {
		healthID = RegisterComponent[game, health](c, "health")
		RegisterCommandBuffer[game, aiCommands](c)
		w = declare[World[game]](c)
		healths = declare[Store[game, health]](c)
		deltas = declare[DeltaQuery1[game, health]](c)
		query = declare[Query1[game, *health]](c)
		commands = declare[CommandBuffer[game, aiCommands]](c)
		sink = declare[CommandSink[game, aiCommands]](c)
	})
	var mask game
	maskSet(&mask, healthID)
	a := w.Spawn()
	healths.Add(a, health{Points: 1})
	b := w.SpawnRaw(mask, []byte{2, 0, 0, 0})
	for _, row := range query.All() {
		row.Get().Points++
	}
	healths.Write(a).Points++
	w.Write[health](b).Points++
	w.WriteRaw(a, healthID)[0]++
	w.AddRaw(b, healthID, []byte{5, 0, 0, 0})
	healths.Remove(a)
	healths.Add(a, health{Points: 6})
	spawned := commands.Reserve()
	commands.Spawn(spawned)
	commands.Add(spawned, health{Points: 7})
	commands.Set(a, health{Points: 8})
	commands.Despawn(b)
	if dropped := sink.Commit(); dropped != 0 {
		t.Fatalf("the commit dropped %d records", dropped)
	}
	s := healths.store
	for range deltas.All() {
		t.Fatal("a world never reset reported a change")
	}
	if s.observed() || s.rowStates.Len() != 0 || len(s.shadowChunks) != 0 || s.owners.Len() != s.len() || s.len() != 2 {
		t.Fatalf("a store never reset keeps %d states, %d shadow chunks and %d rows for %d live", s.rowStates.Len(), len(s.shadowChunks), s.owners.Len(), s.len())
	}
	if *healths.Get(a) != (health{Points: 8}) || *healths.Get(spawned) != (health{Points: 7}) {
		t.Fatal("a write was lost in a world never reset")
	}
	if c := w.Spawn(); c.index() != b.index() || c.generation() == b.generation() {
		t.Fatalf("Spawn answered %#x, want b's index reused at once", uint64(c))
	}
}

func TestFirstResetReportsEveryRowAsAdded(t *testing.T) {
	o := spawnUnobserved(t)
	a := o.world.Spawn()
	o.store.Add(a, health{Points: 7})
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	if got := o.collect()[a]; got != (observedDelta[health]{Delta: DeltaAdded, Value: health{7}}) {
		t.Fatalf("the first window reported %+v for a row present before it", got)
	}
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
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
			take(o.world.world.entities.reserve()) // abandoned: its index is lost
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

// termModel is the reference for one term: what each entity held at the
// last reset and holds now, and the last value of each removed since.
type termModel[T comparable] struct {
	atReset, now, last map[Entity]T
}

func newTermModel[T comparable]() *termModel[T] {
	return &termModel[T]{atReset: map[Entity]T{}, now: map[Entity]T{}, last: map[Entity]T{}}
}

func (m *termModel[T]) remove(e Entity) {
	if value, ok := m.now[e]; ok {
		m.last[e] = value
		delete(m.now, e)
	}
}

func (m *termModel[T]) reset() {
	m.atReset = map[Entity]T{}
	for e, value := range m.now {
		m.atReset[e] = value
	}
	m.last = map[Entity]T{}
}

// delta answers what a delta query yields for e's term, or false if e
// neither has nor had it this window.
func (m *termModel[T]) delta(e Entity) (observedDelta[T], bool) {
	previous, existed := m.atReset[e]
	value, exists := m.now[e]
	switch {
	case exists && !existed:
		return observedDelta[T]{Delta: DeltaAdded, Value: value}, true
	case existed && !exists:
		return observedDelta[T]{Delta: DeltaRemoved, Value: m.last[e], Previous: previous}, true
	case exists && value != previous:
		return observedDelta[T]{Delta: DeltaChanged, Value: value, Previous: previous}, true
	case exists:
		return observedDelta[T]{Delta: DeltaNone, Value: value, Previous: previous}, true
	}
	return observedDelta[T]{}, false
}

// deltaRow is one entity's row of the model test's three-term query.
type deltaRow struct {
	health   observedDelta[health]
	position observedDelta[position]
	frozen   Delta
}

// TestDeltaQueriesMatchAReferenceModel runs random spawns, despawns, adds,
// removes, writes and resets over three components, and checks after every
// step a query of all three, a tag among them, and a query of health
// without frozen, against the reference: every entity that has or had each
// value term, with a delta in one, yielded once.
func TestDeltaQueriesMatchAReferenceModel(t *testing.T) {
	var w World[game]
	var healths Store[game, health]
	var positions Store[game, position]
	var tags Store[game, frozen]
	var all DeltaQuery3[game, health, position, frozen]
	var thawed DeltaQuery2[game, health, Without[frozen]]
	spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, health](c, "health")
		RegisterComponent[game, position](c, "position")
		RegisterComponent[game, frozen](c, "frozen")
		w = declare[World[game]](c)
		healths = declare[Store[game, health]](c)
		positions = declare[Store[game, position]](c)
		tags = declare[Store[game, frozen]](c)
		all = declare[DeltaQuery3[game, health, position, frozen]](c)
		thawed = declare[DeltaQuery2[game, health, Without[frozen]]](c)
	})
	ObservationResetSystem(nil, kernel.NoArg{}, w)
	healthModel := newTermModel[health]()
	positionModel := newTermModel[position]()
	frozenModel := newTermModel[frozen]()
	random := rand.New(rand.NewPCG(11, 13))
	var alive []Entity
	seen := map[Entity]bool{}
	pick := func() Entity { return alive[random.IntN(len(alive))] }
	// listedSteps and overflowedSteps count, per store, the steps whose
	// deltas came from the listing of changed entities and from a walk of
	// every row.
	listedSteps := 0
	overflowedSteps := 0
	for step := range 20000 {
		switch op := random.IntN(100); {
		case op < 12 || len(alive) == 0:
			e := w.Spawn()
			alive = append(alive, e)
			seen[e] = true
		case op < 17:
			i := random.IntN(len(alive))
			w.Despawn(alive[i])
			healthModel.remove(alive[i])
			positionModel.remove(alive[i])
			frozenModel.remove(alive[i])
			alive[i] = alive[len(alive)-1]
			alive = alive[:len(alive)-1]
		case op < 32:
			e := pick()
			value := health{Points: random.Int32N(3)}
			healths.Add(e, value)
			healthModel.now[e] = value
		case op < 40:
			e := pick()
			healths.Remove(e)
			healthModel.remove(e)
		case op < 50:
			e := pick()
			if p := healths.Write(e); p != nil {
				p.Points = random.Int32N(3) // often back to what it was
				healthModel.now[e] = *p
			}
		case op < 62:
			e := pick()
			value := position{X: float32(random.IntN(3))}
			positions.Add(e, value)
			positionModel.now[e] = value
		case op < 70:
			e := pick()
			positions.Remove(e)
			positionModel.remove(e)
		case op < 80:
			e := pick()
			tags.Add(e, frozen{})
			frozenModel.now[e] = frozen{}
		case op < 88:
			e := pick()
			tags.Remove(e)
			frozenModel.remove(e)
		case op < 92:
			ObservationResetSystem(nil, kernel.NoArg{}, w)
			healthModel.reset()
			positionModel.reset()
			frozenModel.reset()
			for e := range seen {
				if !w.Alive(e) {
					delete(seen, e)
				}
			}
		}
		for _, s := range []*storeResource{healths.store, positions.store, tags.store} {
			if s.changedEntitiesOverflowed {
				overflowedSteps++
			} else if len(s.changedEntities) != 0 {
				listedSteps++
			}
		}
		wantAll := map[Entity]deltaRow{}
		wantThawed := map[Entity]observedDelta[health]{}
		for e := range seen {
			h, hasHealth := healthModel.delta(e)
			p, hasPosition := positionModel.delta(e)
			f, hasFrozen := frozenModel.delta(e)
			if hasHealth && hasPosition && hasFrozen && (h.Delta != DeltaNone || p.Delta != DeltaNone || f.Delta != DeltaNone) {
				wantAll[e] = deltaRow{health: h, position: p, frozen: f.Delta}
			}
			if _, frozenNow := frozenModel.now[e]; hasHealth && !frozenNow && h.Delta != DeltaNone {
				wantThawed[e] = h
			}
		}
		gotAll := map[Entity]deltaRow{}
		yielded := 0
		for e, row := range all.All() {
			yielded++
			h, p, _ := row.Get()
			previousHealth, previousPosition, _ := row.Previous()
			healthDelta, positionDelta, frozenDelta := row.Delta()
			gotAll[e] = deltaRow{
				health:   observedDelta[health]{Delta: healthDelta, Value: h, Previous: previousHealth},
				position: observedDelta[position]{Delta: positionDelta, Value: p, Previous: previousPosition},
				frozen:   frozenDelta,
			}
		}
		if yielded != len(wantAll) || len(gotAll) != len(wantAll) {
			t.Fatalf("step %d: the query of all three yielded %d rows for %d entities, want %d", step, yielded, len(gotAll), len(wantAll))
		}
		for e, want := range wantAll {
			if gotAll[e] != want {
				t.Fatalf("step %d: entity %#x yielded %+v, want %+v", step, uint64(e), gotAll[e], want)
			}
		}
		gotThawed := map[Entity]observedDelta[health]{}
		for e, row := range thawed.All() {
			healthDelta, _ := row.Delta()
			h, _ := row.Get()
			previous, _ := row.Previous()
			gotThawed[e] = observedDelta[health]{Delta: healthDelta, Value: h, Previous: previous}
		}
		if len(gotThawed) != len(wantThawed) {
			t.Fatalf("step %d: the query without frozen yielded %d entities, want %d", step, len(gotThawed), len(wantThawed))
		}
		for e, want := range wantThawed {
			if gotThawed[e] != want {
				t.Fatalf("step %d: without frozen, entity %#x yielded %+v, want %+v", step, uint64(e), gotThawed[e], want)
			}
		}
	}
	if listedSteps < 1000 || overflowedSteps < 1000 {
		t.Fatalf("%d steps listed and %d walked every row", listedSteps, overflowedSteps)
	}
}

func TestDeltaQueryCompositionErrors(t *testing.T) {
	expectPanic(t, "delta query terms are values", func() {
		spawn(t, func(c *kernel.Configurator) {
			RegisterComponent[game, health](c, "health")
			declare[DeltaQuery1[game, *health]](c)
		})
	})
	expectPanic(t, "delta query term is optional", func() {
		spawn(t, func(c *kernel.Configurator) {
			RegisterComponent[game, health](c, "health")
			RegisterComponent[game, position](c, "position")
			declare[DeltaQuery2[game, health, Optional[position]]](c)
		})
	})
	expectPanic(t, "With and Without terms only", func() {
		spawn(t, func(c *kernel.Configurator) {
			RegisterComponent[game, health](c, "health")
			declare[DeltaQuery2[game, With[health], Without[health]]](c)
		})
	})
	expectPanic(t, "not registered", func() {
		spawn(t, func(c *kernel.Configurator) {
			declare[DeltaQuery1[game, health]](c)
		})
	})
}

func TestDeltaQueryAllocatesNothing(t *testing.T) {
	var w World[game]
	var healths Store[game, health]
	var positions Store[game, position]
	var both DeltaQuery3[game, health, position, Without[frozen]]
	spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, health](c, "health")
		RegisterComponent[game, position](c, "position")
		RegisterComponent[game, frozen](c, "frozen")
		w = declare[World[game]](c)
		healths = declare[Store[game, health]](c)
		positions = declare[Store[game, position]](c)
		both = declare[DeltaQuery3[game, health, position, Without[frozen]]](c)
	})
	ObservationResetSystem(nil, kernel.NoArg{}, w)
	entities := make([]Entity, 500)
	for i := range entities {
		entities[i] = w.Spawn()
		healths.Add(entities[i], health{})
		positions.Add(entities[i], position{})
	}
	ObservationResetSystem(nil, kernel.NoArg{}, w)
	for i, e := range entities {
		if i%3 == 0 {
			healths.Write(e).Points++
		}
		if i%5 == 0 {
			positions.Remove(e)
		}
	}
	walk := func() {
		for _, row := range both.All() {
			h, p, _ := row.Get()
			benchSum += float32(h.Points) + p.X
		}
	}
	walk()
	if got := testing.AllocsPerRun(20, walk); got != 0 {
		t.Fatalf("a delta query walk allocated %v times", got)
	}
}
