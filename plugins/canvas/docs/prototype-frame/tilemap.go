package main

import (
	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/canvas"
	"prototype.invalid/canvasframe/mx"
)

// Tilemap is a toy tilemap built on canvas alone: tiles are frames of one
// sheet, and it records only the tiles a view sees, in one bulk call.
type Tilemap struct {
	Width, Height int
	TileSize      float32
	Tiles         []uint8
	Sheet         canvas.Image
}

func newTilemap() Tilemap {
	tiles := Tilemap{Width: 64, Height: 64, TileSize: 16, Tiles: make([]uint8, 64*64),
		Sheet: canvas.FromFile("tiles/terrain.png", canvas.ImageOptions{Sheet: canvas.Grid{Columns: 4, Rows: 4}, Pivot: canvas.PivotTopLeft})}
	for x := range tiles.Width {
		tiles.Tiles[20*tiles.Width+x] = 1 // a road
	}
	return tiles
}

// Bounds answers the world rectangle the map covers.
func (t Tilemap) Bounds() m.Rect {
	return m.Rect{Width: float32(t.Width) * t.TileSize, Height: float32(t.Height) * t.TileSize}
}

// Draw records the tiles within visible at layer.
func (t Tilemap) Draw(list canvas.DrawList, visible m.Rect, layer int32) {
	first := m.Vec2{X: max(0, visible.X/t.TileSize), Y: max(0, visible.Y/t.TileSize)}
	last := m.Vec2{X: min(float32(t.Width), (visible.X+visible.Width)/t.TileSize+1), Y: min(float32(t.Height), (visible.Y+visible.Height)/t.TileSize+1)}
	var sprites []canvas.Sprite
	for y := int(first.Y); y < int(last.Y); y++ {
		for x := int(first.X); x < int(last.X); x++ {
			sprites = append(sprites, canvas.Sprite{
				Image: t.Sheet, Frame: uint32(t.Tiles[y*t.Width+x]), Size: m.Vec2{X: t.TileSize, Y: t.TileSize},
				Transform: mx.Translation2x3(float32(x)*t.TileSize, float32(y)*t.TileSize), Layer: layer,
			})
		}
	}
	list.DrawSprites(sprites)
}
