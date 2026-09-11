package gfx

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/m"
)

// sceneStandardLayout and the three beside it are the layout halves of the four
// shader/layout pairs the engine ships, restated here rather than imported:
// scene and canvas both sit above gfx, so gfx cannot see their Go types, and a
// check gfx performs has to be exercised against the pairs gfx is actually
// handed. They mirror scene/mesh.go and canvas/shader.go; the shader halves
// mirror scene/builtin/scene/vertex.wgsl and canvas/builtin/canvas.
var (
	sceneStandardLayout = []VertexAttr{
		Attr(0, Float32x3),  // position
		Attr(12, Float32x3), // normal
		Attr(24, Float32x4), // tangent
		Attr(40, Float32x2), // uv0
		Attr(48, Float32x2), // uv1
		Attr(56, Unorm8x4),  // color
		Attr(60, Uint16x4),  // joints
		Attr(68, Float32x4), // weights
	}
	canvasTriangleLayout = []VertexAttr{
		Attr(0, Float32x2),  // position
		Attr(8, Float32x4),  // color
		Attr(24, Float32x2), // uv
	}
	canvasQuadLayout = []VertexAttr{Attr(0, Float32x2)}
)

// sceneVertexIn is what SceneVertexIn declares under SCENE_SKIN; the no-skin
// variant is its first six.
var sceneVertexIn = []ShaderVertexInput{
	{Name: "position", Location: 0, Kind: VertexScalarFloat, Count: 3},
	{Name: "normal", Location: 1, Kind: VertexScalarFloat, Count: 3},
	{Name: "tangent", Location: 2, Kind: VertexScalarFloat, Count: 4},
	{Name: "uv0", Location: 3, Kind: VertexScalarFloat, Count: 2},
	{Name: "uv1", Location: 4, Kind: VertexScalarFloat, Count: 2},
	{Name: "color", Location: 5, Kind: VertexScalarFloat, Count: 4},
	{Name: "joints", Location: 6, Kind: VertexScalarUint, Count: 4},
	{Name: "weights", Location: 7, Kind: VertexScalarFloat, Count: 4},
}

func TestVertexInterfaceAcceptsEveryBundledShaderAndLayoutPair(t *testing.T) {
	for _, pair := range []struct {
		name   string
		inputs []ShaderVertexInput
		attrs  []VertexAttr
	}{
		{"scene skinned", sceneVertexIn, sceneStandardLayout},
		// The no-skin variant declares six of the eight the layout supplies,
		// which is the direction that has to stay legal.
		{"scene unskinned", sceneVertexIn[:6], sceneStandardLayout},
		{"canvas triangles", []ShaderVertexInput{
			{Name: "position", Location: 0, Kind: VertexScalarFloat, Count: 2},
			{Name: "color", Location: 1, Kind: VertexScalarFloat, Count: 4},
			{Name: "uv", Location: 2, Kind: VertexScalarFloat, Count: 2},
		}, canvasTriangleLayout},
		{"canvas quad", []ShaderVertexInput{
			{Name: "quad", Location: 0, Kind: VertexScalarFloat, Count: 2},
		}, canvasQuadLayout},
	} {
		t.Run(pair.name, func(t *testing.T) {
			if err := CheckVertexInterface("bundled", ShaderLayout{VertexInputs: pair.inputs}, pair.attrs); err != nil {
				t.Fatalf("the bundled pair is refused: %v", err)
			}
		})
	}
}

func TestVertexInterfaceReportsAnInputNoAttributeSupplies(t *testing.T) {
	layout := ShaderLayout{VertexInputs: []ShaderVertexInput{
		{Name: "position", Location: 0, Kind: VertexScalarFloat, Count: 3},
		{Name: "uv0", Location: 3, Kind: VertexScalarFloat, Count: 2},
	}}
	err := CheckVertexInterface("mesh.wgsl", layout, []VertexAttr{Attr(0, Float32x3)})
	var unsupplied ErrVertexInputUnsupplied
	if !errors.As(err, &unsupplied) {
		t.Fatalf("err = %v, want ErrVertexInputUnsupplied", err)
	}
	if unsupplied.Location != 3 || unsupplied.Input != "uv0" || unsupplied.Declared != "vec2<f32>" {
		t.Fatalf("report = %+v, want the unsupplied uv0 at location 3", unsupplied)
	}
}

// The case ticket 5 turns a shipping demo shader into: a three-component float
// read over a two-component unorm, which WebGPU fills out with (x, y, 0) and
// shades as a plausible direction lying in the XY plane.
func TestVertexInterfaceReportsAVec3OverATwoComponentUnorm(t *testing.T) {
	layout := ShaderLayout{VertexInputs: []ShaderVertexInput{
		{Name: "normal", Location: 1, Kind: VertexScalarFloat, Count: 3},
	}}
	err := CheckVertexInterface("mesh.wgsl", layout, []VertexAttr{Attr(0, Float32x3), Attr(12, Unorm16x2)})
	var mismatch ErrVertexInputMismatch
	if !errors.As(err, &mismatch) {
		t.Fatalf("err = %v, want ErrVertexInputMismatch", err)
	}
	if mismatch.Declared != "vec3<f32>" || mismatch.Supplied != "vec2<f32>" || mismatch.Location != 1 {
		t.Fatalf("report = %+v, want vec3<f32> declared over a supplied vec2<f32>", mismatch)
	}
}

// Widening and narrowing are legal in WebGPU and refused here, which is the
// whole point: base-type compatibility is exactly what lets the case above
// through.
func TestVertexInterfaceRefusesTheWideningWebGpuAllows(t *testing.T) {
	for _, c := range []struct {
		name     string
		declared ShaderVertexInput
		supplied VertexAttr
	}{
		{"narrower shader", ShaderVertexInput{Location: 0, Kind: VertexScalarFloat, Count: 2}, Attr(0, Float32x4)},
		{"wider shader", ShaderVertexInput{Location: 0, Kind: VertexScalarFloat, Count: 4}, Attr(0, Float32x2)},
		{"unsigned over signed", ShaderVertexInput{Location: 0, Kind: VertexScalarUint, Count: 4}, Attr(0, Sint16x4)},
		{"float over integer", ShaderVertexInput{Location: 0, Kind: VertexScalarFloat, Count: 4}, Attr(0, Uint8x4)},
	} {
		t.Run(c.name, func(t *testing.T) {
			layout := ShaderLayout{VertexInputs: []ShaderVertexInput{c.declared}}
			err := CheckVertexInterface("mesh.wgsl", layout, []VertexAttr{c.supplied, Attr(16, Float32x4)})
			var mismatch ErrVertexInputMismatch
			if !errors.As(err, &mismatch) {
				t.Fatalf("err = %v, want ErrVertexInputMismatch", err)
			}
		})
	}
}

// A normalized format decodes to float and an integer format does not, so
// naming the stored bytes is not what the rule compares.
func TestVertexInterfaceAcceptsAnyFormatThatDecodesToTheDeclaredType(t *testing.T) {
	for _, typ := range []VertexType{Float32x4, Float16x4, Unorm8x4, Snorm16x4, Unorm1010102} {
		layout := ShaderLayout{VertexInputs: []ShaderVertexInput{
			{Name: "color", Location: 0, Kind: VertexScalarFloat, Count: 4},
		}}
		if err := CheckVertexInterface("mesh.wgsl", layout, []VertexAttr{Attr(0, typ)}); err != nil {
			t.Fatalf("%v over vec4<f32> is refused: %v", typ, err)
		}
	}
}

func TestVertexInterfaceReportsAStrideThatIsNotAMultipleOfFour(t *testing.T) {
	// 30 bytes: legal on Vulkan, Apple-silicon Metal and D3D12, refused by
	// WebGPU, GLES and older Apple GPUs.
	attrs := []VertexAttr{Attr(0, Float32x3), Attr(12, Unorm16x2), Attr(16, Uint8x2), Attr(28, Uint8x2)}
	err := CheckVertexInterface("mesh.wgsl", ShaderLayout{}, attrs)
	var stride ErrVertexStrideAlignment
	if !errors.As(err, &stride) {
		t.Fatalf("err = %v, want ErrVertexStrideAlignment", err)
	}
	if stride.Stride != 30 {
		t.Fatalf("reported stride = %d, want 30", stride.Stride)
	}
}

// A layout with no attributes has nothing to be misaligned about; the draw is
// already dropped upstream on a zero stride.
func TestVertexInterfaceAcceptsAnEmptyLayout(t *testing.T) {
	if err := CheckVertexInterface("mesh.wgsl", ShaderLayout{}, nil); err != nil {
		t.Fatalf("an empty layout is refused: %v", err)
	}
}

// pipelineErrFrames runs n frames through a plugin whose backend reports the
// given layout, drawing mesh each frame, and returns everything the kernel was
// told. It is the whole-plugin path rather than a translator call because what
// is under test is which error path a mismatch takes and how often it is taken.
func pipelineErrFrames(t *testing.T, backend *fakeBackend, mesh MeshDescr, frames int) []error {
	t.Helper()
	p := New()
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})
	for range frames {
		w := recordList(t, k)
		w.Draw(mesh, testMaterial(), MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
	}
	return reported
}

// oneInputBackend reflects a single vertex input over the fake backend's
// standard uniform layout, so a mesh can be paired against a shader that
// declares exactly one thing.
func oneInputBackend(input ShaderVertexInput) *fakeBackend {
	layout := (&fakeBackend{}).ShaderLayout(0)
	layout.VertexInputs = []ShaderVertexInput{input}
	return &fakeBackend{layout: &layout}
}

func TestADrawWhoseLayoutMissesAShaderInputIsDroppedAndReportedOnce(t *testing.T) {
	backend := oneInputBackend(ShaderVertexInput{Name: "uv", Location: 2, Kind: VertexScalarFloat, Count: 2})
	mesh := Mesh(BufferWithBytes(make([]byte, 3*28), true), TopologyTriangleList,
		Attr(0, Float32x3), Attr(12, Float32x4))

	reported := pipelineErrFrames(t, backend, mesh, 3)

	if len(reported) != 1 {
		t.Fatalf("three frames reported %d errors, want 1: %v", len(reported), reported)
	}
	var unsupplied ErrVertexInputUnsupplied
	if !errors.As(reported[0], &unsupplied) {
		t.Fatalf("reported %v, want ErrVertexInputUnsupplied", reported[0])
	}
	if backend.pipes != 0 {
		t.Errorf("the backend was asked for %d pipelines, want none", backend.pipes)
	}
	if len(backend.draws) != 0 {
		t.Errorf("%d draws were encoded, want none", len(backend.draws))
	}
}

// The headline case, end to end: a three-component float read over a
// two-component unorm. WebGPU would fill the third component with zero and
// shade it.
func TestADrawWhoseLayoutSuppliesTheWrongTypeIsDroppedAndReported(t *testing.T) {
	backend := oneInputBackend(ShaderVertexInput{Name: "normal", Location: 1, Kind: VertexScalarFloat, Count: 3})
	mesh := Mesh(BufferWithBytes(make([]byte, 3*16), true), TopologyTriangleList,
		Attr(0, Float32x3), Attr(12, Unorm16x2))

	reported := pipelineErrFrames(t, backend, mesh, 2)

	if len(reported) != 1 {
		t.Fatalf("two frames reported %d errors, want 1: %v", len(reported), reported)
	}
	var mismatch ErrVertexInputMismatch
	if !errors.As(reported[0], &mismatch) {
		t.Fatalf("reported %v, want ErrVertexInputMismatch", reported[0])
	}
	if mismatch.Declared != "vec3<f32>" || mismatch.Supplied != "vec2<f32>" {
		t.Errorf("report = %+v, want vec3<f32> over a supplied vec2<f32>", mismatch)
	}
	if len(backend.draws) != 0 {
		t.Errorf("%d draws were encoded, want none", len(backend.draws))
	}
}

// A layout supplying more than the shader reads is the common case - scene's
// no-skin variant declares six of the eight attributes its mesh carries - and
// it draws.
func TestADrawWhoseLayoutSuppliesMoreThanTheShaderReadsStillDraws(t *testing.T) {
	backend := oneInputBackend(ShaderVertexInput{Name: "position", Location: 0, Kind: VertexScalarFloat, Count: 3})
	mesh := Mesh(BufferWithBytes(make([]byte, 3*28), true), TopologyTriangleList,
		Attr(0, Float32x3), Attr(12, Float32x4))

	reported := pipelineErrFrames(t, backend, mesh, 1)

	if len(reported) != 0 {
		t.Fatalf("the frame reported %v, want nothing", reported)
	}
	if len(backend.draws) != 1 {
		t.Fatalf("%d draws were encoded, want 1", len(backend.draws))
	}
}

func TestADrawWhoseStrideIsNotAMultipleOfFourIsDroppedAndReported(t *testing.T) {
	backend := oneInputBackend(ShaderVertexInput{Name: "position", Location: 0, Kind: VertexScalarFloat, Count: 3})
	// 12 + 12 + 4 + 2 = 30 bytes.
	mesh := Mesh(BufferWithBytes(make([]byte, 3*30), true), TopologyTriangleList,
		Attr(0, Float32x3), Attr(12, Float32x3), Attr(24, Unorm16x2), Attr(28, Uint8x2))

	reported := pipelineErrFrames(t, backend, mesh, 2)

	if len(reported) != 1 {
		t.Fatalf("two frames reported %d errors, want 1: %v", len(reported), reported)
	}
	var stride ErrVertexStrideAlignment
	if !errors.As(reported[0], &stride) {
		t.Fatalf("reported %v, want ErrVertexStrideAlignment", reported[0])
	}
	if stride.Stride != 30 {
		t.Errorf("reported stride = %d, want 30", stride.Stride)
	}
	if len(backend.draws) != 0 {
		t.Errorf("%d draws were encoded, want none", len(backend.draws))
	}
}

// gfx used to read `id, err := backend.NewPipeline(...); if err != nil { return 0 }`,
// which made "gfx refused to build this" and "the backend refused to build
// this" the same silent event from the caller's seat.
func TestABackendPipelineFailureReachesTheCallerOnce(t *testing.T) {
	refusal := errors.New("wgpu: pipeline layout rejected")
	backend := &fakeBackend{pipelineErr: refusal}

	reported := pipelineErrFrames(t, backend, triangle(), 3)

	if len(reported) != 1 {
		t.Fatalf("three frames reported %d errors, want 1: %v", len(reported), reported)
	}
	var failed ErrPipelineFailed
	if !errors.As(reported[0], &failed) {
		t.Fatalf("reported %v, want ErrPipelineFailed", reported[0])
	}
	if !errors.Is(failed.Err, refusal) {
		t.Errorf("Err = %v, want the backend's own error unrewritten", failed.Err)
	}
	if len(backend.draws) != 0 {
		t.Errorf("%d draws were encoded, want none", len(backend.draws))
	}
}
