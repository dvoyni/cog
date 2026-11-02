package container

import "math/bits"

// Bits is a growing set of non-negative indexes, one bit each, kept in an
// Array of words. It grows to the highest index set and never shrinks.
//
// It is not synchronized, as Array is not. The zero Bits is empty and ready
// to use.
type Bits struct {
	words Array[uint64]
}

// Set adds i.
func (b *Bits) Set(i int) {
	word := i >> 6
	if word >= b.words.Len() {
		b.words.Resize(word + 1)
	}
	*b.words.At(word) |= 1 << (i & 63)
}

// Unset removes i.
func (b *Bits) Unset(i int) {
	if word := i >> 6; word < b.words.Len() {
		*b.words.At(word) &^= 1 << (i & 63)
	}
}

// Has reports whether i is in the set.
func (b *Bits) Has(i int) bool {
	word := i >> 6
	return word < b.words.Len() && *b.words.At(word)&(1<<(i&63)) != 0
}

// Next answers the lowest index in the set at or above i, or false if there
// is none. A walk over the set is Next from 0, then from each answer plus 1.
func (b *Bits) Next(i int) (int, bool) {
	word := i >> 6
	if word >= b.words.Len() {
		return 0, false
	}
	bitsLeft := *b.words.At(word) >> (i & 63) << (i & 63)
	for bitsLeft == 0 {
		word++
		if word >= b.words.Len() {
			return 0, false
		}
		bitsLeft = *b.words.At(word)
	}
	return word<<6 + bits.TrailingZeros64(bitsLeft), true
}

// Clear empties the set, keeping its capacity.
func (b *Bits) Clear() {
	for c := range b.words.Chunks() {
		clear(b.words.Chunk(c))
	}
}
