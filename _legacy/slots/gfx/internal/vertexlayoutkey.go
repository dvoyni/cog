package internal

import "github.com/dvoyni/cog/slots/gfx/internal/types"

// vertexTypeBits is how many low bits of a key entry hold the attribute's type;
// the offset sits above them.
const vertexTypeBits = 5

// VertexLayoutKey packs a mesh's vertex layout into a comparable value, one
// offset and type per attribute, so a pipeline cache can key on it.
type VertexLayoutKey [types.MaxVertexAttributes]uint16

// VertexLayoutKeyOf packs layout, and reports false for one no pipeline can be
// built from: too many attributes, an offset outside the stride limit, or a
// type that is not a vertex format.
func VertexLayoutKeyOf(layout []types.VertexAttribute) (VertexLayoutKey, bool) {
	var key VertexLayoutKey
	if len(layout) > len(key) {
		return key, false
	}
	for i := range layout {
		attr := layout[i]
		size := attr.Type.Size()
		if attr.Offset < 0 || attr.Offset+size > types.MaxVertexStride ||
			attr.Type <= types.UnknownVertexType || attr.Type >= types.VertexTypeCount__ {
			return VertexLayoutKey{}, false
		}
		key[i] = uint16(attr.Offset)<<vertexTypeBits | uint16(attr.Type)
	}
	return key, true
}
