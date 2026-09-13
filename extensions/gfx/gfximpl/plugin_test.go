package gfximpl

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"math"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/extensions/gfx/internal"
	"github.com/dvoyni/cog/extensions/storage"
	"github.com/dvoyni/cog/extensions/storage/storageimpl"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// testMaterial builds a material with an inline (fake-compiled) shader.
func testMaterial(params ...gfx.ParameterDescr) gfx.MaterialDescr {
	return gfx.Material(gfx.ShaderWithText("//test"), params...)
}

// fakeBackend records the calls the translator makes and captures the last
// executed op stream so tests can assert the translation without a GPU.
type fakeBackend struct {
	nextID         uint32
	nextTex        uint32
	nextBuf        uint32
	shaders        int
	pipes          int
	textures       int
	samplers       int
	uploads        int
	shaderCode     []byte
	shaderErr      error
	pipelineErr    error
	shaderLabels   []string
	freedSamplers  []gpu.SamplerID
	freedShaders   []gpu.ShaderID
	freedPipelines []gpu.PipelineID
	lastPipelines  []gpu.PipelineDesc

	// lastOps is every call the last Execute's replay made, in replay order:
	// bakes, then each pass's render commands, then releases.
	lastOps    []backendOp
	lastPasses []gpu.PassDesc
	passDraws  []int
	views      [][3]int
	draws      []drawCall
	// indexBinds records every index buffer the pass bound and the width it was
	// bound at, which is the only place the width is observable: a draw call
	// carries a count, not a format.
	indexBinds    []indexBind
	execCount     int
	layout        *gpu.ShaderLayout
	presents      int
	presentAfter  int
	boundTextures []gpu.TextureID
	// transitions accumulate across the frame; emptyTransitions counts the
	// calls gfx promised never to make, so the promise is checked rather than
	// trusted.
	transitions      []placedTransition
	emptyTransitions int

	// Readback, modelled with the real backend's one frame of latency: a
	// capture encoded during frame N is handed back by the drain that follows
	// frame N+1's Execute, because that submit is what resolves its map.
	// captureDescs records every readback the translator asked for, and
	// captureAfter records how many passes had run when it did.
	captureDescs   []gpu.CaptureDesc
	captureAfter   int
	captureLabels  [][]string
	takeCalls      int
	captureResult  func(gpu.CaptureDesc) gpu.Capture
	capturePending *gpu.Capture
	captureReady   *gpu.Capture
}

// Capture records the readback and prepares its result for the drain after the
// next Execute.
func (b *fakeBackend) Capture(desc gpu.CaptureDesc) {
	b.captureDescs = append(b.captureDescs, desc)
	b.captureAfter = len(b.lastPasses)
	labels := make([]string, 0, len(b.lastPasses))
	for _, pass := range b.lastPasses {
		labels = append(labels, pass.Label)
	}
	b.captureLabels = append(b.captureLabels, labels)
	result := defaultCapture()
	if b.captureResult != nil {
		result = b.captureResult(desc)
	}
	b.capturePending = &result
}

func (b *fakeBackend) TakeCapture() (gpu.Capture, bool) {
	b.takeCalls++
	if b.captureReady == nil {
		return gpu.Capture{}, false
	}
	done := *b.captureReady
	b.captureReady = nil
	return done, true
}

// defaultCapture is a two-by-two image with padded rows, so that the ordinary
// path through a test still exercises the un-stride.
func defaultCapture() gpu.Capture {
	return paddedCapture(2, 2, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x * 60), G: uint8(y * 60), B: 7, A: 255}
	})
}

// paddedCapture builds what a backend hands back: rows padded to the GPU's own
// alignment, with the padding filled with a value that is not the picture, so
// a test can tell an un-stride from a straight copy.
func paddedCapture(width, height int, at func(x, y int) color.NRGBA) gpu.Capture {
	const alignment = 256
	rowBytes := (width*4 + alignment - 1) / alignment * alignment
	pixels := make([]byte, rowBytes*height)
	for i := range pixels {
		pixels[i] = 0xAB
	}
	for y := range height {
		for x := range width {
			texel := y*rowBytes + x*4
			c := at(x, y)
			pixels[texel], pixels[texel+1] = c.R, c.G
			pixels[texel+2], pixels[texel+3] = c.B, c.A
		}
	}
	return gpu.Capture{
		Pixels: pixels, Width: width, Height: height,
		Format: gpu.FrameBufferFormat, BytesPerRow: rowBytes,
	}
}

func (b *fakeBackend) id() uint32 { b.nextID++; return b.nextID }

func (b *fakeBackend) Ready() bool { return true }

func (b *fakeBackend) NewTexture() gpu.TextureID { b.nextTex++; return gpu.TextureID(b.nextTex) }
func (b *fakeBackend) NewBuffer() gpu.BufferID   { b.nextBuf++; return gpu.BufferID(b.nextBuf) }

func (b *fakeBackend) NewSampler(gpu.SamplerDesc) (gpu.SamplerID, error) {
	b.samplers++
	return gpu.SamplerID(b.id()), nil
}
func (b *fakeBackend) FreeSampler(id gpu.SamplerID) { b.freedSamplers = append(b.freedSamplers, id) }
func (b *fakeBackend) NewShader(desc gpu.ShaderDesc) (gpu.ShaderID, error) {
	if b.shaderErr != nil {
		return 0, b.shaderErr
	}
	b.shaders++
	b.shaderCode = append(b.shaderCode[:0], desc.Code...)
	b.shaderLabels = append(b.shaderLabels, desc.Label)
	return gpu.ShaderID(b.id()), nil
}
func (b *fakeBackend) FreeShader(id gpu.ShaderID) { b.freedShaders = append(b.freedShaders, id) }

// ShaderLayout reports a fixed layout matching the built-in shader: mvp at 0,
// a "tint" color at 64 (80-byte block), plus a texture+sampler in group 1.
func (b *fakeBackend) ShaderLayout(gpu.ShaderID) gpu.ShaderLayout {
	if b.layout != nil {
		return *b.layout
	}
	return gpu.ShaderLayout{
		UniformSize: 80, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []gpu.UniformMember{{Name: "mvp", Offset: 0}, {Name: "tint", Offset: 64}},
		Resources: []gpu.ShaderResource{
			{Name: "MainSampler", Sampler: true, Group: 1, Binding: 0},
			{Name: "MainTexture", Group: 1, Binding: 1},
		},
	}
}
func (b *fakeBackend) NewPipeline(desc gpu.PipelineDesc) (gpu.PipelineID, error) {
	if b.pipelineErr != nil {
		return 0, b.pipelineErr
	}
	b.pipes++
	b.lastPipelines = append(b.lastPipelines, desc)
	return gpu.PipelineID(b.id()), nil
}
func (b *fakeBackend) FreePipeline(id gpu.PipelineID) {
	b.freedPipelines = append(b.freedPipelines, id)
}
func (b *fakeBackend) ScreenFramebuffer() (gpu.TextureViewID, int, int) {
	return 1, 100, 100
}

// Limits reports what a desktop adapter typically allows, which is far above
// the web floor gfx measures shaders against.
func (b *fakeBackend) Limits() gpu.Limits {
	return gpu.Limits{
		MaxBindGroups:                   8,
		MaxStorageBuffersPerShaderStage: 200,
		MaxStorageBufferBindingSize:     1 << 31,
		MaxUniformBufferBindingSize:     1 << 20,
		MaxBufferSize:                   1 << 31,
	}
}

func (b *fakeBackend) TextureView(texture gpu.TextureID, mip, layer int) gpu.TextureViewID {
	b.views = append(b.views, [3]int{int(texture), mip, layer})
	return gpu.TextureViewID(len(b.views))
}

func (b *fakeBackend) Execute(queue *gpu.Queue) {
	b.execCount++
	// What this frame encodes resolves on the next frame's submit, so the
	// readback the previous frame armed is the one that becomes drainable now.
	b.captureReady, b.capturePending = b.capturePending, nil
	b.lastOps = b.lastOps[:0]
	b.lastPasses = b.lastPasses[:0]
	b.passDraws = b.passDraws[:0]
	queue.ReplayBakes(b)
	queue.ReplayPasses(b)
	queue.ReplayReleases(b)
}

// backendOpKind names which replayed call a backendOp records.
type backendOpKind uint8

const (
	opBakeBuffer backendOpKind = iota
	opBakeTexture
	opAllocateTexture
	opUpdateTexture
	opSetParams
	opSetTexture
	opSetSampler
	opSetBuffer
	opDraw
	opReleaseBuffer
	opReleaseTexture
)

// backendOp is one call a replayed queue made on the fake backend, with the
// arguments that call carried. Only the fields its kind passes are set.
type backendOp struct {
	kind                  backendOpKind
	texture               gpu.TextureID
	buffer                gpu.BufferID
	bufferKind            gpu.BufferKind
	format                gpu.TextureFormat
	width, height, layers int
	layer                 int
	region                gpu.Region
	renderable            bool
	group, binding        int
	offset, size          int
	first, count          int
	indexed               bool
	data                  []byte
}

func (b *fakeBackend) BakeBuffer(id gpu.BufferID, kind gpu.BufferKind, size int, data []byte) {
	b.lastOps = append(b.lastOps, backendOp{kind: opBakeBuffer, buffer: id, bufferKind: kind, size: size, data: data})
}

func (b *fakeBackend) BakeTexture(id gpu.TextureID, width, height int, format gpu.TextureFormat, pixels []byte, mipmaps bool) {
	b.textures++
	b.uploads++
	b.lastOps = append(b.lastOps, backendOp{
		kind: opBakeTexture, texture: id, width: width, height: height, format: format, data: pixels,
	})
}

func (b *fakeBackend) AllocateTexture(id gpu.TextureID, desc gpu.TextureDesc) {
	b.lastOps = append(b.lastOps, backendOp{
		kind: opAllocateTexture, texture: id, width: desc.Width, height: desc.Height, layers: desc.Layers,
		format: desc.Format, renderable: desc.Renderable,
	})
}

func (b *fakeBackend) UpdateTexture(id gpu.TextureID, layer int, region gpu.Region, pixels []byte) {
	b.lastOps = append(b.lastOps, backendOp{kind: opUpdateTexture, texture: id, layer: layer, region: region, data: pixels})
}

func (b *fakeBackend) ReleaseBuffer(id gpu.BufferID) {
	b.lastOps = append(b.lastOps, backendOp{kind: opReleaseBuffer, buffer: id})
}

func (b *fakeBackend) ReleaseTexture(id gpu.TextureID) {
	b.lastOps = append(b.lastOps, backendOp{kind: opReleaseTexture, texture: id})
}

// BeginPass records the pass and returns the backend itself as its RenderPass,
// which counts the draws that land in it.
func (b *fakeBackend) BeginPass(desc gpu.PassDesc) gpu.RenderPass {
	b.lastPasses = append(b.lastPasses, desc)
	b.passDraws = append(b.passDraws, 0)
	return b
}

func (b *fakeBackend) EndPass(gpu.RenderPass) {}

// TransitionTextures records each barrier against the pass it precedes, so a
// test can assert not just that a transition happened but that it happened
// before the pass whose hazard it fixes.
func (b *fakeBackend) TransitionTextures(transitions []gpu.TextureTransition) {
	if len(transitions) == 0 {
		b.emptyTransitions++
	}
	for _, transition := range transitions {
		b.transitions = append(b.transitions, placedTransition{
			TextureTransition: transition, beforePass: len(b.lastPasses),
		})
	}
}

// transitionBefore reports whether the transition was placed, and the index of
// the pass it was placed before.
func (b *fakeBackend) transitionBefore(want gpu.TextureTransition) (int, bool) {
	for _, placed := range b.transitions {
		if placed.TextureTransition == want {
			return placed.beforePass, true
		}
	}
	return -1, false
}

// placedTransition is one barrier and the pass it was recorded ahead of.
type placedTransition struct {
	gpu.TextureTransition
	beforePass int
}

// Present records the implicit present pass and how many declared passes had
// already run, so a test can assert both that it ran and that it ran last.
func (b *fakeBackend) Present() {
	b.presents++
	b.presentAfter = len(b.lastPasses)
}

func (b *fakeBackend) SetPipeline(gpu.PipelineID) {}
func (b *fakeBackend) SetParams(params []byte) {
	b.lastOps = append(b.lastOps, backendOp{kind: opSetParams, data: params})
}

// SetTexture records the binding so a test can assert which texture reached the
// GPU, which is the only way to tell a sampled render target from a draw that
// was silently dropped before it ever bound one.
func (b *fakeBackend) SetTexture(texture gpu.TextureID, group, binding int) {
	b.lastOps = append(b.lastOps, backendOp{kind: opSetTexture, texture: texture, group: group, binding: binding})
	b.boundTextures = append(b.boundTextures, texture)
}

// boundTexture reports whether the texture was bound at any point this frame.
func (b *fakeBackend) boundTexture(id gpu.TextureID) bool {
	return slices.Contains(b.boundTextures, id)
}
func (b *fakeBackend) SetSampler(_ gpu.SamplerID, group, binding int) {
	b.lastOps = append(b.lastOps, backendOp{kind: opSetSampler, group: group, binding: binding})
}
func (b *fakeBackend) SetVertexBuffer(gpu.BufferID, int) {}
func (b *fakeBackend) SetIndexBuffer(buffer gpu.BufferID, offset int, width gpu.IndexWidth) {
	b.indexBinds = append(b.indexBinds, indexBind{buffer: buffer, offset: offset, width: width})
}
func (b *fakeBackend) SetBuffer(group, binding int, buffer gpu.BufferID, offset, size int) {
	b.lastOps = append(b.lastOps, backendOp{
		kind: opSetBuffer, group: group, binding: binding, buffer: buffer, offset: offset, size: size,
	})
}
func (b *fakeBackend) Draw(first, count, instances, firstInstance int, indexed bool) {
	b.lastOps = append(b.lastOps, backendOp{kind: opDraw, first: first, count: count, indexed: indexed})
	if len(b.passDraws) > 0 {
		b.passDraws[len(b.passDraws)-1]++
	}
	b.draws = append(b.draws, drawCall{
		first: first, count: count, instances: instances, firstInstance: firstInstance, indexed: indexed,
	})
}

// indexBind is one recorded index-buffer binding.
type indexBind struct {
	buffer gpu.BufferID
	offset int
	width  gpu.IndexWidth
}

// drawCall is one recorded draw, so tests can assert on the arguments that
// reach the backend rather than on op encodings.
type drawCall struct {
	first, count, instances, firstInstance int
	indexed                                bool
}

// testPlugin registers recordCmd so tests can mutate the OpQueue under its
// write lock.
type testPlugin struct{}

type countingFS struct {
	fs.FS
	opens int
}

func (c *countingFS) Open(name string) (fs.File, error) {
	c.opens++
	return c.FS.Open(name)
}

func (testPlugin) Name() kernel.PluginName { return "gfxtest" }

// Name is the gfx plugin's, not this fixture's: the fixture locks gfx resources.
func (testPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{gfx.Name} }
func (testPlugin) Register(registrar *kernel.Registrar, _ any) error {
	adapter := &testAdapter{}
	registrar.ProvideAdapter[gpu.Backend](adapter)
	registrar.HandleCommand[attachBackendCmd](adapter.attachBackendCmdImpl)
	registrar.HandleCommand[recordCmd](recordCmdImpl)
	registrar.HandleCommand[recordResourcesCmd](recordResourcesCmdImpl)
	return nil
}

func recordCmdImpl() (kernel.Lock, kernel.Execute[recordRequest, recordResponse]) {
	var queue kernel.Write[*gfx.OpQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*gfx.OpQueue]()
		}, func(_ kernel.Kernel, req recordRequest) (recordResponse, error) {
			req.fn(queue.Get())
			return recordResponse{}, nil
		}
}

func recordResourcesCmdImpl() (kernel.Lock, kernel.Execute[recordResourcesRequest, recordResourcesResponse]) {
	var queue kernel.Write[*gfx.ResourceQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*gfx.ResourceQueue]()
		}, func(_ kernel.Kernel, req recordResourcesRequest) (recordResourcesResponse, error) {
			req.fn(queue.Get())
			return recordResourcesResponse{}, nil
		}
}
func newTestKernel(t *testing.T, p *plugin) kernel.Executioner {
	return newTestKernelWithFS(t, p, fstest.MapFS{})
}

func newTestKernelWithFS(t *testing.T, p *plugin, filesystem fs.FS) kernel.Executioner {
	t.Helper()
	return newTestKernelWith(t, p, filesystem, func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	})
}

// newTestKernelWithErrors builds a kernel whose reported errors are collected
// instead of failing the test, for the paths that report one on purpose. Its
// handler keeps the engine running, so a reported error does not end the frames
// that follow it.
func newTestKernelWithErrors(t *testing.T, p *plugin, report func(error)) kernel.Executioner {
	t.Helper()
	return newTestKernelWith(t, p, fstest.MapFS{}, func(err error) bool {
		report(err)
		return false
	})
}

func newTestKernelWith(t *testing.T, p *plugin, filesystem fs.FS, handler kernel.ErrorHandler) kernel.Executioner {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	config := map[kernel.PluginName]any{
		storage.Name: storageimpl.DefaultConfig().WithReadFS("test", 10, filesystem),
	}
	engine := kernel.New(config).Handler(handler).WithPlugins(storageimpl.New(), permanentAdapter{}, p, testPlugin{})
	go engine.Run(ctx)
	<-engine.Ready()
	return engine.Executioner()
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	image := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	image.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return encoded.Bytes()
}

func triangle() gfx.MeshDescr {
	const stride = 28 // vec3 position + vec4 color
	return gfx.Mesh(
		gfx.BufferWithBytes(make([]byte, 3*stride), true),
		gpu.TopologyTriangleList,
		gfx.Attr(0, gpu.Float32x3),
		gfx.Attr(12, gpu.Float32x4),
	)
}

func BenchmarkOpQueueDrawSteadyState(b *testing.B) {
	queue := testOpQueue(&fakeBackend{})
	mesh := triangle()
	material := testMaterial(
		gfx.ColorParam("tint", m.Color{R: 1, G: 1, B: 1, A: 1}),
		gfx.BufferParam("data", gfx.BufferWithBytes(make([]byte, 64), true)),
	)
	params := []gfx.ParameterDescr{
		gfx.MatParam("mvp", m.NewMat4()),
		gfx.FloatParam("time", 1),
	}

	queue.Draw(mesh, material, params...)
	queue.Reset()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		queue.Draw(mesh, material, params...)
		queue.Reset()
	}
}

func BenchmarkTranslateSteadyState(b *testing.B) {
	layout := gpu.ShaderLayout{
		UniformSize: 96, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []gpu.UniformMember{
			{Name: "mvp", Offset: 0},
			{Name: "tint", Offset: 64},
			{Name: "time", Offset: 80},
			{Name: "scale", Offset: 84},
		},
		Resources: []gpu.ShaderResource{
			{Name: "MainSampler", Sampler: true, Group: 1, Binding: 0},
			{Name: "MainTexture", Group: 1, Binding: 1},
			{Name: "Data", StorageBuffer: true, Group: 1, Binding: 2},
		},
	}
	backend := &fakeBackend{layout: &layout}
	translator := newTranslator()
	queue := testOpQueue(backend)
	mesh := gfx.Mesh(
		internal.BakedBuffer(1, 3*28),
		gpu.TopologyTriangleList,
		gfx.Attr(0, gpu.Float32x3), gfx.Attr(12, gpu.Float32x4),
	)
	material := testMaterial(
		gfx.ColorParam("tint", m.Color{R: 1, G: 1, B: 1, A: 1}),
		gfx.FloatParam("scale", 1),
		gfx.TextureParam("MainTexture", internal.BakedTexture(2, 0, 0)),
		gfx.SamplerParam("MainSampler", gpu.SamplerDesc{}),
		gfx.BufferParam("Data", internal.BakedBuffer(3, 64)),
	)
	for range 100 {
		queue.Draw(mesh, material,
			gfx.MatParam("mvp", m.NewMat4()),
			gfx.FloatParam("time", 1),
			gfx.ColorParam("tint", m.Color{R: 0.5, A: 1}),
		)
	}
	translator.translate(&queue, nil, backend, noFiles, gpu.CaptureDesc{}, false)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		translator.translate(&queue, nil, backend, noFiles, gpu.CaptureDesc{}, false)
	}
}

type benchmarkGpuSink struct{}

func (benchmarkGpuSink) BakeBuffer(gpu.BufferID, gpu.BufferKind, int, []byte)                 {}
func (benchmarkGpuSink) BakeTexture(gpu.TextureID, int, int, gpu.TextureFormat, []byte, bool) {}
func (benchmarkGpuSink) AllocateTexture(gpu.TextureID, gpu.TextureDesc)                       {}
func (benchmarkGpuSink) UpdateTexture(gpu.TextureID, int, gpu.Region, []byte)                 {}
func (benchmarkGpuSink) SetPipeline(gpu.PipelineID)                                           {}
func (benchmarkGpuSink) SetParams([]byte)                                                     {}
func (benchmarkGpuSink) SetTexture(gpu.TextureID, int, int)                                   {}

func (benchmarkGpuSink) SetSampler(gpu.SamplerID, int, int)               {}
func (benchmarkGpuSink) SetVertexBuffer(gpu.BufferID, int)                {}
func (benchmarkGpuSink) SetIndexBuffer(gpu.BufferID, int, gpu.IndexWidth) {}
func (benchmarkGpuSink) SetBuffer(int, int, gpu.BufferID, int, int)       {}
func (benchmarkGpuSink) Draw(int, int, int, int, bool)                    {}
func (benchmarkGpuSink) ReleaseBuffer(gpu.BufferID)                       {}
func (benchmarkGpuSink) ReleaseTexture(gpu.TextureID)                     {}

func (benchmarkGpuSink) BeginPass(gpu.PassDesc) gpu.RenderPass      { return benchmarkGpuSink{} }
func (benchmarkGpuSink) EndPass(gpu.RenderPass)                     {}
func (benchmarkGpuSink) TransitionTextures([]gpu.TextureTransition) {}
func (benchmarkGpuSink) Present()                                   {}
func (benchmarkGpuSink) Capture(gpu.CaptureDesc)                    {}

func BenchmarkGpuQueueReplaySteadyState(b *testing.B) {
	var queue gpu.Queue
	queue.Reset()
	queue.BeginPass(gpu.PassDesc{Screen: true, DepthAuto: true})
	for i := range 100 {
		queue.BakeBuffer(gpu.BufferID(i+1), gpu.BufferVertex, 64, []byte{1})
		queue.SetPipeline(1)
		queue.SetParams([]byte{1})
		queue.SetVertexBuffer(gpu.BufferID(i+1), 0)
		queue.Draw(0, 3, 1, 0, false)
		queue.ReleaseBuffer(gpu.BufferID(i + 1))
	}
	queue.EndPass()
	sink := benchmarkGpuSink{}
	b.ReportAllocs()
	for b.Loop() {
		queue.ReplayBakes(sink)
		queue.ReplayPasses(sink)
		queue.ReplayReleases(sink)
	}
}

func countOps(ops []backendOp, kind backendOpKind) int {
	n := 0
	for i := range ops {
		if ops[i].kind == kind {
			n++
		}
	}
	return n
}

func TestPassSelectionIsFrameLocalState(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	queue.Reset()
	if len(internal.OpQueuePasses(&queue)) != 0 || internal.OpQueueCurrent(&queue) != -1 {
		t.Fatalf("after reset: %d passes, current %d, want none declared or selected", len(internal.OpQueuePasses(&queue)), internal.OpQueueCurrent(&queue))
	}
	first := queue.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), Load: gpu.LoadClear})
	second := queue.Pass(gfx.PassDescr{Order: 1, Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto()})
	if internal.OpQueueSelectedPass(&queue) != 1 {
		t.Errorf("selected pass = %d, want the one just declared", internal.OpQueueSelectedPass(&queue))
	}
	queue.SetPass(first)
	if internal.OpQueueSelectedPass(&queue) != 0 {
		t.Errorf("selected pass = %d, want the re-selected first", internal.OpQueueSelectedPass(&queue))
	}
	// An unknown reference leaves the selection alone rather than guessing.
	queue.SetPass(gfx.PassRef(99))
	if internal.OpQueueSelectedPass(&queue) != 0 {
		t.Errorf("selected pass = %d, want the selection unchanged by an unknown ref", internal.OpQueueSelectedPass(&queue))
	}
	_ = second

	queue.Reset()
	if len(internal.OpQueuePasses(&queue)) != 0 || internal.OpQueueCurrent(&queue) != -1 {
		t.Errorf("after reset: %d passes, current %d, want none selected", len(internal.OpQueuePasses(&queue)), internal.OpQueueCurrent(&queue))
	}
}

func testOpQueue(backend gpu.Backend) gfx.OpQueue {
	return *internal.NewOpQueue(idsOf(backend))
}

// idsOf is the id source of a queue built outside a composition, which has no
// adapter handle to read.
func idsOf(backend internal.IDMinter) internal.IDSource {
	return func() internal.IDMinter { return backend }
}

func TestBakeOpsAllocateBakedResourceIDs(t *testing.T) {
	backend := &fakeBackend{}
	queue := *internal.NewResourceQueue(idsOf(backend))
	pixels := []byte{1, 2, 3, 4}
	buffer := queue.BakeBuffer(pixels, true)
	texture := queue.BakeTexture(1, 1, gpu.FormatRGBA8, pixels, true, false)
	rebakedBuffer := queue.ReBakeBuffer(buffer, pixels, true)
	rebakedTexture := queue.ReBakeTexture(texture, 1, 1, gpu.FormatRGBA8, pixels, true, false)

	if buffer.ID() == 0 || texture.ID() == 0 {
		t.Fatalf("baked handles = (%d, %d), want nonzero", buffer.ID(), texture.ID())
	}
	if rebakedBuffer.ID() != buffer.ID() || rebakedTexture.ID() != texture.ID() {
		t.Fatalf("rebaked handles = (%d, %d), want (%d, %d)", rebakedBuffer.ID(), rebakedTexture.ID(), buffer.ID(), texture.ID())
	}
	pixels[0] = 99
	for i := range internal.ResourceQueueOps(&queue) {
		if internal.ResourceQueueOps(&queue)[i].Bytes[0] != 1 {
			t.Fatalf("op %d did not copy caller data", i)
		}
	}
}

func TestBakeBufferCopyDataControlsOwnership(t *testing.T) {
	queue := *internal.NewResourceQueue(idsOf(&fakeBackend{}))
	copied := []byte{1, 2, 3, 4}
	borrowed := []byte{5, 6, 7, 8}
	queue.BakeBuffer(copied, true)
	queue.BakeBuffer(borrowed, false)

	copied[0] = 9
	borrowed[0] = 10
	if got := internal.ResourceQueueOps(&queue)[0].Bytes[0]; got != 1 {
		t.Fatalf("copied buffer byte = %d, want 1", got)
	}
	if got := internal.ResourceQueueOps(&queue)[1].Bytes[0]; got != 10 {
		t.Fatalf("borrowed buffer byte = %d, want 10", got)
	}
	retainedOps := internal.ResourceQueueOps(&queue)
	internal.ResourceQueueReset(&queue)
	if retainedOps[1].Bytes != nil {
		t.Fatal("reset retained borrowed buffer bytes")
	}
}

func TestBakeTextureCopyDataControlsOwnership(t *testing.T) {
	queue := *internal.NewResourceQueue(idsOf(&fakeBackend{}))
	copied := []byte{1, 2, 3, 4}
	borrowed := []byte{5, 6, 7, 8}
	queue.BakeTexture(1, 1, gpu.FormatRGBA8, copied, true, false)
	queue.BakeTexture(1, 1, gpu.FormatRGBA8, borrowed, false, false)

	copied[0] = 9
	borrowed[0] = 10
	if got := internal.ResourceQueueOps(&queue)[0].Bytes[0]; got != 1 {
		t.Fatalf("copied texture byte = %d, want 1", got)
	}
	if got := internal.ResourceQueueOps(&queue)[1].Bytes[0]; got != 10 {
		t.Fatalf("borrowed texture byte = %d, want 10", got)
	}
	retainedOps := internal.ResourceQueueOps(&queue)
	internal.ResourceQueueReset(&queue)
	if retainedOps[1].Bytes != nil {
		t.Fatal("reset retained borrowed texture pixels")
	}
}

func TestTextureArrayAllocationAndLayerUpdateTranslate(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	pixels := []byte{1, 2, 3, 4}
	withResourceQueue(t, k, func(resources *gfx.ResourceQueue) {
		texture := resources.AllocateTexture(64, 32, 4, gpu.FormatRGBA8)
		resources.UpdateTexture(texture, 2, gpu.Region{X: 5, Y: 7, Width: 1, Height: 1}, pixels, true)
	})
	pixels[0] = 9
	k.PublishEvent(app.RenderEvent{}).Wait()

	if got := countOps(backend.lastOps, opAllocateTexture); got != 1 {
		t.Fatalf("texture allocations = %d, want 1", got)
	}
	if got := countOps(backend.lastOps, opUpdateTexture); got != 1 {
		t.Fatalf("texture updates = %d, want 1", got)
	}
	for i := range backend.lastOps {
		op := &backend.lastOps[i]
		switch op.kind {
		case opAllocateTexture:
			if op.width != 64 || op.height != 32 || op.layers != 4 || op.format != gpu.FormatRGBA8 {
				t.Fatalf("allocation metadata = (%d,%d,%d,%d)", op.width, op.height, op.layers, op.format)
			}
		case opUpdateTexture:
			if op.layer != 2 || op.region != (gpu.Region{X: 5, Y: 7, Width: 1, Height: 1}) || op.data[0] != 1 {
				t.Fatalf("update metadata = layer/region/data (%d,%+v,%d)", op.layer, op.region, op.data[0])
			}
		}
	}
}

func TestPersistentResourceTextureSurvivesDroppedFrame(t *testing.T) {
	filesystem := &countingFS{FS: fstest.MapFS{
		"persistent.png": &fstest.MapFile{Data: testPNG(t)},
	}}
	p := newPlugin()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	for range 2 {
		w := recordList(t, k)
		w.Draw(triangle(), testMaterial(gfx.TextureParam("MainTexture", gfx.TextureWithResource("persistent.png"))), gfx.MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	}
	k.PublishEvent(app.RenderEvent{}).Wait()

	var baked, bound gpu.TextureID
	for i := range backend.lastOps {
		op := &backend.lastOps[i]
		switch op.kind {
		case opBakeTexture:
			baked = op.texture
		case opSetTexture:
			bound = op.texture
		}
	}
	if baked == 0 || bound != baked {
		t.Fatalf("persistent texture bake/binding = (%d, %d), want same nonzero ID", baked, bound)
	}
	if filesystem.opens != 1 || backend.uploads != 1 {
		t.Fatalf("resource opens/uploads = (%d, %d), want (1, 1)", filesystem.opens, backend.uploads)
	}
}

func TestPersistentBakeRebakeAndReleaseSurviveDroppedFrame(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	var buffer gfx.BufferDescr
	var texture gfx.TextureDescr
	withResourceQueue(t, k, func(resources *gfx.ResourceQueue) {
		buffer = resources.BakeBuffer([]byte{1, 2, 3, 4}, true)
		texture = resources.BakeTexture(1, 1, gpu.FormatRGBA8, []byte{1, 2, 3, 4}, true, false)
	})
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})

	withResourceQueue(t, k, func(resources *gfx.ResourceQueue) {
		resources.ReBakeBuffer(buffer, []byte{5, 6, 7, 8}, true)
		resources.ReBakeTexture(texture, 1, 1, gpu.FormatRGBA8, []byte{5, 6, 7, 8}, true, false)
		resources.ReleaseBuffer(buffer)
		resources.ReleaseTexture(texture)
	})
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if got := countOps(backend.lastOps, opBakeBuffer); got != 2 {
		t.Errorf("persistent buffer bakes = %d, want 2", got)
	}
	if got := countOps(backend.lastOps, opBakeTexture); got != 2 {
		t.Errorf("persistent texture bakes = %d, want 2", got)
	}
	if got := countOps(backend.lastOps, opReleaseBuffer); got != 1 {
		t.Errorf("persistent buffer releases = %d, want 1", got)
	}
	if got := countOps(backend.lastOps, opReleaseTexture); got != 1 {
		t.Errorf("persistent texture releases = %d, want 1", got)
	}
}

func TestDroppedFrameDiscardsTemporaryUploads(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	w := recordList(t, k)
	w.Draw(triangle(), testMaterial(gfx.TextureParam("MainTexture", gfx.TextureWithBytes(1, 1, gpu.FormatRGBA8, []byte{1, 2, 3, 4}, false, false))), gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	w = recordList(t, k)
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if countOps(backend.lastOps, opBakeTexture) != 0 || countOps(backend.lastOps, opBakeBuffer) != 0 {
		t.Fatal("dropped frame retained temporary texture or geometry uploads")
	}
}

func TestOpQueueTemporaryBufferPool(t *testing.T) {
	backend := &fakeBackend{}
	queue := testOpQueue(backend)

	small := []byte{1, 2, 3, 4}
	large := make([]byte, 16)
	smallBuffer := internal.OpQueueTemporaryBuffer(&queue, gpu.BufferVertex, small, true)
	largeBuffer := internal.OpQueueTemporaryBuffer(&queue, gpu.BufferVertex, large, true)
	small[0] = 99
	if internal.OpQueueOps(&queue)[0].Bytes[0] != 1 {
		t.Fatal("temporary bake op aliases caller data")
	}

	queue.Reset()
	fit := internal.OpQueueTemporaryBuffer(&queue, gpu.BufferVertex, make([]byte, 12), true)
	if fit.ID() != largeBuffer.ID() {
		t.Errorf("best-fit buffer = %d, want %d", fit.ID(), largeBuffer.ID())
	}
	queue.Reset()
	resized := internal.OpQueueTemporaryBuffer(&queue, gpu.BufferVertex, make([]byte, 32), true)
	if resized.ID() != largeBuffer.ID() {
		t.Errorf("resized buffer ID = %d, want reused %d", resized.ID(), largeBuffer.ID())
	}
	if internal.OpQueueTemporaryBuffers(&queue)[1].Size < 32 {
		t.Errorf("resized size = %d, want at least 32", internal.OpQueueTemporaryBuffers(&queue)[1].Size)
	}
	if smallBuffer.ID() == largeBuffer.ID() {
		t.Fatal("simultaneously used temporary buffers share an ID")
	}
}

func TestDrawStoresTemporaryBufferIDsWithoutInlineGeometry(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	mesh := triangle()
	queue.Draw(mesh, testMaterial(), gfx.MatParam("mvp", m.NewMat4()))

	if internal.OpQueueOps(&queue)[0].Kind != internal.OpBakeBuffer || internal.OpQueueOps(&queue)[0].BufferKind != gpu.BufferVertex {
		t.Fatal("draw did not populate a vertex bake op first")
	}
	draw := &internal.OpQueueOps(&queue)[1]
	if internal.MeshVertices(&draw.Mesh).ID() == 0 || draw.Mesh.VertexCount() != 3 {
		t.Fatalf("draw vertex resource = (%d, %d), want nonzero ID and 3 vertices", internal.MeshVertices(&draw.Mesh).ID(), draw.Mesh.VertexCount())
	}
	if len(internal.BufferBytes(internal.MeshVerticesRef(&draw.Mesh))) != 0 {
		t.Fatalf("draw retained %d inline vertex bytes", len(internal.BufferBytes(internal.MeshVerticesRef(&draw.Mesh))))
	}
	if len(internal.OpQueueTemporaryBuffers(&queue)) != 1 || !internal.OpQueueTemporaryBuffers(&queue)[0].Used {
		t.Fatal("draw did not lease one temporary vertex buffer")
	}
}

func TestOpQueueArenasPreserveCallerDataIsolation(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	vertices := make([]byte, 3*28)
	vertices[0] = 1
	layout := []gfx.VertexAttr{gfx.Attr(0, gpu.Float32x3), gfx.Attr(12, gpu.Float32x4)}
	materialParams := []gfx.ParameterDescr{gfx.ColorParam("tint", m.Color{R: 1})}
	drawParams := []gfx.ParameterDescr{gfx.FloatParam("time", 1)}

	queue.Draw(
		gfx.Mesh(gfx.BufferWithBytes(vertices, true), gpu.TopologyTriangleList, layout...),
		gfx.Material(gfx.ShaderWithText("//test"), materialParams...),
		drawParams...,
	)
	vertices[0] = 9
	layout[0] = gfx.Attr(4, gpu.Float32x2)
	materialParams[0] = gfx.ColorParam("tint", m.Color{G: 1})
	drawParams[0] = gfx.FloatParam("time", 9)

	draw := &internal.OpQueueOps(&queue)[len(internal.OpQueueOps(&queue))-1]
	if internal.OpQueueOps(&queue)[0].Bytes[0] != 1 {
		t.Fatalf("recorded vertex byte = %d, want 1", internal.OpQueueOps(&queue)[0].Bytes[0])
	}
	if internal.MeshLayout(&draw.Mesh)[0] != (gfx.Attr(0, gpu.Float32x3)) {
		t.Fatalf("recorded layout = %+v, want original", internal.MeshLayout(&draw.Mesh))
	}
	if internal.ParameterColor(&(draw.Material.Params()[0])) != (m.Color{R: 1}) {
		t.Fatalf("recorded material color = %+v, want red", internal.ParameterColor(&(draw.Material.Params()[0])))
	}
	if internal.ParameterNum(&(draw.Params[0])) != 1 {
		t.Fatalf("recorded draw parameter = %v, want 1", internal.ParameterNum(&(draw.Params[0])))
	}
}

func TestBufferWithBytesCopyDataControlsOwnership(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	copied := make([]byte, 12)
	borrowed := make([]byte, 12)
	copied[0] = 1
	borrowed[0] = 2
	layout := []gfx.VertexAttr{gfx.Attr(0, gpu.Float32x3)}

	queue.Draw(gfx.Mesh(gfx.BufferWithBytes(copied, true), gpu.TopologyTriangleList, layout...), testMaterial())
	queue.Draw(gfx.Mesh(gfx.BufferWithBytes(borrowed, false), gpu.TopologyTriangleList, layout...), testMaterial())
	copied[0] = 9
	borrowed[0] = 10

	if got := internal.OpQueueOps(&queue)[0].Bytes[0]; got != 1 {
		t.Fatalf("copied mesh byte = %d, want 1", got)
	}
	if got := internal.OpQueueOps(&queue)[2].Bytes[0]; got != 10 {
		t.Fatalf("borrowed mesh byte = %d, want 10", got)
	}
	retainedOps := internal.OpQueueOps(&queue)
	queue.Reset()
	if retainedOps[2].Bytes != nil {
		t.Fatal("reset retained borrowed mesh bytes")
	}
}

func TestTextureWithBytesCopyDataControlsOwnership(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	copied := []byte{1, 2, 3, 4}
	borrowed := []byte{5, 6, 7, 8}
	internal.OpQueueBakeTextureIfNeeded(&queue, gfx.TextureWithBytes(1, 1, gpu.FormatRGBA8, copied, true, false))
	internal.OpQueueBakeTextureIfNeeded(&queue, gfx.TextureWithBytes(1, 1, gpu.FormatRGBA8, borrowed, false, false))

	copied[0] = 9
	borrowed[0] = 10
	if got := internal.OpQueueOps(&queue)[0].Bytes[0]; got != 1 {
		t.Fatalf("copied temporary texture byte = %d, want 1", got)
	}
	if got := internal.OpQueueOps(&queue)[1].Bytes[0]; got != 10 {
		t.Fatalf("borrowed temporary texture byte = %d, want 10", got)
	}
	retainedOps := internal.OpQueueOps(&queue)
	queue.Reset()
	if retainedOps[1].Bytes != nil {
		t.Fatal("reset retained borrowed temporary texture pixels")
	}
}

func TestOpQueueBakesInlineMaterialAndDrawParameters(t *testing.T) {
	filesystem := &countingFS{FS: fstest.MapFS{
		"shared.png": &fstest.MapFile{Data: testPNG(t)},
	}}
	p := newPlugin()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	queue := recordList(t, k)
	material := testMaterial(
		gfx.TextureParam("MaterialTexture", gfx.TextureWithResource("shared.png")),
		gfx.BufferParam("MaterialBuffer", gfx.BufferWithBytes([]byte{1, 2, 3, 4}, true)),
	)
	drawTexture := gfx.TextureWithBytes(1, 1, gpu.FormatRGBA8, []byte{5, 6, 7, 8}, true, false)
	drawBuffer := gfx.BufferWithBytes([]byte{9, 10, 11, 12}, true)

	queue.Draw(triangle(), material,
		gfx.TextureParam("DrawTexture", drawTexture),
		gfx.BufferParam("DrawBuffer", drawBuffer),
	)
	draw := &internal.OpQueueOps(queue)[len(internal.OpQueueOps(queue))-1]
	for _, param := range append(draw.Material.Params(), draw.Params...) {
		switch internal.ParameterKind(&param) {
		case internal.ParamTexture:
			if internal.TextureSource(internal.ParameterTextureRef(&param)) == gfx.TextureSourceBytes || len(internal.TexturePixels(internal.ParameterTextureRef(&param))) != 0 {
				t.Errorf("texture param %q was not remapped to a baked ID", param.Name())
			}
		case internal.ParamBuffer:
			if internal.BufferSource(internal.ParameterBufferRef(&param)) != gfx.BufferSourceBaked || internal.ParameterBuffer(&param).ID() == 0 || len(internal.BufferBytes(internal.ParameterBufferRef(&param))) != 0 {
				t.Errorf("buffer param %q was not remapped to a baked ID", param.Name())
			}
		}
	}
	if filesystem.opens != 0 {
		t.Errorf("resource texture opened during recording %d times, want 0", filesystem.opens)
	}

	queue.Reset()
	queue.Draw(triangle(), material)
	if filesystem.opens != 0 {
		t.Errorf("resource texture opened during rerecording %d times, want 0", filesystem.opens)
	}
}

func TestOpQueueTemporaryTexturePool(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	first := internal.OpQueueBakeTextureIfNeeded(&queue, gfx.TextureWithBytes(1, 1, gpu.FormatRGBA8, []byte{1, 2, 3, 4}, true, false))
	second := internal.OpQueueBakeTextureIfNeeded(&queue, gfx.TextureWithBytes(1, 1, gpu.FormatRGBA8, []byte{5, 6, 7, 8}, true, false))
	if first.ID() == second.ID() {
		t.Fatal("simultaneously used temporary textures share an ID")
	}

	queue.Reset()
	reused := internal.OpQueueBakeTextureIfNeeded(&queue, gfx.TextureWithBytes(1, 1, gpu.FormatRGBA8, []byte{9, 10, 11, 12}, true, false))
	if reused.ID() != first.ID() {
		t.Errorf("reused temporary texture ID = %d, want %d", reused.ID(), first.ID())
	}
	if len(internal.OpQueueOps(&queue)) != 1 || internal.OpQueueOps(&queue)[0].Kind != internal.OpBakeTexture {
		t.Fatal("temporary texture did not populate one bake op")
	}
}

func TestBakedResourcesTranslateToBakedBindings(t *testing.T) {
	p := newPlugin()
	layout := gpu.ShaderLayout{
		UniformSize: 80, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []gpu.UniformMember{{Name: "mvp", Offset: 0}},
		Resources: []gpu.ShaderResource{
			{Name: "MainSampler", Sampler: true, Group: 1, Binding: 0},
			{Name: "MainTexture", Group: 1, Binding: 1},
			{Name: "Data", StorageBuffer: true, Group: 1, Binding: 2},
		},
	}
	backend := &fakeBackend{layout: &layout}
	k := newTestKernel(t, p)
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	var texture gfx.TextureDescr
	var buffer gfx.BufferDescr
	withResourceQueue(t, k, func(resources *gfx.ResourceQueue) {
		texture = resources.BakeTexture(1, 1, gpu.FormatRGBA8, []byte{255, 255, 255, 255}, true, false)
		buffer = resources.BakeBuffer([]byte{1, 2, 3, 4}, true)
	})
	w := recordList(t, k)
	material := testMaterial(
		gfx.TextureParam("MainTexture", texture),
		gfx.SamplerParam("MainSampler", gpu.SamplerDesc{}),
		gfx.BufferParam("Data", buffer),
	)
	w.Draw(triangle(), material, gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	var gotTexture gpu.TextureID
	var gotBuffer gpu.BufferID
	for i := range backend.lastOps {
		op := &backend.lastOps[i]
		switch op.kind {
		case opSetTexture:
			gotTexture = op.texture
		case opSetBuffer:
			gotBuffer = op.buffer
		}
	}
	if gotTexture != texture.ID() {
		t.Errorf("bound baked texture = %d, want %d", gotTexture, texture.ID())
	}
	if gotBuffer != buffer.ID() {
		t.Errorf("bound baked buffer = %d, want %d", gotBuffer, buffer.ID())
	}

	withResourceQueue(t, k, func(resources *gfx.ResourceQueue) {
		if got := resources.ReBakeTexture(texture, 2, 1, gpu.FormatRGBA8, make([]byte, 8), true, false); got.ID() != texture.ID() {
			t.Errorf("rebaked texture = %d, want %d", got.ID(), texture.ID())
		}
		if got := resources.ReBakeBuffer(buffer, []byte{5, 6, 7, 8}, true); got.ID() != buffer.ID() {
			t.Errorf("rebaked buffer = %d, want %d", got.ID(), buffer.ID())
		}
	})
	w = recordList(t, k)
	w.Draw(triangle(), material, gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	var rebakedTexture gpu.TextureID
	var rebakedBuffer gpu.BufferID
	for i := range backend.lastOps {
		op := &backend.lastOps[i]
		switch op.kind {
		case opBakeTexture:
			rebakedTexture = op.texture
		case opBakeBuffer:
			if op.bufferKind == gpu.BufferStorage {
				rebakedBuffer = op.buffer
			}
		}
	}
	if rebakedTexture != texture.ID() || rebakedBuffer != buffer.ID() {
		t.Errorf("rebaked resources = (%d, %d), want (%d, %d)", rebakedTexture, rebakedBuffer, texture.ID(), buffer.ID())
	}
	if countOps(backend.lastOps, opSetTexture) != 1 || countOps(backend.lastOps, opSetBuffer) != 1 {
		t.Error("rebake frame did not bind the baked resources")
	}

	withResourceQueue(t, k, func(resources *gfx.ResourceQueue) {
		resources.ReleaseTexture(texture)
		resources.ReleaseBuffer(buffer)
	})
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	var releasedTexture gpu.TextureID
	var releasedBuffer gpu.BufferID
	for i := range backend.lastOps {
		op := &backend.lastOps[i]
		switch op.kind {
		case opReleaseTexture:
			releasedTexture = op.texture
		case opReleaseBuffer:
			releasedBuffer = op.buffer
		}
	}
	if releasedTexture != texture.ID() || releasedBuffer != buffer.ID() {
		t.Errorf("released resources = (%d, %d), want (%d, %d)", releasedTexture, releasedBuffer, texture.ID(), buffer.ID())
	}
}

func TestConsumeTranslatesDraws(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	// Record a frame: clear + one triangle with a tint material.
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{}) // ensure clean start
	w := recordRaw(t, k)
	w.Pass(gfx.PassDescr{
		Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(),
		Load: gpu.LoadClear, Clear: m.Color{A: 1},
		DepthLoad: gpu.LoadClear, DepthClear: 0.5,
	})
	mat := testMaterial(gfx.ColorParam("tint", m.Color{R: 1, G: 1, B: 1, A: 1}))
	w.Draw(triangle(), mat, gfx.MatParam("mvp", m.NewMat4()))

	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.execCount == 0 {
		t.Fatal("render did not execute the recorded list")
	}
	if backend.shaders != 1 {
		t.Errorf("shaders created = %d, want 1", backend.shaders)
	}
	if backend.pipes != 1 {
		t.Errorf("pipelines created = %d, want 1", backend.pipes)
	}
	if len(backend.lastPasses) != 1 {
		t.Fatalf("passes = %d, want 1", len(backend.lastPasses))
	}
	pass := backend.lastPasses[0]
	if pass.Clear != (m.Color{A: 1}) || pass.DepthClear != 0.5 || pass.Load != gpu.LoadClear || pass.DepthLoad != gpu.LoadClear {
		t.Errorf("clear state = (%+v, %v, %v, %v), want (black, 0.5, clear, clear)", pass.Clear, pass.DepthClear, pass.Load, pass.DepthLoad)
	}
	if got := countOps(backend.lastOps, opDraw); got != 1 {
		t.Errorf("draw ops = %d, want 1", got)
	}
	if got := countOps(backend.lastOps, opSetParams); got != 1 {
		t.Errorf("uniform ops = %d, want 1", got)
	}
	// The single draw is non-indexed with 3 vertices.
	for i := range backend.lastOps {
		if backend.lastOps[i].kind == opDraw {
			first, count, indexed := backend.lastOps[i].first, backend.lastOps[i].count, backend.lastOps[i].indexed
			if first != 0 || count != 3 || indexed {
				t.Errorf("draw = (first %d, count %d, indexed %v), want (0, 3, false)", first, count, indexed)
			}
		}
	}
}

func TestConsumeCachesShaderAndPipeline(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	for frame := 0; frame < 3; frame++ {
		w := recordList(t, k)
		w.Draw(triangle(), testMaterial(), gfx.MatParam("mvp", m.NewMat4()))
		w.Draw(triangle(), testMaterial(), gfx.MatParam("mvp", m.Translation4(1, 0, 0)))
		k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
	}
	if backend.shaders != 1 {
		t.Errorf("shaders created across 3 frames = %d, want 1 (cached)", backend.shaders)
	}
	if backend.pipes != 1 {
		t.Errorf("pipelines created across 3 frames = %d, want 1 (cached)", backend.pipes)
	}
}

func TestPipelineCacheDistinguishesEqualStrideLayouts(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	const stride = 28
	vertices := gfx.BufferWithBytes(make([]byte, 3*stride), false)
	w := recordList(t, k)
	w.Draw(gfx.Mesh(vertices, gpu.TopologyTriangleList,
		gfx.Attr(0, gpu.Float32x3), gfx.Attr(12, gpu.Float32x4),
	), testMaterial())
	w.Draw(gfx.Mesh(vertices, gpu.TopologyTriangleList,
		gfx.Attr(0, gpu.Float32x2), gfx.Attr(12, gpu.Float32x4),
	), testMaterial())
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.pipes != 2 {
		t.Fatalf("pipelines for distinct equal-stride layouts = %d, want 2", backend.pipes)
	}
}

func TestVertexLayoutKeyUsesZeroOnlyForMissingAttributes(t *testing.T) {
	key, ok := internal.VertexLayoutKeyOf([]gfx.VertexAttr{
		gfx.Attr(0, gpu.Float32),
		gfx.Attr(256, gpu.Float32x4),
		gfx.Attr(internal.MaxVertexStride-4, gpu.Unorm1010102),
	})
	if !ok {
		t.Fatal("valid vertex layout was rejected")
	}
	for i := 0; i < 3; i++ {
		if key[i] == 0 {
			t.Fatalf("attribute %d packed to reserved zero", i)
		}
	}
	for i := 3; i < len(key); i++ {
		if key[i] != 0 {
			t.Fatalf("missing attribute %d packed to %d, want zero", i, key[i])
		}
	}
}

func TestVertexLayoutKeyRejectsUnsupportedLayouts(t *testing.T) {
	tooMany := make([]gfx.VertexAttr, internal.MaxVertexAttributes+1)
	for i := range tooMany {
		tooMany[i] = gfx.Attr(0, gpu.Float32)
	}
	tests := []struct {
		name   string
		layout []gfx.VertexAttr
	}{
		{name: "unknown type", layout: []gfx.VertexAttr{gfx.Attr(0, gpu.UnknownVertexType)}},
		{name: "type count sentinel", layout: []gfx.VertexAttr{gfx.Attr(0, internal.VertexTypeCount)}},
		{name: "negative offset", layout: []gfx.VertexAttr{gfx.Attr(-1, gpu.Float32)}},
		{name: "attribute exceeds stride limit", layout: []gfx.VertexAttr{gfx.Attr(internal.MaxVertexStride-2, gpu.Float32)}},
		{name: "too many attributes", layout: tooMany},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, ok := internal.VertexLayoutKeyOf(test.layout); ok {
				t.Fatal("unsupported vertex layout was accepted")
			}
		})
	}
}

func TestDrawParamsPackByNameAndOverrideMaterial(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	layout := gpu.ShaderLayout{
		UniformSize: 80, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []gpu.UniformMember{{Name: "camera", Offset: 0}, {Name: "tint", Offset: 64}},
	}
	backend := &fakeBackend{layout: &layout}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	red := m.Color{R: 1, G: 0, B: 0, A: 1}
	green := m.Color{R: 0, G: 1, B: 0, A: 1}
	camera := m.Translation4(3, 4, 5)
	w := recordList(t, k)
	mat := testMaterial(gfx.ColorParam("tint", red))
	w.Draw(triangle(), mat, gfx.MatParam("camera", camera), gfx.ColorParam("tint", green))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	var params []byte
	for i := range backend.lastOps {
		if backend.lastOps[i].kind == opSetParams {
			params = backend.lastOps[i].data
		}
	}
	if len(params) != 80 {
		t.Fatalf("params len = %d, want 80 (reflected uniform size)", len(params))
	}
	// The draw tint overrides the material tint at its reflected offset.
	got := m.Color{
		R: math.Float32frombits(binary.LittleEndian.Uint32(params[64:])),
		G: math.Float32frombits(binary.LittleEndian.Uint32(params[68:])),
		B: math.Float32frombits(binary.LittleEndian.Uint32(params[72:])),
		A: math.Float32frombits(binary.LittleEndian.Uint32(params[76:])),
	}
	if got != green {
		t.Errorf("tint at offset 64 = %v, want %v", got, green)
	}
	// An arbitrary reflected matrix name receives the draw matrix.
	if tx := math.Float32frombits(binary.LittleEndian.Uint32(params[48:])); tx != camera[12] {
		t.Errorf("camera[12] at offset 48 = %v, want %v", tx, camera[12])
	}
}

func TestStorageResolvesMaterialTexture(t *testing.T) {
	filesystem := &countingFS{FS: fstest.MapFS{
		"hero.png": &fstest.MapFile{Data: testPNG(t)},
	}}
	p := newPlugin()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	for frame := 0; frame < 2; frame++ {
		w := recordList(t, k)
		mat := testMaterial(gfx.TextureParam("MainTexture", gfx.TextureWithResource("hero.png")), gfx.SamplerParam("MainSampler", gpu.SamplerDesc{}))
		w.Draw(triangle(), mat, gfx.MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
	}
	if filesystem.opens != 1 {
		t.Errorf("texture resource opened %d times, want 1 (cached after first)", filesystem.opens)
	}
	if backend.textures != 1 || backend.uploads != 1 {
		t.Errorf("textures=%d uploads=%d, want 1 and 1", backend.textures, backend.uploads)
	}
	if backend.samplers != 1 {
		t.Errorf("samplers=%d, want 1", backend.samplers)
	}
}

func TestStorageResolvesShaderResource(t *testing.T) {
	// What reaches the backend is the flattened source, not the file's bytes, so
	// the fixture is code rather than a comment: comments blank to spaces on the
	// way through.
	const source = "const marker = 1;"
	filesystem := &countingFS{FS: fstest.MapFS{
		"shader.wgsl": &fstest.MapFile{Data: []byte(source)},
	}}
	p := newPlugin()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	for range 2 {
		w := recordList(t, k)
		w.Draw(triangle(), gfx.Material(gfx.ShaderWithResource("shader.wgsl")), gfx.MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
	}
	if filesystem.opens != 1 {
		t.Errorf("shader resource opened %d times, want 1", filesystem.opens)
	}
	if string(backend.shaderCode) != source+"\n" {
		t.Errorf("shader source = %q, want %q", backend.shaderCode, source+"\n")
	}
	if backend.shaders != 1 {
		t.Errorf("shaders created = %d, want 1", backend.shaders)
	}
}

func TestReleaseCachedResourceReleasesPathAndAllowsReload(t *testing.T) {
	filesystem := &countingFS{FS: fstest.MapFS{
		"hero.png":    &fstest.MapFile{Data: testPNG(t)},
		"shader.wgsl": &fstest.MapFile{Data: []byte("// storage shader")},
	}}
	p := newPlugin()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	material := gfx.Material(
		gfx.ShaderWithResource("shader.wgsl"),
		gfx.TextureParam("MainTexture", gfx.TextureWithResource("hero.png")),
	)

	w := recordList(t, k)
	w.Draw(triangle(), material, gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if len(p.translator.textures) != 1 || len(p.translator.shaders) != 1 || len(p.translator.pipelines) != 1 {
		t.Fatalf("initial caches = textures %d shaders %d pipelines %d, want 1 each", len(p.translator.textures), len(p.translator.shaders), len(p.translator.pipelines))
	}
	k.ExecuteCommand[gfx.ReleaseCachedResourceCmd](gfx.ReleaseCachedResourceRequest{})
	k.ExecuteCommand[gfx.ReleaseCachedResourceCmd](gfx.ReleaseCachedResourceRequest{Path: "hero.png"})
	k.ExecuteCommand[gfx.ReleaseCachedResourceCmd](gfx.ReleaseCachedResourceRequest{Path: "shader.wgsl"})
	w = recordList(t, k)
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(p.translator.textures) != 0 || len(p.translator.shaders) != 0 || len(p.translator.pipelines) != 0 || len(p.translator.layouts) != 0 || len(p.translator.parameterPlans) != 0 {
		t.Fatal("path release retained translator cache entries")
	}
	if len(backend.freedShaders) != 1 || len(backend.freedPipelines) != 1 {
		t.Fatalf("path release freed shaders/pipelines = (%d, %d), want (1, 1)", len(backend.freedShaders), len(backend.freedPipelines))
	}
	if countOps(backend.lastOps, opReleaseTexture) != 1 {
		t.Fatal("path release did not emit one texture release")
	}

	w = recordList(t, k)
	w.Draw(triangle(), material, gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if filesystem.opens != 4 || backend.shaders != 2 || backend.uploads != 2 {
		t.Fatalf("reload opens/shaders/uploads = (%d, %d, %d), want (4, 2, 2)", filesystem.opens, backend.shaders, backend.uploads)
	}
}

func TestFreeCachedResourcesClearsTranslatorOwnedCachesOnly(t *testing.T) {
	filesystem := fstest.MapFS{"hero.png": &fstest.MapFile{Data: testPNG(t)}}
	p := newPlugin()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	var explicit gfx.TextureDescr
	withResourceQueue(t, k, func(resources *gfx.ResourceQueue) {
		explicit = resources.BakeTexture(1, 1, gpu.FormatRGBA8, []byte{1, 2, 3, 4}, true, false)
	})
	w := recordList(t, k)
	w.Draw(triangle(), testMaterial(
		gfx.TextureParam("MainTexture", gfx.TextureWithResource("hero.png")),
		gfx.SamplerParam("MainSampler", gpu.SamplerDesc{}),
	), gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	cachedTexture := p.translator.textures["hero.png"]
	if cachedTexture.ID() == 0 || cachedTexture.ID() == explicit.ID() {
		t.Fatalf("cached/explicit texture IDs = (%d, %d), want distinct nonzero IDs", cachedTexture.ID(), explicit.ID())
	}

	k.ExecuteCommand[gfx.FreeCachedResourcesCmd](gfx.FreeCachedResourcesRequest{})
	w = recordList(t, k)
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if len(p.translator.shaders) != 0 || len(p.translator.pipelines) != 0 || len(p.translator.samplers) != 0 || len(p.translator.layouts) != 0 || len(p.translator.parameterPlans) != 0 {
		t.Fatal("global cleanup retained translator-owned caches")
	}
	if len(backend.freedShaders) != 1 || len(backend.freedPipelines) != 1 || len(backend.freedSamplers) != 1 {
		t.Fatalf("global cleanup freed shader/pipeline/sampler = (%d, %d, %d), want (1, 1, 1)", len(backend.freedShaders), len(backend.freedPipelines), len(backend.freedSamplers))
	}
	if countOps(backend.lastOps, opReleaseTexture) != 1 {
		t.Fatal("global cached cleanup did not release exactly one cached texture")
	}
	for i := range backend.lastOps {
		if backend.lastOps[i].kind == opReleaseTexture && backend.lastOps[i].texture != cachedTexture.ID() {
			t.Fatalf("global cleanup released texture %d, want cached texture %d (explicit %d)", backend.lastOps[i].texture, cachedTexture.ID(), explicit.ID())
		}
	}
}

func TestFailedTextureResourceLoadIsRetried(t *testing.T) {
	files := fstest.MapFS{}
	filesystem := &countingFS{FS: files}
	p := newPlugin()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	material := testMaterial(gfx.TextureParam("MainTexture", gfx.TextureWithResource("later.png")))

	for attempt := range 2 {
		w := recordList(t, k)
		w.Draw(triangle(), material, gfx.MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
		if attempt == 0 {
			if len(p.translator.textures) != 0 {
				t.Fatal("failed texture load was cached")
			}
			files["later.png"] = &fstest.MapFile{Data: testPNG(t)}
		}
	}
	if filesystem.opens != 2 || len(p.translator.textures) != 1 || backend.uploads != 1 {
		t.Fatalf("retry opens/cache/uploads = (%d, %d, %d), want (2, 1, 1)", filesystem.opens, len(p.translator.textures), backend.uploads)
	}
}

// A failed shader is cached as failed and reported once, following the
// reportedMissingBackend precedent. Without the cache the next frame re-reads
// every source, re-flattens, re-fails and re-reports - at the frame rate. The
// developer loop is unchanged, because it goes through eviction: fix the file,
// hot-reload evicts, the next frame retries and reports afresh.
func TestFailedShaderIsCachedAsFailedAndEvictedByItsPath(t *testing.T) {
	files := fstest.MapFS{}
	filesystem := &countingFS{FS: files}
	p := newPlugin()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errorsReported := 0
	config := map[kernel.PluginName]any{
		storage.Name: storageimpl.DefaultConfig().WithReadFS("test", 10, filesystem),
	}
	engine := kernel.New(config).Handler(func(error) bool {
		errorsReported++
		return false
	}).WithPlugins(storageimpl.New(), permanentAdapter{}, p, testPlugin{})
	go engine.Run(ctx)
	<-engine.Ready()
	k := engine.Executioner()
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	material := gfx.Material(gfx.ShaderWithResource("later.wgsl"))
	draw := func() {
		w := recordList(t, k)
		w.Draw(triangle(), material)
		k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
	}

	draw()
	if len(p.translator.shaders) != 1 {
		t.Fatalf("failed shader entries cached = %d, want 1", len(p.translator.shaders))
	}
	if filesystem.opens != 1 || errorsReported != 1 || backend.shaders != 0 {
		t.Fatalf("first frame opens/errors/shaders = (%d, %d, %d), want (1, 1, 0)", filesystem.opens, errorsReported, backend.shaders)
	}

	// The second frame neither re-reads nor re-reports: the entry says what
	// happened, and the draw is dropped on the strength of it.
	draw()
	if filesystem.opens != 1 || errorsReported != 1 {
		t.Fatalf("second frame opens/errors = (%d, %d), want (1, 1)", filesystem.opens, errorsReported)
	}

	// The path was recorded even though it could not be opened, which is what
	// lets the hot-reload of the file the author just wrote clear the failure.
	files["later.wgsl"] = &fstest.MapFile{Data: []byte("const marker = 1;")}
	k.ExecuteCommand[gfx.ReleaseCachedResourceCmd](gfx.ReleaseCachedResourceRequest{Path: "later.wgsl"})
	draw()
	if filesystem.opens != 2 || len(p.translator.shaders) != 1 || backend.shaders != 1 || errorsReported != 1 {
		t.Fatalf("after eviction opens/cache/shaders/errors = (%d, %d, %d, %d), want (2, 1, 1, 1)",
			filesystem.opens, len(p.translator.shaders), backend.shaders, errorsReported)
	}
}

// Three things break the old single probe of t.shaders[ShaderWithResource(path)]:
// a path may root several variants, a path may be an included source of modules
// rooted elsewhere, and a ShaderWithText shader can include resources.
func TestEvictionScansTheForwardIncludeSet(t *testing.T) {
	filesystem := &countingFS{FS: fstest.MapFS{
		"root.wgsl":   &fstest.MapFile{Data: []byte("#include ./shared.wgsl\nconst root = 1;")},
		"shared.wgsl": &fstest.MapFile{Data: []byte("const shared = 1;")},
	}}
	p := newPlugin()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	// Two variants rooted at one path, plus a text shader that includes the same
	// shared source: three modules, none of them found by that probe.
	materials := []gfx.MaterialDescr{
		gfx.Material(gfx.ShaderWithResource("root.wgsl")),
		gfx.Material(gfx.ShaderWithResource("root.wgsl", gfx.ShaderDefine("HQ"))),
		gfx.Material(gfx.ShaderWithText("#include shared.wgsl\nconst inline = 1;")),
	}
	w := recordList(t, k)
	for _, material := range materials {
		w.Draw(triangle(), material)
	}
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if len(p.translator.shaders) != 3 {
		t.Fatalf("cached modules = %d, want 3", len(p.translator.shaders))
	}

	k.ExecuteCommand[gfx.ReleaseCachedResourceCmd](gfx.ReleaseCachedResourceRequest{Path: "shared.wgsl"})
	w = recordList(t, k)
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if len(p.translator.shaders) != 0 {
		t.Fatalf("releasing an included source left %d modules cached", len(p.translator.shaders))
	}
	if len(backend.freedShaders) != 3 {
		t.Fatalf("freed %d backend shaders, want 3", len(backend.freedShaders))
	}
}

func TestTextureWithBytesReuploadsEveryFrame(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	pixels := []byte{255, 255, 255, 255}
	texture := gfx.TextureWithBytes(1, 1, gpu.FormatRGBA8, pixels, false, false)
	for frame := 0; frame < 2; frame++ {
		w := recordList(t, k)
		w.Draw(triangle(), testMaterial(gfx.TextureParam("MainTexture", texture)), gfx.MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
		for i := range backend.lastOps {
			if backend.lastOps[i].kind == opBakeTexture {
				id := backend.lastOps[i].texture
				if id == 0 {
					t.Errorf("frame %d baked texture has zero ID", frame)
				}
			}
		}
	}
	if backend.uploads != 2 {
		t.Errorf("texture uploads = %d, want 2", backend.uploads)
	}
}

func TestBufferWithBytesReuploadsEveryFrame(t *testing.T) {
	p := newPlugin()
	layout := gpu.ShaderLayout{
		UniformSize: 80, UniformGroup: 0, UniformBinding: 0,
		Uniforms:  []gpu.UniformMember{{Name: "mvp", Offset: 0}},
		Resources: []gpu.ShaderResource{{Name: "Data", StorageBuffer: true, Group: 1, Binding: 0}},
	}
	k := newTestKernel(t, p)
	backend := &fakeBackend{layout: &layout}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	buffer := gfx.BufferWithBytes([]byte{1, 2, 3, 4}, false)
	for frame := 0; frame < 2; frame++ {
		w := recordList(t, k)
		w.Draw(triangle(), testMaterial(gfx.BufferParam("Data", buffer)), gfx.MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
		storageBakes := 0
		for i := range backend.lastOps {
			if backend.lastOps[i].kind != opBakeBuffer {
				continue
			}
			id := backend.lastOps[i].buffer
			if backend.lastOps[i].bufferKind != gpu.BufferStorage {
				continue
			}
			storageBakes++
			if id == 0 {
				t.Errorf("frame %d baked buffer has zero ID", frame)
			}
		}
		if storageBakes != 1 {
			t.Errorf("frame %d storage buffer bakes = %d, want 1", frame, storageBakes)
		}
	}
}

// recordList runs a scoped write against the OpQueue through a helper command
// so tests record under the proper resource lock.
// recordList opens the frame's queue with a screen pass already declared, since
// every draw names a pass and most tests do not care which one.
func recordList(t *testing.T, k kernel.Executioner) *gfx.OpQueue {
	t.Helper()
	queue := recordRaw(t, k)
	queue.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), Label: "test"})
	return queue
}

// recordRaw opens the frame's queue with no pass declared.
func recordRaw(t *testing.T, k kernel.Executioner) *gfx.OpQueue {
	t.Helper()
	var captured *gfx.OpQueue
	k.ExecuteCommand[recordCmd](recordRequest{fn: func(l *gfx.OpQueue) { captured = l }})
	return captured
}

type recordCmd kernel.Command[recordRequest, recordResponse]
type recordRequest struct{ fn func(*gfx.OpQueue) }
type recordResponse struct{}

func withResourceQueue(t *testing.T, k kernel.Executioner, use func(*gfx.ResourceQueue)) {
	t.Helper()
	k.ExecuteCommand[recordResourcesCmd](recordResourcesRequest{fn: use})
}

type recordResourcesCmd kernel.Command[recordResourcesRequest, recordResourcesResponse]
type recordResourcesRequest struct{ fn func(*gfx.ResourceQueue) }
type recordResourcesResponse struct{}

func TestTemporaryBufferUploadsOnceForEveryDrawThatBindsIt(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	arena := make([]byte, 3*gpu.StorageAlignment)
	arena[0] = 7

	buffer := queue.TemporaryBuffer(arena, true)
	arena[0] = 9
	for i := range 3 {
		queue.Draw(triangle(), testMaterial(),
			gfx.BufferRangeParam("records", buffer, i*gpu.StorageAlignment, gpu.StorageAlignment))
	}

	bakes := 0
	for i := range internal.OpQueueOps(&queue) {
		if internal.OpQueueOps(&queue)[i].Kind == internal.OpBakeBuffer && internal.OpQueueOps(&queue)[i].BufferKind == gpu.BufferStorage {
			bakes++
			if internal.OpQueueOps(&queue)[i].Bytes[0] != 7 {
				t.Fatal("the temporary arena aliases caller data past the call")
			}
		}
	}
	if bakes != 1 {
		t.Fatalf("the arena uploaded %d times, want once for the whole frame", bakes)
	}
	for i := range internal.OpQueueOps(&queue) {
		if internal.OpQueueOps(&queue)[i].Kind != internal.OpDraw {
			continue
		}
		param := internal.OpQueueOps(&queue)[i].Params[0]
		if internal.ParameterBuffer(&param).ID() != buffer.ID() || internal.BufferSource(internal.ParameterBufferRef(&param)) != gfx.BufferSourceBaked {
			t.Fatalf("draw bound %+v, want the one baked arena %+v", internal.ParameterBuffer(&param), buffer)
		}
	}
}

// A shader that declares no uniform block must get no uniform binding. Scene
// declares none — all of its numeric data is storage — and emitting one anyway
// puts an entry in group 0 that the pipeline layout does not have, which fails
// CreateBindGroup and takes the whole frame's command buffer down.
func TestAShaderWithoutAUniformBlockGetsNoUniformBinding(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{layout: &gpu.ShaderLayout{Resources: []gpu.ShaderResource{
		{Name: "records", StorageBuffer: true, Group: 0, Binding: 0},
	}}}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	w := recordRaw(t, k)
	w.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto()})
	w.Draw(triangle(), testMaterial(),
		gfx.BufferParam("records", gfx.BufferWithBytes(make([]byte, 64), true)))

	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if got := countOps(backend.lastOps, opDraw); got != 1 {
		t.Fatalf("draw ops = %d, want 1", got)
	}
	if got := countOps(backend.lastOps, opSetParams); got != 0 {
		t.Fatalf("uniform ops = %d, want none: the shader declares no uniform block", got)
	}
}

// One root path with several supplies is several shaders, not one shader with
// several states, and each reaches the backend under its own label. Without the
// label four modules from one root source would be named identically, and the
// storage-buffer limit report - the diagnostic conditional compilation exists to
// make unnecessary - could not say which variant tripped it.
func TestEachVariantIsItsOwnModuleUnderItsOwnLabel(t *testing.T) {
	filesystem := &countingFS{FS: fstest.MapFS{
		"scene.wgsl": &fstest.MapFile{Data: []byte("//#if SKIN\nconst skin = 1;\n//#endif\nconst always = 1;")},
	}}
	p := newPlugin()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	w := recordList(t, k)
	for _, opts := range [][]gfx.ShaderOption{
		nil,
		{gfx.ShaderDefine("SKIN")},
		{gfx.ShaderDefine("MORPH")},
		{gfx.ShaderDefine("SKIN"), gfx.ShaderDefine("MORPH")},
	} {
		w.Draw(triangle(), gfx.Material(gfx.ShaderWithResource("scene.wgsl", opts...)))
	}
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(p.translator.shaders) != 4 || backend.shaders != 4 {
		t.Fatalf("four supplies cached %d modules and built %d, want 4 and 4",
			len(p.translator.shaders), backend.shaders)
	}
	want := []string{
		"scene.wgsl",
		"scene.wgsl [SKIN]",
		"scene.wgsl [MORPH]",
		"scene.wgsl [MORPH SKIN]",
	}
	for i, label := range want {
		if backend.shaderLabels[i] != label {
			t.Errorf("module %d is labelled %q, want %q", i, backend.shaderLabels[i], label)
		}
	}
	// The cut is real, not just a distinct key: the last module built carries
	// the declaration its define guards.
	if !bytes.Contains(backend.shaderCode, []byte("const skin")) {
		t.Error("the last variant lost its guarded declaration")
	}
}

// No line number ever crosses the backend boundary as data: gogpu returns an
// internal parse-error type, so errors.As can never recover a line, and gfx must
// not import a backend's parser in any case. So gfx appends the rendered segment
// table and lets the reader subtract.
func TestABackendCompileFailureCarriesTheSegmentTable(t *testing.T) {
	filesystem := &countingFS{FS: fstest.MapFS{
		"root.wgsl": &fstest.MapFile{Data: []byte("#include ./part.wgsl\nconst root = 1;")},
		"part.wgsl": &fstest.MapFile{Data: []byte("const part = 1;\nconst more = 2;")},
	}}
	p := newPlugin()
	var reported []error
	k := newTestKernelWith(t, p, filesystem, func(err error) bool {
		reported = append(reported, err)
		return false
	})
	backend := &fakeBackend{shaderErr: errors.New("wgpu: parse error: line 3, column 12: expected ';'")}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	w := recordList(t, k)
	w.Draw(triangle(), gfx.Material(gfx.ShaderWithResource("root.wgsl", gfx.ShaderDefine("HQ"))))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(reported) != 1 {
		t.Fatalf("the frame reported %d errors, want 1: %v", len(reported), reported)
	}
	var refused gfx.ErrShaderSource
	if !errors.As(reported[0], &refused) {
		t.Fatalf("reported %v, want an ErrShaderSource", reported[0])
	}
	if !errors.Is(refused.Err, backend.shaderErr) {
		t.Errorf("Err = %v, want the backend's own error unrewritten", refused.Err)
	}
	rendered := refused.Error()
	for _, want := range []string{
		`root.wgsl [HQ]`,
		"line 3, column 12",
		"flattened: 1-1 root.wgsl; 2-3 part.wgsl (root.wgsl:1); 4-4 root.wgsl@2",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("Error() = %q, missing %q", rendered, want)
		}
	}
}
