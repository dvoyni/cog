package main

import (
	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/canvas"
	"prototype.invalid/canvasframe/kernel"
	"prototype.invalid/canvasframe/mx"
)

var (
	unitImage = canvas.FromFile("units/unit.png", canvas.ImageOptions{Pivot: canvas.PivotBottom})
	heroImage = canvas.FromFile("units/hero.png", canvas.ImageOptions{Pivot: canvas.PivotBottom})
	shadow    = canvas.FromFile("units/shadow.png")
	// heroField is the hero's distance field, 8 texels beyond the art. Its
	// size and pivot are measured on the art, so given the hero's transform
	// it lines up with the hero at any pivot.
	heroField = canvas.FromFile("units/hero.png", canvas.ImageOptions{Pivot: canvas.PivotBottom, Convert: canvas.DistanceField{Spread: 8}})
)

// drawUnits records the units, y-sorted, each with its shadow under it and
// its name over it, the hero with a halo.
func drawUnits(list canvas.DrawList, game kernel.Read[*Game]) {
	g := game.Get()
	black := m.Color{A: 1}
	var living []canvas.Sprite
	for _, unit := range g.units {
		at := mx.Translation2x3(unit.Position.X, unit.Position.Y)
		order := canvas.OrderOf(unit.Position.Y)
		list.DrawSprite(canvas.Sprite{Image: shadow, Transform: at, Tint: m.Color{A: 0.4}, Layer: layerUnits, Order: order - 1})
		sprite := canvas.Sprite{Image: unitImage, Transform: at, Layer: layerUnits, Order: order}
		if unit.Dissolving > 0 {
			sprite.Material = g.dissolve
			list.DrawSpriteWith(sprite, Dissolve{Amount: unit.Dissolving, Edge: m.NewColorSrgb8(255, 120, 40, 255)})
			continue
		}
		living = append(living, sprite)
		list.DrawTextWith(canvas.Text{
			String: unit.Name, Language: unit.Language, Size: 12, Pivot: canvas.PivotBottom,
			Transform: mx.Translation2x3(unit.Position.X, unit.Position.Y-36), Layer: layerLabels,
		}, canvas.Outline(2, black))
	}
	list.DrawSprites(living)

	hero := g.hero
	at := mx.Translation2x3(hero.Position.X, hero.Position.Y)
	order := canvas.OrderOf(hero.Position.Y)
	list.DrawSpriteWith(canvas.Sprite{Image: heroField, Material: canvas.SDFMaterial, Transform: at, Layer: layerUnits, Order: order},
		canvas.Halo(2, 8, m.NewColorSrgb8(255, 220, 120, 255)))
	list.DrawSprite(canvas.Sprite{Image: heroImage, Transform: at, Layer: layerUnits, Order: order})
	list.DrawTextWith(canvas.Text{
		String: hero.Name, Font: g.title, Size: 14, Pivot: canvas.PivotBottom,
		Transform: mx.Translation2x3(hero.Position.X, hero.Position.Y-36), Layer: layerLabels,
	}, canvas.Outline(2, black))

	// A wrong argument type: refused and reported once.
	list.DrawSpriteWith(canvas.Sprite{Image: unitImage, Material: g.dissolve, Transform: at, Layer: layerUnits}, Wind{})
}
