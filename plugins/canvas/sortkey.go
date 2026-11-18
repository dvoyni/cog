package canvas

import "math"

// OrderOf answers the order that sorts entries as y sorts, for y-sorting
// with no scale to choose: Order: canvas.OrderOf(position.Y). Each float
// maps onto an int32 of its own, in the same order, so OrderOf(y)-1 sorts
// just under y, as a unit's shadow does. −0 maps as +0, and every NaN after
// every number, +Inf included. It keeps the order, not the distance:
// OrderOf(1) is 1065353216, so y-sorted entries and hand-stacked ones belong
// in different layers.
func OrderOf(y float32) int32 {
	if math.IsNaN(float64(y)) {
		return math.MaxInt32
	}
	yBits := math.Float32bits(y)
	// −0's magnitude is +0's, so both map to 0.
	if yBits&signBit != 0 {
		return -int32(yBits &^ signBit)
	}
	return int32(yBits)
}

// signBit is the sign bit of a 32-bit number: a float32's, the rest of its
// yBits its magnitude, and an int32's, flipped in each half of a sortKey.
const signBit = 1 << 31

// sortKey is an entry's layer and order as one 64-bit number, ordered as
// the two are: the layer in the high half and the order in the low, each
// with its sign bit flipped, so an unsigned comparison orders them as
// signed ones.
type sortKey uint64

// makeSortKey answers the sort key of an entry in layer, of order.
func makeSortKey(layer, order int32) sortKey {
	return sortKey(uint64(uint32(layer)^signBit)<<32 | uint64(uint32(order)^signBit))
}
