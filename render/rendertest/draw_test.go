package rendertest_test

import (
	"errors"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/platform/filesystems"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

// drawOrder is the order of the command list the draw tests record into.
const drawOrder render.Order = 10

// The draw tests' shaders: plain.wgsl has one vertex and one fragment
// function, its vertex one reading a vec3<f32> at location 0; choice.wgsl
// has a second fragment function beside them; and depth.wgsl a vertex
// function alone.
const (
	plainDeclaration = `{
		"entryPoints": [{"name": "vertex_main", "stage": "vertex"}, {"name": "fragment_main", "stage": "fragment"}],
		"vertexInputs": {"vertex_main": [{"location": 0, "type": "vec3<f32>"}]}
	}`
	choiceDeclaration = `{
		"entryPoints": [{"name": "vertex_main", "stage": "vertex"}, {"name": "fragment_main", "stage": "fragment"}, {"name": "fragment_outline", "stage": "fragment"}],
		"vertexInputs": {"vertex_main": [{"location": 0, "type": "vec3<f32>"}]}
	}`
	depthDeclaration = `{
		"entryPoints": [{"name": "vertex_main", "stage": "vertex"}],
		"vertexInputs": {"vertex_main": [{"location": 0, "type": "vec3<f32>"}]}
	}`
)

// positionLayout is a vertex layout of one vec3<f32> at location 0.
var positionLayout = render.VertexLayout{Stride: 12, Attributes: []render.VertexAttribute{{Format: render.VertexFormatFloat32x3}}}

// drawScene is what the draw tests draw with, made in their first frame: a
// colour texture and a depth texture to draw into, a vertex buffer of four
// positions, an index buffer of six 16-bit indices, and a mesh of each:
// indexed, of both, and plain, of the vertices alone.
type drawScene struct {
	color, depth           render.Texture
	vertices, indices      render.Buffer
	indexedMesh, plainMesh render.Mesh
}

// makeDrawFiles answers the files of the draw tests' shaders, each beside
// its declaration.
func makeDrawFiles(t *testing.T) *filesystems.MemoryFS {
	files := filesystems.NewMemoryFS()
	for name, declaration := range map[string]string{"plain.wgsl": plainDeclaration, "choice.wgsl": choiceDeclaration, "depth.wgsl": depthDeclaration} {
		putFile(t, files, name, []byte("// "+name))
		putFile(t, files, name+".json", []byte(declaration))
	}
	return files
}

// makeDrawScene makes a drawScene through inventory.
func makeDrawScene(inventory render.Inventory) drawScene {
	scene := drawScene{
		color:    inventory.MakeTexture[byte](render.TextureDescriptor{Label: "colour", Width: 8, Height: 8, Format: render.TextureFormatRGBA8Unorm}, nil),
		depth:    inventory.MakeTexture[byte](render.TextureDescriptor{Label: "depth", Width: 8, Height: 8, Format: render.TextureFormatDepth32Float}, nil),
		vertices: inventory.MakeBuffer("vertices", render.BufferUsageFlagVertex, make([]float32, 12)),
		indices:  inventory.MakeBuffer("indices", render.BufferUsageFlagIndex, []uint16{0, 1, 2, 2, 1, 3}),
	}
	vertexBuffers := []render.VertexBuffer{{Range: render.BufferRange{Buffer: scene.vertices}, Layout: positionLayout}}
	scene.indexedMesh = inventory.MakeMesh(render.MeshDescriptor{Label: "quad", VertexBuffers: vertexBuffers, Indices: render.IndexBuffer{Range: render.BufferRange{Buffer: scene.indices}, Format: render.IndexFormatUint16}})
	scene.plainMesh = inventory.MakeMesh(render.MeshDescriptor{Label: "strip", VertexBuffers: vertexBuffers, Topology: render.PrimitiveTopologyTriangleStrip})
	return scene
}

// colorPass answers the descriptor of a pass labelled label into scene's
// colour and depth textures.
func (s drawScene) colorPass(label string) render.PassDescriptor {
	return render.PassDescriptor{Label: label, ColorTargets: []render.ColorTarget{{Texture: s.color, Clear: true}}, DepthStencil: render.DepthStencilTarget{Texture: s.depth, Clear: true, ClearDepth: 1}}
}

// depthPass answers the descriptor of a pass labelled label into scene's
// depth texture alone.
func (s drawScene) depthPass(label string) render.PassDescriptor {
	return render.PassDescriptor{Label: label, DepthStencil: render.DepthStencilTarget{Texture: s.depth, Clear: true, ClearDepth: 1}}
}

// recordPass records into frame, in a command list at drawOrder, a pass of
// descriptor whose draws draw records.
func recordPass(frame render.Frame, descriptor render.PassDescriptor, draw func(pass *render.Pass)) {
	list := frame.BeginCommandList("draws", drawOrder)
	pass := list.BeginPass(descriptor)
	draw(&pass)
	pass.End()
	list.End()
}

// spawnDrawHarness spawns h with the draw tests' shaders mounted, running
// duringFrame in each frame's recording task, and steps frameCount frames.
func spawnDrawHarness(t *testing.T, h *rendertest.Harness, frameCount int, duringFrame func(frameIndex int, inventory render.Inventory, frame render.Frame)) {
	t.Helper()
	h.Spawn(nil, &loaderPlugin{files: makeDrawFiles(t), duringFrame: duringFrame})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	for range frameCount {
		h.Step()
	}
}

// findPassDraws answers the draws of each pass labelled label h's reader
// read, in the order drawn, frame after frame.
func findPassDraws(h *rendertest.Harness, label string) [][]render.InspectedDraw {
	var draws [][]render.InspectedDraw
	for _, inspection := range h.Frames() {
		for _, list := range inspection.CommandLists {
			for _, pass := range list.Passes {
				if pass.Label == label {
					draws = append(draws, pass.Draws)
				}
			}
		}
	}
	return draws
}

// findPassCommands answers the commands of the first pass labelled label gfx
// recorded.
func findPassCommands(h *rendertest.Harness, label string) []recording.Command {
	for _, frame := range h.RecordedFrames() {
		for _, buffer := range readSubmittedBuffers(frame) {
			for _, command := range buffer.Commands {
				if descriptor, ok := command.Arguments.(gfx.RenderPassDescriptor); ok && descriptor.Label == label {
					return command.Commands
				}
			}
		}
	}
	return nil
}

// makeInstanceCounts answers the instance count of each of draws.
func makeInstanceCounts(draws []render.InspectedDraw) []uint32 {
	var instanceCounts []uint32
	for _, draw := range draws {
		instanceCounts = append(instanceCounts, draw.InstanceCount)
	}
	return instanceCounts
}

// makePlainMaterial answers the material a draw of shader, plain.wgsl,
// draws with when its material names no entry point and has the zero
// State.
func makePlainMaterial(shader render.Shader) render.InspectedMaterial {
	return render.InspectedMaterial{Shader: shader, VertexEntryPoint: "vertex_main", FragmentEntryPoint: "fragment_main"}
}

// findCreatedPipelines answers the descriptor of each render pipeline gfx
// made, in order.
func findCreatedPipelines(calls []recording.Call) []gfx.RenderPipelineDescriptor {
	var descriptors []gfx.RenderPipelineDescriptor
	for _, index := range findCallIndices(calls, recording.CallKindCreateRenderPipeline) {
		descriptors = append(descriptors, calls[index].Arguments.(recording.CreateRenderPipelineArguments).Descriptor)
	}
	return descriptors
}

// countReports answers how many reports of kind h's kernel logged.
func countReports(h *rendertest.Harness, kind render.ReportKind) int {
	count := 0
	for _, reported := range makeReportKinds(h) {
		if reported == kind {
			count++
		}
	}
	return count
}

func TestADrawRecordsItsPipelineBuffersAndCountsAndADrawOfTheSameKeyReusesThePipeline(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene drawScene
	var shader render.Shader
	spawnDrawHarness(t, h, 2, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makeDrawScene(inventory)
			shader = inventory.AcquireShader("plain.wgsl")
		}
		crate := inventory.MakeMaterial(render.MaterialDescriptor{Label: "crate", Shader: shader})
		barrel := inventory.MakeMaterial(render.MaterialDescriptor{Label: "barrel", Shader: shader})
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(crate)
			pass.Draw(scene.indexedMesh, render.DrawArgs{InstanceCount: 3, FirstInstance: 2})
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
			pass.SetMaterial(barrel)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
		inventory.ReleaseMaterial(crate)
		inventory.ReleaseMaterial(barrel)
	})

	if pipelines := findCreatedPipelines(h.Calls()); len(pipelines) != 1 {
		t.Errorf("render made %d pipelines over two frames of three draws of one key, from three materials, want one", len(pipelines))
	}
	bufferCalls := readBufferCalls(h.Calls())
	vertices, _ := bufferCalls.findCreated[recording.CreateBufferArguments](scene.vertices)
	indices, _ := bufferCalls.findCreated[recording.CreateBufferArguments](scene.indices)
	var kinds []recording.CommandKind
	for _, command := range findPassCommands(h, "scene") {
		kinds = append(kinds, command.Kind)
		switch arguments := command.Arguments.(type) {
		case recording.SetVertexBufferArguments:
			if arguments != (recording.SetVertexBufferArguments{Slot: 0, Buffer: vertices.Buffer}) {
				t.Errorf("a draw set vertex buffer %+v, want the vertices at slot 0 and offset 0", arguments)
			}
		case recording.SetIndexBufferArguments:
			if arguments != (recording.SetIndexBufferArguments{Buffer: indices.Buffer, Format: gfx.IndexFormatUint16}) {
				t.Errorf("a draw set index buffer %+v, want the 16-bit indices at offset 0", arguments)
			}
		}
	}
	drawKinds := []recording.CommandKind{recording.CommandKindSetVertexBuffer, recording.CommandKindSetIndexBuffer, recording.CommandKindDrawIndexed}
	if want := slices.Concat([]recording.CommandKind{recording.CommandKindSetPipeline}, drawKinds, drawKinds, drawKinds); !slices.Equal(kinds, want) {
		t.Errorf("the pass recorded %v, want %v: the pipeline set once, then each draw's buffers and draw", kinds, want)
	}
	material := makePlainMaterial(shader)
	want := []render.InspectedDraw{
		{Material: material, IndexCount: 6, InstanceCount: 3, FirstInstance: 2},
		{Material: material, IndexCount: 6, InstanceCount: 1},
		{Material: material, IndexCount: 6, InstanceCount: 1},
	}
	for frameIndex, draws := range findPassDraws(h, "scene") {
		if !reflect.DeepEqual(draws, want) {
			t.Errorf("frame %d: the reader read draws\n%+v\nwant\n%+v", frameIndex, draws, want)
		}
	}
}

func TestADrawOfAMeshWithNoIndicesDrawsTheVerticesItsRangesHoldAndOnlyAnIndexedStripNamesItsIndexFormat(t *testing.T) {
	h := rendertest.NewHarness(t)
	var shader render.Shader
	var scene drawScene
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeDrawScene(inventory)
		shader = inventory.AcquireShader("plain.wgsl")
		material := inventory.MakeMaterial(render.MaterialDescriptor{Shader: shader})
		// half is the last two of the four vertices, from offset 24, and
		// indexedStrip the quad's indices drawn as a strip.
		half := inventory.MakeMesh(render.MeshDescriptor{VertexBuffers: []render.VertexBuffer{{Range: render.BufferRange{Buffer: scene.vertices, Offset: 24}, Layout: positionLayout}}})
		indexedStrip := inventory.MakeMesh(render.MeshDescriptor{
			VertexBuffers: []render.VertexBuffer{{Range: render.BufferRange{Buffer: scene.vertices}, Layout: positionLayout}},
			Indices:       render.IndexBuffer{Range: render.BufferRange{Buffer: scene.indices}, Format: render.IndexFormatUint16},
			Topology:      render.PrimitiveTopologyTriangleStrip,
		})
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(material)
			pass.Draw(scene.plainMesh, render.DrawArgs{})
			pass.Draw(half, render.DrawArgs{})
			pass.Draw(indexedStrip, render.DrawArgs{})
		})
	})

	material := makePlainMaterial(shader)
	want := [][]render.InspectedDraw{{{Material: material, VertexCount: 4, InstanceCount: 1}, {Material: material, VertexCount: 2, InstanceCount: 1}, {Material: material, IndexCount: 6, InstanceCount: 1}}}
	if draws := findPassDraws(h, "scene"); !reflect.DeepEqual(draws, want) {
		t.Errorf("the reader read draws %+v, want %+v", draws, want)
	}
	vertices, _ := readBufferCalls(h.Calls()).findCreated[recording.CreateBufferArguments](scene.vertices)
	var vertexBuffers []recording.SetVertexBufferArguments
	for _, command := range findPassCommands(h, "scene") {
		if arguments, ok := command.Arguments.(recording.SetVertexBufferArguments); ok {
			vertexBuffers = append(vertexBuffers, arguments)
		}
	}
	if want := []recording.SetVertexBufferArguments{{Buffer: vertices.Buffer}, {Buffer: vertices.Buffer, Offset: 24}, {Buffer: vertices.Buffer}}; !slices.Equal(vertexBuffers, want) {
		t.Errorf("the draws set vertex buffers %+v, want %+v: the second at its range's offset", vertexBuffers, want)
	}
	var stripIndexFormats []gfx.IndexFormat
	for _, pipeline := range findCreatedPipelines(h.Calls()) {
		stripIndexFormats = append(stripIndexFormats, pipeline.Primitive.StripIndexFormat)
	}
	if want := []gfx.IndexFormat{gfx.IndexFormatUndefined, gfx.IndexFormatUndefined, gfx.IndexFormatUint16}; !slices.Equal(stripIndexFormats, want) {
		t.Errorf("the pipelines of a strip drawn with no indices, a list, and a strip drawn with 16-bit indices name strip index formats %v, want %v", stripIndexFormats, want)
	}
}

func TestRenderStatesZeroValueMakesWebGPUsDefaultPipelineStateAndTheReaderReadsAStateBack(t *testing.T) {
	// blended sets every field of a pass of one colour target and a depth
	// target to a value other than its default, but the alpha blend.
	blended := render.State{
		Depth: render.DepthState{Compare: render.CompareFunctionLess, Write: true, Bias: 2, BiasSlopeScale: 1.5, BiasClamp: 0.25},
		Stencil: render.StencilState{
			Front:         render.StencilFaceState{Compare: render.CompareFunctionEqual, FailOperation: render.StencilOperationZero, DepthFailOperation: render.StencilOperationInvert, PassOperation: render.StencilOperationReplace},
			Back:          render.StencilFaceState{Compare: render.CompareFunctionNotEqual, PassOperation: render.StencilOperationIncrementWrap},
			UnreadBits:    0x0F,
			UnwrittenBits: 0xF0,
		},
		CullMode:        render.CullModeBack,
		FrontFace:       render.FrontFaceClockwise,
		AlphaToCoverage: true,
	}
	blended.ColorTargets[0] = render.ColorTargetState{
		Blend:             render.BlendState{Color: render.BlendComponent{SourceFactor: render.BlendFactorSourceAlpha, DestinationFactor: render.BlendFactorOneMinusSourceAlpha, Operation: render.BlendOperationReverseSubtract}},
		UnwrittenChannels: render.ColorWriteFlagAlpha,
	}
	h := rendertest.NewHarness(t)
	var shader render.Shader
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene := makeDrawScene(inventory)
		shader = inventory.AcquireShader("plain.wgsl")
		plain := inventory.MakeMaterial(render.MaterialDescriptor{Shader: shader})
		blending := inventory.MakeMaterial(render.MaterialDescriptor{Shader: shader, State: blended})
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(plain)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
			pass.SetMaterial(blending)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
	})

	pipelines := findCreatedPipelines(h.Calls())
	if len(pipelines) != 2 {
		t.Fatalf("render made %d pipelines for two render states, want two", len(pipelines))
	}
	defaultFace := gfx.StencilFaceState{Compare: gfx.CompareFunctionAlways, FailOp: gfx.StencilOperationKeep, DepthFailOp: gfx.StencilOperationKeep, PassOp: gfx.StencilOperationKeep}
	wantDefault := pipelines[0]
	wantDefault.Primitive = gfx.PrimitiveState{}
	wantDefault.DepthStencil = gfx.DepthStencilState{Format: gfx.TextureFormatDepth32Float, DepthCompare: gfx.CompareFunctionAlways, StencilFront: defaultFace, StencilBack: defaultFace, StencilReadMask: ^uint32(0), StencilWriteMask: ^uint32(0)}
	wantDefault.Multisample = gfx.MultisampleState{Count: 1, Mask: ^uint64(0)}
	wantDefault.Fragment.Targets = []gfx.ColorTargetState{{Format: gfx.TextureFormatRGBA8Unorm, WriteMask: gfx.ColorWriteFlagAll}}
	if got := pipelines[0]; got.Primitive != wantDefault.Primitive || got.DepthStencil != wantDefault.DepthStencil || got.Multisample != wantDefault.Multisample || !slices.Equal(got.Fragment.Targets, wantDefault.Fragment.Targets) {
		t.Errorf("the zero State made a pipeline of\n%+v %+v %+v %+v\nwant WebGPU's defaults\n%+v %+v %+v %+v",
			got.Primitive, got.DepthStencil, got.Multisample, got.Fragment.Targets, wantDefault.Primitive, wantDefault.DepthStencil, wantDefault.Multisample, wantDefault.Fragment.Targets)
	}
	wantBlend := gfx.BlendState{
		Color: gfx.BlendComponent{SrcFactor: gfx.BlendFactorSrcAlpha, DstFactor: gfx.BlendFactorOneMinusSrcAlpha, Operation: gfx.BlendOperationReverseSubtract},
		Alpha: gfx.BlendComponent{SrcFactor: gfx.BlendFactorOne, DstFactor: gfx.BlendFactorZero, Operation: gfx.BlendOperationAdd},
	}
	if targets := pipelines[1].Fragment.Targets; len(targets) != 1 || targets[0].Blend != wantBlend || targets[0].WriteMask != gfx.ColorWriteFlagRed|gfx.ColorWriteFlagGreen|gfx.ColorWriteFlagBlue {
		t.Errorf("the blending state made colour targets %+v, want blending %+v, its alpha component's zero values WebGPU's defaults, writing every channel but alpha", targets, wantBlend)
	}
	draws := findPassDraws(h, "scene")
	if len(draws) != 1 || len(draws[0]) != 2 || draws[0][0].Material.State != (render.State{}) || draws[0][1].Material.State != blended {
		t.Errorf("the reader read draws %+v, want the zero state and then %+v", draws, blended)
	}
}

func TestAPipelineThatFailsIsReportedOnceAndItsDrawsSkippedWithoutRetryingUntilItsShaderIsFreed(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindPipelineFailed)
	var scene drawScene
	var shader render.Shader
	var integers render.Mesh
	spawnDrawHarness(t, h, 4, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		switch frameIndex {
		case 0:
			scene = makeDrawScene(inventory)
			// integers feeds location 0 a u32, which the shader reads as a
			// vec3<f32>.
			integers = inventory.MakeMesh(render.MeshDescriptor{VertexBuffers: []render.VertexBuffer{{Range: render.BufferRange{Buffer: scene.vertices}, Layout: render.VertexLayout{Stride: 4, Attributes: []render.VertexAttribute{{Format: render.VertexFormatUint32}}}}}})
			shader = inventory.AcquireShader("plain.wgsl")
		case 2:
			inventory.ReleaseShader(shader)
		case 3:
			shader = inventory.AcquireShader("plain.wgsl")
		}
		material := inventory.MakeMaterial(render.MaterialDescriptor{Shader: shader})
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(material)
			pass.Draw(integers, render.DrawArgs{})
			pass.Draw(integers, render.DrawArgs{})
		})
		inventory.ReleaseMaterial(material)
	})

	// Each try reflects the vertex inputs gfx checks the mesh's buffers
	// against, and gfx refuses it before the backend makes a pipeline.
	if count := len(findCallIndices(h.Calls(), recording.CallKindReflectVertexInputs)); count != 2 {
		t.Errorf("render tried %d pipelines over four frames of a pipeline gfx refuses, its shader freed after the third, want two: one for each shader", count)
	}
	if count := countReports(h, render.ReportKindPipelineFailed); count != 2 {
		t.Errorf("render reported %d failed pipelines, want two: one for each shader", count)
	}
	for frameIndex, draws := range findPassDraws(h, "scene") {
		if len(draws) != 0 {
			t.Errorf("frame %d drew %+v, want its draws skipped", frameIndex, draws)
		}
	}
	if reports := h.Reports(); len(reports) == 0 || !errors.Is(reports[0], gfx.ErrVertexMismatch) {
		t.Errorf("render reported %v, want gfx's ErrVertexMismatch", reports)
	}
}

func TestADrawNamingAFailedOrFreedBufferOrShaderIsSkippedAndReportedOnce(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindDrawSkipped)
	h.ExpectReport(render.ReportKindLoadFailed)
	var scene drawScene
	var failed, working render.Material
	var freed, past render.Mesh
	spawnDrawHarness(t, h, 3, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makeDrawScene(inventory)
			failed = inventory.MakeMaterial(render.MaterialDescriptor{Label: "failed", Shader: inventory.AcquireShader("missing.wgsl")})
			working = inventory.MakeMaterial(render.MaterialDescriptor{Label: "working", Shader: inventory.AcquireShader("plain.wgsl")})
			// freed is over a buffer released this frame, so freed when it
			// ends, and past over a range ending past its buffer's.
			freedBuffer := inventory.MakeBuffer("freed", render.BufferUsageFlagVertex, make([]float32, 12))
			freed = inventory.MakeMesh(render.MeshDescriptor{Label: "freed", VertexBuffers: []render.VertexBuffer{{Range: render.BufferRange{Buffer: freedBuffer}, Layout: positionLayout}}})
			past = inventory.MakeMesh(render.MeshDescriptor{Label: "past", VertexBuffers: []render.VertexBuffer{{Range: render.BufferRange{Buffer: scene.vertices, Offset: 40, Size: 12}, Layout: positionLayout}}})
			inventory.ReleaseBuffer(freedBuffer)
		}
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(failed)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
			pass.SetMaterial(working)
			pass.Draw(past, render.DrawArgs{})
			pass.Draw(past, render.DrawArgs{})
			pass.Draw(freed, render.DrawArgs{InstanceCount: 2})
			pass.Draw(freed, render.DrawArgs{InstanceCount: 2})
			pass.Draw(scene.plainMesh, render.DrawArgs{})
		})
	})

	if count := countReports(h, render.ReportKindDrawSkipped); count != 3 {
		t.Errorf("render reported %d skipped draws over three frames, want three: once for the material of the failed shader, once for the mesh past its buffer, and once for the mesh over the freed buffer", count)
	}
	draws := findPassDraws(h, "scene")
	wantInstanceCounts := [][]uint32{{2, 2, 1}, {1}, {1}}
	for frameIndex, frameDraws := range draws {
		if !slices.Equal(makeInstanceCounts(frameDraws), wantInstanceCounts[frameIndex]) {
			t.Errorf("frame %d drew %+v, want draws of %v instances: the mesh over the buffer freed at the first frame's end drawn in it alone", frameIndex, frameDraws, wantInstanceCounts[frameIndex])
		}
	}
	if len(draws) != 3 {
		t.Errorf("the reader read %d frames of the pass, want three", len(draws))
	}
}

func TestAnEmptyEntryPointPicksTheShadersOnlyFunctionAndAMissingOrAmbiguousOneIsRefusedAtMakeMaterial(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindMaterialRefused)
	var plain, choice render.Shader
	spawnDrawHarness(t, h, 2, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene := makeDrawScene(inventory)
		plain = inventory.AcquireShader("plain.wgsl")
		choice = inventory.AcquireShader("choice.wgsl")
		materials := []render.Material{
			inventory.MakeMaterial(render.MaterialDescriptor{Label: "only functions", Shader: plain}),
			inventory.MakeMaterial(render.MaterialDescriptor{Label: "outline named", Shader: choice, FragmentEntryPoint: "fragment_outline"}),
		}
		if frameIndex == 0 {
			materials = append(materials,
				inventory.MakeMaterial(render.MaterialDescriptor{Label: "missing vertex", Shader: plain, VertexEntryPoint: "vertex_skinned"}),
				inventory.MakeMaterial(render.MaterialDescriptor{Label: "fragment of the vertex stage", Shader: plain, FragmentEntryPoint: "vertex_main"}),
				inventory.MakeMaterial(render.MaterialDescriptor{Label: "ambiguous", Shader: choice}),
			)
		}
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			for _, material := range materials {
				pass.SetMaterial(material)
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
			}
		})
	})

	if count := countReports(h, render.ReportKindMaterialRefused); count != 3 {
		t.Errorf("render refused %d materials, want three: a vertex function the shader lacks, a fragment one of another stage, and none named of two", count)
	}
	if count := countReports(h, render.ReportKindDrawSkipped); count != 0 {
		t.Errorf("render reported %d skipped draws, want none: a refused material's draws are skipped as its refusal reported", count)
	}
	want := []render.InspectedDraw{
		{Material: makePlainMaterial(plain), IndexCount: 6, InstanceCount: 1},
		{Material: render.InspectedMaterial{Shader: choice, VertexEntryPoint: "vertex_main", FragmentEntryPoint: "fragment_outline"}, IndexCount: 6, InstanceCount: 1},
	}
	for frameIndex, draws := range findPassDraws(h, "scene") {
		if !reflect.DeepEqual(draws, want) {
			t.Errorf("frame %d drew\n%+v\nwant\n%+v", frameIndex, draws, want)
		}
	}
}

func TestEntryPointsJoinThePipelineKeyAndNotTheVariantsSoMaterialsOfTwoFunctionsShareOneVariant(t *testing.T) {
	h := rendertest.NewHarness(t)
	var shaders [2]render.Shader
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene := makeDrawScene(inventory)
		shaders[0] = inventory.AcquireShader("choice.wgsl")
		shaders[1] = inventory.AcquireShader("choice.wgsl")
		lit := inventory.MakeMaterial(render.MaterialDescriptor{Shader: shaders[0], FragmentEntryPoint: "fragment_main"})
		outline := inventory.MakeMaterial(render.MaterialDescriptor{Shader: shaders[1], FragmentEntryPoint: "fragment_outline"})
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(lit)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
			pass.SetMaterial(outline)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
	})

	if shaders[0] != shaders[1] {
		t.Errorf("two acquires of one file answered variants %v, want one", shaders)
	}
	if count := len(findCallIndices(h.Calls(), recording.CallKindCreateShaderModule)); count != 1 {
		t.Errorf("render made %d shader modules, want one for the variant both materials share", count)
	}
	var fragmentEntryPoints []string
	for _, pipeline := range findCreatedPipelines(h.Calls()) {
		fragmentEntryPoints = append(fragmentEntryPoints, pipeline.Fragment.EntryPoint)
	}
	if want := []string{"fragment_main", "fragment_outline"}; !slices.Equal(fragmentEntryPoints, want) {
		t.Errorf("render made pipelines of fragment entry points %q, want %q: one for each", fragmentEntryPoints, want)
	}
}

func TestAShaderWithNoFragmentStageDrawsOnlyIntoPassesWithoutColourTargets(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindPipelineFailed)
	var depth, plain render.Shader
	spawnDrawHarness(t, h, 2, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene := makeDrawScene(inventory)
		depth = inventory.AcquireShader("depth.wgsl")
		plain = inventory.AcquireShader("plain.wgsl")
		depthOnly := inventory.MakeMaterial(render.MaterialDescriptor{Shader: depth, State: render.State{Depth: render.DepthState{Compare: render.CompareFunctionLess, Write: true}}})
		alphaTested := inventory.MakeMaterial(render.MaterialDescriptor{Shader: plain})
		recordPass(frame, scene.depthPass("shadow map"), func(pass *render.Pass) {
			pass.SetMaterial(depthOnly)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
			pass.SetMaterial(alphaTested)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
		list := frame.BeginCommandList("colour", drawOrder+1)
		pass := list.BeginPass(scene.colorPass("colour"))
		pass.SetMaterial(depthOnly)
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.End()
		list.End()
	})

	pipelines := findCreatedPipelines(h.Calls())
	if len(pipelines) != 2 {
		t.Fatalf("render made %d pipelines, want two: the depth-only one and the alpha-tested one into the shadow map", len(pipelines))
	}
	if fragment := pipelines[0].Fragment; fragment.Module != (gfx.ShaderModule{}) {
		t.Errorf("the material of a shader with no @fragment made a pipeline of fragment stage %+v, want none", fragment)
	}
	if fragment := pipelines[1].Fragment; fragment.EntryPoint != "fragment_main" || len(fragment.Targets) != 0 {
		t.Errorf("a material with a fragment function drawn into a pass without colour targets made fragment stage %+v, want fragment_main with no targets", fragment)
	}
	if count := countReports(h, render.ReportKindPipelineFailed); count != 1 {
		t.Errorf("render reported %d failed pipelines, want one: the depth-only material drawn into colour targets, once", count)
	}
	shadowDraws := findPassDraws(h, "shadow map")
	colourDraws := findPassDraws(h, "colour")
	if len(shadowDraws) != 2 || len(colourDraws) != 2 || len(shadowDraws[0]) != 2 || len(shadowDraws[1]) != 2 || len(colourDraws[0]) != 0 || len(colourDraws[1]) != 0 {
		t.Fatalf("the frames drew %+v into the shadow map and %+v into the colour target, want both materials into the first and nothing into the second", shadowDraws, colourDraws)
	}
	if entryPoint := shadowDraws[0][0].Material.FragmentEntryPoint; entryPoint != "" {
		t.Errorf("the reader read the depth-only draw's fragment entry point %q, want none", entryPoint)
	}
}

func TestMeshesAndMaterialsAreFreedAtTheEndOfTheFrameTheirMakerReleasesThemAndPipelinesWithTheirShader(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindReleaseRefused)
	h.ExpectReport(render.ReportKindDrawSkipped)
	h.ExpectReport(render.ReportKindMeshRefused)
	var scene drawScene
	var plain render.Shader
	// released and kept are materials of one shader, held throughout, the
	// first released in the second frame; plainMaterial is of a shader
	// released then.
	var released, kept, plainMaterial render.Material
	spawnDrawHarness(t, h, 4, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		switch frameIndex {
		case 0:
			scene = makeDrawScene(inventory)
			choice := inventory.AcquireShader("choice.wgsl")
			plain = inventory.AcquireShader("plain.wgsl")
			released = inventory.MakeMaterial(render.MaterialDescriptor{Shader: choice, FragmentEntryPoint: "fragment_main"})
			kept = inventory.MakeMaterial(render.MaterialDescriptor{Shader: choice, FragmentEntryPoint: "fragment_outline"})
			plainMaterial = inventory.MakeMaterial(render.MaterialDescriptor{Shader: plain})
			// refused are meshes of an index buffer of no format, of a stride
			// no multiple of 4, and of an attribute no multiple of 4 into its
			// vertex; paired is one of a two-byte attribute two bytes in,
			// which is aligned.
			refused := map[string]render.MeshDescriptor{
				"an index buffer of no format":  {Indices: render.IndexBuffer{Range: render.BufferRange{Buffer: scene.indices}}},
				"a stride of 6":                 {VertexBuffers: []render.VertexBuffer{{Range: render.BufferRange{Buffer: scene.vertices}, Layout: render.VertexLayout{Stride: 6, Attributes: []render.VertexAttribute{{Format: render.VertexFormatUnorm8x2}}}}}},
				"a float attribute at offset 2": {VertexBuffers: []render.VertexBuffer{{Range: render.BufferRange{Buffer: scene.vertices}, Layout: render.VertexLayout{Stride: 16, Attributes: []render.VertexAttribute{{Format: render.VertexFormatFloat32x3, Offset: 2}}}}}},
			}
			for name, descriptor := range refused {
				if mesh := inventory.MakeMesh(descriptor); mesh != (render.Mesh{}) {
					t.Errorf("a mesh of %s answered %v, want the zero Mesh", name, mesh)
				}
			}
			paired := render.MeshDescriptor{VertexBuffers: []render.VertexBuffer{{Range: render.BufferRange{Buffer: scene.vertices}, Layout: render.VertexLayout{Stride: 4, Attributes: []render.VertexAttribute{{Format: render.VertexFormatUnorm8x2, Offset: 2}}}}}}
			if mesh := inventory.MakeMesh(paired); mesh == (render.Mesh{}) {
				t.Error("a mesh of a two-byte attribute two bytes into its vertex answered the zero Mesh, want one")
			}
		case 1:
			inventory.ReleaseMesh(scene.indexedMesh)
			inventory.ReleaseMaterial(released)
			inventory.ReleaseMesh(scene.indexedMesh)
			inventory.ReleaseMaterial(released)
			inventory.ReleaseShader(plain)
		}
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(kept)
			pass.Draw(scene.indexedMesh, render.DrawArgs{InstanceCount: 1})
			pass.Draw(scene.plainMesh, render.DrawArgs{InstanceCount: 2})
			pass.SetMaterial(released)
			pass.Draw(scene.plainMesh, render.DrawArgs{InstanceCount: 3})
			pass.SetMaterial(plainMaterial)
			pass.Draw(scene.plainMesh, render.DrawArgs{InstanceCount: 4})
		})
	})

	wantInstanceCounts := [][]uint32{{1, 2, 3, 4}, {1, 2, 3, 4}, {2}, {2}}
	for frameIndex, draws := range findPassDraws(h, "scene") {
		if !slices.Equal(makeInstanceCounts(draws), wantInstanceCounts[frameIndex]) {
			t.Errorf("frame %d drew %+v, want draws of %v instances: after the second frame released the indexed mesh, a material and a shader, only the draw naming none of them", frameIndex, draws, wantInstanceCounts[frameIndex])
		}
	}
	if count := countReports(h, render.ReportKindDrawSkipped); count != 3 {
		t.Errorf("render reported %d skipped draws over the two frames after the second, want three: the freed mesh, the freed material and the freed shader's material, each once", count)
	}
	if count := countReports(h, render.ReportKindReleaseRefused); count != 2 {
		t.Errorf("render refused %d releases, want two: a second release of the mesh and of the material", count)
	}
	if count := countReports(h, render.ReportKindMeshRefused); count != 3 {
		t.Errorf("render refused %d meshes, want three: of an index buffer of no format, of a stride of 6, and of a float attribute at offset 2", count)
	}
	pipelineReleases := findCallIndices(h.Calls(), recording.CallKindReleaseRenderPipeline)
	if frameIndex, freed := readShaderCalls(h.Calls()).findFreeingFrame(plain); !freed || frameIndex != 1 || len(pipelineReleases) != 1 {
		t.Errorf("the plain shader was freed at the end of frame %d, %v, with %d pipelines released, want frame 1 with its one pipeline", frameIndex, freed, len(pipelineReleases))
	}
}

func TestListsDrawingOneKeyAtOnceShareOnePipeline(t *testing.T) {
	const listCount = 4
	h := rendertest.NewHarness(t)
	// nextOrder hands each recording task an order of its own.
	var nextOrder atomic.Int32
	h.Spawn(nil, &loaderPlugin{files: makeDrawFiles(t), recordingTaskCount: listCount, duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene := makeDrawScene(inventory)
		material := inventory.MakeMaterial(render.MaterialDescriptor{Shader: inventory.AcquireShader("plain.wgsl")})
		list := frame.BeginCommandList("draws", drawOrder+render.Order(nextOrder.Add(1)))
		pass := list.BeginPass(scene.colorPass("scene"))
		pass.SetMaterial(material)
		for range 16 {
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		}
		pass.End()
		list.End()
	}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()

	if count := len(findCreatedPipelines(h.Calls())); count != 1 {
		t.Errorf("render made %d pipelines for %d lists drawing one key at once, want one", count, listCount)
	}
	draws := findPassDraws(h, "scene")
	if len(draws) != listCount {
		t.Fatalf("the reader read %d passes, want %d", len(draws), listCount)
	}
	for index, passDraws := range draws {
		if len(passDraws) != 16 {
			t.Errorf("pass %d drew %d times, want 16", index, len(passDraws))
		}
	}
}

func TestRenderStatesThatDrawAlikeInAPassShareOnePipeline(t *testing.T) {
	// spelt names WebGPU's defaults outright, and unusedTarget blends a colour
	// target the scene's pass lacks.
	spelt := render.State{
		Depth:   render.DepthState{Compare: render.CompareFunctionAlways},
		Stencil: render.StencilState{Front: render.StencilFaceState{Compare: render.CompareFunctionAlways, PassOperation: render.StencilOperationKeep}},
	}
	spelt.ColorTargets[0] = render.ColorTargetState{Blend: render.BlendState{Color: render.BlendComponent{SourceFactor: render.BlendFactorOne, DestinationFactor: render.BlendFactorZero, Operation: render.BlendOperationAdd}}}
	var unusedTarget render.State
	unusedTarget.ColorTargets[3] = render.ColorTargetState{UnwrittenChannels: render.ColorWriteFlagRed}
	// depthOnly tests depth, which a pass with no depth target does not.
	depthOnly := render.State{Depth: render.DepthState{Compare: render.CompareFunctionLess, Write: true}}
	h := rendertest.NewHarness(t)
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene := makeDrawScene(inventory)
		shader := inventory.AcquireShader("plain.wgsl")
		var materials []render.Material
		for _, state := range []render.State{{}, spelt, unusedTarget, depthOnly} {
			materials = append(materials, inventory.MakeMaterial(render.MaterialDescriptor{Shader: shader, State: state}))
		}
		draw := func(pass *render.Pass) {
			for _, material := range materials {
				pass.SetMaterial(material)
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
			}
		}
		recordPass(frame, scene.colorPass("scene"), draw)
		list := frame.BeginCommandList("colour alone", drawOrder+1)
		pass := list.BeginPass(render.PassDescriptor{Label: "colour alone", ColorTargets: []render.ColorTarget{{Texture: scene.color}}})
		draw(&pass)
		pass.End()
		list.End()
	})

	if count := len(findCreatedPipelines(h.Calls())); count != 3 {
		t.Errorf("render made %d pipelines, want three: one in the scene for the zero state, the one spelling its defaults and the one blending a target the pass lacks, one there for the depth test, and one in the pass of a colour target alone for all four", count)
	}
}

func TestAMeshMadeInTheSlotOfAFreedOneReportsItsOwnSkippedDraw(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindDrawSkipped)
	var meshes []render.Mesh
	var scene drawScene
	var material render.Material
	spawnDrawHarness(t, h, 2, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makeDrawScene(inventory)
			material = inventory.MakeMaterial(render.MaterialDescriptor{Shader: inventory.AcquireShader("plain.wgsl")})
		}
		// past is over a range ending past its buffer's, made and released
		// each frame, so the second frame's is made in the first's slot.
		past := inventory.MakeMesh(render.MeshDescriptor{Label: "past", VertexBuffers: []render.VertexBuffer{{Range: render.BufferRange{Buffer: scene.vertices, Offset: 40, Size: 12}, Layout: positionLayout}}})
		meshes = append(meshes, past)
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(material)
			pass.Draw(past, render.DrawArgs{})
		})
		inventory.ReleaseMesh(past)
	})

	if len(meshes) != 2 || meshes[0] == meshes[1] {
		t.Fatalf("the two frames made meshes %v, want two of their own", meshes)
	}
	if count := countReports(h, render.ReportKindDrawSkipped); count != 2 {
		t.Errorf("render reported %d skipped draws, want two: one for each mesh, the second made in the first's freed slot", count)
	}
}
