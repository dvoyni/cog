// Package store is a throwaway stand-in for an ECS component store living in a
// DIFFERENT package from the loop that ranges over it. Its only job is to let
// the benchmarks in the parent package measure whether a package boundary
// changes range-over-func escape behaviour.
package store

import "iter"

type Entity uint32

type Body struct{ X, Y, VX, VY float32 }

type Store struct {
	ids  []Entity
	data []Body
}

func New(n int) *Store {
	s := &Store{ids: make([]Entity, n), data: make([]Body, n)}
	for i := range n {
		s.ids[i] = Entity(i)
		s.data[i] = Body{X: float32(i), Y: float32(i), VX: 1, VY: 2}
	}
	return s
}

func (s *Store) Len() int { return len(s.ids) }

// All is small enough to inline across the package boundary.
func (s *Store) All() iter.Seq2[Entity, Body] {
	return func(yield func(Entity, Body) bool) {
		for i, id := range s.ids {
			if !yield(id, s.data[i]) {
				return
			}
		}
	}
}

// AllFat stands in for an All that is too expensive for the inliner, so the
// range statement sees an opaque func value rather than a literal it can
// devirtualize. //go:noinline models "over the cost budget" exactly and does not
// depend on guessing the budget.
//
//go:noinline
func (s *Store) AllFat() iter.Seq2[Entity, Body] {
	return func(yield func(Entity, Body) bool) {
		for i, id := range s.ids {
			if !yield(id, s.data[i]) {
				return
			}
		}
	}
}

// ValStore exercises a method on a stored *value* (non-pointer receiver).
type ValStore struct {
	ids  []Entity
	data []Body
}

func NewVal(n int) ValStore {
	s := New(n)
	return ValStore{ids: s.ids, data: s.data}
}

func (s ValStore) All() iter.Seq2[Entity, Body] {
	return func(yield func(Entity, Body) bool) {
		for i, id := range s.ids {
			if !yield(id, s.data[i]) {
				return
			}
		}
	}
}

// AnyStore is the "register to instantiate" mechanism: generic code is reached
// through a non-generic interface whose implementation was instantiated at
// registration time.
type AnyStore interface {
	Count() int
	SumX() float32
}

func (s *Store) Count() int { return len(s.ids) }

func (s *Store) SumX() float32 {
	var sum float32
	for i := range s.data {
		sum += s.data[i].X
	}
	return sum
}
