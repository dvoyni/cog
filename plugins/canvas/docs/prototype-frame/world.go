package main

import (
	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/canvas"
	"prototype.invalid/canvasframe/kernel"
	"prototype.invalid/canvasframe/meshfile"
	"prototype.invalid/canvasframe/mx"
)

var (
	lake  = canvas.MeshFromFile("meshes/lake.mesh")
	grass = canvas.MeshFromFile("meshes/grass.mesh")
	// rock was never put into the memory mount: its draw is reported once
	// and draws nothing.
	rock  = canvas.MeshFromFile("meshes/rock.mesh")
	water = canvas.FromFile("water.png", canvas.ImageOptions{Wrap: canvas.WrapBoth})
	blade = canvas.FromFile("grass.png", canvas.ImageOptions{Pivot: canvas.PivotBottom})
)

// drawWorld sets this frame's cameras and records the ground: the tilemap,
// the lake, the grass and the hero's trail.
func drawWorld(list canvas.DrawList, game kernel.Read[*Game], targets kernel.Read[canvas.Targets]) {
	g := game.Get()
	for _, camera := range g.cameras {
		list.SetCamera(camera)
	}

	// An entry is drawn by every camera whose layers hold it, so the tilemap
	// records the tiles of every view it is seen in, once.
	seen := m.Rect{}
	for _, camera := range g.cameras {
		if camera.Layers[0].From != layerGround {
			continue
		}
		visible, drawn := camera.VisibleRect(targets.Get())
		if !drawn {
			visible = g.tiles.Bounds()
		}
		seen = union(seen, visible)
	}
	g.tiles.Draw(list, seen, layerGround)

	list.DrawMesh(canvas.Mesh{File: lake, Image: water, Transform: mx.Translation2x3(420, 300), Layer: layerGround, Order: 1})
	list.DrawMesh(canvas.Mesh{File: rock, Transform: mx.Translation2x3(500, 200), Layer: layerGround, Order: 1})

	// Three tufts of one mesh file, in one bulk call: one instanced draw.
	tufts := []canvas.Mesh{
		{File: grass, Image: blade, Material: g.grass, Transform: mx.Translation2x3(250, 320), Layer: layerGrass},
		{File: grass, Image: blade, Material: g.grass, Transform: mx.Translation2x3(270, 330), Layer: layerGrass},
		{File: grass, Image: blade, Material: g.grass, Transform: mx.TRS2x3(m.Vec2{X: 290, Y: 318}, 0, m.Vec2{X: -1, Y: 1}), Layer: layerGrass},
	}
	list.DrawMeshesWith(tufts, []Wind{{Strength: 3, Phase: 0}, {Strength: 3, Phase: 1}, {Strength: 2, Phase: 2}})

	// The hero's trail, rebuilt every frame: a strip fading to nothing.
	var strip []canvas.Vertex
	for index, point := range g.trail {
		fade := float32(index+1) / float32(len(g.trail))
		color := m.Color{R: 1, G: 1, B: 1, A: fade}
		strip = append(strip, canvas.Vertex{Position: point.Add(m.Vec2{Y: -3}), Color: color}, canvas.Vertex{Position: point.Add(m.Vec2{Y: 3}), Color: color})
	}
	list.DrawTriangles(canvas.DrawTrianglesOptions{Layer: layerUnits, Order: canvas.OrderOf(g.hero.Position.Y) - 2}, canvas.Triangles[canvas.Vertex]{
		Vertices: strip, Topology: meshfile.TriangleStrip,
		Bounds: meshfile.CalculateBounds(strip, canvas.Vertex.Position3),
	})
}

func union(a, b m.Rect) m.Rect {
	if a == (m.Rect{}) {
		return b
	}
	low := m.Vec2{X: min(a.X, b.X), Y: min(a.Y, b.Y)}
	high := m.Vec2{X: max(a.X+a.Width, b.X+b.Width), Y: max(a.Y+a.Height, b.Y+b.Height)}
	return m.Rect{X: low.X, Y: low.Y, Width: high.X - low.X, Height: high.Y - low.Y}
}
