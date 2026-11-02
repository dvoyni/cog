package ecs

const (
	pageShift = 12
	pageSize  = 1 << pageShift
	pageMask  = pageSize - 1
)

// pages is a table of T addressed by entity index, which may be anywhere in
// the 32-bit range. Its pages are allocated only when an index on them is
// first written, so a world whose entities sit far from 0, such as a mirror's
// local range, costs only the pages it uses. Pages never move.
//
// It is not synchronized. A missing page reads as zero T.
type pages[T any] struct {
	table []*[pageSize]T
}

// at returns the item at index, or nil if its page does not exist.
func (p *pages[T]) at(index uint32) *T {
	page := int(index >> pageShift)
	if page >= len(p.table) || p.table[page] == nil {
		return nil
	}
	return &p.table[page][index&pageMask]
}

// make returns the item at index, allocating its page if it does not exist.
func (p *pages[T]) make(index uint32) *T {
	page := int(index >> pageShift)
	if page >= len(p.table) {
		p.table = append(p.table, make([]*[pageSize]T, page+1-len(p.table))...)
	}
	if p.table[page] == nil {
		p.table[page] = new([pageSize]T)
	}
	return &p.table[page][index&pageMask]
}
