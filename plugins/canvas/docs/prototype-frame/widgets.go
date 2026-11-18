package main

import (
	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/canvas"
	"prototype.invalid/canvasframe/mx"
)

// Theme is a toy UI's images.
type Theme struct {
	Panel, Button, ButtonPressed, Bar canvas.Image
}

func newTheme() Theme {
	sliced := canvas.ImageOptions{Slice: canvas.Insets{Left: 12, Top: 12, Right: 12, Bottom: 12}, Pivot: canvas.PivotTopLeft}
	return Theme{
		Panel:  canvas.FromFile("ui/panel.png", sliced),
		Button: canvas.FromFile("ui/button.png", sliced),
		// The same file with other options that change no pixel shares its
		// region.
		ButtonPressed: canvas.FromFile("ui/button.png", canvas.ImageOptions{Slice: canvas.Insets{Left: 12, Top: 14, Right: 12, Bottom: 10}, Pivot: canvas.PivotTopLeft}),
		Bar:           canvas.FromFile("ui/bar.png", canvas.ImageOptions{Pivot: canvas.PivotTopLeft}),
	}
}

// Button is a toy UI widget built on canvas alone.
type Button struct {
	Position m.Vec2
	Label    string
	Pressed  bool
}

// DrawButton records button, a sliced panel sized to its label's layout, and
// answers the rectangle it covers.
func DrawButton(list canvas.DrawList, fonts canvas.Fonts, theme Theme, button Button) m.Rect {
	label := canvas.Text{String: button.Label, Size: 18, Layer: layerUI, Order: 4}
	size := fonts.Layout(label).Size().Add(m.Vec2{X: 32, Y: 16})
	image := theme.Button
	if button.Pressed {
		image = theme.ButtonPressed
	}
	list.DrawSprite(canvas.Sprite{Image: image, DrawMode: canvas.Sliced, Size: size,
		Transform: mx.Translation2x3(button.Position.X, button.Position.Y), Layer: layerUI, Order: 3})
	centre := button.Position.Add(size.MulS(0.5))
	label.Transform = mx.Translation2x3(centre.X, centre.Y)
	list.DrawText(label)
	return m.Rect{X: button.Position.X, Y: button.Position.Y, Width: size.X, Height: size.Y}
}
