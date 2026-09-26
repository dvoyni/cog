package internal

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// sceneSkinnedLayout and the three beside it are the layout halves of the
// shader/layout pairs the engine ships, restated here rather than imported:
// scene and canvas both sit above gfx, so gfx cannot see their Go types, and a
// check gfx performs has to be exercised against the pairs gfx is actually
// handed. They mirror bundles/model/internal/vertexpack.go and bundles/canvas/internal/shader.go; the shader
// halves mirror bundles/model/internal/builtin/model/vertex.wgsl and bundles/canvas/internal/builtin/canvas.
//
// Scene ships two named layouts, and the standard one is the skinned one's
// first six rows - the same reslice scene itself makes, so the pair below
// cannot disagree about the six they share.
var (
	sceneSkinnedLayout = []descriptors.VertexAttr{
		descriptors.Attr(0, descriptors.Float32x3),  // position
		descriptors.Attr(12, descriptors.Unorm16x2), // normal  - oct32
		descriptors.Attr(16, descriptors.Uint32),    // tangent - oct 15/15 + handedness
		descriptors.Attr(20, descriptors.Unorm16x2), // uv0     - against the mesh record
		descriptors.Attr(24, descriptors.Unorm16x2), // uv1     - against the mesh record
		descriptors.Attr(28, descriptors.Unorm8x4),  // color
		descriptors.Attr(32, descriptors.Uint8x4),   // joints
		descriptors.Attr(36, descriptors.Unorm8x4),  // weights
	}
	sceneStandardLayout  = sceneSkinnedLayout[:6]
	canvasTriangleLayout = []descriptors.VertexAttr{
		descriptors.Attr(0, descriptors.Float32x2),  // position
		descriptors.Attr(8, descriptors.Float32x4),  // color
		descriptors.Attr(24, descriptors.Float32x2), // uv
	}
	canvasQuadLayout = []descriptors.VertexAttr{descriptors.Attr(0, descriptors.Float32x2)}
)

// sceneVertexIn is what SceneVertexIn declares under SCENE_SKIN; the no-skin
// variant is its first six.
var sceneVertexIn = []shader.ShaderVertexInput{
	{Name: "position", Location: 0, Kind: shader.VertexScalarFloat, Count: 3},
	{Name: "normal", Location: 1, Kind: shader.VertexScalarFloat, Count: 2},
	{Name: "tangent", Location: 2, Kind: shader.VertexScalarUint, Count: 1},
	{Name: "uv0", Location: 3, Kind: shader.VertexScalarFloat, Count: 2},
	{Name: "uv1", Location: 4, Kind: shader.VertexScalarFloat, Count: 2},
	{Name: "color", Location: 5, Kind: shader.VertexScalarFloat, Count: 4},
	{Name: "joints", Location: 6, Kind: shader.VertexScalarUint, Count: 4},
	{Name: "weights", Location: 7, Kind: shader.VertexScalarFloat, Count: 4},
}

func TestVertexInterfaceAcceptsEveryBundledShaderAndLayoutPair(t *testing.T) {
	for _, pair := range []struct {
		name   string
		inputs []shader.ShaderVertexInput
		attrs  []descriptors.VertexAttr
	}{
		{"scene skinned", sceneVertexIn, sceneSkinnedLayout},
		{"scene standard", sceneVertexIn[:6], sceneStandardLayout},
		// A skinned-layout mesh under the no-skin variant is the pairing a
		// plain-bound geometry's static sibling makes: six inputs declared
		// against eight attributes supplied, which is the direction that has to
		// stay legal.
		{"scene standard variant over a skinned mesh", sceneVertexIn[:6], sceneSkinnedLayout},
		{"canvas triangles", []shader.ShaderVertexInput{
			{Name: "position", Location: 0, Kind: shader.VertexScalarFloat, Count: 2},
			{Name: "color", Location: 1, Kind: shader.VertexScalarFloat, Count: 4},
			{Name: "uv", Location: 2, Kind: shader.VertexScalarFloat, Count: 2},
		}, canvasTriangleLayout},
		{"canvas quad", []shader.ShaderVertexInput{
			{Name: "quad", Location: 0, Kind: shader.VertexScalarFloat, Count: 2},
		}, canvasQuadLayout},
	} {
		t.Run(pair.name, func(t *testing.T) {
			if err := CheckVertexInterface("bundled", shader.ShaderLayout{VertexInputs: pair.inputs}, pair.attrs); err != nil {
				t.Fatalf("the bundled pair is refused: %v", err)
			}
		})
	}
}

func TestVertexInterfaceReportsAnInputNoAttributeSupplies(t *testing.T) {
	layout := shader.ShaderLayout{VertexInputs: []shader.ShaderVertexInput{
		{Name: "position", Location: 0, Kind: shader.VertexScalarFloat, Count: 3},
		{Name: "uv0", Location: 3, Kind: shader.VertexScalarFloat, Count: 2},
	}}
	err := CheckVertexInterface("mesh.wgsl", layout, []descriptors.VertexAttr{descriptors.Attr(0, descriptors.Float32x3)})
	var unsupplied shader.ErrVertexInputUnsupplied
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
	layout := shader.ShaderLayout{VertexInputs: []shader.ShaderVertexInput{
		{Name: "normal", Location: 1, Kind: shader.VertexScalarFloat, Count: 3},
	}}
	err := CheckVertexInterface("mesh.wgsl", layout, []descriptors.VertexAttr{descriptors.Attr(0, descriptors.Float32x3), descriptors.Attr(12, descriptors.Unorm16x2)})
	var mismatch shader.ErrVertexInputMismatch
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
		declared shader.ShaderVertexInput
		supplied descriptors.VertexAttr
	}{
		{"narrower shader", shader.ShaderVertexInput{Location: 0, Kind: shader.VertexScalarFloat, Count: 2}, descriptors.Attr(0, descriptors.Float32x4)},
		{"wider shader", shader.ShaderVertexInput{Location: 0, Kind: shader.VertexScalarFloat, Count: 4}, descriptors.Attr(0, descriptors.Float32x2)},
		{"unsigned over signed", shader.ShaderVertexInput{Location: 0, Kind: shader.VertexScalarUint, Count: 4}, descriptors.Attr(0, descriptors.Sint16x4)},
		{"float over integer", shader.ShaderVertexInput{Location: 0, Kind: shader.VertexScalarFloat, Count: 4}, descriptors.Attr(0, descriptors.Uint8x4)},
	} {
		t.Run(c.name, func(t *testing.T) {
			layout := shader.ShaderLayout{VertexInputs: []shader.ShaderVertexInput{c.declared}}
			err := CheckVertexInterface("mesh.wgsl", layout, []descriptors.VertexAttr{c.supplied, descriptors.Attr(16, descriptors.Float32x4)})
			var mismatch shader.ErrVertexInputMismatch
			if !errors.As(err, &mismatch) {
				t.Fatalf("err = %v, want ErrVertexInputMismatch", err)
			}
		})
	}
}

// A normalized format decodes to float and an integer format does not, so
// naming the stored bytes is not what the rule compares.
func TestVertexInterfaceAcceptsAnyFormatThatDecodesToTheDeclaredType(t *testing.T) {
	for _, typ := range []descriptors.VertexType{descriptors.Float32x4, descriptors.Float16x4, descriptors.Unorm8x4, descriptors.Snorm16x4, descriptors.Unorm1010102} {
		layout := shader.ShaderLayout{VertexInputs: []shader.ShaderVertexInput{
			{Name: "color", Location: 0, Kind: shader.VertexScalarFloat, Count: 4},
		}}
		if err := CheckVertexInterface("mesh.wgsl", layout, []descriptors.VertexAttr{descriptors.Attr(0, typ)}); err != nil {
			t.Fatalf("%v over vec4<f32> is refused: %v", typ, err)
		}
	}
}

func TestVertexInterfaceReportsAStrideThatIsNotAMultipleOfFour(t *testing.T) {
	// 30 bytes: legal on Vulkan, Apple-silicon Metal and D3D12, refused by
	// WebGPU, GLES and older Apple GPUs.
	attrs := []descriptors.VertexAttr{descriptors.Attr(0, descriptors.Float32x3), descriptors.Attr(12, descriptors.Unorm16x2), descriptors.Attr(16, descriptors.Uint8x2), descriptors.Attr(28, descriptors.Uint8x2)}
	err := CheckVertexInterface("mesh.wgsl", shader.ShaderLayout{}, attrs)
	var stride types.ErrVertexStrideAlignment
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
	if err := CheckVertexInterface("mesh.wgsl", shader.ShaderLayout{}, nil); err != nil {
		t.Fatalf("an empty layout is refused: %v", err)
	}
}

// pipelineErrFrames runs n frames through a plugin whose backend reports the
// given layout, drawing mesh each frame, and returns everything the kernel was
// told. It is the whole-plugin path rather than a translator call because what
// is under test is which error path a mismatch takes and how often it is taken.
func pipelineErrFrames(t *testing.T, backend *fakeBackend, mesh descriptors.MeshDescr, frames int) []error {
	t.Helper()
	p := newPlugin()
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	for range frames {
		w := recordList(t, k)
		w.Draw(mesh, testMaterial(), 1, 0, descriptors.MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
	}
	return reported
}

// oneInputBackend reflects a single vertex input over the fake backend's
// standard uniform layout, so a mesh can be paired against a shader that
// declares exactly one thing.
func oneInputBackend(input shader.ShaderVertexInput) *fakeBackend {
	layout := (&fakeBackend{}).ShaderLayout(0)
	layout.VertexInputs = []shader.ShaderVertexInput{input}
	return &fakeBackend{layout: &layout}
}

func TestADrawWhoseLayoutMissesAShaderInputIsDroppedAndReportedOnce(t *testing.T) {
	backend := oneInputBackend(shader.ShaderVertexInput{Name: "uv", Location: 2, Kind: shader.VertexScalarFloat, Count: 2})
	mesh := descriptors.Mesh(descriptors.BufferWithBytes(make([]byte, 3*28), true), types.TopologyTriangleList,
		descriptors.Attr(0, descriptors.Float32x3), descriptors.Attr(12, descriptors.Float32x4))

	reported := pipelineErrFrames(t, backend, mesh, 3)

	if len(reported) != 1 {
		t.Fatalf("three frames reported %d errors, want 1: %v", len(reported), reported)
	}
	var unsupplied shader.ErrVertexInputUnsupplied
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
	backend := oneInputBackend(shader.ShaderVertexInput{Name: "normal", Location: 1, Kind: shader.VertexScalarFloat, Count: 3})
	mesh := descriptors.Mesh(descriptors.BufferWithBytes(make([]byte, 3*16), true), types.TopologyTriangleList,
		descriptors.Attr(0, descriptors.Float32x3), descriptors.Attr(12, descriptors.Unorm16x2))

	reported := pipelineErrFrames(t, backend, mesh, 2)

	if len(reported) != 1 {
		t.Fatalf("two frames reported %d errors, want 1: %v", len(reported), reported)
	}
	var mismatch shader.ErrVertexInputMismatch
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
	backend := oneInputBackend(shader.ShaderVertexInput{Name: "position", Location: 0, Kind: shader.VertexScalarFloat, Count: 3})
	mesh := descriptors.Mesh(descriptors.BufferWithBytes(make([]byte, 3*28), true), types.TopologyTriangleList,
		descriptors.Attr(0, descriptors.Float32x3), descriptors.Attr(12, descriptors.Float32x4))

	reported := pipelineErrFrames(t, backend, mesh, 1)

	if len(reported) != 0 {
		t.Fatalf("the frame reported %v, want nothing", reported)
	}
	if len(backend.draws) != 1 {
		t.Fatalf("%d draws were encoded, want 1", len(backend.draws))
	}
}

func TestADrawWhoseStrideIsNotAMultipleOfFourIsDroppedAndReported(t *testing.T) {
	backend := oneInputBackend(shader.ShaderVertexInput{Name: "position", Location: 0, Kind: shader.VertexScalarFloat, Count: 3})
	// 12 + 12 + 4 + 2 = 30 bytes.
	mesh := descriptors.Mesh(descriptors.BufferWithBytes(make([]byte, 3*30), true), types.TopologyTriangleList,
		descriptors.Attr(0, descriptors.Float32x3), descriptors.Attr(12, descriptors.Float32x3), descriptors.Attr(24, descriptors.Unorm16x2), descriptors.Attr(28, descriptors.Uint8x2))

	reported := pipelineErrFrames(t, backend, mesh, 2)

	if len(reported) != 1 {
		t.Fatalf("two frames reported %d errors, want 1: %v", len(reported), reported)
	}
	var stride types.ErrVertexStrideAlignment
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
	refusal := errors.New("gogpu: pipeline layout rejected")
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
