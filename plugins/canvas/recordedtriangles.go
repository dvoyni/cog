package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/libs/meshfile"
	"github.com/dvoyni/cog/plugins/render"
)

// recordedTriangles is a mesh DrawTriangles recorded for one frame: its
// options, its vertices' layout, whether that is canvas.Vertex's, which
// canvas's own mesh vertex stage draws, its topology and bounds, and its
// vertex bytes and its index bytes, of indexFormat, none for no indices,
// each copied into the arena beside it, its draw list's or the frame's.
type recordedTriangles struct {
	options        DrawTrianglesOptions
	layout         meshfile.Layout
	standardLayout bool
	topology       render.PrimitiveTopology
	bounds         m.Box3
	vertices       []byte
	indices        []byte
	indexFormat    render.IndexFormat
}

// batchKey answers what the triangles, the frame's at index, share with the
// entries they batch with, their image as drawn image, as a sprite's
// batchKey answers it, and their own index, so they draw alone.
func (t *recordedTriangles) batchKey(image *drawnImage, defaultFilter Filter, index uint32) batchKey {
	key := makeImageBatchKey(t.options.Material, t.options.Image, image, t.options.Clip, t.options.Filter, defaultFilter)
	key.triangles = index + 1
	return key
}
