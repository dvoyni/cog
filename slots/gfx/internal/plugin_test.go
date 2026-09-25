package internal

import (
	"bytes"
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

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"

	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// testMaterial builds a material with an inline (fake-compiled) shader.
func testMaterial(params ...ParameterDescr) MaterialDescr {
	return Material(ShaderWithText("//test"), params...)
}

// fakeBackend records the calls the translator makes and captures the last
// executed op stream so tests can assert the translation without a GPU.
type fakeBackend struct {
	// uniforms is the frame's uniform arena as BakeUniforms handed it over,
	// which SetUniformBlock reads its slot back out of.
	uniforms []UniformBlock
	// formats is what each texture was allocated or baked in, the fake's stand
	// in for gogpu's bakedTextureDescs: written when a bake is replayed and
	// dropped on release, so a texture is unknown until its frame's Execute.
	formats        map[TextureID]TextureFormat
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
	freedSamplers  []SamplerID
	freedShaders   []ShaderID
	freedPipelines []PipelineID
	lastPipelines  []PipelineDesc

	// lastOps is every call the last Execute's replay made, in replay order:
	// bakes, then each pass's render commands, then releases.
	lastOps    []backendOp
	lastPasses []PassDesc
	passDraws  []int
	views      [][3]int
	draws      []drawCall
	// indexBinds records every index buffer the pass bound and the width it was
	// bound at, which is the only place the width is observable: a draw call
	// carries a count, not a format.
	indexBinds    []indexBind
	execCount     int
	layout        *ShaderLayout
	presents      int
	presentAfter  int
	boundTextures []TextureID
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
	captureDescs   []CaptureDesc
	captureAfter   int
	captureLabels  [][]string
	takeCalls      int
	captureResult  func(CaptureDesc) Capture
	capturePending *Capture
	captureReady   *Capture
}

// Capture records the readback and prepares its result for the drain after the
// next Execute.
func (b *fakeBackend) Capture(desc CaptureDesc) {
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

func (b *fakeBackend) TakeCapture() (Capture, bool) {
	b.takeCalls++
	if b.captureReady == nil {
		return Capture{}, false
	}
	done := *b.captureReady
	b.captureReady = nil
	return done, true
}

// defaultCapture is a two-by-two image with padded rows, so that the ordinary
// path through a test still exercises the un-stride.
func defaultCapture() Capture {
	return paddedCapture(2, 2, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x * 60), G: uint8(y * 60), B: 7, A: 255}
	})
}

// paddedCapture builds what a backend hands back: rows padded to the GPU's own
// alignment, with the padding filled with a value that is not the picture, so
// a test can tell an un-stride from a straight copy.
func paddedCapture(width, height int, at func(x, y int) color.NRGBA) Capture {
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
	return Capture{
		Pixels: pixels, Width: width, Height: height,
		Format: FrameBufferFormat, BytesPerRow: rowBytes,
	}
}

func (b *fakeBackend) id() uint32 { b.nextID++; return b.nextID }

func (b *fakeBackend) Ready() bool { return true }

func (b *fakeBackend) NewTexture() TextureID { b.nextTex++; return TextureID(b.nextTex) }
func (b *fakeBackend) NewBuffer() BufferID   { b.nextBuf++; return BufferID(b.nextBuf) }

func (b *fakeBackend) NewSampler(SamplerDesc) (SamplerID, error) {
	b.samplers++
	return SamplerID(b.id()), nil
}
func (b *fakeBackend) FreeSampler(id SamplerID) { b.freedSamplers = append(b.freedSamplers, id) }
func (b *fakeBackend) NewShader(desc ShaderDesc) (ShaderID, error) {
	if b.shaderErr != nil {
		return 0, b.shaderErr
	}
	b.shaders++
	b.shaderCode = append(b.shaderCode[:0], desc.Code...)
	b.shaderLabels = append(b.shaderLabels, desc.Label)
	return ShaderID(b.id()), nil
}
func (b *fakeBackend) FreeShader(id ShaderID) { b.freedShaders = append(b.freedShaders, id) }

// ShaderLayout reports a fixed layout matching the built-in shader: mvp at 0,
// a "tint" color at 64 (80-byte block), plus a texture+sampler in group 1.
func (b *fakeBackend) ShaderLayout(ShaderID) ShaderLayout {
	if b.layout != nil {
		return *b.layout
	}
	return ShaderLayout{
		UniformSize: 80, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []UniformMember{{Name: "mvp", Offset: 0}, {Name: "tint", Offset: 64}},
		Resources: []ShaderResource{
			{Name: "MainSampler", Sampler: true, Group: 1, Binding: 0},
			{Name: "MainTexture", Group: 1, Binding: 1},
		},
	}
}
func (b *fakeBackend) NewPipeline(desc PipelineDesc) (PipelineID, error) {
	if b.pipelineErr != nil {
		return 0, b.pipelineErr
	}
	b.pipes++
	b.lastPipelines = append(b.lastPipelines, desc)
	return PipelineID(b.id()), nil
}
func (b *fakeBackend) FreePipeline(id PipelineID) {
	b.freedPipelines = append(b.freedPipelines, id)
}
func (b *fakeBackend) ScreenFramebuffer() (TextureViewID, int, int) {
	return 1, 100, 100
}

// Limits reports what a desktop adapter typically allows, which is far above
// the web floor gfx measures shaders against.
func (b *fakeBackend) Limits() Limits {
	return Limits{
		MaxBindGroups:                   8,
		MaxStorageBuffersPerShaderStage: 200,
		MaxStorageBufferBindingSize:     1 << 31,
		MaxUniformBufferBindingSize:     1 << 20,
		MaxBufferSize:                   1 << 31,
	}
}

// TextureFormat answers from the formats recorded when a texture was allocated
// or baked, which is the backend's own record in gogpu too. A texture whose
// bake this backend has not replayed yet is unknown, exactly as there.
func (b *fakeBackend) TextureFormat(id TextureID) (TextureFormat, bool) {
	format, ok := b.formats[id]
	return format, ok
}

func (b *fakeBackend) TextureView(texture TextureID, mip, layer int) TextureViewID {
	b.views = append(b.views, [3]int{int(texture), mip, layer})
	return TextureViewID(len(b.views))
}

func (b *fakeBackend) Execute(queue *Queue) {
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
	testOpBakeBuffer backendOpKind = iota
	testOpBakeTexture
	testOpAllocateTexture
	testOpUpdateTexture
	testOpSetUniformBlock
	testOpSetTexture
	testOpSetSampler
	testOpSetBuffer
	testOpDraw
	testOpReleaseBuffer
	testOpReleaseTexture
)

// backendOp is one call a replayed queue made on the fake backend, with the
// arguments that call carried. Only the fields its kind passes are set.
type backendOp struct {
	kind                  backendOpKind
	texture               TextureID
	buffer                BufferID
	bufferKind            BufferKind
	format                TextureFormat
	width, height, layers int
	layer                 int
	region                Region
	renderable            bool
	group, binding        int
	offset, size          int
	first, count          int
	indexed               bool
	data                  []byte
}

func (b *fakeBackend) BakeBuffer(id BufferID, kind BufferKind, size int, data []byte) {
	b.lastOps = append(b.lastOps, backendOp{kind: testOpBakeBuffer, buffer: id, bufferKind: kind, size: size, data: data})
}

func (b *fakeBackend) BakeTexture(id TextureID, width, height int, format TextureFormat, pixels []byte, mipmaps bool) {
	b.textures++
	b.uploads++
	b.recordFormat(id, format)
	b.lastOps = append(b.lastOps, backendOp{
		kind: testOpBakeTexture, texture: id, width: width, height: height, format: format, data: pixels,
	})
}

func (b *fakeBackend) AllocateTexture(id TextureID, desc TextureDesc) {
	b.recordFormat(id, desc.Format)
	b.lastOps = append(b.lastOps, backendOp{
		kind: testOpAllocateTexture, texture: id, width: desc.Width, height: desc.Height, layers: desc.Layers,
		format: desc.Format, renderable: desc.Renderable,
	})
}

func (b *fakeBackend) UpdateTexture(id TextureID, layer int, region Region, pixels []byte) {
	b.lastOps = append(b.lastOps, backendOp{kind: testOpUpdateTexture, texture: id, layer: layer, region: region, data: pixels})
}

func (b *fakeBackend) ReleaseBuffer(id BufferID) {
	b.lastOps = append(b.lastOps, backendOp{kind: testOpReleaseBuffer, buffer: id})
}

func (b *fakeBackend) recordFormat(id TextureID, format TextureFormat) {
	if b.formats == nil {
		b.formats = map[TextureID]TextureFormat{}
	}
	b.formats[id] = format
}

func (b *fakeBackend) ReleaseTexture(id TextureID) {
	delete(b.formats, id)
	b.lastOps = append(b.lastOps, backendOp{kind: testOpReleaseTexture, texture: id})
}

// BeginPass records the pass and returns the backend itself as its RenderPass,
// which counts the draws that land in it.
func (b *fakeBackend) BeginPass(desc PassDesc) RenderPass {
	b.lastPasses = append(b.lastPasses, desc)
	b.passDraws = append(b.passDraws, 0)
	return b
}

func (b *fakeBackend) EndPass(RenderPass) {}

// TransitionTextures records each barrier against the pass it precedes, so a
// test can assert not just that a transition happened but that it happened
// before the pass whose hazard it fixes.
func (b *fakeBackend) TransitionTextures(transitions []TextureTransition) {
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
func (b *fakeBackend) transitionBefore(want TextureTransition) (int, bool) {
	for _, placed := range b.transitions {
		if placed.TextureTransition == want {
			return placed.beforePass, true
		}
	}
	return -1, false
}

// placedTransition is one barrier and the pass it was recorded ahead of.
type placedTransition struct {
	TextureTransition
	beforePass int
}

// Present records the implicit present pass and how many declared passes had
// already run, so a test can assert both that it ran and that it ran last.
func (b *fakeBackend) Present() {
	b.presents++
	b.presentAfter = len(b.lastPasses)
}

func (b *fakeBackend) SetPipeline(PipelineID)             {}
func (b *fakeBackend) BakeUniforms(blocks []UniformBlock) { b.uniforms = blocks }
func (b *fakeBackend) SetUniformBlock(slot int) {
	b.lastOps = append(b.lastOps, backendOp{kind: testOpSetUniformBlock, data: bytes.Clone(b.uniforms[slot][:])})
}

// SetTexture records the binding so a test can assert which texture reached the
// GPU, which is the only way to tell a sampled render target from a draw that
// was silently dropped before it ever bound one.
func (b *fakeBackend) SetTexture(texture TextureID, group, binding int) {
	b.lastOps = append(b.lastOps, backendOp{kind: testOpSetTexture, texture: texture, group: group, binding: binding})
	b.boundTextures = append(b.boundTextures, texture)
}

// boundTexture reports whether the texture was bound at any point this frame.
func (b *fakeBackend) boundTexture(id TextureID) bool {
	return slices.Contains(b.boundTextures, id)
}
func (b *fakeBackend) SetSampler(_ SamplerID, group, binding int) {
	b.lastOps = append(b.lastOps, backendOp{kind: testOpSetSampler, group: group, binding: binding})
}
func (b *fakeBackend) SetVertexBuffer(BufferID, int) {}
func (b *fakeBackend) SetIndexBuffer(buffer BufferID, offset int, width IndexWidth) {
	b.indexBinds = append(b.indexBinds, indexBind{buffer: buffer, offset: offset, width: width})
}
func (b *fakeBackend) SetBuffer(group, binding int, buffer BufferID, offset, size int) {
	b.lastOps = append(b.lastOps, backendOp{
		kind: testOpSetBuffer, group: group, binding: binding, buffer: buffer, offset: offset, size: size,
	})
}
func (b *fakeBackend) Draw(first, count, instances, firstInstance int, indexed bool) {
	b.lastOps = append(b.lastOps, backendOp{kind: testOpDraw, first: first, count: count, indexed: indexed})
	if len(b.passDraws) > 0 {
		b.passDraws[len(b.passDraws)-1]++
	}
	b.draws = append(b.draws, drawCall{
		first: first, count: count, instances: instances, firstInstance: firstInstance, indexed: indexed,
	})
}

// indexBind is one recorded index-buffer binding.
type indexBind struct {
	buffer BufferID
	offset int
	width  IndexWidth
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

// testGfxBackend is the Adapter this fixture fills gfx's backend Port as.
type testGfxBackend kernel.Adapter[BackendPort]

// Name is the gfx plugin's, not this fixture's: the fixture locks gfx resources.
func (testPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }
func (testPlugin) Register(registrar *kernel.Registrar, _ any) error {
	adapter := &testAdapter{}
	registrar.ProvideAdapter[testGfxBackend](Backend(adapter))
	registrar.HandleCommand[attachBackendCmd](adapter.attachBackendCmdImpl)
	registrar.HandleCommand[recordCmd](recordCmdImpl)
	registrar.HandleCommand[recordResourcesCmd](recordResourcesCmdImpl)
	return nil
}

func recordCmdImpl() (kernel.Lock, kernel.Execute[recordRequest, recordResponse]) {
	var queue kernel.Write[*OpQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*OpQueue]()
		}, func(_ kernel.Kernel, req recordRequest) recordResponse {
			req.fn(queue.Get())
			return recordResponse{}
		}
}

func recordResourcesCmdImpl() (kernel.Lock, kernel.Execute[recordResourcesRequest, recordResourcesResponse]) {
	var queue kernel.Write[*ResourceQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*ResourceQueue]()
		}, func(_ kernel.Kernel, req recordResourcesRequest) recordResourcesResponse {
			req.fn(queue.Get())
			return recordResourcesResponse{}
		}
}
func newTestKernel(t *testing.T, p *plugin) kernel.Executioner {
	return newTestKernelWithFS(t, p, fstest.MapFS{})
}

func newTestKernelWithFS(t *testing.T, p *plugin, filesystem fs.FS) kernel.Executioner {
	t.Helper()
	return newTestKernelWith(t, p, filesystem, func(err error) error {
		t.Errorf("unexpected kernel error: %v", err)
		return err
	})
}

// newTestKernelWithErrors builds a kernel whose reported errors are collected
// instead of failing the test, for the paths that report one on purpose. Its
// handler keeps the engine running, so a reported error does not end the frames
// that follow it.
func newTestKernelWithErrors(t *testing.T, p *plugin, report func(error)) kernel.Executioner {
	t.Helper()
	return newTestKernelWith(t, p, fstest.MapFS{}, func(err error) error {
		report(err)
		return nil
	})
}

func newTestKernelWith(t *testing.T, p *plugin, filesystem fs.FS, handler kernel.ErrorHandler) kernel.Executioner {
	t.Helper()
	engine := kernel.New(nil).Handler(handler).WithPlugins(storageplugin.New(), permanentAdapter{}, readMountAdapter{storage.ReadMount{Id: "test", Priority: 10, FS: filesystem}}, appplugin.New(), mainLoopAdapter{}, p, testPlugin{})
	go engine.Run()
	t.Cleanup(engine.Quit)
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

func triangle() MeshDescr {
	const stride = 28 // vec3 position + vec4 color
	return Mesh(
		BufferWithBytes(make([]byte, 3*stride), true),
		TopologyTriangleList,
		Attr(0, Float32x3),
		Attr(12, Float32x4),
	)
}

func BenchmarkOpQueueDrawSteadyState(b *testing.B) {
	queue := testOpQueue(&fakeBackend{})
	mesh := triangle()
	material := testMaterial(
		ColorParam("tint", m.Color{R: 1, G: 1, B: 1, A: 1}),
		BufferParam("data", BufferWithBytes(make([]byte, 64), true)),
	)
	params := []ParameterDescr{
		MatParam("mvp", m.NewMat4()),
		FloatParam("time", 1),
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
	layout := ShaderLayout{
		UniformSize: 96, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []UniformMember{
			{Name: "mvp", Offset: 0},
			{Name: "tint", Offset: 64},
			{Name: "time", Offset: 80},
			{Name: "scale", Offset: 84},
		},
		Resources: []ShaderResource{
			{Name: "MainSampler", Sampler: true, Group: 1, Binding: 0},
			{Name: "MainTexture", Group: 1, Binding: 1},
			{Name: "Data", StorageBuffer: true, Group: 1, Binding: 2},
		},
	}
	backend := &fakeBackend{layout: &layout}
	translator := newTranslator()
	queue := testOpQueue(backend)
	// Without a pass every draw is a stray and the bench translates none of them.
	queue.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto()})
	mesh := Mesh(
		BakedBuffer(1, 3*28),
		TopologyTriangleList,
		Attr(0, Float32x3), Attr(12, Float32x4),
	)
	material := testMaterial(
		ColorParam("tint", m.Color{R: 1, G: 1, B: 1, A: 1}),
		FloatParam("scale", 1),
		TextureParam("MainTexture", BakedTexture(2, 0, 0)),
		SamplerParam("MainSampler", SamplerDesc{}),
		BufferParam("Data", BakedBuffer(3, 64)),
	)
	for range 100 {
		queue.Draw(mesh, material,
			MatParam("mvp", m.NewMat4()),
			FloatParam("time", 1),
			ColorParam("tint", m.Color{R: 0.5, A: 1}),
		)
	}
	// The zero Kernel is legal here because nothing this frame reaches it: every
	// texture in the material is already baked, so no cache loads and no failure
	// is reported. A kernel is only ever touched on a miss.
	translator.translate(kernel.Kernel{}, &queue, nil, backend, noFiles, CaptureDesc{}, false)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		translator.translate(kernel.Kernel{}, &queue, nil, backend, noFiles, CaptureDesc{}, false)
	}
}

type benchmarkGpuSink struct{}

func (benchmarkGpuSink) BakeUniforms([]UniformBlock)                                  {}
func (benchmarkGpuSink) BakeBuffer(BufferID, BufferKind, int, []byte)                 {}
func (benchmarkGpuSink) BakeTexture(TextureID, int, int, TextureFormat, []byte, bool) {}
func (benchmarkGpuSink) AllocateTexture(TextureID, TextureDesc)                       {}
func (benchmarkGpuSink) UpdateTexture(TextureID, int, Region, []byte)                 {}
func (benchmarkGpuSink) SetPipeline(PipelineID)                                       {}
func (benchmarkGpuSink) SetUniformBlock(int)                                          {}
func (benchmarkGpuSink) SetTexture(TextureID, int, int)                               {}

func (benchmarkGpuSink) SetSampler(SamplerID, int, int)           {}
func (benchmarkGpuSink) SetVertexBuffer(BufferID, int)            {}
func (benchmarkGpuSink) SetIndexBuffer(BufferID, int, IndexWidth) {}
func (benchmarkGpuSink) SetBuffer(int, int, BufferID, int, int)   {}
func (benchmarkGpuSink) Draw(int, int, int, int, bool)            {}
func (benchmarkGpuSink) ReleaseBuffer(BufferID)                   {}
func (benchmarkGpuSink) ReleaseTexture(TextureID)                 {}

func (benchmarkGpuSink) BeginPass(PassDesc) RenderPass          { return benchmarkGpuSink{} }
func (benchmarkGpuSink) EndPass(RenderPass)                     {}
func (benchmarkGpuSink) TransitionTextures([]TextureTransition) {}
func (benchmarkGpuSink) Present()                               {}
func (benchmarkGpuSink) Capture(CaptureDesc)                    {}

func BenchmarkGpuQueueReplaySteadyState(b *testing.B) {
	var queue Queue
	queue.Reset()
	queue.BeginPass(PassDesc{Screen: true, DepthAuto: true})
	for i := range 100 {
		queue.BakeBuffer(BufferID(i+1), BufferVertex, 64, []byte{1})
		queue.SetPipeline(1)
		queue.SetUniformBlock()[0] = 1
		queue.SetVertexBuffer(BufferID(i+1), 0)
		queue.Draw(0, 3, 1, 0, false)
		queue.ReleaseBuffer(BufferID(i + 1))
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
	if len(OpQueuePasses(&queue)) != 0 || OpQueueCurrent(&queue) != -1 {
		t.Fatalf("after reset: %d passes, current %d, want none declared or selected", len(OpQueuePasses(&queue)), OpQueueCurrent(&queue))
	}
	first := queue.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto(), Load: LoadClear})
	second := queue.Pass(PassDescr{Order: 1, Target: ScreenTarget(), Depth: DepthAuto()})
	if OpQueueSelectedPass(&queue) != 1 {
		t.Errorf("selected pass = %d, want the one just declared", OpQueueSelectedPass(&queue))
	}
	queue.SetPass(first)
	if OpQueueSelectedPass(&queue) != 0 {
		t.Errorf("selected pass = %d, want the re-selected first", OpQueueSelectedPass(&queue))
	}
	// An unknown reference leaves the selection alone rather than guessing.
	queue.SetPass(PassRef(99))
	if OpQueueSelectedPass(&queue) != 0 {
		t.Errorf("selected pass = %d, want the selection unchanged by an unknown ref", OpQueueSelectedPass(&queue))
	}
	_ = second

	queue.Reset()
	if len(OpQueuePasses(&queue)) != 0 || OpQueueCurrent(&queue) != -1 {
		t.Errorf("after reset: %d passes, current %d, want none selected", len(OpQueuePasses(&queue)), OpQueueCurrent(&queue))
	}
}

func testOpQueue(backend Backend) OpQueue {
	return *NewOpQueue(idsOf(backend))
}

// idsOf is the id source of a queue built outside a composition, which has no
// adapter handle to read.
func idsOf(backend IDMinter) IDSource {
	return func() IDMinter { return backend }
}

func TestBakeOpsAllocateBakedResourceIDs(t *testing.T) {
	backend := &fakeBackend{}
	queue := *NewResourceQueue(idsOf(backend))
	pixels := []byte{1, 2, 3, 4}
	buffer := queue.BakeBuffer(pixels, true)
	texture := queue.BakeTexture(1, 1, FormatRGBA8, pixels, true, false)
	rebakedBuffer := queue.ReBakeBuffer(buffer, pixels, true)
	rebakedTexture := queue.ReBakeTexture(texture, 1, 1, FormatRGBA8, pixels, true, false)

	if buffer.ID() == 0 || texture.ID() == 0 {
		t.Fatalf("baked handles = (%d, %d), want nonzero", buffer.ID(), texture.ID())
	}
	if rebakedBuffer.ID() != buffer.ID() || rebakedTexture.ID() != texture.ID() {
		t.Fatalf("rebaked handles = (%d, %d), want (%d, %d)", rebakedBuffer.ID(), rebakedTexture.ID(), buffer.ID(), texture.ID())
	}
	pixels[0] = 99
	for i := range ResourceQueueOps(&queue) {
		if ResourceQueueOps(&queue)[i].Bytes[0] != 1 {
			t.Fatalf("op %d did not copy caller data", i)
		}
	}
}

func TestBakeBufferCopyDataControlsOwnership(t *testing.T) {
	queue := *NewResourceQueue(idsOf(&fakeBackend{}))
	copied := []byte{1, 2, 3, 4}
	borrowed := []byte{5, 6, 7, 8}
	queue.BakeBuffer(copied, true)
	queue.BakeBuffer(borrowed, false)

	copied[0] = 9
	borrowed[0] = 10
	if got := ResourceQueueOps(&queue)[0].Bytes[0]; got != 1 {
		t.Fatalf("copied buffer byte = %d, want 1", got)
	}
	if got := ResourceQueueOps(&queue)[1].Bytes[0]; got != 10 {
		t.Fatalf("borrowed buffer byte = %d, want 10", got)
	}
	retainedOps := ResourceQueueOps(&queue)
	ResourceQueueReset(&queue)
	if retainedOps[1].Bytes != nil {
		t.Fatal("reset retained borrowed buffer bytes")
	}
}

func TestBakeTextureCopyDataControlsOwnership(t *testing.T) {
	queue := *NewResourceQueue(idsOf(&fakeBackend{}))
	copied := []byte{1, 2, 3, 4}
	borrowed := []byte{5, 6, 7, 8}
	queue.BakeTexture(1, 1, FormatRGBA8, copied, true, false)
	queue.BakeTexture(1, 1, FormatRGBA8, borrowed, false, false)

	copied[0] = 9
	borrowed[0] = 10
	if got := ResourceQueueOps(&queue)[0].Bytes[0]; got != 1 {
		t.Fatalf("copied texture byte = %d, want 1", got)
	}
	if got := ResourceQueueOps(&queue)[1].Bytes[0]; got != 10 {
		t.Fatalf("borrowed texture byte = %d, want 10", got)
	}
	retainedOps := ResourceQueueOps(&queue)
	ResourceQueueReset(&queue)
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
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		texture := resources.AllocateTexture(64, 32, 4, FormatRGBA8)
		resources.UpdateTexture(texture, 2, Region{X: 5, Y: 7, Width: 1, Height: 1}, pixels, true)
	})
	pixels[0] = 9
	k.PublishEvent(app.RenderEvent{}).Wait()

	if got := countOps(backend.lastOps, testOpAllocateTexture); got != 1 {
		t.Fatalf("texture allocations = %d, want 1", got)
	}
	if got := countOps(backend.lastOps, testOpUpdateTexture); got != 1 {
		t.Fatalf("texture updates = %d, want 1", got)
	}
	for i := range backend.lastOps {
		op := &backend.lastOps[i]
		switch op.kind {
		case testOpAllocateTexture:
			if op.width != 64 || op.height != 32 || op.layers != 4 || op.format != FormatRGBA8 {
				t.Fatalf("allocation metadata = (%d,%d,%d,%d)", op.width, op.height, op.layers, op.format)
			}
		case testOpUpdateTexture:
			if op.layer != 2 || op.region != (Region{X: 5, Y: 7, Width: 1, Height: 1}) || op.data[0] != 1 {
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
		w.Draw(triangle(), testMaterial(TextureParam("MainTexture", TextureWithResource("persistent.png"))), MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
	}
	k.PublishEvent(app.RenderEvent{}).Wait()

	var baked, bound TextureID
	for i := range backend.lastOps {
		op := &backend.lastOps[i]
		switch op.kind {
		case testOpBakeTexture:
			baked = op.texture
		case testOpSetTexture:
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

	var buffer BufferDescr
	var texture TextureDescr
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		buffer = resources.BakeBuffer([]byte{1, 2, 3, 4}, true)
		texture = resources.BakeTexture(1, 1, FormatRGBA8, []byte{1, 2, 3, 4}, true, false)
	})
	k.ExecuteCommand[PresentCmd](PresentRequest{})

	withResourceQueue(t, k, func(resources *ResourceQueue) {
		resources.ReBakeBuffer(buffer, []byte{5, 6, 7, 8}, true)
		resources.ReBakeTexture(texture, 1, 1, FormatRGBA8, []byte{5, 6, 7, 8}, true, false)
		resources.ReleaseBuffer(buffer)
		resources.ReleaseTexture(texture)
	})
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if got := countOps(backend.lastOps, testOpBakeBuffer); got != 2 {
		t.Errorf("persistent buffer bakes = %d, want 2", got)
	}
	if got := countOps(backend.lastOps, testOpBakeTexture); got != 2 {
		t.Errorf("persistent texture bakes = %d, want 2", got)
	}
	if got := countOps(backend.lastOps, testOpReleaseBuffer); got != 1 {
		t.Errorf("persistent buffer releases = %d, want 1", got)
	}
	if got := countOps(backend.lastOps, testOpReleaseTexture); got != 1 {
		t.Errorf("persistent texture releases = %d, want 1", got)
	}
}

func TestDroppedFrameDiscardsTemporaryUploads(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	w := recordList(t, k)
	w.Draw(triangle(), testMaterial(TextureParam("MainTexture", TextureWithBytes(1, 1, FormatRGBA8, []byte{1, 2, 3, 4}, false, false))), MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	w = recordList(t, k)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if countOps(backend.lastOps, testOpBakeTexture) != 0 || countOps(backend.lastOps, testOpBakeBuffer) != 0 {
		t.Fatal("dropped frame retained temporary texture or geometry uploads")
	}
}

func TestOpQueueTemporaryBufferPool(t *testing.T) {
	backend := &fakeBackend{}
	queue := testOpQueue(backend)

	small := []byte{1, 2, 3, 4}
	large := make([]byte, 16)
	smallBuffer := OpQueueTemporaryBuffer(&queue, BufferVertex, small, true)
	largeBuffer := OpQueueTemporaryBuffer(&queue, BufferVertex, large, true)
	small[0] = 99
	if OpQueueOps(&queue)[0].Bytes[0] != 1 {
		t.Fatal("temporary bake op aliases caller data")
	}

	queue.Reset()
	fit := OpQueueTemporaryBuffer(&queue, BufferVertex, make([]byte, 12), true)
	if fit.ID() != largeBuffer.ID() {
		t.Errorf("best-fit buffer = %d, want %d", fit.ID(), largeBuffer.ID())
	}
	queue.Reset()
	resized := OpQueueTemporaryBuffer(&queue, BufferVertex, make([]byte, 32), true)
	if resized.ID() != largeBuffer.ID() {
		t.Errorf("resized buffer ID = %d, want reused %d", resized.ID(), largeBuffer.ID())
	}
	if OpQueueTemporaryBuffers(&queue)[1].Size < 32 {
		t.Errorf("resized size = %d, want at least 32", OpQueueTemporaryBuffers(&queue)[1].Size)
	}
	if smallBuffer.ID() == largeBuffer.ID() {
		t.Fatal("simultaneously used temporary buffers share an ID")
	}
}

func TestDrawStoresTemporaryBufferIDsWithoutInlineGeometry(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	mesh := triangle()
	queue.Draw(mesh, testMaterial(), MatParam("mvp", m.NewMat4()))

	if OpQueueOps(&queue)[0].Kind != OpBakeBuffer || OpQueueOps(&queue)[0].BufferKind != BufferVertex {
		t.Fatal("draw did not populate a vertex bake op first")
	}
	draw := &OpQueueOps(&queue)[1]
	if MeshVertices(&draw.Mesh).ID() == 0 || draw.Mesh.VertexCount() != 3 {
		t.Fatalf("draw vertex resource = (%d, %d), want nonzero ID and 3 vertices", MeshVertices(&draw.Mesh).ID(), draw.Mesh.VertexCount())
	}
	if BufferBytes(MeshVerticesRef(&draw.Mesh)).Len() != 0 {
		t.Fatalf("draw retained %d inline vertex bytes", BufferBytes(MeshVerticesRef(&draw.Mesh)).Len())
	}
	if len(OpQueueTemporaryBuffers(&queue)) != 1 || !OpQueueTemporaryBuffers(&queue)[0].Used {
		t.Fatal("draw did not lease one temporary vertex buffer")
	}
}

func TestOpQueueArenasPreserveCallerDataIsolation(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	vertices := make([]byte, 3*28)
	vertices[0] = 1
	layout := []VertexAttr{Attr(0, Float32x3), Attr(12, Float32x4)}
	materialParams := []ParameterDescr{ColorParam("tint", m.Color{R: 1})}
	drawParams := []ParameterDescr{FloatParam("time", 1)}

	queue.Draw(
		Mesh(BufferWithBytes(vertices, true), TopologyTriangleList, layout...),
		Material(ShaderWithText("//test"), materialParams...),
		drawParams...,
	)
	vertices[0] = 9
	layout[0] = Attr(4, Float32x2)
	materialParams[0] = ColorParam("tint", m.Color{G: 1})
	drawParams[0] = FloatParam("time", 9)

	draw := &OpQueueOps(&queue)[len(OpQueueOps(&queue))-1]
	if OpQueueOps(&queue)[0].Bytes[0] != 1 {
		t.Fatalf("recorded vertex byte = %d, want 1", OpQueueOps(&queue)[0].Bytes[0])
	}
	if MeshLayout(&draw.Mesh)[0] != (Attr(0, Float32x3)) {
		t.Fatalf("recorded layout = %+v, want original", MeshLayout(&draw.Mesh))
	}
	if ParameterColor(&(draw.Material.Params()[0])) != (m.Color{R: 1}) {
		t.Fatalf("recorded material color = %+v, want red", ParameterColor(&(draw.Material.Params()[0])))
	}
	if ParameterNum(&(draw.Params[0])) != 1 {
		t.Fatalf("recorded draw parameter = %v, want 1", ParameterNum(&(draw.Params[0])))
	}
}

func TestBufferWithBytesCopyDataControlsOwnership(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	copied := make([]byte, 12)
	borrowed := make([]byte, 12)
	copied[0] = 1
	borrowed[0] = 2
	layout := []VertexAttr{Attr(0, Float32x3)}

	queue.Draw(Mesh(BufferWithBytes(copied, true), TopologyTriangleList, layout...), testMaterial())
	queue.Draw(Mesh(BufferWithBytes(borrowed, false), TopologyTriangleList, layout...), testMaterial())
	copied[0] = 9
	borrowed[0] = 10

	if got := OpQueueOps(&queue)[0].Bytes[0]; got != 1 {
		t.Fatalf("copied mesh byte = %d, want 1", got)
	}
	if got := OpQueueOps(&queue)[2].Bytes[0]; got != 10 {
		t.Fatalf("borrowed mesh byte = %d, want 10", got)
	}
	retainedOps := OpQueueOps(&queue)
	queue.Reset()
	if retainedOps[2].Bytes != nil {
		t.Fatal("reset retained borrowed mesh bytes")
	}
}

func TestTextureWithBytesCopyDataControlsOwnership(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	copied := []byte{1, 2, 3, 4}
	borrowed := []byte{5, 6, 7, 8}
	OpQueueBakeTextureIfNeeded(&queue, TextureWithBytes(1, 1, FormatRGBA8, copied, true, false))
	OpQueueBakeTextureIfNeeded(&queue, TextureWithBytes(1, 1, FormatRGBA8, borrowed, false, false))

	copied[0] = 9
	borrowed[0] = 10
	if got := OpQueueOps(&queue)[0].Bytes[0]; got != 1 {
		t.Fatalf("copied temporary texture byte = %d, want 1", got)
	}
	if got := OpQueueOps(&queue)[1].Bytes[0]; got != 10 {
		t.Fatalf("borrowed temporary texture byte = %d, want 10", got)
	}
	retainedOps := OpQueueOps(&queue)
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
		TextureParam("MaterialTexture", TextureWithResource("shared.png")),
		BufferParam("MaterialBuffer", BufferWithBytes([]byte{1, 2, 3, 4}, true)),
	)
	drawTexture := TextureWithBytes(1, 1, FormatRGBA8, []byte{5, 6, 7, 8}, true, false)
	drawBuffer := BufferWithBytes([]byte{9, 10, 11, 12}, true)

	queue.Draw(triangle(), material,
		TextureParam("DrawTexture", drawTexture),
		BufferParam("DrawBuffer", drawBuffer),
	)
	draw := &OpQueueOps(queue)[len(OpQueueOps(queue))-1]
	for _, param := range append(draw.Material.Params(), draw.Params...) {
		switch ParameterKind(&param) {
		case ParamTexture:
			// An inline run is baked into a temporary texture at record time
			// and comes back as an id; a path is left alone for the render
			// thread's cache. Either way no pixels survive the recording.
			texture := ParameterTextureRef(&param)
			if texture.Blob.Len() != 0 || (texture.Path() == "" && texture.ID() == 0) {
				t.Errorf("texture param %q was not remapped to a baked ID", param.Name())
			}
		case ParamBuffer:
			if BufferSource(ParameterBufferRef(&param)) != BufferSourceBaked || ParameterBuffer(&param).ID() == 0 || BufferBytes(ParameterBufferRef(&param)).Len() != 0 {
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
	first := OpQueueBakeTextureIfNeeded(&queue, TextureWithBytes(1, 1, FormatRGBA8, []byte{1, 2, 3, 4}, true, false))
	second := OpQueueBakeTextureIfNeeded(&queue, TextureWithBytes(1, 1, FormatRGBA8, []byte{5, 6, 7, 8}, true, false))
	if first.ID() == second.ID() {
		t.Fatal("simultaneously used temporary textures share an ID")
	}

	queue.Reset()
	reused := OpQueueBakeTextureIfNeeded(&queue, TextureWithBytes(1, 1, FormatRGBA8, []byte{9, 10, 11, 12}, true, false))
	if reused.ID() != first.ID() {
		t.Errorf("reused temporary texture ID = %d, want %d", reused.ID(), first.ID())
	}
	if len(OpQueueOps(&queue)) != 1 || OpQueueOps(&queue)[0].Kind != OpBakeTexture {
		t.Fatal("temporary texture did not populate one bake op")
	}
}

func TestBakedResourcesTranslateToBakedBindings(t *testing.T) {
	p := newPlugin()
	layout := ShaderLayout{
		UniformSize: 80, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []UniformMember{{Name: "mvp", Offset: 0}},
		Resources: []ShaderResource{
			{Name: "MainSampler", Sampler: true, Group: 1, Binding: 0},
			{Name: "MainTexture", Group: 1, Binding: 1},
			{Name: "Data", StorageBuffer: true, Group: 1, Binding: 2},
		},
	}
	backend := &fakeBackend{layout: &layout}
	k := newTestKernel(t, p)
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	var texture TextureDescr
	var buffer BufferDescr
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		texture = resources.BakeTexture(1, 1, FormatRGBA8, []byte{255, 255, 255, 255}, true, false)
		buffer = resources.BakeBuffer([]byte{1, 2, 3, 4}, true)
	})
	w := recordList(t, k)
	material := testMaterial(
		TextureParam("MainTexture", texture),
		SamplerParam("MainSampler", SamplerDesc{}),
		BufferParam("Data", buffer),
	)
	w.Draw(triangle(), material, MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	var gotTexture TextureID
	var gotBuffer BufferID
	for i := range backend.lastOps {
		op := &backend.lastOps[i]
		switch op.kind {
		case testOpSetTexture:
			gotTexture = op.texture
		case testOpSetBuffer:
			gotBuffer = op.buffer
		}
	}
	if gotTexture != texture.ID() {
		t.Errorf("bound baked texture = %d, want %d", gotTexture, texture.ID())
	}
	if gotBuffer != buffer.ID() {
		t.Errorf("bound baked buffer = %d, want %d", gotBuffer, buffer.ID())
	}

	withResourceQueue(t, k, func(resources *ResourceQueue) {
		if got := resources.ReBakeTexture(texture, 2, 1, FormatRGBA8, make([]byte, 8), true, false); got.ID() != texture.ID() {
			t.Errorf("rebaked texture = %d, want %d", got.ID(), texture.ID())
		}
		if got := resources.ReBakeBuffer(buffer, []byte{5, 6, 7, 8}, true); got.ID() != buffer.ID() {
			t.Errorf("rebaked buffer = %d, want %d", got.ID(), buffer.ID())
		}
	})
	w = recordList(t, k)
	w.Draw(triangle(), material, MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	var rebakedTexture TextureID
	var rebakedBuffer BufferID
	for i := range backend.lastOps {
		op := &backend.lastOps[i]
		switch op.kind {
		case testOpBakeTexture:
			rebakedTexture = op.texture
		case testOpBakeBuffer:
			if op.bufferKind == BufferStorage {
				rebakedBuffer = op.buffer
			}
		}
	}
	if rebakedTexture != texture.ID() || rebakedBuffer != buffer.ID() {
		t.Errorf("rebaked resources = (%d, %d), want (%d, %d)", rebakedTexture, rebakedBuffer, texture.ID(), buffer.ID())
	}
	if countOps(backend.lastOps, testOpSetTexture) != 1 || countOps(backend.lastOps, testOpSetBuffer) != 1 {
		t.Error("rebake frame did not bind the baked resources")
	}

	withResourceQueue(t, k, func(resources *ResourceQueue) {
		resources.ReleaseTexture(texture)
		resources.ReleaseBuffer(buffer)
	})
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	var releasedTexture TextureID
	var releasedBuffer BufferID
	for i := range backend.lastOps {
		op := &backend.lastOps[i]
		switch op.kind {
		case testOpReleaseTexture:
			releasedTexture = op.texture
		case testOpReleaseBuffer:
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
	k.ExecuteCommand[PresentCmd](PresentRequest{}) // ensure clean start
	w := recordRaw(t, k)
	w.Pass(PassDescr{
		Target: ScreenTarget(), Depth: DepthAuto(),
		Load: LoadClear, Clear: m.Color{A: 1},
		DepthLoad: LoadClear, DepthClear: 0.5,
	})
	mat := testMaterial(ColorParam("tint", m.Color{R: 1, G: 1, B: 1, A: 1}))
	w.Draw(triangle(), mat, MatParam("mvp", m.NewMat4()))

	k.ExecuteCommand[PresentCmd](PresentRequest{})
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
	if pass.Clear != (m.Color{A: 1}) || pass.DepthClear != 0.5 || pass.Load != LoadClear || pass.DepthLoad != LoadClear {
		t.Errorf("clear state = (%+v, %v, %v, %v), want (black, 0.5, clear, clear)", pass.Clear, pass.DepthClear, pass.Load, pass.DepthLoad)
	}
	if got := countOps(backend.lastOps, testOpDraw); got != 1 {
		t.Errorf("draw ops = %d, want 1", got)
	}
	if got := countOps(backend.lastOps, testOpSetUniformBlock); got != 1 {
		t.Errorf("uniform ops = %d, want 1", got)
	}
	// The single draw is non-indexed with 3 vertices.
	for i := range backend.lastOps {
		if backend.lastOps[i].kind == testOpDraw {
			first, count, indexed := backend.lastOps[i].first, backend.lastOps[i].count, backend.lastOps[i].indexed
			if first != 0 || count != 3 || indexed {
				t.Errorf("draw = (first %d, count %d, indexed %v), want (0, 3, false)", first, count, indexed)
			}
		}
	}
}

// The module is built once across three frames although testMaterial builds a
// fresh descriptor for every draw. Inline text is identified by the run of bytes
// it wraps, not copied and not hashed, so a literal is one address and one entry
// however many descriptors are written around it - which is why ShaderWithText
// routes through the string rather than through []byte(text), whose conversion
// would allocate a fresh identity per call and a cache entry per draw.
func TestConsumeCachesShaderAndPipeline(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	for frame := 0; frame < 3; frame++ {
		w := recordList(t, k)
		w.Draw(triangle(), testMaterial(), MatParam("mvp", m.NewMat4()))
		w.Draw(triangle(), testMaterial(), MatParam("mvp", m.Translation4(1, 0, 0)))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
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
	vertices := BufferWithBytes(make([]byte, 3*stride), false)
	w := recordList(t, k)
	w.Draw(Mesh(vertices, TopologyTriangleList,
		Attr(0, Float32x3), Attr(12, Float32x4),
	), testMaterial())
	w.Draw(Mesh(vertices, TopologyTriangleList,
		Attr(0, Float32x2), Attr(12, Float32x4),
	), testMaterial())
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.pipes != 2 {
		t.Fatalf("pipelines for distinct equal-stride layouts = %d, want 2", backend.pipes)
	}
}

func TestVertexLayoutKeyUsesZeroOnlyForMissingAttributes(t *testing.T) {
	key, ok := VertexLayoutKeyOf([]VertexAttr{
		Attr(0, Float32),
		Attr(256, Float32x4),
		Attr(MaxVertexStride-4, Unorm1010102),
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
	tooMany := make([]VertexAttr, MaxVertexAttributes+1)
	for i := range tooMany {
		tooMany[i] = Attr(0, Float32)
	}
	tests := []struct {
		name   string
		layout []VertexAttr
	}{
		{name: "unknown type", layout: []VertexAttr{Attr(0, UnknownVertexType)}},
		{name: "type count sentinel", layout: []VertexAttr{Attr(0, VertexTypeCount)}},
		{name: "negative offset", layout: []VertexAttr{Attr(-1, Float32)}},
		{name: "attribute exceeds stride limit", layout: []VertexAttr{Attr(MaxVertexStride-2, Float32)}},
		{name: "too many attributes", layout: tooMany},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, ok := VertexLayoutKeyOf(test.layout); ok {
				t.Fatal("unsupported vertex layout was accepted")
			}
		})
	}
}

func TestDrawParamsPackByNameAndOverrideMaterial(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	layout := ShaderLayout{
		UniformSize: 80, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []UniformMember{{Name: "camera", Offset: 0}, {Name: "tint", Offset: 64}},
	}
	backend := &fakeBackend{layout: &layout}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	red := m.Color{R: 1, G: 0, B: 0, A: 1}
	green := m.Color{R: 0, G: 1, B: 0, A: 1}
	camera := m.Translation4(3, 4, 5)
	w := recordList(t, k)
	mat := testMaterial(ColorParam("tint", red))
	w.Draw(triangle(), mat, MatParam("camera", camera), ColorParam("tint", green))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	var params []byte
	for i := range backend.lastOps {
		if backend.lastOps[i].kind == testOpSetUniformBlock {
			params = backend.lastOps[i].data
		}
	}
	if len(params) != UniformBlockSize {
		t.Fatalf("params len = %d, want a whole %d-byte block", len(params), UniformBlockSize)
	}
	// Past the reflected uniform size the block stays zero.
	if !bytes.Equal(params[80:], make([]byte, UniformBlockSize-80)) {
		t.Errorf("block tail past the reflected size = %x..., want zeroes", params[80:84])
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
		mat := testMaterial(TextureParam("MainTexture", TextureWithResource("hero.png")), SamplerParam("MainSampler", SamplerDesc{}))
		w.Draw(triangle(), mat, MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
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
		w.Draw(triangle(), Material(ShaderWithResource("shader.wgsl")), MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
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
	material := Material(
		ShaderWithResource("shader.wgsl"),
		TextureParam("MainTexture", TextureWithResource("hero.png")),
	)

	w := recordList(t, k)
	w.Draw(triangle(), material, MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	// Both caches are asked about through what a game can see - one upload and
	// one module built for one path each - rather than by reaching into the
	// tables that hold them.
	if backend.uploads != 1 || backend.shaders != 1 || len(p.translator.pipelines) != 1 {
		t.Fatalf("initial uploads/shaders/pipelines = (%d, %d, %d), want 1 each", backend.uploads, backend.shaders, len(p.translator.pipelines))
	}
	k.ExecuteCommand[ReleaseCachedResourceCmd](ReleaseCachedResourceRequest{})
	k.ExecuteCommand[ReleaseCachedResourceCmd](ReleaseCachedResourceRequest{Path: "hero.png"})
	k.ExecuteCommand[ReleaseCachedResourceCmd](ReleaseCachedResourceRequest{Path: "shader.wgsl"})
	w = recordList(t, k)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(p.translator.pipelines) != 0 || len(p.translator.layouts) != 0 || len(p.translator.parameterPlans) != 0 {
		t.Fatal("path release retained translator cache entries")
	}
	if len(backend.freedShaders) != 1 || len(backend.freedPipelines) != 1 {
		t.Fatalf("path release freed shaders/pipelines = (%d, %d), want (1, 1)", len(backend.freedShaders), len(backend.freedPipelines))
	}
	if countOps(backend.lastOps, testOpReleaseTexture) != 1 {
		t.Fatal("path release did not emit one texture release")
	}

	w = recordList(t, k)
	w.Draw(triangle(), material, MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
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
	var explicit TextureDescr
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		explicit = resources.BakeTexture(1, 1, FormatRGBA8, []byte{1, 2, 3, 4}, true, false)
	})
	w := recordList(t, k)
	w.Draw(triangle(), testMaterial(
		TextureParam("MainTexture", TextureWithResource("hero.png")),
		SamplerParam("MainSampler", SamplerDesc{}),
	), MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	// The cached texture is observable as the bake this frame emitted that the
	// game did not ask for by hand, which is all the test needs to name it.
	cachedTexture := TextureID(0)
	for i := range backend.lastOps {
		if backend.lastOps[i].kind == testOpBakeTexture && backend.lastOps[i].texture != explicit.ID() {
			cachedTexture = backend.lastOps[i].texture
		}
	}
	if cachedTexture == 0 {
		t.Fatalf("no cached texture bake beside the explicit one (%d)", explicit.ID())
	}

	k.ExecuteCommand[FreeCachedResourcesCmd](FreeCachedResourcesRequest{})
	w = recordList(t, k)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if len(p.translator.pipelines) != 0 || len(p.translator.samplers) != 0 || len(p.translator.layouts) != 0 || len(p.translator.parameterPlans) != 0 {
		t.Fatal("global cleanup retained translator-owned caches")
	}
	if len(backend.freedShaders) != 1 || len(backend.freedPipelines) != 1 || len(backend.freedSamplers) != 1 {
		t.Fatalf("global cleanup freed shader/pipeline/sampler = (%d, %d, %d), want (1, 1, 1)", len(backend.freedShaders), len(backend.freedPipelines), len(backend.freedSamplers))
	}
	if countOps(backend.lastOps, testOpReleaseTexture) != 1 {
		t.Fatal("global cached cleanup did not release exactly one cached texture")
	}
	for i := range backend.lastOps {
		if backend.lastOps[i].kind == testOpReleaseTexture && backend.lastOps[i].texture != cachedTexture {
			t.Fatalf("global cleanup released texture %d, want cached texture %d (explicit %d)", backend.lastOps[i].texture, cachedTexture, explicit.ID())
		}
	}
}

// A failed texture read is cached as failed and reported once, which is the
// behaviour the shader cache always had and the texture cache never did: a
// missing file used to be re-opened and fully re-decoded every frame, forever,
// reported nowhere.
//
// This replaces TestFailedTextureResourceLoadIsRetried, which asserted that
// every-frame re-read as a feature. The rename is the point: a behaviour was
// deliberately given up, and release is the only lever that retries now.
func TestFailedTextureIsCachedAsFailedAndEvictedByItsPath(t *testing.T) {
	files := fstest.MapFS{}
	filesystem := &countingFS{FS: files}
	p := newPlugin()
	errorsReported := 0
	k := newTestKernelWith(t, p, filesystem, func(err error) error {
		errorsReported++
		return nil
	})
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	material := testMaterial(TextureParam("MainTexture", TextureWithResource("later.png")))
	draw := func() {
		w := recordList(t, k)
		w.Draw(triangle(), material, MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
	}
	release := func() {
		k.ExecuteCommand[ReleaseCachedResourceCmd](ReleaseCachedResourceRequest{Path: "later.png"})
	}

	draw()
	if filesystem.opens != 1 || errorsReported != 1 || backend.uploads != 0 {
		t.Fatalf("first frame opens/errors/uploads = (%d, %d, %d), want (1, 1, 0)", filesystem.opens, errorsReported, backend.uploads)
	}

	// The second frame neither re-opens nor re-reports: the entry says what
	// happened, and the binding falls back to id 0 on the strength of it.
	draw()
	if filesystem.opens != 1 || errorsReported != 1 {
		t.Fatalf("second frame opens/errors = (%d, %d), want (1, 1)", filesystem.opens, errorsReported)
	}

	// Releasing the path drops the entry and forgets the report it filed, so the
	// path is opened again and the failure is said afresh instead of swallowed.
	release()
	draw()
	if filesystem.opens != 2 || errorsReported != 2 {
		t.Fatalf("after eviction opens/errors = (%d, %d), want (2, 2)", filesystem.opens, errorsReported)
	}

	// And with the file finally in place, the same release is what makes the
	// retry upload rather than fail again.
	files["later.png"] = &fstest.MapFile{Data: testPNG(t)}
	release()
	draw()
	if filesystem.opens != 3 || backend.uploads != 1 || errorsReported != 2 {
		t.Fatalf("after the fix opens/uploads/errors = (%d, %d, %d), want (3, 1, 2)", filesystem.opens, backend.uploads, errorsReported)
	}
}

// A failed shader is cached as failed and reported once. Without the cache the
// next frame re-reads every source, re-flattens, re-fails and re-reports - at
// the frame rate. Releasing the path is what retries it, and nothing in the
// engine issues that release: the command below is the lever, and a game is the
// only thing that pulls it.
func TestFailedShaderIsCachedAsFailedAndEvictedByItsPath(t *testing.T) {
	files := fstest.MapFS{}
	filesystem := &countingFS{FS: files}
	p := newPlugin()
	errorsReported := 0
	engine := kernel.New(nil).Handler(func(err error) error {
		errorsReported++
		return nil
	}).WithPlugins(storageplugin.New(), permanentAdapter{}, readMountAdapter{storage.ReadMount{Id: "test", Priority: 10, FS: filesystem}}, appplugin.New(), mainLoopAdapter{}, p, testPlugin{})
	go engine.Run()
	t.Cleanup(engine.Quit)
	<-engine.Ready()
	k := engine.Executioner()
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	material := Material(ShaderWithResource("later.wgsl"))
	draw := func() {
		w := recordList(t, k)
		w.Draw(triangle(), material)
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
	}

	draw()
	if filesystem.opens != 1 || errorsReported != 1 || backend.shaders != 0 {
		t.Fatalf("first frame opens/errors/shaders = (%d, %d, %d), want (1, 1, 0)", filesystem.opens, errorsReported, backend.shaders)
	}

	// The second frame neither re-reads nor re-reports: the entry says what
	// happened, and the draw is dropped on the strength of it.
	draw()
	if filesystem.opens != 1 || errorsReported != 1 {
		t.Fatalf("second frame opens/errors = (%d, %d), want (1, 1)", filesystem.opens, errorsReported)
	}

	// The path was recorded as the failed entry's one source even though it could
	// not be opened, which is what makes a release naming it reach the failure at
	// all: a failed entry has to evict like any other, or a coarse release leaves
	// failures behind it.
	files["later.wgsl"] = &fstest.MapFile{Data: []byte("const marker = 1;")}
	k.ExecuteCommand[ReleaseCachedResourceCmd](ReleaseCachedResourceRequest{Path: "later.wgsl"})
	draw()
	if filesystem.opens != 2 || backend.shaders != 1 || errorsReported != 1 {
		t.Fatalf("after eviction opens/shaders/errors = (%d, %d, %d), want (2, 1, 1)",
			filesystem.opens, backend.shaders, errorsReported)
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
	materials := []MaterialDescr{
		Material(ShaderWithResource("root.wgsl")),
		Material(ShaderWithResource("root.wgsl", ShaderDefine("HQ"))),
		Material(ShaderWithText("#include shared.wgsl\nconst inline = 1;")),
	}
	w := recordList(t, k)
	for _, material := range materials {
		w.Draw(triangle(), material)
	}
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if backend.shaders != 3 {
		t.Fatalf("modules built = %d, want 3", backend.shaders)
	}

	k.ExecuteCommand[ReleaseCachedResourceCmd](ReleaseCachedResourceRequest{Path: "shared.wgsl"})
	w = recordList(t, k)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	// Every one of the three went, which is what handing all three ids back says:
	// an entry the release missed would still be holding its module.
	if len(backend.freedShaders) != 3 {
		t.Fatalf("freed %d backend shaders, want 3", len(backend.freedShaders))
	}
}

// An inline shader with no #include has an empty source set, so no path can
// name it: flatten notes a source only for a resource root and for the includes
// it walks. FreeCachedResourcesCmd is its release, and it is the only one.
//
// It gains no second route. Noting the inline root under the name a report calls
// it by would make it evictable, and that is a sentinel inside a namespace of
// real paths - inventing a path for the one thing defined by not having one is
// the wrong direction.
func TestAnInlineShaderWithNoIncludeIsReachedOnlyByTheGlobalFree(t *testing.T) {
	filesystem := &countingFS{FS: fstest.MapFS{
		"root.wgsl": &fstest.MapFile{Data: []byte("const root = 1;")},
	}}
	p := newPlugin()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	w := recordList(t, k)
	w.Draw(triangle(), Material(ShaderWithResource("root.wgsl")))
	w.Draw(triangle(), Material(ShaderWithText("const inline = 1;")))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if backend.shaders != 2 {
		t.Fatalf("modules built = %d, want 2", backend.shaders)
	}

	// The one path in the frame evicts the module rooted at it and leaves the
	// inline one, which no path reaches.
	k.ExecuteCommand[ReleaseCachedResourceCmd](ReleaseCachedResourceRequest{Path: "root.wgsl"})
	w = recordList(t, k)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if len(backend.freedShaders) != 1 {
		t.Fatalf("releasing a path freed %d modules, want 1", len(backend.freedShaders))
	}

	k.ExecuteCommand[FreeCachedResourcesCmd](FreeCachedResourcesRequest{})
	w = recordList(t, k)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if len(backend.freedShaders) != 2 {
		t.Fatalf("the global free left the inline module resident: freed %d, want 2", len(backend.freedShaders))
	}
}

func TestTextureWithBytesReuploadsEveryFrame(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	pixels := []byte{255, 255, 255, 255}
	texture := TextureWithBytes(1, 1, FormatRGBA8, pixels, false, false)
	for frame := 0; frame < 2; frame++ {
		w := recordList(t, k)
		w.Draw(triangle(), testMaterial(TextureParam("MainTexture", texture)), MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
		for i := range backend.lastOps {
			if backend.lastOps[i].kind == testOpBakeTexture {
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
	layout := ShaderLayout{
		UniformSize: 80, UniformGroup: 0, UniformBinding: 0,
		Uniforms:  []UniformMember{{Name: "mvp", Offset: 0}},
		Resources: []ShaderResource{{Name: "Data", StorageBuffer: true, Group: 1, Binding: 0}},
	}
	k := newTestKernel(t, p)
	backend := &fakeBackend{layout: &layout}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	buffer := BufferWithBytes([]byte{1, 2, 3, 4}, false)
	for frame := 0; frame < 2; frame++ {
		w := recordList(t, k)
		w.Draw(triangle(), testMaterial(BufferParam("Data", buffer)), MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
		storageBakes := 0
		for i := range backend.lastOps {
			if backend.lastOps[i].kind != testOpBakeBuffer {
				continue
			}
			id := backend.lastOps[i].buffer
			if backend.lastOps[i].bufferKind != BufferStorage {
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
func recordList(t *testing.T, k kernel.Executioner) *OpQueue {
	t.Helper()
	queue := recordRaw(t, k)
	queue.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto(), Label: "test"})
	return queue
}

// recordRaw opens the frame's queue with no pass declared.
func recordRaw(t *testing.T, k kernel.Executioner) *OpQueue {
	t.Helper()
	var captured *OpQueue
	k.ExecuteCommand[recordCmd](recordRequest{fn: func(l *OpQueue) { captured = l }})
	return captured
}

type recordCmd kernel.Command[recordRequest, recordResponse]
type recordRequest struct{ fn func(*OpQueue) }
type recordResponse struct{}

func withResourceQueue(t *testing.T, k kernel.Executioner, use func(*ResourceQueue)) {
	t.Helper()
	k.ExecuteCommand[recordResourcesCmd](recordResourcesRequest{fn: use})
}

type recordResourcesCmd kernel.Command[recordResourcesRequest, recordResourcesResponse]
type recordResourcesRequest struct{ fn func(*ResourceQueue) }
type recordResourcesResponse struct{}

func TestTemporaryBufferUploadsOnceForEveryDrawThatBindsIt(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	arena := make([]byte, 3*StorageAlignment)
	arena[0] = 7

	buffer := queue.TemporaryBuffer(arena, true)
	arena[0] = 9
	for i := range 3 {
		queue.Draw(triangle(), testMaterial(),
			BufferRangeParam("records", buffer, i*StorageAlignment, StorageAlignment))
	}

	bakes := 0
	for i := range OpQueueOps(&queue) {
		if OpQueueOps(&queue)[i].Kind == OpBakeBuffer && OpQueueOps(&queue)[i].BufferKind == BufferStorage {
			bakes++
			if OpQueueOps(&queue)[i].Bytes[0] != 7 {
				t.Fatal("the temporary arena aliases caller data past the call")
			}
		}
	}
	if bakes != 1 {
		t.Fatalf("the arena uploaded %d times, want once for the whole frame", bakes)
	}
	for i := range OpQueueOps(&queue) {
		if OpQueueOps(&queue)[i].Kind != OpDraw {
			continue
		}
		param := OpQueueOps(&queue)[i].Params[0]
		if ParameterBuffer(&param).ID() != buffer.ID() || BufferSource(ParameterBufferRef(&param)) != BufferSourceBaked {
			t.Fatalf("draw bound %+v, want the one baked arena %+v", ParameterBuffer(&param), buffer)
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
	backend := &fakeBackend{layout: &ShaderLayout{Resources: []ShaderResource{
		{Name: "records", StorageBuffer: true, Group: 0, Binding: 0},
	}}}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	w := recordRaw(t, k)
	w.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto()})
	w.Draw(triangle(), testMaterial(),
		BufferParam("records", BufferWithBytes(make([]byte, 64), true)))

	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if got := countOps(backend.lastOps, testOpDraw); got != 1 {
		t.Fatalf("draw ops = %d, want 1", got)
	}
	if got := countOps(backend.lastOps, testOpSetUniformBlock); got != 0 {
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
	for _, opts := range [][]ShaderOption{
		nil,
		{ShaderDefine("SKIN")},
		{ShaderDefine("MORPH")},
		{ShaderDefine("SKIN"), ShaderDefine("MORPH")},
	} {
		w.Draw(triangle(), Material(ShaderWithResource("scene.wgsl", opts...)))
	}
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.shaders != 4 {
		t.Fatalf("four supplies built %d modules, want 4", backend.shaders)
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
	k := newTestKernelWith(t, p, filesystem, func(err error) error {
		reported = append(reported, err)
		return nil
	})
	backend := &fakeBackend{shaderErr: errors.New("gogpu: parse error: line 3, column 12: expected ';'")}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	w := recordList(t, k)
	w.Draw(triangle(), Material(ShaderWithResource("root.wgsl", ShaderDefine("HQ"))))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(reported) != 1 {
		t.Fatalf("the frame reported %d errors, want 1: %v", len(reported), reported)
	}
	var refused ErrShaderSource
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
