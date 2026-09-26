package internal

import (
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"
	"github.com/dvoyni/cog/slots/gfx/internal/shader"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// testState is the state a test set draws with when the test is not about
// state: alpha-blended, depth-tested and written.
var testState = types.DrawState{Blend: types.BlendAlpha, DepthCompare: types.CompareLess, DepthWrite: true}

// testShader compiles programSource through the engine's reflection port -
// whatever layout the attached fake answers with - and uploads it as a shader
// of its own.
func testShader(t testing.TB, k kernel.Executioner) types.ShaderID {
	t.Helper()
	compiled := compileShader(k, nil, shader.ShaderWithText(programSource))
	if compiled.Err != nil {
		t.Fatalf("compile the test shader: %v", compiled.Err)
	}
	var id types.ShaderID
	withShaders(k, func(k kernel.Kernel, q *ResourceQueue) {
		id = q.NewShader()
		q.UploadProgram(k, id, compiled.Program)
	})
	return id
}

// testSet creates a set of draw params on a shader of its own, drawn under
// testState.
func testSet(t testing.TB, k kernel.Executioner, params ...descriptors.ParameterDescr) descriptors.DrawParams {
	t.Helper()
	return testSetOn(t, k, testShader(t, k), testState, params...)
}

// testSetOn creates a set of draw params on shader under state.
func testSetOn(t testing.TB, k kernel.Executioner, shader types.ShaderID, state types.DrawState, params ...descriptors.ParameterDescr) descriptors.DrawParams {
	t.Helper()
	var set descriptors.DrawParams
	withShaders(k, func(k kernel.Kernel, q *ResourceQueue) {
		set = q.NewDrawParams(k, shader, state, params...)
	})
	return set
}

// drawSampling draws through sampler with texture bound as MainTexture for the
// rest of this frame. The texture goes in the frame's version rather than in a
// set, because a render target a frame samples is a temporary.
func drawSampling(q *OpQueue, ref descriptors.PassRef, sampler descriptors.DrawParams, texture descriptors.TextureDescr) {
	q.SetDrawParams(kernel.Kernel{}, sampler, descriptors.TextureParam("MainTexture", texture))
	q.Draw(ref, triangle(), sampler, 1, 0)
}
