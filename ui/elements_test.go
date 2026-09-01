package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/m"

	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/storage"
)

// testAssets returns an in-memory filesystem with a sprite of the given size.
func testAssets(t testing.TB, spriteWidth, spriteHeight int) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		"sprite.png": &fstest.MapFile{Data: testPNG(t, spriteWidth, spriteHeight)},
	}
}

// testLookup builds a LookupAccess backed by an in-memory Lookup over files.
func testLookup(files fstest.MapFS) canvas.LookupAccess {
	return canvas.NewLookupAccess(kernel.Kernel{}, canvas.NewLookup(canvas.DefaultConfig()), storage.NewReadFS("test", files))
}

func testPNG(t testing.TB, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 1, G: 1, B: 1, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return out.Bytes()
}

func TestSpriteScaleAffectsIntrinsicSizeOnly(t *testing.T) {
	lookup := testLookup(testAssets(t, 20, 10))
	visual, payload := Sprite(SpriteParams{Path: "sprite.png", Scale: 2})
	if got, want := visual.DefaultSize(lookup, payload), (m.Vec2{X: 40, Y: 20}); got != want {
		t.Fatalf("DefaultSize = %+v, want %+v", got, want)
	}

	queue := &canvas.OpQueue{}
	visual.Draw(lookup, queue, State{
		Rect:        Rect{X: 2, Y: 3, Width: 80, Height: 30},
		ContentRect: Rect{X: 10, Y: 11, Width: 64, Height: 14},
	}, payload)
	if queue.OpCount() != 1 {
		t.Fatalf("sprite op count = %d, want 1", queue.OpCount())
	}
	transform := queue.Ops(nil)[0].Transform
	if got, want := transform.Position, (m.Vec2{X: 42, Y: 18}); got != want {
		t.Fatalf("sprite position = %+v, want centered pivot %+v", got, want)
	}
	if got, want := transform.Origin, (m.Vec2{X: 0.5, Y: 0.5}); got != want {
		t.Fatalf("sprite origin = %+v, want %+v", got, want)
	}
}

func TestSpriteRotatesAroundCenterOfArrangedBounds(t *testing.T) {
	visual, payload := Sprite(SpriteParams{Path: "sprite.png", Rotation: 1})
	queue := &canvas.OpQueue{}
	visual.Draw(canvas.LookupAccess{}, queue, State{Rect: Rect{X: 10, Y: 20, Width: 80, Height: 40}}, payload)

	transform := queue.Ops(nil)[0].Transform
	if got, want := transform.Position, (m.Vec2{X: 50, Y: 40}); got != want {
		t.Fatalf("sprite position = %+v, want centered pivot %+v", got, want)
	}
	if got, want := transform.Origin, (m.Vec2{X: 0.5, Y: 0.5}); got != want {
		t.Fatalf("sprite origin = %+v, want %+v", got, want)
	}
}

func TestSprite9SlicedDrawsOnOuterRect(t *testing.T) {
	visual, payload := Sprite9Sliced(Sprite9SlicedParams{Path: "frame", Insets: canvas.SpriteFrame{Left: 2, Top: 2, Right: 2, Bottom: 2}})
	queue := &canvas.OpQueue{}
	visual.Draw(canvas.LookupAccess{}, queue, State{
		Rect:        Rect{X: 2, Y: 3, Width: 100, Height: 50},
		ContentRect: Rect{X: 18, Y: 19, Width: 68, Height: 18},
	}, payload)
	op := queue.Ops(nil)[0]
	if got, want := op.Transform.Position, (m.Vec2{X: 2, Y: 3}); got != want {
		t.Fatalf("nine-slice position = %+v, want outer origin %+v", got, want)
	}
}

func TestSprite9SliceTiledBuildsNineOperations(t *testing.T) {
	params := Sprite9SliceTiledParams{
		Images: Sprite9SliceImages{
			Center: "center", Top: "top", Right: "right", Bottom: "bottom", Left: "left",
			TopLeft: "top-left", TopRight: "top-right", BottomRight: "bottom-right", BottomLeft: "bottom-left",
		},
		Insets: Insets{Top: 2, Right: 3, Bottom: 4, Left: 5}, Scale: 2,
	}
	visual, payload := Sprite9SliceTiled(params)
	if got, want := visual.DefaultSize(canvas.LookupAccess{}, payload), (m.Vec2{X: 16, Y: 12}); got != want {
		t.Fatalf("DefaultSize = %+v, want %+v", got, want)
	}
	queue := &canvas.OpQueue{}
	visual.Draw(canvas.LookupAccess{}, queue, State{Rect: Rect{Width: 100, Height: 50}}, payload)
	if queue.OpCount() != 9 {
		t.Fatalf("nine-slice op count = %d, want 9", queue.OpCount())
	}
}

func TestSprite9SliceTiledDrawsOnOuterRect(t *testing.T) {
	visual, payload := Sprite9SliceTiled(Sprite9SliceTiledParams{
		Images: Sprite9SliceImages{TopLeft: "top-left"},
		Insets: Insets{Top: 2, Right: 2, Bottom: 2, Left: 2},
	})
	queue := &canvas.OpQueue{}
	visual.Draw(canvas.LookupAccess{}, queue, State{
		Rect:        Rect{X: 2, Y: 3, Width: 100, Height: 50},
		ContentRect: Rect{X: 18, Y: 19, Width: 68, Height: 18},
	}, payload)
	ops := queue.Ops(nil)
	if len(ops) == 0 {
		t.Fatal("top-left operation not recorded")
	}
	if got, want := ops[0].Transform.Position, (m.Vec2{X: 2, Y: 3}); got != want {
		t.Fatalf("top-left position = %+v, want outer origin %+v", got, want)
	}
}

func TestSprite9SliceTiledResolvesInsetsFromCornerSizes(t *testing.T) {
	files := testAssets(t, 8, 8)
	files["top-left.png"] = &fstest.MapFile{Data: testPNG(t, 5, 7)}
	files["bottom-right.png"] = &fstest.MapFile{Data: testPNG(t, 3, 4)}
	lookup := testLookup(files)
	params := Sprite9SliceTiledParams{
		Images: Sprite9SliceImages{TopLeft: "top-left.png", BottomRight: "bottom-right.png"},
		Scale:  2,
	}
	visual, payload := Sprite9SliceTiled(params)
	if got, want := visual.DefaultSize(lookup, payload), (m.Vec2{X: 16, Y: 22}); got != want {
		t.Fatalf("DefaultSize = %+v, want derived/scaled size %+v", got, want)
	}
	queue := &canvas.OpQueue{}
	visual.Draw(lookup, queue, State{Rect: Rect{Width: 100, Height: 50}}, payload)
	found := 0
	for _, op := range queue.Ops(nil) {
		if op.Path == params.Images.TopLeft {
			if got, want := op.Transform.Size, (m.Vec2{X: 10, Y: 14}); got != want {
				t.Fatalf("top-left size = %+v, want derived/scaled size %+v", got, want)
			}
			found++
		}
		if op.Path == params.Images.BottomRight {
			if got, want := op.Transform.Size, (m.Vec2{X: 6, Y: 8}); got != want {
				t.Fatalf("bottom-right size = %+v, want derived/scaled size %+v", got, want)
			}
			found++
		}
	}
	if found != 2 {
		t.Fatalf("corner operations found = %d, want 2", found)
	}
}

func TestSprite9SliceTiledInsetsCanOverrideOrDisableCalculatedSizes(t *testing.T) {
	files := testAssets(t, 8, 8)
	files["top-left.png"] = &fstest.MapFile{Data: testPNG(t, 5, 7)}
	files["bottom-right.png"] = &fstest.MapFile{Data: testPNG(t, 3, 4)}
	lookup := testLookup(files)
	visual, payload := Sprite9SliceTiled(Sprite9SliceTiledParams{
		Images: Sprite9SliceImages{TopLeft: "top-left.png", BottomRight: "bottom-right.png"},
		Insets: Insets{Top: 2, Right: 4, Bottom: -1, Left: -1},
		Scale:  2,
	})
	if got, want := visual.DefaultSize(lookup, payload), (m.Vec2{X: 8, Y: 4}); got != want {
		t.Fatalf("DefaultSize = %+v, want overridden/disabled size %+v", got, want)
	}
}

func TestSprite9SliceTiledEmptyCenterDrawsWhiteBorderOnly(t *testing.T) {
	visual, payload := Sprite9SliceTiled(Sprite9SliceTiledParams{
		Insets: Insets{Top: 2, Right: 2, Bottom: 2, Left: 2},
		Tint:   m.Color{A: 1},
	})
	queue := &canvas.OpQueue{}
	visual.Draw(canvas.LookupAccess{}, queue, State{Rect: Rect{Width: 20, Height: 10}}, payload)
	if queue.OpCount() != 8 {
		t.Fatalf("border op count = %d, want 8", queue.OpCount())
	}
}

func TestVisualStatesExpandMultibitKeysAndHighestBitWins(t *testing.T) {
	const selected VisualState = VisualUserDefinedBase
	packed := packVisualStates(VisualStates[string]{
		VisualDisabled | VisualHovered: "shared",
		VisualPressed:                  "pressed",
		selected:                       "selected",
	})
	if got := packed.value(VisualDisabled, "default"); got != "shared" {
		t.Fatalf("disabled value = %q, want shared", got)
	}
	if got := packed.value(VisualHovered, "default"); got != "shared" {
		t.Fatalf("hovered value = %q, want shared", got)
	}
	if got := packed.value(VisualDisabled|VisualHovered|VisualPressed, "default"); got != "pressed" {
		t.Fatalf("combined value = %q, want pressed", got)
	}
	if got := packed.value(VisualPressed|selected, "default"); got != "selected" {
		t.Fatalf("user-defined precedence = %q, want selected", got)
	}
	if got := packed.value(VisualActive, "default"); got != "default" {
		t.Fatalf("unconfigured value = %q, want default", got)
	}
}

var interactiveVisualSink any

func BenchmarkInteractiveColorConstruction(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, interactiveVisualSink = InteractiveColor(InteractiveColorParams{
			Default: m.Color{R: 0.2, A: 1},
			Colors: VisualStates[m.Color]{
				VisualHovered: {R: 0.5, A: 1},
				VisualPressed: {R: 0.8, A: 1},
			},
		})
	}
}

func TestInteractiveSpriteResolvesPathAndTintIndependently(t *testing.T) {
	_, params := InteractiveSprite(InteractiveSpriteParams{
		Default: SpriteParams{Path: "default", Tint: m.Color{R: 0.1}},
		Path:    VisualStates[string]{VisualActive: "active"},
		Tint:    VisualStates[m.Color]{VisualHovered: {R: 0.5}},
	})
	state := VisualActive | VisualHovered
	if got := params.path.value(state, params.defaultValue.Path); got != "active" {
		t.Fatalf("path = %q, want active", got)
	}
	if got := params.tint.value(state, params.defaultValue.Tint); got != (m.Color{R: 0.5}) {
		t.Fatalf("tint = %+v, want hover tint", got)
	}
}

func TestTextWithoutFontDrawsNothing(t *testing.T) {
	visual, payload := Text(TextParams{Text: "Label"})
	if got := visual.DefaultSize(canvas.LookupAccess{}, payload); got != (m.Vec2{}) {
		t.Fatalf("DefaultSize = %+v, want zero without a font", got)
	}
	queue := &canvas.OpQueue{}
	visual.Draw(canvas.LookupAccess{}, queue, State{Rect: Rect{Width: 80, Height: 20}}, payload)
	if queue.OpCount() != 0 {
		t.Fatalf("text op count = %d, want 0 without a font", queue.OpCount())
	}
}

func TestColorDrawsOnePanelOperation(t *testing.T) {
	visual, payload := Color(ColorParams{Color: m.Color{R: 0.2, A: 1}})
	if got := visual.DefaultSize(canvas.LookupAccess{}, payload); got != (m.Vec2{}) {
		t.Fatalf("DefaultSize = %+v, want zero", got)
	}
	queue := &canvas.OpQueue{}
	visual.Draw(canvas.LookupAccess{}, queue, State{VisualState: VisualHovered, Rect: Rect{Width: 20, Height: 10}}, payload)
	if queue.OpCount() != 1 {
		t.Fatalf("color op count = %d, want 1", queue.OpCount())
	}
}

func TestButtonOmitsIdentityWhenDisabledOrEmpty(t *testing.T) {
	if got := Button(ButtonParams{ID: "enabled"}).id; got != "enabled" {
		t.Fatalf("enabled button ID = %q, want enabled", got)
	}
	disabled := Button(ButtonParams{ID: "disabled", Disabled: true})
	if disabled.id != "disabled" || !disabled.addState.Has(VisualDisabled) {
		t.Fatalf("disabled button = ID %q state %v, want stable ID and disabled state", disabled.id, disabled.addState)
	}
	if got := Button(ButtonParams{}).id; got != "" {
		t.Fatalf("empty button ID = %q, want empty", got)
	}
}
