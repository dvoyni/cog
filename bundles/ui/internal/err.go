package internal

import "strconv"

// ErrLayoutNoSuchElement reports a subtree filter naming an index the tick's
// tree does not have. It can only be raised inside the tick - the tree is
// declared afresh every tick and nothing outside it knows how large it is -
// and it is raised rather than answered with an empty array, because an empty
// array reads as a subtree that laid out nothing.
type ErrLayoutNoSuchElement struct {
	Index, Count int
}

func (e ErrLayoutNoSuchElement) Error() string {
	if e.Count == 0 {
		return "ui: element " + strconv.Itoa(e.Index) +
			" is not in this tick's tree, which declared no elements at all"
	}
	return "ui: element " + strconv.Itoa(e.Index) +
		" is not in this tick's tree, which has " + strconv.Itoa(e.Count) +
		" elements indexed 0 to " + strconv.Itoa(e.Count-1)
}
