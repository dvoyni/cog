package ecs

import (
	"maps"
	"math/rand/v2"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

// queryWorld spawns a world of n entities, each with a position of X i, a
// velocity of X 1 on every even one, and frozen on every multiple of three.
func queryWorld(t *testing.T, n int, declareQueries func(c *kernel.Configurator)) (*harness, []Entity) {
	var w World[game]
	var positions Store[game, position]
	var velocities Store[game, velocity]
	var tags Store[game, frozen]
	h := spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, position](c)
		RegisterComponent[game, velocity](c)
		RegisterComponent[game, frozen](c)
		w = declare[World[game]](c)
		positions = declare[Store[game, position]](c)
		velocities = declare[Store[game, velocity]](c)
		tags = declare[Store[game, frozen]](c)
		declareQueries(c)
	})
	entities := make([]Entity, n)
	for i := range entities {
		e := w.Spawn()
		entities[i] = e
		positions.Add(e, position{X: float32(i)})
		if i%2 == 0 {
			velocities.Add(e, velocity{X: 1})
		}
		if i%3 == 0 {
			tags.Add(e, frozen{})
		}
	}
	return h, entities
}

func TestQueryWritesThroughPointersAndCopiesValues(t *testing.T) {
	var move Query2[game, *position, velocity]
	var read Query1[game, position]
	queryWorld(t, 10, func(c *kernel.Configurator) {
		move = declare[Query2[game, *position, velocity]](c)
		read = declare[Query1[game, position]](c)
	})
	visits := 0
	for _, row := range move.All() {
		pos, vel := row.Get()
		pos.Y += vel.X
		vel.X = 100 // a copy: the store keeps 1
		visits++
	}
	if visits != 5 {
		t.Fatalf("the query visited %d entities, want the 5 with a velocity", visits)
	}
	for _, row := range move.All() {
		if _, vel := row.Get(); vel.X != 1 {
			t.Fatal("writing a value term's copy changed the store")
		}
	}
	for e, row := range read.All() {
		pos := row.Get()
		want := float32(0)
		if int(pos.X)%2 == 0 {
			want = 1
		}
		if pos.Y != want {
			t.Fatalf("entity %#x holds Y %v, want %v", uint64(e), pos.Y, want)
		}
	}
}

func TestQueryFilters(t *testing.T) {
	var with Query2[game, position, With[frozen]]
	var without Query2[game, position, Without[frozen]]
	queryWorld(t, 12, func(c *kernel.Configurator) {
		with = declare[Query2[game, position, With[frozen]]](c)
		without = declare[Query2[game, position, Without[frozen]]](c)
	})
	withCount := 0
	for _, row := range with.All() {
		if pos, _ := row.Get(); int(pos.X)%3 != 0 {
			t.Fatalf("With[frozen] yielded X %v", pos.X)
		}
		withCount++
	}
	withoutCount := 0
	for _, row := range without.All() {
		if pos, _ := row.Get(); int(pos.X)%3 == 0 {
			t.Fatalf("Without[frozen] yielded X %v", pos.X)
		}
		withoutCount++
	}
	if withCount != 4 || withoutCount != 8 {
		t.Fatalf("With visited %d and Without %d, want 4 and 8", withCount, withoutCount)
	}
}

// TestQueryMatchesAReferenceAcrossChunks walks stores spread over several
// chunks, each filled in its own shuffled order, with each store in turn the
// smallest, a tag's among them, and checks what the walks yield and mark
// against a pass over every entity.
func TestQueryMatchesAReferenceAcrossChunks(t *testing.T) {
	for seed := range uint64(6) {
		var w World[game]
		var positions Store[game, position]
		var velocities Store[game, velocity]
		var tags Store[game, frozen]
		var changes Changes[game, position]
		var move Query3[game, *position, velocity, Without[frozen]]
		var still Query2[game, *position, With[frozen]]
		spawn(t, func(c *kernel.Configurator) {
			RegisterComponent[game, position](c)
			RegisterComponent[game, velocity](c)
			RegisterComponent[game, frozen](c)
			w = declare[World[game]](c)
			positions = declare[Store[game, position]](c)
			velocities = declare[Store[game, velocity]](c)
			tags = declare[Store[game, frozen]](c)
			changes = declare[Changes[game, position]](c)
			move = declare[Query3[game, *position, velocity, Without[frozen]]](c)
			still = declare[Query2[game, *position, With[frozen]]](c)
		})
		ObservationReset(nil, kernel.NoArg{}, w)
		random := rand.New(rand.NewPCG(seed, 7))
		entities := make([]Entity, 600)
		for i := range entities {
			entities[i] = w.Spawn()
		}
		percents := [3][3]int{{90, 30, 60}, {30, 90, 60}, {60, 60, 10}}[seed%3]
		for _, i := range random.Perm(len(entities)) {
			if random.IntN(100) < percents[0] {
				positions.Add(entities[i], position{X: float32(i)})
			}
		}
		for _, i := range random.Perm(len(entities)) {
			if random.IntN(100) < percents[1] {
				velocities.Add(entities[i], velocity{X: float32(i % 7)})
			}
		}
		for _, i := range random.Perm(len(entities)) {
			if random.IntN(100) < percents[2] {
				tags.Add(entities[i], frozen{})
			}
		}
		ObservationReset(nil, kernel.NoArg{}, w)
		moved := map[Entity]bool{}
		for e, row := range move.All() {
			pos, vel, _ := row.Get()
			if moved[e] || pos != positions.Get(e) || vel != *velocities.Get(e) {
				t.Fatalf("seed %d: move yielded entity %#x twice, or not its own terms", seed, uint64(e))
			}
			moved[e] = true
			pos.Y++
		}
		stilled := map[Entity]bool{}
		for e, row := range still.All() {
			pos, _ := row.Get()
			if stilled[e] || pos != positions.Get(e) {
				t.Fatalf("seed %d: still yielded entity %#x twice, or not its own position", seed, uint64(e))
			}
			stilled[e] = true
			pos.Y++
		}
		for _, e := range entities {
			if moved[e] != (positions.Has(e) && velocities.Has(e) && !tags.Has(e)) ||
				stilled[e] != (positions.Has(e) && tags.Has(e)) {
				t.Fatalf("seed %d: entity %#x moved %v and stilled %v, against its components", seed, uint64(e), moved[e], stilled[e])
			}
		}
		marked := map[Entity]bool{}
		for e := range changes.All() {
			marked[e] = true
		}
		maps.Copy(stilled, moved)
		markedRows := 0 // a row marked but unchanged is not a change, so count marks
		for row := range positions.store.len() {
			if *positions.store.rowStates.At(row) == rowChanged {
				markedRows++
			}
		}
		if !maps.Equal(marked, stilled) || markedRows != len(stilled) {
			t.Fatalf("seed %d: %d positions changed and %d marked, want the %d yielded", seed, len(marked), markedRows, len(stilled))
		}
	}
}

func TestQueryRemovingTheCurrentEntityIsSafe(t *testing.T) {
	var walk Query1[game, velocity]
	var velocities Store[game, velocity]
	queryWorld(t, 600, func(c *kernel.Configurator) {
		walk = declare[Query1[game, velocity]](c)
		velocities = declare[Store[game, velocity]](c)
	})
	seen := map[Entity]bool{}
	for e := range walk.All() {
		if seen[e] {
			t.Fatalf("entity %#x visited twice", uint64(e))
		}
		seen[e] = true
		velocities.Remove(e)
	}
	if len(seen) != 300 || velocities.store.len() != 0 {
		t.Fatalf("visited %d entities and left %d rows, want 300 and 0", len(seen), velocities.store.len())
	}
}

func TestQueryStopsOnBreak(t *testing.T) {
	var walk Query1[game, position]
	queryWorld(t, 10, func(c *kernel.Configurator) { walk = declare[Query1[game, position]](c) })
	visits := 0
	for range walk.All() {
		if visits++; visits == 3 {
			break
		}
	}
	if visits != 3 {
		t.Fatalf("the query went on to %d visits after a break", visits)
	}
}

func TestQueryAllocatesNothing(t *testing.T) {
	var move Query3[game, *position, velocity, Without[frozen]]
	queryWorld(t, 1000, func(c *kernel.Configurator) {
		move = declare[Query3[game, *position, velocity, Without[frozen]]](c)
	})
	got := testing.AllocsPerRun(100, func() {
		for _, row := range move.All() {
			pos, vel, _ := row.Get()
			pos.X += vel.X
		}
	})
	if got != 0 {
		t.Fatalf("walking a query allocated %v times", got)
	}
}

func TestQueryCompositionErrors(t *testing.T) {
	type unregistered struct{ Value int32 }
	expectPanic(t, "not registered", func() {
		spawn(t, func(c *kernel.Configurator) { declare[Query1[game, unregistered]](c) })
	})
	expectPanic(t, "Without terms only", func() {
		spawn(t, func(c *kernel.Configurator) {
			RegisterComponent[game, frozen](c)
			declare[Query1[game, Without[frozen]]](c)
		})
	})
}
