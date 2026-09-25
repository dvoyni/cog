package internal

// StorageAlignment is the offset alignment a storage binding requires. A record
// a draw binds a range of therefore pads up to a multiple of it - a pad, not a
// cap on what a record may hold.
const StorageAlignment = 256

// IndexWidth is how wide one element of an index buffer is. There are exactly
// two, fixed by the platform rather than chosen: WebGPU has no uint8 index
// format, so geometry that was authored at a byte per index is stored at two.
//
// Nothing about it is a fidelity call - an index is exact or it is broken - so
// gfx neither derives nor validates the choice: it carries whatever width the
// caller declared its bytes to be in, and the only thing it can check is that
// the bytes divide by it.
//
// The zero value is IndexUint32, the width that is legal for any mesh, so a
// descriptor built without naming one is wide rather than wrong.
type IndexWidth uint8

const (
	IndexUint32 IndexWidth = iota
	IndexUint16
)

// Bytes reports how many bytes one index of this width occupies.
func (w IndexWidth) Bytes() int {
	if w == IndexUint16 {
		return 2
	}
	return 4
}

func (w IndexWidth) String() string {
	if w == IndexUint16 {
		return "uint16"
	}
	return "uint32"
}
