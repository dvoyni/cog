package types

import "github.com/dvoyni/cog/bundles/ecs"

// The open-addressed map from one Entity to its dense solver row. It is
// pairtable.go's sibling and not the same table: a Joint's party is looked up
// by one Entity, where a Contact is keyed by two.

// entityCell is one cell. A cell naming NoEntity is empty, which a live Entity
// never is: generations start at 1.
type entityCell struct {
	entity ecs.Entity
	at     int32
}

// entityTableMin is where a fresh table starts. It doubles whenever it is half
// full, and never shrinks on its own.
const entityTableMin = 64

// entityTable is the open-addressed map from an Entity to its dense Body row,
// the same shape as the Contact list's pair map and for the same reason: a hash
// over the whole gather ate the win once, so this one is built only when the
// scene has Joints and is probed once per jointed party per tick rather than
// once per party per iteration.
type entityTable struct {
	cells []entityCell
	used  int
}

// reset empties the table, keeping the memory it has grown.
func (t *entityTable) reset() {
	for i := range t.cells {
		t.cells[i] = entityCell{}
	}
	t.used = 0
}

func (t *entityTable) lookup(e ecs.Entity) (int32, bool) {
	if len(t.cells) == 0 {
		return 0, false
	}
	mask := uint64(len(t.cells) - 1)
	for at := hashEntity(e) & mask; ; at = (at + 1) & mask {
		cell := &t.cells[at]
		switch {
		case cell.entity == ecs.NoEntity:
			return 0, false
		case cell.entity == e:
			return cell.at, true
		}
	}
}

func (t *entityTable) put(e ecs.Entity, at int32) {
	if t.used*2 >= len(t.cells) {
		t.grow()
	}
	mask := uint64(len(t.cells) - 1)
	for i := hashEntity(e) & mask; ; i = (i + 1) & mask {
		cell := &t.cells[i]
		if cell.entity == ecs.NoEntity {
			*cell = entityCell{entity: e, at: at}
			t.used++
			return
		}
		if cell.entity == e {
			cell.at = at
			return
		}
	}
}

func (t *entityTable) grow() {
	size := len(t.cells) * 2
	if size < entityTableMin {
		size = entityTableMin
	}
	old := t.cells
	t.cells = make([]entityCell, size)
	t.used = 0
	for i := range old {
		if old[i].entity != ecs.NoEntity {
			t.put(old[i].entity, old[i].at)
		}
	}
}

// hashEntity is splitmix's finaliser over one Entity id, for the same reason
// hashPair mixes: Entity ids are small dense integers and would cluster.
func hashEntity(e ecs.Entity) uint64 {
	x := uint64(e) * 0x9E3779B97F4A7C15
	x ^= x >> 29
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 32
	return x
}
