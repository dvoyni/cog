package ecs

import (
	"slices"
	"unsafe"
)

const (
	indexPageShift = 12
	indexPageSize  = 1 << indexPageShift
	indexPageMask  = indexPageSize - 1
)

// indexTable is a table of T addressed by entity index, which may be anywhere
// in the 32-bit range. It is split into pages of indexPageSize entries, each
// allocated only when an index on it is first written, so a world whose
// entities sit far from 0, such as a mirror's local range, costs only the
// pages it uses. Pages never move.
//
// It is not synchronized. An entry on a missing page reads as zero T.
type indexTable[T any] struct {
	pages []*[indexPageSize]T
}

// at returns the entry at index, or nil if its page does not exist.
func (t *indexTable[T]) at(index uint32) *T {
	page := int(index >> indexPageShift)
	if page >= len(t.pages) || t.pages[page] == nil {
		return nil
	}
	return &t.pages[page][index&indexPageMask]
}

// atOrCreate returns the entry at index, allocating its page if it does not
// exist.
func (t *indexTable[T]) atOrCreate(index uint32) *T {
	page := int(index >> indexPageShift)
	if page >= len(t.pages) {
		t.pages = append(t.pages, make([]*[indexPageSize]T, page+1-len(t.pages))...)
	}
	if t.pages[page] == nil {
		t.pages[page] = new([indexPageSize]T)
	}
	return &t.pages[page][index&indexPageMask]
}

// shrink releases the pages whose entries are all zero, which read the same
// once their page is gone. It scans every page.
func (t *indexTable[T]) shrink() {
	used := 0
	for i, page := range t.pages {
		if page == nil {
			continue
		}
		raw := unsafe.Slice((*byte)(unsafe.Pointer(page)), unsafe.Sizeof(*page))
		if slices.ContainsFunc(raw, func(b byte) bool { return b != 0 }) {
			used = i + 1
		} else {
			t.pages[i] = nil
		}
	}
	t.pages = slices.Clone(t.pages[:used])
}
