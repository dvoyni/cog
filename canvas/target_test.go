package canvas

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// targetTestConfig is the smallest atlas the harness will accept. None of these
// tests put anything in it: a render target is not an atlas entry.
func targetTestConfig() Config {
	return Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
}

// quadVertices finds the six-vertex upload one texture draw produced and
// returns its positions and uvs, so a test can assert the rectangle canvas
// built rather than the picture a device would have drawn.
func quadVertices(t *testing.T, backend *testBackend) (positions, uvs []m.Vec2) {
	t.Helper()
	const stride = 32
	var data []byte
	for _, buffer := range backend.buffers {
		if buffer.kind == gfx.BufferVertex && len(buffer.data) == 6*stride {
			data = buffer.data
		}
	}
	if data == nil {
		t.Fatalf("no six-vertex upload among %d buffers", len(backend.buffers))
	}
	for i := 0; i < 6; i++ {
		base := i * stride
		positions = append(positions, m.Vec2{X: floatAt(data, base), Y: floatAt(data, base+4)})
		uvs = append(uvs, m.Vec2{X: floatAt(data, base+24), Y: floatAt(data, base+28)})
	}
	return positions, uvs
}

// Part 1 of the ticket: a canvas layer renders into a texture. Canvas mints
// nothing and names nothing - the app allocates the target and hands it over,
// exactly as a scene camera does, because minting a texture takes the gfx queue
// and a canvas recorder does not hold it.
func TestALayerWithATargetRendersIntoItsTextureRatherThanTheScreen(t *testing.T) {
	k, _, backend := testKernelGfx(t, fstest.MapFS{}, targetTestConfig(), func(write *OpQueue, gfxWrite *gfx.OpQueue) {
		target, _ := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		write.SetLayerTarget(1, target)
		write.FillRect(1, m.Rect{Width: 10, Height: 10}, m.Color{R: 1, A: 1})
		write.FillRect(2, m.Rect{Width: 10, Height: 10}, m.Color{G: 1, A: 1})
	})
	runFrame(k)

	if len(backend.passes) != 2 {
		t.Fatalf("GPU passes = %d, want the texture layer kept apart from the screen layer", len(backend.passes))
	}
	offscreen, screen := backend.passes[0], backend.passes[1]
	if offscreen.Screen || offscreen.NoColor || offscreen.Target == 0 {
		t.Errorf("first pass = %+v, want a texture attachment", offscreen)
	}
	if !screen.Screen {
		t.Errorf("second pass = %+v, want the screen", screen)
	}
}

// Depth is per attachment, and DepthAuto pools one texture per target size, so
// a run that inherited the previous run's depth would z-test against whatever
// the other target left behind. The clear and the discard therefore bracket
// every run, not just the frame's first and last layer.
func TestEveryTargetRunClearsAndDiscardsItsOwnDepth(t *testing.T) {
	k, _, backend := testKernelGfx(t, fstest.MapFS{}, targetTestConfig(), func(write *OpQueue, gfxWrite *gfx.OpQueue) {
		target, _ := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		write.FillRect(0, m.Rect{Width: 10, Height: 10}, m.Color{A: 1})
		write.SetLayerTarget(1, target)
		write.FillRect(1, m.Rect{Width: 10, Height: 10}, m.Color{R: 1, A: 1})
		write.FillRect(2, m.Rect{Width: 10, Height: 10}, m.Color{G: 1, A: 1})
	})
	runFrame(k)

	if len(backend.passes) != 3 {
		t.Fatalf("GPU passes = %d, want screen / texture / screen unmerged", len(backend.passes))
	}
	for i, pass := range backend.passes {
		if pass.DepthLoad != gfx.LoadClear || pass.DepthClear != 1 {
			t.Errorf("pass %d depth load = (%v, %v), want LoadClear at 1", i, pass.DepthLoad, pass.DepthClear)
		}
		if pass.DepthStore != gfx.StoreDiscard {
			t.Errorf("pass %d depth store = %v, want StoreDiscard", i, pass.DepthStore)
		}
	}
}

// A contiguous run of same-target layers still collapses: giving a layer a
// target does not cost the frame a pass per layer.
func TestLayersSharingATargetStillCollapseToOnePass(t *testing.T) {
	k, _, backend := testKernelGfx(t, fstest.MapFS{}, targetTestConfig(), func(write *OpQueue, gfxWrite *gfx.OpQueue) {
		target, _ := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		write.SetLayerTarget(1, target)
		write.SetLayerTarget(2, target)
		write.FillRect(1, m.Rect{Width: 10, Height: 10}, m.Color{R: 1, A: 1})
		write.FillRect(2, m.Rect{Width: 10, Height: 10}, m.Color{G: 1, A: 1})
	})
	runFrame(k)

	if len(backend.passes) != 1 {
		t.Fatalf("GPU passes = %d, want the two layers on one target merged into 1", len(backend.passes))
	}
}

// A frame that renders a canvas layer into a texture wants that texture cleared
// and the screen cleared too, which one frame-global clear cannot express. The
// clear belongs to the layer that names it.
func TestEveryLayerClearsItsOwnTarget(t *testing.T) {
	offscreenClear := m.Color{R: 1, A: 1}
	screenClear := m.Color{B: 1, A: 1}
	k, _, backend := testKernelGfx(t, fstest.MapFS{}, targetTestConfig(), func(write *OpQueue, gfxWrite *gfx.OpQueue) {
		target, _ := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		write.SetLayerTarget(0, target)
		write.Clear(0, offscreenClear)
		write.FillRect(0, m.Rect{Width: 10, Height: 10}, m.Color{G: 1, A: 1})
		write.Clear(1, screenClear)
		write.FillRect(1, m.Rect{Width: 10, Height: 10}, m.Color{G: 1, A: 1})
	})
	runFrame(k)

	if len(backend.passes) != 2 {
		t.Fatalf("GPU passes = %d, want one per target", len(backend.passes))
	}
	if backend.passes[0].Load != gfx.LoadClear || backend.passes[0].Clear != offscreenClear {
		t.Errorf("texture pass load = (%v, %+v), want the clear recorded on its layer",
			backend.passes[0].Load, backend.passes[0].Clear)
	}
	if backend.passes[1].Load != gfx.LoadClear || backend.passes[1].Clear != screenClear {
		t.Errorf("screen pass load = (%v, %+v), want the clear recorded on its layer",
			backend.passes[1].Load, backend.passes[1].Clear)
	}
}

// Everything in canvas measures against the viewport, and a texture-targeted
// layer is not on the viewport. Scene's precedent decides it: the target's size
// when it has one, the viewport otherwise.
func TestATextureTargetedLayerMeasuresAgainstItsTextureNotTheViewport(t *testing.T) {
	k, _, backend := testKernelGfx(t, fstest.MapFS{}, targetTestConfig(), func(write *OpQueue, gfxWrite *gfx.OpQueue) {
		target, _ := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		write.SetLayerTarget(0, target)
		write.FillRect(0, m.Rect{Width: 10, Height: 10}, m.Color{A: 1})
		write.FillRect(1, m.Rect{Width: 10, Height: 10}, m.Color{A: 1})
	})
	runFrame(k)

	if len(backend.drawParams) != 2 {
		t.Fatalf("draws = %d, want one per layer", len(backend.drawParams))
	}
	// canvasViewport sits at uniform offset 48 and carries the logical size a
	// draw converts to clip space against.
	if w, h := floatAt(backend.drawParams[0], 48), floatAt(backend.drawParams[0], 52); w != 64 || h != 32 {
		t.Errorf("texture layer viewport = (%v,%v), want the target's 64x32", w, h)
	}
	if w, h := floatAt(backend.drawParams[1], 48), floatAt(backend.drawParams[1], 52); w != 100 || h != 100 {
		t.Errorf("screen layer viewport = (%v,%v), want the app viewport's 100x100", w, h)
	}
}

// Part 2: a sprite sourced from a gfx texture rather than a resource path. It
// cannot join the atlas batch - that binding is a texture_2d_array and an
// arbitrary texture is a texture_2d - so it gets its own path, and its natural
// size is the texture's own.
func TestSpriteTextureSizesItselfFromTheTexture(t *testing.T) {
	k, _, backend := testKernelGfx(t, fstest.MapFS{}, targetTestConfig(), func(write *OpQueue, gfxWrite *gfx.OpQueue) {
		_, texture := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		write.SpriteTexture(0, texture, SpriteTransform{Position: m.Vec2{X: 5, Y: 7}}, nil)
	})
	runFrame(k)

	positions, uvs := quadVertices(t, backend)
	if positions[0] != (m.Vec2{X: 5, Y: 7}) || positions[2] != (m.Vec2{X: 69, Y: 39}) {
		t.Errorf("quad corners = %v and %v, want (5,7) to (69,39): the texture's own 64x32", positions[0], positions[2])
	}
	if uvs[0] != (m.Vec2{}) || uvs[2] != (m.Vec2{X: 1, Y: 1}) {
		t.Errorf("quad uvs = %v and %v, want the whole texture", uvs[0], uvs[2])
	}
}

// The finding the cameras demo had to hand-roll a shader around: canvas's
// built-in triangle shader runs every texel through the key-colour ramp, and
// the ramp is not an identity on a rendered image - it desaturates every dark,
// low-green pixel to grey, with no key colour that switches it off. A texture
// drawn as itself needs a material that samples and returns.
func TestATextureDrawDoesNotGoThroughTheKeyColourRamp(t *testing.T) {
	k, _, backend := testKernelGfx(t, fstest.MapFS{}, targetTestConfig(), func(write *OpQueue, gfxWrite *gfx.OpQueue) {
		_, texture := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		write.SpriteTexture(0, texture, SpriteTransform{}, nil)
	})
	runFrame(k)

	if len(backend.pipelines) != 1 {
		t.Fatalf("pipelines = %d, want 1", len(backend.pipelines))
	}
	source := backend.pipelineShader(0)
	if source == "" {
		t.Fatal("pipeline was built from a shader the backend never saw")
	}
	if strings.Contains(source, "keyColorRamp") {
		t.Error("a texture draw went through the key-colour ramp, which silently desaturates a rendered image")
	}
	if !strings.Contains(source, "texture_2d<f32>") {
		t.Error("texture shader does not bind a plain 2D texture")
	}
}

// Part 2's other half: an arbitrary shape sourcing the same texture, which is
// what a composited camera panel, a masked minimap or a curved display needs.
// DrawTriangles could always bind a texture; what it could not do was sample it
// without the ramp.
func TestDrawTextureBindsTheTextureToACustomShape(t *testing.T) {
	white := m.Color{R: 1, G: 1, B: 1, A: 1}
	k, _, backend := testKernelGfx(t, fstest.MapFS{}, targetTestConfig(), func(write *OpQueue, gfxWrite *gfx.OpQueue) {
		_, texture := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		write.DrawTexture(0, texture, []Vertex{
			{Position: m.Vec2{}, Color: white},
			{Position: m.Vec2{X: 40}, Color: white, UV: m.Vec2{X: 1}},
			{Position: m.Vec2{Y: 20}, Color: white, UV: m.Vec2{Y: 1}},
		}, nil)
	})
	runFrame(k)

	if backend.draws != 1 || len(backend.pipelines) != 1 {
		t.Fatalf("draws/pipelines = (%d,%d), want one textured triangle", backend.draws, len(backend.pipelines))
	}
	if strings.Contains(backend.pipelineShader(0), "keyColorRamp") {
		t.Error("DrawTexture went through the key-colour ramp")
	}
}

// Part 3 is interop, and it needs no code at all: the unit of exchange is a
// plain gfx texture, so the same handle a canvas layer rendered into is the one
// a later canvas layer samples. A scene material takes it through
// gfx.TextureParam the same way.
func TestATextureACanvasLayerRenderedIsSampledByALaterLayer(t *testing.T) {
	k, _, backend := testKernelGfx(t, fstest.MapFS{}, targetTestConfig(), func(write *OpQueue, gfxWrite *gfx.OpQueue) {
		target, texture := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		write.SetLayerTarget(0, target)
		write.Clear(0, m.Color{R: 1, A: 1})
		write.FillRect(0, m.Rect{Width: 10, Height: 10}, m.Color{G: 1, A: 1})
		write.SpriteTexture(1, texture, SpriteTransform{Size: m.Vec2{X: 50, Y: 50}}, nil)
	})
	runFrame(k)

	if len(backend.passes) != 2 {
		t.Fatalf("GPU passes = %d, want the offscreen layer then the screen one", len(backend.passes))
	}
	if backend.passes[0].Screen || backend.passes[0].Target == 0 {
		t.Errorf("first pass = %+v, want the texture", backend.passes[0])
	}
	if !backend.passes[1].Screen {
		t.Errorf("second pass = %+v, want the screen", backend.passes[1])
	}
	positions, _ := quadVertices(t, backend)
	if positions[2] != (m.Vec2{X: 50, Y: 50}) {
		t.Errorf("composited quad = %v, want the explicit 50x50 size", positions[2])
	}
}

// Skip, never substitute: a texture whose size nothing knows yet - a resource
// path that has not baked - has no natural size to draw at, and guessing one
// would put a wrongly-sized rectangle on screen rather than nothing.
func TestASpriteTextureOfUnknownSizeDrawsNothing(t *testing.T) {
	k, _, backend := testKernel(t, fstest.MapFS{}, targetTestConfig(), func(write *OpQueue) {
		write.SpriteTexture(0, gfx.TextureWithResource("unbaked.png"), SpriteTransform{}, nil)
	})
	runFrame(k)

	if backend.draws != 0 {
		t.Fatalf("draws = %d, want none: the texture has no size to be natural at", backend.draws)
	}
}

func TestTextureShaderParses(t *testing.T) {
	assertBuiltinShaderLowers(t, textureShaderPath)
}

// Recording order is the contract, and a texture sprite emits its own draw
// rather than joining either batcher. It therefore has to close both: a
// triangle batch still open behind it would flush after the sprite and land on
// top of what the caller recorded underneath.
func TestATextureSpriteDrawsAfterTheTrianglesRecordedBeforeIt(t *testing.T) {
	white := m.Color{R: 1, G: 1, B: 1, A: 1}
	k, _, backend := testKernelGfx(t, fstest.MapFS{}, targetTestConfig(), func(write *OpQueue, gfxWrite *gfx.OpQueue) {
		_, texture := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		write.DrawTriangles(0, []Vertex{
			{Position: m.Vec2{}, Color: white},
			{Position: m.Vec2{X: 4}, Color: white},
			{Position: m.Vec2{Y: 4}, Color: white},
		}, nil)
		write.SpriteTexture(0, texture, SpriteTransform{Size: m.Vec2{X: 20, Y: 10}}, nil)
	})
	runFrame(k)

	if backend.draws != 2 {
		t.Fatalf("draws = %d, want the triangle list then the sprite", backend.draws)
	}
	// The triangle list is three vertices and the sprite quad is six, so the
	// upload sizes say which draw went first.
	var sizes []int
	for _, buffer := range backend.buffers {
		if buffer.kind == gfx.BufferVertex {
			sizes = append(sizes, len(buffer.data)/32)
		}
	}
	if len(sizes) != 2 || sizes[0] != 3 || sizes[1] != 6 {
		t.Fatalf("vertex uploads = %v, want the 3-vertex list before the 6-vertex quad", sizes)
	}
}

