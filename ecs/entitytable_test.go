package ecs

import (
	"math/rand/v2"
	"slices"
	"sync"
	"testing"
)

// entityModel is the entity table's rules written plainly: a reservation
// takes its entity off the free list at once, and the end of a window walks
// the reservations it remembers.
type entityModel struct {
	firstIndex, lastIndex uint32
	liveGenerations       map[uint32]uint32
	lastGenerations       map[uint32]uint32
	freeEntities          []Entity
	nextFreshIndex        int64
	observing             bool
	despawned             []uint32
	// reservedFree and reservedFresh are the reservations since the last
	// settle, in the order taken.
	reservedFree, reservedFresh []Entity
}

func newEntityModel(first, last uint32) *entityModel {
	return &entityModel{
		firstIndex:      first,
		lastIndex:       last,
		liveGenerations: map[uint32]uint32{},
		lastGenerations: map[uint32]uint32{},
		nextFreshIndex:  int64(first),
	}
}

func (m *entityModel) alive(e Entity) bool {
	return e.generation() != 0 && m.liveGenerations[e.index()] == e.generation()
}

func (m *entityModel) reserve() Entity {
	if n := len(m.freeEntities); n > 0 {
		e := m.freeEntities[n-1]
		m.freeEntities = m.freeEntities[:n-1]
		m.reservedFree = append(m.reservedFree, e)
		return e
	}
	if m.nextFreshIndex > int64(m.lastIndex) {
		return NoEntity
	}
	e := makeEntity(uint32(m.nextFreshIndex), 1)
	m.nextFreshIndex++
	m.reservedFresh = append(m.reservedFresh, e)
	return e
}

// settle forgets the reservations; with reclaim, each never spawned goes
// back on the free list at its next generation: those from the free list
// in the order they lay there, then the fresh ones.
func (m *entityModel) settle(reclaim bool) {
	if reclaim {
		reserved := slices.Clone(m.reservedFree)
		slices.Reverse(reserved)
		for _, e := range append(reserved, m.reservedFresh...) {
			if m.lastGenerations[e.index()] != e.generation() {
				m.lastGenerations[e.index()] = e.generation()
				m.freeEntities = append(m.freeEntities, makeEntity(e.index(), nextGeneration(e.generation())))
			}
		}
	}
	m.reservedFree = nil
	m.reservedFresh = nil
}

func (m *entityModel) setAlive(e Entity) {
	m.liveGenerations[e.index()] = e.generation()
	m.lastGenerations[e.index()] = e.generation()
}

func (m *entityModel) spawn() Entity {
	if !m.observing {
		m.settle(false)
	}
	e := m.reserve()
	if e != NoEntity {
		m.setAlive(e)
	}
	return e
}

func (m *entityModel) spawnReserved(e Entity) bool {
	if m.liveGenerations[e.index()] != 0 || e.generation() != nextGeneration(m.lastGenerations[e.index()]) {
		return false
	}
	m.setAlive(e)
	return true
}

func (m *entityModel) place(e Entity) bool {
	if e.generation() == 0 || (e.index() >= m.firstIndex && e.index() <= m.lastIndex) || m.liveGenerations[e.index()] != 0 {
		return false
	}
	m.setAlive(e)
	return true
}

func (m *entityModel) free(index uint32) {
	if index >= m.firstIndex && index <= m.lastIndex {
		m.freeEntities = append(m.freeEntities, makeEntity(index, nextGeneration(m.lastGenerations[index])))
	}
}

func (m *entityModel) despawn(e Entity) {
	m.liveGenerations[e.index()] = 0
	if m.observing {
		m.despawned = append(m.despawned, e.index())
		return
	}
	m.settle(false)
	m.free(e.index())
}

func (m *entityModel) endWindow() bool {
	if !m.observing {
		m.settle(false)
		m.observing = true
		return true
	}
	m.settle(true)
	for _, index := range m.despawned {
		m.free(index)
	}
	m.despawned = nil
	return false
}

func TestEntityTableMatchesTheModel(t *testing.T) {
	for seed := range uint64(20) {
		const first, last = 10, 60
		var table entityTable[game]
		table.init(first, last, 8)
		model := newEntityModel(first, last)
		random := rand.New(rand.NewPCG(seed, 7))
		var seen, reserved, alive []Entity
		for step := range 3000 {
			switch op := random.IntN(100); {
			case op < 15:
				got, want := table.reserve(), model.reserve()
				if got != want {
					t.Fatalf("seed %d step %d: reserve answered %#x, want %#x", seed, step, uint64(got), uint64(want))
				}
				seen = append(seen, got)
				reserved = append(reserved, got)
			case op < 45:
				got, want := table.spawn(), model.spawn()
				if got != want {
					t.Fatalf("seed %d step %d: spawn answered %#x, want %#x", seed, step, uint64(got), uint64(want))
				}
				seen = append(seen, got)
				alive = append(alive, got)
			case op < 55 && len(reserved) > 0:
				e := reserved[random.IntN(len(reserved))] // may be stale, spawned, or NoEntity
				if got, want := table.spawnReserved(e), model.spawnReserved(e); got != want {
					t.Fatalf("seed %d step %d: spawnReserved(%#x) answered %v, want %v", seed, step, uint64(e), got, want)
				}
				alive = append(alive, e)
			case op < 60:
				e := makeEntity(uint32(100+random.IntN(4)), uint32(random.IntN(3)))
				if got, want := table.place(e) == nil, model.place(e); got != want {
					t.Fatalf("seed %d step %d: place(%#x) succeeded %v, want %v", seed, step, uint64(e), got, want)
				}
				seen = append(seen, e)
				alive = append(alive, e)
			case op < 90 && len(alive) > 0:
				i := random.IntN(len(alive))
				e := alive[i]
				alive[i] = alive[len(alive)-1]
				alive = alive[:len(alive)-1]
				if model.alive(e) {
					table.despawn(e)
					model.despawn(e)
				}
			case op >= 96:
				if got, want := table.endWindow(), model.endWindow(); got != want {
					t.Fatalf("seed %d step %d: endWindow answered started %v, want %v", seed, step, got, want)
				}
				reserved = reserved[:0]
			}
			for _, e := range seen {
				if table.alive(e) != model.alive(e) {
					t.Fatalf("seed %d step %d: alive(%#x) is %v, want %v", seed, step, uint64(e), table.alive(e), model.alive(e))
				}
			}
		}
	}
}

func TestEntityTableReusesTheLastFreedFirst(t *testing.T) {
	var table entityTable[game]
	table.init(0, 100, 0)
	a := table.spawn()
	b := table.spawn()
	table.despawn(a)
	table.despawn(b)
	if got := table.spawn(); got != makeEntity(b.index(), 2) {
		t.Fatalf("spawn answered %#x, want b's index, freed last, at generation 2", uint64(got))
	}
	if got := table.spawn(); got != makeEntity(a.index(), 2) {
		t.Fatalf("spawn answered %#x, want a's index at generation 2", uint64(got))
	}
}

func TestEntityTableMasks(t *testing.T) {
	var table entityTable[game]
	table.init(0, 100, 0)
	e := table.spawn()
	table.setComponent(e, 3)
	table.setComponent(e, 100)
	table.unsetComponent(e, 3)
	if mask, ok := table.mask(e); !ok || maskHas(&mask, 3) || !maskHas(&mask, 100) {
		t.Fatalf("mask answered %b, %v after setting 3 and 100 and unsetting 3", mask, ok)
	}
	if mask, ok := table.despawn(e); !ok || !maskHas(&mask, 100) {
		t.Fatalf("despawn answered %b, %v, want the mask e had", mask, ok)
	}
	if _, ok := table.mask(e); ok {
		t.Fatal("mask answered a despawned entity")
	}
	if _, ok := table.despawn(e); ok {
		t.Fatal("despawn ended e twice")
	}
	if reused := table.spawn(); table.slots.at(reused.index()).componentMask != (game{}) {
		t.Fatal("a reused index kept its last entity's mask")
	}
}

func TestConcurrentReservesAreDistinct(t *testing.T) {
	var table entityTable[game]
	table.init(0, 1<<20, 0)
	spawned := make([]Entity, 500)
	for i := range spawned {
		spawned[i] = table.spawn()
	}
	for _, e := range spawned {
		table.despawn(e)
	}
	table.spawn() // settles, leaving a free list to reserve from first
	const reservers, each = 8, 1000
	got := make([][]Entity, reservers)
	var wait sync.WaitGroup
	for g := range reservers {
		wait.Go(func() {
			for range each {
				got[g] = append(got[g], table.reserve())
			}
		})
	}
	wait.Wait()
	seen := map[Entity]bool{}
	for _, entities := range got {
		for _, e := range entities {
			if e == NoEntity || seen[e] {
				t.Fatalf("reserve answered %#x twice or NoEntity", uint64(e))
			}
			seen[e] = true
		}
	}
}
