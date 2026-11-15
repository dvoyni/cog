package render

import (
	"slices"
	"sync"

	"github.com/dvoyni/cog/gfx"
)

// vertexLayoutInterner numbers the vertex layouts of meshes, each list of a
// mesh's buffer layouts once, compared by value, so a pipeline's key holds
// its mesh's layouts as one comparable number. A number is kept for the
// program's life, since the pipelines keyed by it are, and a program draws
// few layouts. Its mutex guards it, taken inside the asset table's, and it
// takes no other.
type vertexLayoutInterner struct {
	mutex sync.Mutex
	// layouts are the lists interned, by number, as gfx's pipelines take
	// them, stepped per vertex.
	layouts [][]gfx.VertexBufferLayout
}

// intern answers the number of the layouts of vertexBuffers, numbering them
// first when no list of the same layouts has one.
func (i *vertexLayoutInterner) intern(vertexBuffers []VertexBuffer) uint32 {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	for number, layouts := range i.layouts {
		if slices.EqualFunc(layouts, vertexBuffers, func(layout gfx.VertexBufferLayout, vertexBuffer VertexBuffer) bool {
			return layout.ArrayStride == vertexBuffer.Layout.Stride && slices.EqualFunc(layout.Attributes, vertexBuffer.Layout.Attributes, func(gfxAttribute gfx.VertexAttribute, attribute VertexAttribute) bool {
				return gfxAttribute == gfx.VertexAttribute{Format: gfx.VertexFormat(attribute.Format), Offset: attribute.Offset, ShaderLocation: attribute.Location}
			})
		}) {
			return uint32(number)
		}
	}
	layouts := make([]gfx.VertexBufferLayout, len(vertexBuffers))
	for index, vertexBuffer := range vertexBuffers {
		attributes := make([]gfx.VertexAttribute, len(vertexBuffer.Layout.Attributes))
		for attributeIndex, attribute := range vertexBuffer.Layout.Attributes {
			attributes[attributeIndex] = gfx.VertexAttribute{Format: gfx.VertexFormat(attribute.Format), Offset: attribute.Offset, ShaderLocation: attribute.Location}
		}
		layouts[index] = gfx.VertexBufferLayout{ArrayStride: vertexBuffer.Layout.Stride, StepMode: gfx.VertexStepModeVertex, Attributes: attributes}
	}
	i.layouts = append(i.layouts, layouts)
	return uint32(len(i.layouts) - 1)
}

// findGfxLayouts answers the layouts numbered number as gfx's pipelines take
// them, stepped per vertex, which the caller does not change.
func (i *vertexLayoutInterner) findGfxLayouts(number uint32) []gfx.VertexBufferLayout {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	return i.layouts[number]
}
