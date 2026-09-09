package canvas

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

func triangle(x float32) [3]Vertex {
	return [3]Vertex{
		{Position: m.Vec2{X: x, Y: 0}, Color: m.Color{A: 1}},
		{Position: m.Vec2{X: x + 4, Y: 0}, Color: m.Color{A: 1}},
		{Position: m.Vec2{X: x, Y: 4}, Color: m.Color{A: 1}},
	}
}

func trianglesConfig() Config {
	return Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
}

// Naming a material used to force-flush unconditionally, so even two identical
// custom-material draws were two draws. One rule now serves both batchers: the
// material joins the key by fingerprint and the parameters join it by value.
func TestTrianglesSharingAMaterialAndValuesMerge(t *testing.T) {
	custom := gfx.MaterialWithState(gfx.ShaderWithText("fn customMark() {}"), gfx.StateOverlay2D)
	k, _, backend := testKernel(t, fstest.MapFS{}, trianglesConfig(), func(write *OpQueue) {
		first, second := triangle(0), triangle(8)
		write.DrawTriangles(0, first[:], &custom, gfx.FloatParam("amount", 0.5))
		write.DrawTriangles(0, second[:], &custom, gfx.FloatParam("amount", 0.5))
	})
	runFrame(k)
	if backend.draws != 1 {
		t.Fatalf("draws = %d, want the two identical custom-material draws merged", backend.draws)
	}
}

// A triangles draw parameter is per material by rule, not by shortcoming: a
// triangle batch is concatenated vertices with no instance index, so there is
// nothing to hang a per-item array on and two values really are two draws.
func TestTrianglesDifferingInAParameterValueSplit(t *testing.T) {
	custom := gfx.MaterialWithState(gfx.ShaderWithText("fn customMark() {}"), gfx.StateOverlay2D)
	k, _, backend := testKernel(t, fstest.MapFS{}, trianglesConfig(), func(write *OpQueue) {
		first, second := triangle(0), triangle(8)
		write.DrawTriangles(0, first[:], &custom, gfx.FloatParam("amount", 0.5))
		write.DrawTriangles(0, second[:], &custom, gfx.FloatParam("amount", 0.25))
	})
	runFrame(k)
	if backend.draws != 2 {
		t.Fatalf("draws = %d, want 2: differing parameter values are two materials", backend.draws)
	}
}

// An unrecognised parameter name used to be a reason to bail out of the batch.
// It is a key field now, so two draws that agree on it are one draw.
func TestTrianglesWithAnUnrecognisedParameterStillBatch(t *testing.T) {
	k, _, backend := testKernel(t, fstest.MapFS{}, trianglesConfig(), func(write *OpQueue) {
		first, second := triangle(0), triangle(8)
		write.DrawTriangles(0, first[:], nil, gfx.FloatParam("customValue", 3))
		write.DrawTriangles(0, second[:], nil, gfx.FloatParam("customValue", 3))
	})
	runFrame(k)
	if backend.draws != 1 {
		t.Fatalf("draws = %d, want 1", backend.draws)
	}
}

// SetLayerMaterial is applied at flush, not positionally: this records
// everything first and names the set afterwards, which is the caller the
// mechanism exists for - a navigator running after every screen's update
// handler.
func TestSetLayerMaterialReachesDrawsAlreadyRecorded(t *testing.T) {
	sprite := gfx.MaterialWithState(gfx.ShaderWithText("fn layerSpriteMark() {}"), gfx.StateOverlay2D)
	triangles := gfx.MaterialWithState(gfx.ShaderWithText("fn layerTrianglesMark() {}"), gfx.StateOverlay2D)
	k, _, backend := testKernel(t, fstest.MapFS{}, trianglesConfig(), func(write *OpQueue) {
		write.Sprite(0, "", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
		verts := triangle(0)
		write.DrawTriangles(0, verts[:], nil)
		// After everything is recorded, as the real caller runs.
		write.SetLayerMaterial(0, MaterialSet{Sprite: &sprite, Triangles: &triangles})
	})
	runFrame(k)
	sources := map[string]bool{}
	for i := range backend.pipelines {
		sources[strings.TrimSpace(backend.pipelineShader(i))] = true
	}
	if !sources["fn layerSpriteMark() {}"] || !sources["fn layerTrianglesMark() {}"] {
		t.Fatalf("pipeline shaders = %v, want the layer set's sprite and triangles materials", sources)
	}
}

// A nil slot keeps its built-in, so a set is an override rather than a
// whole-cloth requirement.
func TestANilSlotKeepsItsBuiltIn(t *testing.T) {
	sprite := gfx.MaterialWithState(gfx.ShaderWithText("fn layerSpriteMark() {}"), gfx.StateOverlay2D)
	k, _, backend := testKernel(t, fstest.MapFS{}, trianglesConfig(), func(write *OpQueue) {
		verts := triangle(0)
		write.DrawTriangles(0, verts[:], nil)
		write.SetLayerMaterial(0, MaterialSet{Sprite: &sprite})
	})
	runFrame(k)
	if len(backend.pipelines) != 1 {
		t.Fatalf("pipelines = %d, want 1", len(backend.pipelines))
	}
	if source := strings.TrimSpace(backend.pipelineShader(0)); source == "fn layerSpriteMark() {}" {
		t.Fatal("a triangles draw took the layer set's sprite slot")
	}
}

// A draw that names its own material has said what it wants, so it takes
// neither the scope's slot nor the scope's parameters: a scope's material and
// its parameters are one unit.
func TestADrawNamingItsOwnMaterialIgnoresTheLayerSet(t *testing.T) {
	own := gfx.MaterialWithState(gfx.ShaderWithText("fn ownMark() {}"), gfx.StateOverlay2D)
	layerSprite := gfx.MaterialWithState(gfx.ShaderWithText("fn layersMark() {}"), gfx.StateOverlay2D)
	k, _, backend := testKernel(t, fstest.MapFS{}, trianglesConfig(), func(write *OpQueue) {
		write.Sprite(0, "", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, &own)
		write.SetLayerMaterial(0, MaterialSet{
			Sprite: &layerSprite,
			Params: []gfx.ParameterDescr{gfx.FloatParam("customValue", 9)},
		})
	})
	runFrame(k)
	if len(backend.pipelines) != 1 || strings.TrimSpace(backend.pipelineShader(0)) != "fn ownMark() {}" {
		t.Fatalf("pipeline shaders = %v, want the draw's own material", backend.pipelines)
	}
	if got := floatAt(backend.drawParams[0], 180); got != 0 {
		t.Fatalf("the scope's parameter reached a draw that named its own material as %v", got)
	}
}

// The set's parameters are per batch, not per sprite: they reach the uniform
// block, which is what a custom material extends, rather than becoming a storage
// array the shader never declared.
func TestTheLayerSetsParametersAreAppliedPerBatch(t *testing.T) {
	sprite := gfx.MaterialWithState(gfx.ShaderWithText("fn layerSpriteMark() {}"), gfx.StateOverlay2D)
	k, errs := testKernelCapturing(t, fstest.MapFS{}, trianglesConfig(), func(write *OpQueue) {
		write.Sprite(0, "", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
		write.SetLayerMaterial(0, MaterialSet{
			Sprite: &sprite,
			Params: []gfx.ParameterDescr{gfx.FloatParam("customValue", 9)},
		})
	})
	runFrame(k)
	if len(*errs) != 0 {
		t.Fatalf("reported %v; a scope parameter is per batch and belongs in the uniform block", *errs)
	}
}

// The last call in a tick wins, with the values the set held at that moment: it
// clones into the queue's arena at call time, so mutating the material
// afterwards does not retroactively change what was recorded.
func TestTheLastLayerMaterialInATickWins(t *testing.T) {
	first := gfx.MaterialWithState(gfx.ShaderWithText("fn firstMark() {}"), gfx.StateOverlay2D)
	second := gfx.MaterialWithState(gfx.ShaderWithText("fn secondMark() {}"), gfx.StateOverlay2D)
	k, _, backend := testKernel(t, fstest.MapFS{}, trianglesConfig(), func(write *OpQueue) {
		write.SetLayerMaterial(0, MaterialSet{Sprite: &first})
		write.SetLayerMaterial(0, MaterialSet{Sprite: &second})
		write.Sprite(0, "", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
	})
	runFrame(k)
	if len(backend.pipelines) != 1 || strings.TrimSpace(backend.pipelineShader(0)) != "fn secondMark() {}" {
		t.Fatalf("pipeline shaders = %v, want the second set", backend.pipelines)
	}
}

// A layer set is per-frame state and is cleared with the rest of it, so a set
// named on one tick does not leak into the next.
func TestALayerMaterialDoesNotSurviveTheFrame(t *testing.T) {
	sprite := gfx.MaterialWithState(gfx.ShaderWithText("fn layerSpriteMark() {}"), gfx.StateOverlay2D)
	first := true
	k, _, backend := testKernel(t, fstest.MapFS{}, trianglesConfig(), func(write *OpQueue) {
		if first {
			write.SetLayerMaterial(0, MaterialSet{Sprite: &sprite})
			first = false
		}
		write.Sprite(0, "", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
	})
	runFrame(k)
	runFrame(k)
	if len(backend.pipelines) != 2 {
		t.Fatalf("pipelines = %d, want one per frame: the set applies to the first only", len(backend.pipelines))
	}
	if strings.TrimSpace(backend.pipelineShader(1)) == "fn layerSpriteMark() {}" {
		t.Fatal("the layer set survived into the next frame")
	}
}

// One name carried at two kinds would pack an array the shader strides through
// wrongly, and a wrong stride is a wrong picture with nothing reported. The
// element size is a key field for exactly that reason.
func TestOneArrayNameAtTwoKindsSplitsTheBatch(t *testing.T) {
	k, _, backend := testKernel(t, fstest.MapFS{}, trianglesConfig(), func(write *OpQueue) {
		write.Sprite(0, "", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil,
			gfx.FloatParam("wobble", 1))
		write.Sprite(0, "", SpriteTransform{Position: m.Vec2{X: 8}, Size: m.Vec2{X: 4, Y: 4}}, nil,
			gfx.VecParam("wobble", m.Vec4{X: 1}))
	})
	runFrame(k)
	if got := len(spriteInstances(backend)); got != 2 {
		t.Fatalf("draws = %d, want 2: a four-byte element and a sixteen-byte one cannot share an array", got)
	}
}
