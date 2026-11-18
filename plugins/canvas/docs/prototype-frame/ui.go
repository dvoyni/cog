package main

import (
	"fmt"

	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/canvas"
	"prototype.invalid/canvasframe/kernel"
	"prototype.invalid/canvasframe/mx"
)

var disc = canvas.MeshFromFile("meshes/disc.mesh")

// drawUI records the interface, seen through the UI camera, whose top left is
// the world's origin, one unit to a point.
func drawUI(list canvas.DrawList, fontsHandle kernel.Read[canvas.Fonts], game kernel.Read[*Game]) {
	g := game.Get()
	fonts := fontsHandle.Get()
	white := m.Color{R: 1, G: 1, B: 1, A: 1}

	// A sliced panel, its image's pivot its top left.
	list.DrawSprite(canvas.Sprite{Image: g.theme.Panel, DrawMode: canvas.Sliced, Size: m.Vec2{X: 320, Y: 150},
		Transform: mx.Translation2x3(16, 16), Layer: layerUI})

	// The hero's health: a bar emptying from its right.
	list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{X: 32, Y: 32, Width: 200, Height: 12}, Tint: m.Color{A: 0.6}, Layer: layerUI, Order: 1})
	list.DrawSprite(canvas.Sprite{Image: g.theme.Bar, Size: m.Vec2{X: 200, Y: 12}, Transform: mx.Translation2x3(32, 32),
		Crop: canvas.Crop{Right: 1 - g.hero.Health}, Layer: layerUI, Order: 2})

	// A prompt in markup: an icon, and a word in a registered colour.
	list.DrawText(canvas.Text{String: `Press \i:button-a to \c:keyword{attack}`, Markup: true, Size: 16, Pivot: canvas.PivotTopLeft,
		Transform: mx.Translation2x3(32, 56), Layer: layerUI, Order: 2})

	// What a player typed draws as written: no markup is read.
	list.DrawText(canvas.Text{String: `Player\i:coin`, Size: 16, Pivot: canvas.PivotTopLeft, Transform: mx.Translation2x3(32, 80), Layer: layerUI, Order: 2})

	// The coins, an icon span beside a number.
	list.DrawText(canvas.Text{Spans: []canvas.Span{{Icon: "coin"}, {Text: fmt.Sprintf(" %d", g.coins), Tint: m.NewColorSrgb8(255, 220, 120, 255)}},
		Size: 16, Pivot: canvas.PivotTopLeft, Transform: mx.Translation2x3(240, 28), Layer: layerUI, Order: 2})

	// A greeting in Japanese, in the title set, through the run's language.
	list.DrawText(canvas.Text{String: "ようこそ、旅人", Language: "ja", Font: g.title, Size: 20, Pivot: canvas.PivotTopLeft,
		Transform: mx.Translation2x3(32, 104), Layer: layerUI, Order: 2})

	// A button sized to its label's layout, measured before it is drawn.
	DrawButton(list, fonts, g.theme, Button{Position: m.Vec2{X: 32, Y: 180}, Label: "Attack"})
	DrawButton(list, fonts, g.theme, Button{Position: m.Vec2{X: 150, Y: 180}, Label: "Retreat", Pressed: true})

	// A scrolled log, clipped to its box.
	log := m.Rect{X: 960, Y: 560, Width: 300, Height: 140}
	list.DrawShape(canvas.Shape{Kind: canvas.Stroke, Rect: log, Tint: white, Layer: layerUI, Order: 1})
	list.DrawText(canvas.Text{String: "Mira found a coin.\nBram is hurt.\nWisp fades away.\nTor waits by the water.", Size: 14,
		WrapWidth: 280, Pivot: canvas.PivotTopLeft, Transform: mx.Translation2x3(970, 540), Clip: log, Layer: layerUI, Order: 2})

	// The minimap: the minimap camera's texture on a disc mesh.
	list.DrawMesh(canvas.Mesh{File: disc, Image: g.minimapImage, Transform: mx.TRS2x3(m.Vec2{X: 1180, Y: 100}, 0, m.Vec2{X: 80, Y: 80}), Layer: layerUI})
	list.DrawShape(canvas.Shape{Kind: canvas.Line, From: m.Vec2{X: 1180, Y: 100}, To: m.Vec2{X: 1180, Y: 30}, Width: 2, Tint: white, Layer: layerUI, Order: 1})
}
