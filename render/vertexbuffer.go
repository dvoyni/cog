package render

import "fmt"

// VertexBuffer is one vertex buffer of a mesh: a range of a buffer, its
// offset where the mesh's first vertex is, and the layout its vertices are
// read in.
type VertexBuffer struct {
	Range  BufferRange
	Layout VertexLayout
}

// check answers why b cannot be drawn from: an offset or a stride no
// multiple of 4, or an attribute at an offset no multiple of its format's
// alignment.
func (b VertexBuffer) check() error {
	if b.Range.Offset%4 != 0 || b.Layout.Stride%4 != 0 {
		return fmt.Errorf("is at offset %d with a stride of %d, where both are multiples of 4", b.Range.Offset, b.Layout.Stride)
	}
	for _, attribute := range b.Layout.Attributes {
		if alignment := attribute.Format.alignment(); attribute.Offset%alignment != 0 {
			return fmt.Errorf("has its attribute at location %d at offset %d, no multiple of %d, its format's alignment", attribute.Location, attribute.Offset, alignment)
		}
	}
	return nil
}
