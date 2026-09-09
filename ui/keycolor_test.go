package ui

import (
	"testing"

	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/m"
)

// drawSprite runs one sprite visual and returns the single op it recorded.
func drawSprite(t *testing.T, params SpriteParams) canvas.Op {
	t.Helper()
	queue := &canvas.OpQueue{}
	visual, payload := Sprite(params)
	visual.Draw(testLookup(testAssets(t, 8, 8)), queue, State{
		Rect:        Rect{Width: 40, Height: 20},
		ContentRect: Rect{Width: 40, Height: 20},
	}, payload)
	ops := queue.Ops(nil)
	if len(ops) != 1 {
		t.Fatalf("recorded %d ops, want 1", len(ops))
	}
	return ops[0]
}

// A key colour named on a sprite reaches the draw as the reserved keyColor
// slot - the same slot a game's own board draws already use, so a unit icon in
// the UI shades identically to the unit on the board.
func TestASpriteKeyColourReachesTheDraw(t *testing.T) {
	want := m.NewColorSrgb(0.2, 0.4, 0.9, 1)
	param, ok := drawSprite(t, SpriteParams{Path: "sprite.png", KeyColor: want}).Param(canvas.KeyColorSlot)
	if !ok {
		t.Fatal("the draw named no key colour")
	}
	got, isColor := param.ColorValue()
	if !isColor || got != want {
		t.Fatalf("key colour = %+v (%v), want %+v", got, isColor, want)
	}
}

// A sprite naming no key colour names none at all, so canvas's own mid-grey
// default applies. ui must not invent a second default that could drift from it.
func TestASpriteWithNoKeyColourNamesNone(t *testing.T) {
	if _, ok := drawSprite(t, SpriteParams{Path: "sprite.png"}).Param(canvas.KeyColorSlot); ok {
		t.Fatal("a sprite with no key colour named the slot anyway")
	}
}

// The tint slot is unaffected either way: a key-coloured sprite still gets its
// tint, because the two are separate slots in the instance record.
func TestAKeyColouredSpriteStillCarriesItsTint(t *testing.T) {
	op := drawSprite(t, SpriteParams{Path: "sprite.png", KeyColor: m.NewColorSrgb(0.2, 0.4, 0.9, 1)})
	param, ok := op.Param(canvas.TintSlot)
	if !ok {
		t.Fatal("the draw named no tint")
	}
	got, _ := param.ColorValue()
	if got != (m.Color{R: 1, G: 1, B: 1, A: 1}) {
		t.Fatalf("tint = %+v, want opaque white", got)
	}
}

// An interactive sprite carries the key colour through its Default, so a
// hoverable unit icon keeps its player colour in every visual state.
func TestAnInteractiveSpriteCarriesTheKeyColourThroughItsDefault(t *testing.T) {
	want := m.NewColorSrgb(0.9, 0.3, 0.1, 1)
	queue := &canvas.OpQueue{}
	visual, payload := InteractiveSprite(InteractiveSpriteParams{
		Default: SpriteParams{Path: "sprite.png", KeyColor: want},
	})
	visual.Draw(testLookup(testAssets(t, 8, 8)), queue, State{
		Rect:        Rect{Width: 40, Height: 20},
		ContentRect: Rect{Width: 40, Height: 20},
	}, payload)
	ops := queue.Ops(nil)
	if len(ops) != 1 {
		t.Fatalf("recorded %d ops, want 1", len(ops))
	}
	param, ok := ops[0].Param(canvas.KeyColorSlot)
	if !ok {
		t.Fatal("the interactive draw named no key colour")
	}
	if got, _ := param.ColorValue(); got != want {
		t.Fatalf("key colour = %+v, want %+v", got, want)
	}
}
