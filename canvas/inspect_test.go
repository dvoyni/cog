package canvas

import (
	"github.com/dvoyni/cog/m"
	"testing"

	"github.com/dvoyni/cog/gfx"
)

type customInspectVertex struct {
	Position m.Vec2
}

func (customInspectVertex) VertexLayout() []gfx.VertexAttr {
	return []gfx.VertexAttr{gfx.Attr(0, gfx.Float32x2)}
}

func TestOpsReturnRecordedOperationsInFlushOrder(t *testing.T) {
	write := &OpQueue{}
	write.SetLayerTransform(2, m.Rect{Width: 200, Height: 100}, AspectOverlap)
	write.Text(5, "font.ttf", "score", TextDraw{Position: m.Vec2{X: 4}, Size: 12})
	write.SetClip(m.Rect{X: 1, Y: 2, Width: 3, Height: 4})
	write.Sprite(2, "hero.png", SpriteTransform{Scale: 2, FlipX: true, TileY: true},
		nil, gfx.ColorParam("tint", m.Color{R: 1, A: 1}))
	write.RemoveClip()
	write.DrawTriangles(2, []Vertex{
		{Position: m.Vec2{X: 1}}, {Position: m.Vec2{X: 2}}, {Position: m.Vec2{X: 3}},
	}, nil)

	ops := write.Ops(nil)
	if len(ops) != 3 {
		t.Fatalf("ops = %d, want 3", len(ops))
	}
	// Layer 2 flushes before layer 5, and recording order holds within a layer.
	if ops[0].Kind != OpSprite || ops[0].Layer != 2 || ops[0].Path != "hero.png" {
		t.Fatalf("first op = %+v, want the layer 2 sprite", ops[0])
	}
	if !ops[0].Transform.FlipX || !ops[0].Transform.TileY || ops[0].Transform.Scale != 2 {
		t.Fatalf("sprite transform = %+v, want the recorded flip, tiling and scale", ops[0].Transform)
	}
	if !ops[0].HasClip || ops[0].Clip != (m.Rect{X: 1, Y: 2, Width: 3, Height: 4}) {
		t.Fatalf("sprite clip = (%+v, %v), want the active clip", ops[0].Clip, ops[0].HasClip)
	}
	if tint, ok := ops[0].ColorParam("tint"); !ok || tint != (m.Color{R: 1, A: 1}) {
		t.Fatalf("sprite tint = (%+v, %v), want the recorded parameter", tint, ok)
	}
	if ops[1].Kind != OpTriangles || len(ops[1].Vertices) != 3 || ops[1].Vertices[2].Position.X != 3 {
		t.Fatalf("second op = %+v, want the recorded triangle list", ops[1])
	}
	if ops[1].HasClip {
		t.Fatal("triangles kept a removed clip")
	}
	if ops[2].Kind != OpText || ops[2].Layer != 5 || ops[2].Text != "score" || ops[2].FontPath != "font.ttf" {
		t.Fatalf("third op = %+v, want the layer 5 text", ops[2])
	}

	window, aspect, ok := write.LayerWindow(2)
	if !ok || window != (m.Rect{Width: 200, Height: 100}) || aspect != AspectOverlap {
		t.Fatalf("layer window = (%+v, %v, %v), want the transform set for layer 2", window, aspect, ok)
	}
	if _, _, ok := write.LayerWindow(5); ok {
		t.Fatal("layer without a transform reported one")
	}
}

func TestOpsOmitVerticesOfCustomVertexLayouts(t *testing.T) {
	write := &OpQueue{}
	write.DrawTriangles(0, []customInspectVertex{{}, {}, {}}, nil)

	ops := write.Ops(nil)
	if len(ops) != 1 || ops[0].Kind != OpTriangles {
		t.Fatalf("ops = %+v, want one triangle op", ops)
	}
	if ops[0].Vertices != nil {
		t.Fatalf("custom layout vertices = %+v, want none", ops[0].Vertices)
	}
}

func TestOpsAreEmptyAfterReset(t *testing.T) {
	write := &OpQueue{}
	write.Clear(0, m.Color{G: 1, A: 1})
	write.FillRect(1, m.Rect{Width: 10, Height: 10}, ShapeDraw{Color: m.Color{A: 1}})
	if color, ok := write.LayerClear(0); !ok || color != (m.Color{G: 1, A: 1}) {
		t.Fatalf("clear colour = (%+v, %v), want the recorded clear", color, ok)
	}

	write.Reset()
	if ops := write.Ops(nil); len(ops) != 0 {
		t.Fatalf("ops after reset = %+v, want none", ops)
	}
	if _, ok := write.LayerClear(0); ok {
		t.Fatal("clear colour survived a reset")
	}
}

// The recording-inspection surface has to report a texture-sourced sprite, or a
// recorder's tests cannot tell one from a FillRect: both carry an empty Path.
func TestOpsReportTheTextureASpriteAndACustomShapeSource(t *testing.T) {
	texture := gfx.TextureWithBytes(8, 4, gfx.FormatRGBA8, make([]byte, 8*4*4), true, false)
	white := m.Color{R: 1, G: 1, B: 1, A: 1}
	write := &OpQueue{}
	write.SpriteTexture(0, texture, SpriteTransform{Size: m.Vec2{X: 20, Y: 10}}, nil)
	write.DrawTexture(1, texture, []Vertex{
		{Position: m.Vec2{}, Color: white},
		{Position: m.Vec2{X: 4}, Color: white},
		{Position: m.Vec2{Y: 4}, Color: white},
	}, nil)
	write.FillRect(2, m.Rect{Width: 5, Height: 5}, ShapeDraw{Color: white})

	ops := write.Ops(nil)
	if len(ops) != 3 {
		t.Fatalf("ops = %d, want 3", len(ops))
	}
	if width, height := ops[0].Texture.Size(); ops[0].Kind != OpSprite || ops[0].Path != "" || width != 8 || height != 4 {
		t.Fatalf("sprite op = %+v, want the recorded texture and no path", ops[0])
	}
	if ops[1].Kind != OpTriangles || ops[1].Texture.ID() != texture.ID() {
		t.Fatalf("triangles op = %+v, want the texture DrawTexture bound", ops[1])
	}
	if width, height := ops[2].Texture.Size(); width != 0 || height != 0 {
		t.Fatalf("fill rect texture = %dx%d, want none: it names a path, not a texture", width, height)
	}
}

// SetLayerTarget is inspectable the way SetLayerTransform is, so a recorder can
// assert where a layer went without running the pipeline. A layer nobody gave a
// target reports none, which is the screen.
func TestLayerTargetReportsWhereALayerDraws(t *testing.T) {
	texture := gfx.TextureWithBytes(64, 32, gfx.FormatRGBA8Srgb, make([]byte, 64*32*4), true, false)
	target := gfx.TextureTarget(texture, 0, 0)
	write := &OpQueue{}
	write.SetLayerTarget(3, target)
	write.FillRect(3, m.Rect{Width: 5, Height: 5}, ShapeDraw{Color: m.Color{A: 1}})
	write.FillRect(4, m.Rect{Width: 5, Height: 5}, ShapeDraw{Color: m.Color{A: 1}})

	if got, ok := write.LayerTarget(3); !ok || got != target {
		t.Fatalf("layer 3 target = (%+v, %v), want the one it was given", got, ok)
	}
	if _, ok := write.LayerTarget(4); ok {
		t.Fatal("a layer without a target reported one")
	}

	write.Reset()
	if _, ok := write.LayerTarget(3); ok {
		t.Fatal("a layer target survived a reset; targets are frame-local")
	}
}
