package ecs

// Entity is an identity in one world: an index and a generation, 64 bits in
// all. It holds no data itself. Generations start at 1, so no live entity is
// NoEntity.
type Entity uint64

// NoEntity is no entity.
const NoEntity Entity = 0

func makeEntity(index, generation uint32) Entity {
	return Entity(uint64(generation)<<32 | uint64(index))
}

func (e Entity) index() uint32 { return uint32(e) }

func (e Entity) generation() uint32 { return uint32(e >> 32) }

// nextGeneration answers the generation after generation, skipping 0 when it
// wraps.
func nextGeneration(generation uint32) uint32 {
	if generation++; generation == 0 {
		generation++
	}
	return generation
}
