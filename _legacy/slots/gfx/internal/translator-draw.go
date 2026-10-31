package internal

import (
	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"
)

// reportIndexLengthOf returns the report for a malformed index buffer the first
// time that shape is seen, and nothing on the frames after it. The draw is
// dropped either way: the caller returns before emitting anything, so
// report-once-drop-always holds here the way it does for a failed pipeline.
// label is the shader's, as the set's program carries it.
func (t *translator) reportIndexLengthOf(m *types.MeshDescr, label string) error {
	if t.indexLengthSeen(m) {
		return nil
	}
	return types.ErrIndexBufferLength{Shader: label, Length: m.Indices.Size, Width: m.IndexWidth.Bytes()}
}

// indexLengthSeen reports whether a malformed index buffer of this shape was
// reported already, and marks it reported.
func (t *translator) indexLengthSeen(m *types.MeshDescr) bool {
	key := indexLengthKey{length: m.Indices.Size, width: m.IndexWidth}
	if _, seen := t.badIndexLengths[key]; seen {
		return true
	}
	t.badIndexLengths[key] = struct{}{}
	return false
}

// textureViewName renders a declared dimension the way the shader spells it, so
// the report can be matched against the source it names.
func textureViewName(view shader.TextureViewDimension) string {
	if view == shader.TextureView2DArray {
		return "texture_2d_array"
	}
	return "texture_2d"
}

// textureViewNameOfLayers renders what a texture's layer count makes it, which
// is how a supplied texture's dimension is known: more than one layer is an
// array texture and one is flat.
func textureViewNameOfLayers(layers int) string {
	if layers > 1 {
		return "texture_2d_array"
	}
	return "single-layer"
}
