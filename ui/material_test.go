package ui

import (
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
	"golang.org/x/image/font/gofont/goregular"
)

// materialProbe records the material set every element it is put on inherited,
// which is what a custom Visual sees: the set arrives through the State it
// already gets, and it picks the slot for the family it draws.
type materialProbe struct {
	id  ID
	set *canvas.MaterialSet
}

func (materialProbe) DefaultSize(canvas.LookupAccess, ID) m.Vec2 { return m.Vec2{X: 10, Y: 10} }

func (probe materialProbe) Draw(_ canvas.LookupAccess, _ *canvas.OpQueue, state State, _ ID) {
	*probe.set = state.Materials
}

// probeElement returns an element whose visual writes the set it inherited into
// seen.
func probeElement(seen *canvas.MaterialSet) Element {
	return NewElement().Visual(materialProbe{set: seen}, ID(""))
}

// runTree lays out and draws one root, so every visual's Draw runs with the
// state it inherited.
func runTree(t *testing.T, frameDefault canvas.MaterialSet, root Element) {
	t.Helper()
	context := processor{}
	context.process(testLookup(fstest.MapFS{}), []Element{root}, []canvas.Layer{0}, globalState{
		Screen:    Rect{Width: 200, Height: 200},
		Materials: frameDefault,
	}, &canvas.OpQueue{})
}

// One modifier on a root reaches every visual beneath it, however deep.
func TestAMaterialModifierReachesEveryVisualBeneathIt(t *testing.T) {
	sprite := gfx.MaterialWithState(gfx.ShaderWithText("fn markSprite() {}"), gfx.StateOverlay2D)
	var near, deep canvas.MaterialSet
	runTree(t, canvas.MaterialSet{}, NewElement().
		Material(canvas.MaterialSet{Sprite: &sprite}).
		Children(
			probeElement(&near),
			NewElement().Children(NewElement().Children(probeElement(&deep))),
		))
	if near.Sprite != &sprite || deep.Sprite != &sprite {
		t.Fatalf("inherited sprite slots = (%p, %p), want %p at both depths", near.Sprite, deep.Sprite, &sprite)
	}
}

// The frame default seeds every root, and any element replaces it.
func TestTheFrameDefaultSeedsRootsAndAnElementReplacesIt(t *testing.T) {
	fromFrame := gfx.MaterialWithState(gfx.ShaderWithText("fn markFrame() {}"), gfx.StateOverlay2D)
	fromElement := gfx.MaterialWithState(gfx.ShaderWithText("fn markElement() {}"), gfx.StateOverlay2D)
	var seeded, replaced canvas.MaterialSet
	runTree(t, canvas.MaterialSet{Sprite: &fromFrame}, NewElement().Children(
		probeElement(&seeded),
		NewElement().Material(canvas.MaterialSet{Sprite: &fromElement}).Children(probeElement(&replaced)),
	))
	if seeded.Sprite != &fromFrame {
		t.Fatalf("seeded slot = %p, want the frame default %p", seeded.Sprite, &fromFrame)
	}
	if replaced.Sprite != &fromElement {
		t.Fatalf("replaced slot = %p, want the element's own %p", replaced.Sprite, &fromElement)
	}
}

// A child naming an empty set stops inheriting. The opt-out costs nothing to
// have and is what an element that must draw with the built-ins uses.
func TestAnEmptySetStopsInheriting(t *testing.T) {
	sprite := gfx.MaterialWithState(gfx.ShaderWithText("fn markSprite() {}"), gfx.StateOverlay2D)
	var optedOut canvas.MaterialSet
	runTree(t, canvas.MaterialSet{}, NewElement().
		Material(canvas.MaterialSet{Sprite: &sprite}).
		Children(NewElement().Material(canvas.MaterialSet{}).Children(probeElement(&optedOut))))
	if optedOut.Sprite != nil {
		t.Fatalf("opted-out slot = %p, want nil", optedOut.Sprite)
	}
}

// Every built-in visual passes the sprite slot, because a sprite, a nine-slice,
// a glyph run and a fill are all sprite draws.
func TestEveryBuiltInVisualPassesTheSpriteSlot(t *testing.T) {
	sprite := gfx.MaterialWithState(gfx.ShaderWithText("fn markSprite() {}"), gfx.StateOverlay2D)
	files := testAssets(t, 8, 8)
	// A real face, so the text visual's measurement resolves rather than
	// reporting through a kernel this test does not have.
	files["font.ttf"] = &fstest.MapFile{Data: goregular.TTF}
	lookup := testLookup(files)
	state := State{
		Rect:        Rect{Width: 40, Height: 20},
		ContentRect: Rect{Width: 40, Height: 20},
		Materials:   canvas.MaterialSet{Sprite: &sprite},
	}
	cases := map[string]func(*canvas.OpQueue){
		"sprite": func(q *canvas.OpQueue) {
			visual, params := Sprite(SpriteParams{Path: "sprite.png"})
			visual.Draw(lookup, q, state, params)
		},
		"nine-slice": func(q *canvas.OpQueue) {
			visual, params := Sprite9Sliced(Sprite9SlicedParams{Path: "sprite.png", Insets: canvas.SpriteFrame{Left: 1, Right: 1, Top: 1, Bottom: 1}})
			visual.Draw(lookup, q, state, params)
		},
		"fill": func(q *canvas.OpQueue) {
			visual, params := Color(ColorParams{Color: m.Color{R: 1, A: 1}})
			visual.Draw(lookup, q, state, params)
		},
		"text": func(q *canvas.OpQueue) {
			visual, params := Text(TextParams{Font: Font{Path: "font.ttf", Size: 12}, Text: "hi"})
			visual.Draw(lookup, q, state, params)
		},
	}
	for name, draw := range cases {
		queue := &canvas.OpQueue{}
		draw(queue)
		ops := queue.Ops(nil)
		if len(ops) != 1 {
			t.Fatalf("%s recorded %d ops, want 1", name, len(ops))
		}
		if !ops[0].HasMaterial {
			t.Fatalf("%s did not pass the inherited sprite slot", name)
		}
	}
}
