package render

import (
	"slices"
	"sync"

	"github.com/dvoyni/cog/plugins/gfx"
)

// vertexLayoutInterner numbers the lists of vertex buffer layouts draws read,
// a mesh's or the instance buffers set, each list once, compared by value
// with the step its buffers advance by, so a pipeline's key holds them as
// comparable numbers. Zero numbers the empty list, of a draw with no buffers
// of the kind. A number is kept for the program's life, since the pipelines
// keyed by it are, and a program draws few layouts. Its mutex guards it,
// taken inside the asset table's, and it takes no other.
type vertexLayoutInterner struct {
	mutex sync.Mutex
	// layouts are the lists interned, as gfx's pipelines take them, each
	// numbered by its index plus one.
	layouts [][]gfx.VertexBufferLayout
}

// intern answers the number of the layouts of vertexBuffers, stepped by
// stepMode, numbering them first when no list of the same layouts has one;
// zero for none.
func (i *vertexLayoutInterner) intern(vertexBuffers []VertexBuffer, stepMode gfx.VertexStepMode) uint32 {
	if len(vertexBuffers) == 0 {
		return 0
	}
	i.mutex.Lock()
	defer i.mutex.Unlock()
	for index, layouts := range i.layouts {
		if slices.EqualFunc(layouts, vertexBuffers, func(layout gfx.VertexBufferLayout, vertexBuffer VertexBuffer) bool {
			return layout.StepMode == stepMode && layout.ArrayStride == vertexBuffer.Layout.Stride && slices.EqualFunc(layout.Attributes, vertexBuffer.Layout.Attributes, func(gfxAttribute gfx.VertexAttribute, attribute VertexAttribute) bool {
				return gfxAttribute == gfx.VertexAttribute{Format: gfx.VertexFormat(attribute.Format), Offset: attribute.Offset, ShaderLocation: attribute.Location}
			})
		}) {
			return uint32(index + 1)
		}
	}
	layouts := make([]gfx.VertexBufferLayout, len(vertexBuffers))
	for index, vertexBuffer := range vertexBuffers {
		attributes := make([]gfx.VertexAttribute, len(vertexBuffer.Layout.Attributes))
		for attributeIndex, attribute := range vertexBuffer.Layout.Attributes {
			attributes[attributeIndex] = gfx.VertexAttribute{Format: gfx.VertexFormat(attribute.Format), Offset: attribute.Offset, ShaderLocation: attribute.Location}
		}
		layouts[index] = gfx.VertexBufferLayout{ArrayStride: vertexBuffer.Layout.Stride, StepMode: stepMode, Attributes: attributes}
	}
	i.layouts = append(i.layouts, layouts)
	return uint32(len(i.layouts))
}

// findGfxLayouts answers the layouts numbered number as gfx's pipelines take
// them, none for zero, which the caller does not change.
func (i *vertexLayoutInterner) findGfxLayouts(number uint32) []gfx.VertexBufferLayout {
	if number == 0 {
		return nil
	}
	i.mutex.Lock()
	defer i.mutex.Unlock()
	return i.layouts[number-1]
}
