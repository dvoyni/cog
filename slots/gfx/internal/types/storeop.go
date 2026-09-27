package types

import "strconv"

// StoreOp says whether a pass's results survive it.
type StoreOp uint8

const (
	StoreKeep StoreOp = iota
	StoreDiscard
)

// String spells the store op for a debug document.
func (store StoreOp) String() string {
	switch store {
	case StoreKeep:
		return "keep"
	case StoreDiscard:
		return "discard"
	}
	return "unknown(" + strconv.Itoa(int(store)) + ")"
}
