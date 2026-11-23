package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// loadedMesh is a mesh file as the renderer loaded it at its first draw:
// whether it was tried; the buffers of its vertices and its indices, none
// for no indices, and the mesh over them, made through render, none for a
// file that failed to load or whose mesh render refused, which draws nothing
// until its path is unloaded; its bounds; and whether its layout is
// canvas.Vertex's, which canvas's own mesh vertex stage draws.
type loadedMesh struct {
	tried          bool
	vertexBuffer   render.Buffer
	indexBuffer    render.Buffer
	mesh           render.Mesh
	bounds         m.Box3
	standardLayout bool
}

// unload gives back through inventory what l made, which the frame's draws
// still draw, and forgets it was tried, so its next draw loads it again.
func (l *loadedMesh) unload(inventory render.Inventory) {
	if l.mesh != (render.Mesh{}) {
		inventory.ReleaseMesh(l.mesh)
	}
	if l.vertexBuffer != (render.Buffer{}) {
		inventory.ReleaseBuffer(l.vertexBuffer)
	}
	if l.indexBuffer != (render.Buffer{}) {
		inventory.ReleaseBuffer(l.indexBuffer)
	}
	*l = loadedMesh{}
}
