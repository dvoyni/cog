// PROTOTYPE for https://github.com/dvoyni/cog/issues/871: a game's plugin
// drawing two frames through canvas's API. Run with GOWORK=off go run . for
// split screen, or GOWORK=off go run . pixel for one pixel-perfect camera.
package main

import (
	"fmt"
	"os"

	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/canvas"
	"prototype.invalid/canvasframe/kernel"
	"prototype.invalid/canvasframe/platform"
	"prototype.invalid/canvasframe/platform/filesystems"
	"prototype.invalid/canvasframe/render"
)

// The game's layers: every entry of a lower layer is drawn under every entry of
// a higher one, and each camera draws the layer ranges it names.
const (
	layerGround int32 = iota // the tilemap and the lake
	layerGrass               // swaying grass, a mesh of the game's own layout
	layerUnits               // y-sorted units, their shadows and the hero's trail
	layerLabels              // names over the units
	layerUI     int32 = 100
)

// Game is the game's own resource.
type Game struct {
	pixelPerfect bool
	time         float32
	hero         Unit
	units        []Unit
	trail        []m.Vec2
	tiles        Tilemap
	theme        Theme
	// cameras are this frame's, kept so a click is converted through them.
	cameras      []canvas.Camera
	click        m.Vec2
	minimap      render.Texture
	minimapImage canvas.Image
	dissolve     canvas.Material
	grass        canvas.Material
	title        canvas.FontSet
	coins        int
}

// Unit is one of the game's units.
type Unit struct {
	Name     string
	Language string
	Position m.Vec2
	Health   float32
	// Dissolving is how far the unit has dissolved, zero while it lives.
	Dissolving float32
}

func main() {
	pixelPerfect := len(os.Args) > 1 && os.Args[1] == "pixel"
	config := canvas.Config{
		Language:        "en",
		RegroupedLayers: []int32{layerGround},
		DefaultFontSet: canvas.FontSetDescriptor{
			Label:   "body",
			Default: canvas.Font{Path: "fonts/inter.ttf"},
			Languages: []canvas.LanguageFonts{
				{Languages: []string{"ja"}, Fonts: []canvas.Font{{Path: "fonts/noto-sans-jp.otf", FieldEm: 64}}},
			},
		},
	}
	if pixelPerfect {
		config.DefaultFilter = canvas.FilterNearest
	}
	// The game's plugin owns a memory mount, bound as a read mount, and keeps
	// it as a resource of its own; every plugin reads it through platform's FS.
	memory := filesystems.NewMemoryFS()
	prototype := canvas.NewPrototype(config, memory, m.Vec2{X: 1280, Y: 720}, 2)
	imageSizes()

	game := newGame(pixelPerfect)
	gameRead, gameWrite := kernel.Share(&game)
	fontsRead, fontsWrite := kernel.Share(prototype.Fonts())
	targets, _ := kernel.Share(prototype.Targets())
	var files platform.FS = memory
	filesRead, _ := kernel.Share(&files)
	memoryRead, _ := kernel.Share(&memory)
	assets := prototype.Assets()

	// At start, or behind a loading screen.
	loadFonts(fontsWrite, filesRead, gameWrite)
	putMeshes(memoryRead)
	makeMaterials(assets, gameWrite)

	for range 2 {
		update(gameWrite, targets)
		animate(assets, gameRead)
		drawWorld(prototype.BeginList("world"), gameRead, targets)
		drawUnits(prototype.BeginList("units"), gameRead)
		drawUI(prototype.BeginList("ui"), fontsRead, gameRead)
		prototype.End()
	}
}

func newGame(pixelPerfect bool) *Game {
	game := &Game{pixelPerfect: pixelPerfect, click: m.Vec2{X: 1000, Y: 300}, coins: 120}
	game.hero = Unit{Name: "Mira", Position: m.Vec2{X: 300, Y: 260}, Health: 0.7}
	game.units = []Unit{
		{Name: "ハナコ", Language: "ja", Position: m.Vec2{X: 340, Y: 240}, Health: 1},
		{Name: "Bram", Position: m.Vec2{X: 260, Y: 300}, Health: 0.4},
		{Name: "Wisp", Position: m.Vec2{X: 700, Y: 520}, Health: 0, Dissolving: 0.3},
		{Name: "Tor", Position: m.Vec2{X: 720, Y: 480}, Health: 0.9},
		{Name: "Far", Position: m.Vec2{X: 2000, Y: 2000}, Health: 1},
	}
	game.tiles = newTilemap()
	game.theme = newTheme()
	return game
}

// imageSizes tells the prototype the sizes of the game's image files, which
// it cannot read.
func imageSizes() {
	for path, size := range map[string]m.Vec2{
		"tiles/terrain.png": {X: 64, Y: 64}, "water.png": {X: 64, Y: 64}, "grass.png": {X: 16, Y: 32},
		"units/hero.png": {X: 24, Y: 32}, "units/unit.png": {X: 24, Y: 32}, "units/shadow.png": {X: 24, Y: 8},
		"ui/panel.png": {X: 48, Y: 48}, "ui/button.png": {X: 32, Y: 32}, "ui/bar.png": {X: 64, Y: 8},
		"ui/prompts.png": {X: 128, Y: 128}, "ui/coin.png": {X: 16, Y: 16},
	} {
		canvas.PrototypeImageSize(path, size.X, size.Y)
	}
}

// animate sets the game's clock on the materials that animate by it: canvas's
// pass carries no time, since game time stops when the game pauses.
func animate(assets canvas.Assets, game kernel.Read[*Game]) {
	g := game.Get()
	assets.SetMaterialUniform(g.grass, "time", g.time)
}

// update moves the game on, works out this frame's cameras, and converts the
// click through last frame's.
func update(game kernel.Write[*Game], targets kernel.Read[canvas.Targets]) {
	g := game.Get()
	g.time += 1.0 / 60
	g.hero.Position = g.hero.Position.Add(m.Vec2{X: 2, Y: 1})
	g.trail = append(g.trail, g.hero.Position)

	// The click goes to the topmost world camera whose viewport holds it.
	routed := false
	for index := len(g.cameras) - 1; index >= 0 && !routed; index-- {
		camera := g.cameras[index]
		if camera.Texture != (render.Texture{}) || camera.Layers[0].From == layerUI {
			continue
		}
		if world, inside := camera.ScreenToWorld(g.click, targets.Get()); inside {
			fmt.Printf("\ngame: the click at (%.0f,%.0f) pt is world (%.0f,%.0f) through camera %d\n", g.click.X, g.click.Y, world.X, world.Y, index+1)
			routed = true
		}
	}
	if !routed {
		fmt.Printf("\ngame: the click at (%.0f,%.0f) pt converts through no camera: no target drawn yet\n", g.click.X, g.click.Y)
	}

	world := canvas.Layers(layerGround, layerLabels+1)
	sky := m.NewColorSrgb8(30, 40, 60, 255)
	minimap := canvas.Camera{
		Texture: g.minimap, Position: m.Vec2{X: 512, Y: 512}, Size: m.Vec2{X: 1024, Y: 1024},
		Layers: canvas.Layers(layerGround, layerUnits+1), Clear: true, ClearColor: sky,
	}
	ui := canvas.Camera{Pivot: m.Vec2{X: -0.5, Y: -0.5}, Layers: canvas.Layers(layerUI, layerUI+1), Order: 10}
	if g.pixelPerfect {
		g.cameras = []canvas.Camera{
			minimap,
			{Position: g.hero.Position, Size: m.Vec2{X: 320, Y: 180}, Fit: canvas.FitInscribe, PixelPerfect: true, Layers: world, Clear: true, ClearColor: sky},
			ui,
		}
		return
	}
	g.cameras = []canvas.Camera{
		minimap,
		{Position: g.hero.Position, Viewport: m.Rect{Width: 0.5, Height: 1}, Size: m.Vec2{Y: 360}, Layers: world, Clear: true, ClearColor: sky, Samples: 4},
		{Position: g.units[3].Position, Viewport: m.Rect{X: 0.5, Width: 0.5, Height: 1}, Size: m.Vec2{Y: 360}, Zoom: 1.5, Layers: world, Clear: true, ClearColor: sky, Samples: 4},
		ui,
	}
}
