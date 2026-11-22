package canvas

// AxisTag is the tag of a variable font's axis: its four bytes, such as
// wght's for its weight.
type AxisTag [4]byte

// AxisTagFromName answers the tag of the axis name names, four bytes such as
// "wght". A name of other than four bytes answers the zero tag, which names
// no axis: a Variation of it is ignored, reported once when its set is made.
func AxisTagFromName(name string) AxisTag {
	var tag AxisTag
	if len(name) != len(tag) {
		return tag
	}
	copy(tag[:], name)
	return tag
}
