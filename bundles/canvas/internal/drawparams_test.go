package internal

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// A material's shader is compiled, and its set created, the first time a batch
// draws with it, and never again: a steady-state frame compiles and creates
// nothing, which is what keeps the one expensive step off the hot path.
func TestAMaterialCompilesOnceAndItsSetIsCreatedOnce(t *testing.T) {
	custom := MaterialWithState(gfx.ShaderWithText("fn onceMark() {}"), gfx.StateOverlay2D())
	k, _, backend := testKernel(t, fstest.MapFS{}, trianglesConfig(), func(write *OpQueue) {
		write.Sprite(0, "", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, &custom)
		write.Sprite(1, "", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, &custom)
		write.Sprite(2, "", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
	})
	runFrame(k)
	compiles, shaders := backend.reflections, len(backend.shaderSources)
	if compiles != 2 || shaders != 2 {
		t.Fatalf("first frame compiled %d and created %d shaders, want the custom and the built-in sprite once each", compiles, shaders)
	}
	draws := backend.draws
	runFrame(k)
	runFrame(k)
	if backend.reflections != compiles || len(backend.shaderSources) != shaders {
		t.Fatalf("steady frames compiled %d and created %d more shaders, want none",
			backend.reflections-compiles, len(backend.shaderSources)-shaders)
	}
	if backend.draws != 3*draws {
		t.Fatalf("draws = %d over three frames, want %d each", backend.draws, draws)
	}
}

// A shader that does not compile is reported once, with the compile's own
// error, and every draw through it draws nothing - while the draws around it
// under other materials still draw.
func TestAMaterialWhoseShaderDoesNotCompileIsReportedOnceAndDrawsNothing(t *testing.T) {
	missing := MaterialWithState(gfx.ShaderWithResource("shaders/missing.wgsl"), gfx.StateOverlay2D())
	k, errs, backend := testKernelCapturing(t, fstest.MapFS{}, trianglesConfig(), func(write *OpQueue) {
		write.Sprite(0, "", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, &missing)
		write.Sprite(1, "", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
	})
	runFrame(k)
	runFrame(k)
	if len(*errs) != 1 || !strings.Contains((*errs)[0].Error(), `canvas: material shader "shaders/missing.wgsl"`) {
		t.Fatalf("reported %v, want the one failed compile, naming the shader", *errs)
	}
	if backend.draws != 2 {
		t.Fatalf("draws = %d over two frames, want the built-in's one a frame and none through the broken shader", backend.draws)
	}
}

// Every batch of one set sets the same bindings, so a batch never inherits what
// an earlier batch of the same material set in the same frame. An untextured
// triangle list after a textured one under the one built-in material samples
// white - the zero texture - rather than the texture before it.
func TestABatchDoesNotInheritABindingAnEarlierBatchSet(t *testing.T) {
	textured := gfx.TextureWithBytes(1, 1, gfx.FormatRGBA8, []byte{255, 0, 0, 255}, true, false)
	k, _, backend := testKernelGfx(t, fstest.MapFS{}, trianglesConfig(), func(write *OpQueue, _ *gfx.OpQueue) {
		first, second := triangle(0), triangle(8)
		write.DrawTriangles(0, first[:], nil, gfx.TextureParam(TextureSlot, textured))
		write.DrawTriangles(0, second[:], nil)
	})
	runFrame(k)
	if backend.draws != 2 {
		t.Fatalf("draws = %d, want the textured list and the plain one apart", backend.draws)
	}
	var first, second []gfx.TextureID
	for _, bound := range backend.textures {
		switch bound.draw {
		case 0:
			first = append(first, bound.id)
		case 1:
			second = append(second, bound.id)
		}
	}
	if len(first) != 1 || first[0] == 0 {
		t.Fatalf("the textured list bound %v, want its texture", first)
	}
	if len(second) != 1 || second[0] != 0 {
		t.Fatalf("the plain list bound %v, want white, the zero texture, not the list before it's", second)
	}
}

// One scope parameter list serves every family, so a parameter naming a
// binding the bound shader never declared is dropped without a report: a fade
// amount the texture shader has no binding for is not a mistake.
func TestAScopeParameterTheShaderDoesNotDeclareIsDroppedQuietly(t *testing.T) {
	k, _, backend := testKernel(t, fstest.MapFS{}, trianglesConfig(), func(write *OpQueue) {
		write.Sprite(0, "", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
		verts := triangle(0)
		write.DrawTriangles(0, verts[:], nil)
		write.SetMaterial(MaterialSet{Params: []gfx.ParameterDescr{gfx.FloatParam("declaredNowhere", 1)}})
	})
	runFrame(k)
	if backend.draws != 2 {
		t.Fatalf("draws = %d, want both families drawn", backend.draws)
	}
}
