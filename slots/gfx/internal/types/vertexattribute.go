package types

type VertexLayoutKey [MaxVertexAttributes]uint16

// VertexAttribute describes one attribute of the single interleaved vertex array: its
// byte offset and element type. Attributes bind to shader @location values in the
// order given.
type VertexAttribute struct {
	Offset int
	Type   VertexType
}

func VertexLayoutKeyOf(layout []VertexAttribute) (VertexLayoutKey, bool) {
	var key VertexLayoutKey
	if len(layout) > len(key) {
		return key, false
	}
	for i := range layout {
		attr := layout[i]
		size := attr.Type.Size()
		if attr.Offset < 0 || attr.Offset+size > MaxVertexStride ||
			attr.Type <= UnknownVertexType || attr.Type >= VertexTypeCount__ {
			return VertexLayoutKey{}, false
		}
		key[i] = uint16(attr.Offset)<<vertexTypeBits | uint16(attr.Type)
	}
	return key, true
}
