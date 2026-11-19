package canvas_test

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

func TestAConversionAnswersFalseBeforeItsTargetIsDrawnAndThePointsWorldPositionAfter(t *testing.T) {
	// A click 16 points right of the window's centre and 8 pixels right of
	// and below the texture's: at 2 points a unit in the window, from (100,
	// 50), and at 2 pixels a unit in the texture, 64×32 pixels fitting a
	// logical size 16 units square, from (-10, 0).
	h := canvastest.NewHarness(t)
	textures := &textureCamera{
		descriptor: render.TextureDescriptor{Label: "minimap", Width: 64, Height: 32, Format: render.TextureFormatRGBA8Unorm},
		camera:     canvas.Camera{Position: m.Vec2{X: -10}, Size: m.Vec2{X: 16, Y: 16}, Clear: true},
	}
	converter := &clickConverter{
		windowCamera: canvas.Camera{Position: m.Vec2{X: 100, Y: 50}, Zoom: 2, Clear: true},
		textures:     textures,
		click:        m.Vec2{X: 48, Y: 24},
	}
	h.Spawn(nil, nil, textures, converter)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	conversions := converter.convertedClicks()
	if len(conversions) != 2 {
		t.Fatalf("the converter converted in %d frames, want 2", len(conversions))
	}
	if got := conversions[0]; got.inWindow || got.inTexture {
		t.Errorf("the first frame, before either target was drawn, converted %+v, want false through both", got)
	}
	want := frameConversion{window: m.Vec2{X: 108, Y: 50}, texture: m.Vec2{X: -2, Y: 4}, inWindow: true, inTexture: true}
	if got := conversions[1]; got != want {
		t.Errorf("the second frame converted %+v, want %+v, through both targets as the first frame drew them", got, want)
	}
}
