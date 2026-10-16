package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/libs/m"
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

func drawnTint(t *testing.T, op canvas.Op) m.Color {
	t.Helper()
	param, ok := op.Param(canvas.TintSlot)
	if !ok {
		t.Fatal("the draw named no tint")
	}
	got, isColor := param.ColorValue()
	if !isColor {
		t.Fatal("the tint is not a colour")
	}
	return got
}

// A sprite naming no tint draws at canvas's neutral one, so it shows its art as
// painted rather than at the doubled brightness a white tint now means.
func TestASpriteWithNoTintDrawsAtTheNeutralTint(t *testing.T) {
	if got := drawnTint(t, drawSprite(t, SpriteParams{Path: "sprite.png"})); got != canvas.DefaultTint() {
		t.Fatalf("tint = %+v, want %+v", got, canvas.DefaultTint())
	}
}

// A tint that is named reaches the draw untouched: ui does not carry it onto
// the ramp, the caller already did.
func TestASpriteTintReachesTheDrawAsGiven(t *testing.T) {
	want := m.Color{R: 0.1, G: 0.2, B: 0.3, A: 0.4}
	if got := drawnTint(t, drawSprite(t, SpriteParams{Path: "sprite.png", Tint: want})); got != want {
		t.Fatalf("tint = %+v, want %+v", got, want)
	}
}

// FlipX reaches the draw's transform, so a sprite can face the other way
// without art of its own.
func TestASpriteFlipReachesTheDraw(t *testing.T) {
	if !drawSprite(t, SpriteParams{Path: "sprite.png", FlipX: true}).Transform.FlipX {
		t.Fatal("a flipped sprite drew unflipped")
	}
	if drawSprite(t, SpriteParams{Path: "sprite.png"}).Transform.FlipX {
		t.Fatal("an unflipped sprite drew flipped")
	}
}
