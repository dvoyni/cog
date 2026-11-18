package main

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/canvas"
	"prototype.invalid/canvasframe/kernel"
	"prototype.invalid/canvasframe/meshfile"
	"prototype.invalid/canvasframe/platform"
	"prototype.invalid/canvasframe/platform/filesystems"
	"prototype.invalid/canvasframe/render"
)

// loadFonts makes the game's font sets and registers its icons and colours,
// through Fonts' writing side.
func loadFonts(fontsHandle kernel.Write[canvas.Fonts], files kernel.Read[platform.FS], game kernel.Write[*Game]) {
	fonts := canvas.ElevateFonts(fontsHandle, files)
	g := game.Get()
	g.title = fonts.MakeFontSet(canvas.FontSetDescriptor{
		Label:   "title",
		Default: canvas.Font{Path: "fonts/inter-variable.ttf", Variations: [4]canvas.Variation{{Tag: canvas.Tag("wght"), Value: 800}}},
		Languages: []canvas.LanguageFonts{
			{Languages: []string{"ja", "zh-Hans"}, Fonts: []canvas.Font{{Path: "fonts/noto-sans-cjk.ttc", Index: 0, FieldEm: 64}}},
		},
	})
	prompts := canvas.FromFile("ui/prompts.png", canvas.ImageOptions{Sheet: canvas.Grid{Columns: 4, Rows: 4}})
	fonts.RegisterIcon("button-a", canvas.IconDescriptor{Image: prompts, Frame: 0})
	fonts.RegisterIcon("coin", canvas.IconDescriptor{Image: canvas.FromFile("ui/coin.png"), Scale: 1.2})
	fonts.RegisterColor("keyword", m.NewColorSrgb8(255, 200, 80, 255))
	fonts.Preload(g.title, "Hollow Vale")
}

// Dissolve is the dissolve material's argument. Its padding is written out
// as _ fields, so that its layout is the WGSL struct's: edge at offset 16.
type Dissolve struct {
	Amount float32
	_      [3]float32
	Edge   m.Color
}

// Wind is the grass material's argument.
type Wind struct{ Strength, Phase float32 }

// makeMaterials makes the game's 2D materials and the minimap's texture.
func makeMaterials(assets canvas.Assets, game kernel.Write[*Game]) {
	g := game.Get()
	g.dissolve = assets.MakeMaterial(canvas.MaterialDescriptor{
		Label: "dissolve", Shader: "shaders/dissolve.wgsl", FragmentEntryPoint: "dissolve",
		Argument: canvas.ArgumentOf[Dissolve](),
	})
	g.grass = assets.MakeMaterial(canvas.MaterialDescriptor{
		Label: "grass", Shader: "shaders/grass.wgsl", VertexEntryPoint: "grass_vertex", FragmentEntryPoint: "grass_fragment",
		Argument: canvas.ArgumentOf[Wind](), CullMargin: 6,
	})
	g.minimap = render.MakeRenderTexture("minimap", 256, 256)
	g.minimapImage = canvas.FromTexture(g.minimap)
}

// GrassVertex is a vertex layout of the game's own: a position, and how far
// the vertex sways, which grass.wgsl's vertex stage reads.
type GrassVertex struct {
	Position m.Vec2
	Sway     float32
	_        float32
}

// VertexLayout answers GrassVertex's layout.
func (GrassVertex) VertexLayout() meshfile.Layout {
	return meshfile.Layout{Stride: 16, Attributes: []meshfile.Attribute{
		{Format: meshfile.Float32x2, Offset: 0, Location: 0},
		{Format: meshfile.Float32, Offset: 8, Location: 1},
	}}
}

// putMeshes builds the game's mesh files and puts them into its memory mount,
// where canvas reads them by path at their first draw.
func putMeshes(memory kernel.Read[*filesystems.MemoryFS]) {
	// The lake: a polygon whose UVs are its positions over the water image's
	// size, so the tiling water repeats across it.
	var lake []canvas.Vertex
	var lakeIndices []uint32
	lake = append(lake, canvas.Vertex{})
	for step := range 12 {
		angle := float64(step) / 12 * 2 * math.Pi
		radius := 90 + 25*math.Sin(3*angle)
		position := m.Vec2{X: float32(radius * math.Cos(angle)), Y: float32(0.6 * radius * math.Sin(angle))}
		lake = append(lake, canvas.Vertex{Position: position, UV: position.DivS(64)})
		lakeIndices = append(lakeIndices, 0, uint32(step+1), uint32(step+1)%12+1)
	}
	put(memory, "meshes/lake.mesh", lake, lakeIndices)

	// The minimap's disc, its UVs over the whole minimap image.
	var disc []canvas.Vertex
	var discIndices []uint32
	disc = append(disc, canvas.Vertex{UV: m.Vec2{X: 0.5, Y: 0.5}})
	for step := range 24 {
		angle := float64(step) / 24 * 2 * math.Pi
		position := m.Vec2{X: float32(math.Cos(angle)), Y: float32(math.Sin(angle))}
		disc = append(disc, canvas.Vertex{Position: position, UV: position.MulS(0.5).AddS(0.5)})
		discIndices = append(discIndices, 0, uint32(step+1), uint32(step+1)%24+1)
	}
	put(memory, "meshes/disc.mesh", disc, discIndices)

	// A tuft of grass in the game's own layout, swaying more at its tip.
	grass := []GrassVertex{
		{Position: m.Vec2{X: -6, Y: 0}}, {Position: m.Vec2{X: 6, Y: 0}},
		{Position: m.Vec2{X: -3, Y: -14}, Sway: 0.6}, {Position: m.Vec2{X: 3, Y: -14}, Sway: 0.6},
		{Position: m.Vec2{X: 0, Y: -24}, Sway: 1},
	}
	put(memory, "meshes/grass.mesh", grass, []uint32{0, 1, 2, 1, 3, 2, 2, 3, 4})
}

func put[V interface {
	meshfile.VertexType
	Position3() m.Vec3
}](memory kernel.Read[*filesystems.MemoryFS], path string, vertices []V, indices []uint32) {
	bounds := meshfile.CalculateBounds(vertices, V.Position3)
	data, err := meshfile.Encode(vertices, indices, meshfile.TriangleList, bounds)
	if err != nil {
		panic(err)
	}
	memory.Get().Put(path, data)
}

// Position3 answers the vertex's position with a zero z, for
// meshfile.CalculateBounds.
func (v GrassVertex) Position3() m.Vec3 { return m.Vec3{X: v.Position.X, Y: v.Position.Y} }
