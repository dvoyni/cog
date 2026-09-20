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
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// testMaterial builds a material with an inline (fake-compiled) shader.
func testMaterial(params ...gfx.ParameterDescr) gfx.MaterialDescr {
	return gfx.Material(gfx.ShaderWithText("//test"), params...)
}

// fakeBackend records the calls the translator makes and captures the last
// executed op stream so tests can assert the translation without a GPU.
type fakeBackend struct {
	// formats is what each texture was allocated or baked in, the fake's stand
	// in for gogpu's bakedTextureDescs: written when a bake is replayed and
	// dropped on release, so a texture is unknown until its frame's Execute.
	formats        map[gfx.TextureID]gfx.TextureFormat
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
	freedSamplers  []gfx.SamplerID
	freedShaders   []gfx.ShaderID
	freedPipelines []gfx.PipelineID
	lastPipelines  []gfx.PipelineDesc

	// lastOps is every call the last Execute's replay made, in replay order:
	// bakes, then each pass's render commands, then releases.
	lastOps    []backendOp
	lastPasses []gfx.PassDesc
	passDraws  []int
	views      [][3]int
	draws      []drawCall
	// indexBinds records every index buffer the pass bound and the width it was
	// bound at, which is the only place the width is observable: a draw call
	// carries a count, not a format.
	indexBinds    []indexBind
	execCount     int
	layout        *gfx.ShaderLayout
	presents      int
	presentAfter  int
	boundTextures []gfx.TextureID
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
	captureDescs   []gfx.CaptureDesc
	captureAfter   int
	captureLabels  [][]string
	takeCalls      int
	captureResult  func(gfx.CaptureDesc) gfx.Capture
	capturePending *gfx.Capture
	captureReady   *gfx.Capture
}

// Capture records the readback and prepares its result for the drain after the
// next Execute.
func (b *fakeBackend) Capture(desc gfx.CaptureDesc) {
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

func (b *fakeBackend) TakeCapture() (gfx.Capture, bool) {
	b.takeCalls++
	if b.captureReady == nil {
		return gfx.Capture{}, false
	}
	done := *b.captureReady
	b.captureReady = nil
	return done, true
}

// defaultCapture is a two-by-two image with padded rows, so that the ordinary
// path through a test still exercises the un-stride.
func defaultCapture() gfx.Capture {
	return paddedCapture(2, 2, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x * 60), G: uint8(y * 60), B: 7, A: 255}
	})
}

// paddedCapture builds what a backend hands back: rows padded to the GPU's own
// alignment, with the padding filled with a value that is not the picture, so
// a test can tell an un-stride from a straight copy.
func paddedCapture(width, height int, at func(x, y int) color.NRGBA) gfx.Capture {
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
	return gfx.Capture{
		Pixels: pixels, Width: width, Height: height,
		Format: gfx.FrameBufferFormat, BytesPerRow: rowBytes,
	}
}

func (b *fakeBackend) id() uint32 { b.nextID++; return b.nextID }

func (b *fakeBackend) Ready() bool { return true }

func (b *fakeBackend) NewTexture() gfx.TextureID { b.nextTex++; return gfx.TextureID(b.nextTex) }
func (b *fakeBackend) NewBuffer() gfx.BufferID   { b.nextBuf++; return gfx.BufferID(b.nextBuf) }

func (b *fakeBackend) NewSampler(gfx.SamplerDesc) (gfx.SamplerID, error) {
	b.samplers++
	return gfx.SamplerID(b.id()), nil
}
func (b *fakeBackend) FreeSampler(id gfx.SamplerID) { b.freedSamplers = append(b.freedSamplers, id) }
func (b *fakeBackend) NewShader(desc gfx.ShaderDesc) (gfx.ShaderID, error) {
	if b.shaderErr != nil {
		return 0, b.shaderErr
	}
	b.shaders++
	b.shaderCode = append(b.shaderCode[:0], desc.Code...)
	b.shaderLabels = append(b.shaderLabels, desc.Label)
	return gfx.ShaderID(b.id()), nil
}
func (b *fakeBackend) FreeShader(id gfx.ShaderID) { b.freedShaders = append(b.freedShaders, id) }

// ShaderLayout reports a fixed layout matching the built-in shader: mvp at 0,
// a "tint" color at 64 (80-byte block), plus a texture+sampler in group 1.
func (b *fakeBackend) ShaderLayout(gfx.ShaderID) gfx.ShaderLayout {
	if b.layout != nil {
		return *b.layout
	}
	return gfx.ShaderLayout{
		UniformSize: 80, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []gfx.UniformMember{{Name: "mvp", Offset: 0}, {Name: "tint", Offset: 64}},
		Resources: []gfx.ShaderResource{
			{Name: "MainSampler", Sampler: true, Group: 1, Binding: 0},
			{Name: "MainTexture", Group: 1, Binding: 1},
		},
	}
}
func (b *fakeBackend) NewPipeline(desc gfx.PipelineDesc) (gfx.PipelineID, error) {
	if b.pipelineErr != nil {
		return 0, b.pipelineErr
	}
	b.pipes++
	b.lastPipelines = append(b.lastPipelines, desc)
	return gfx.PipelineID(b.id()), nil
}
func (b *fakeBackend) FreePipeline(id gfx.PipelineID) {
	b.freedPipelines = append(b.freedPipelines, id)
}
func (b *fakeBackend) ScreenFramebuffer() (gfx.TextureViewID, int, int) {
	return 1, 100, 100
}

// Limits reports what a desktop adapter typically allows, which is far above
// the web floor gfx measures shaders against.
func (b *fakeBackend) Limits() gfx.Limits {
	return gfx.Limits{
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
func (b *fakeBackend) TextureFormat(id gfx.TextureID) (gfx.TextureFormat, bool) {
	format, ok := b.formats[id]
	return format, ok
}

func (b *fakeBackend) TextureView(texture gfx.TextureID, mip, layer int) gfx.TextureViewID {
	b.views = append(b.views, [3]int{int(texture), mip, layer})
	return gfx.TextureViewID(len(b.views))
}

func (b *fakeBackend) Execute(queue *gfx.Queue) {
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
	texture               gfx.TextureID
	buffer                gfx.BufferID
	bufferKind            gfx.BufferKind
	format                gfx.TextureFormat
	width, height, layers int
	layer                 int
	region                gfx.Region
	renderable            bool
	group, binding        int
	offset, size          int
	first, count          int
	indexed               bool
	data                  []byte
}

func (b *fakeBackend) BakeBuffer(id gfx.BufferID, kind gfx.BufferKind, size int, data []byte) {
	b.lastOps = append(b.lastOps, backendOp{kind: opBakeBuffer, buffer: id, bufferKind: kind, size: size, data: data})
}

func (b *fakeBackend) BakeTexture(id gfx.TextureID, width, height int, format gfx.TextureFormat, pixels []byte, mipmaps bool) {
	b.textures++
	b.uploads++
	b.recordFormat(id, format)
	b.lastOps = append(b.lastOps, backendOp{
		kind: opBakeTexture, texture: id, width: width, height: height, format: format, data: pixels,
	})
}

func (b *fakeBackend) AllocateTexture(id gfx.TextureID, desc gfx.TextureDesc) {
	b.recordFormat(id, desc.Format)
	b.lastOps = append(b.lastOps, backendOp{
		kind: opAllocateTexture, texture: id, width: desc.Width, height: desc.Height, layers: desc.Layers,
		format: desc.Format, renderable: desc.Renderable,
	})
}

func (b *fakeBackend) UpdateTexture(id gfx.TextureID, layer int, region gfx.Region, pixels []byte) {
	b.lastOps = append(b.lastOps, backendOp{kind: opUpdateTexture, texture: id, layer: layer, region: region, data: pixels})
}

func (b *fakeBackend) ReleaseBuffer(id gfx.BufferID) {
	b.lastOps = append(b.lastOps, backendOp{kind: opReleaseBuffer, buffer: id})
}

func (b *fakeBackend) recordFormat(id gfx.TextureID, format gfx.TextureFormat) {
	if b.formats == nil {
		b.formats = map[gfx.TextureID]gfx.TextureFormat{}
	}
	b.formats[id] = format
}

func (b *fakeBackend) ReleaseTexture(id gfx.TextureID) {
	delete(b.formats, id)
	b.lastOps = append(b.lastOps, backendOp{kind: opReleaseTexture, texture: id})
}

// BeginPass records the pass and returns the backend itself as its RenderPass,
// which counts the draws that land in it.
func (b *fakeBackend) BeginPass(desc gfx.PassDesc) gfx.RenderPass {
	b.lastPasses = append(b.lastPasses, desc)
	b.passDraws = append(b.passDraws, 0)
	return b
}

func (b *fakeBackend) EndPass(gfx.RenderPass) {}

// TransitionTextures records each barrier against the pass it precedes, so a
// test can assert not just that a transition happened but that it happened
// before the pass whose hazard it fixes.
func (b *fakeBackend) TransitionTextures(transitions []gfx.TextureTransition) {
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
func (b *fakeBackend) transitionBefore(want gfx.TextureTransition) (int, bool) {
	for _, placed := range b.transitions {
		if placed.TextureTransition == want {
			return placed.beforePass, true
		}
	}
	return -1, false
}

// placedTransition is one barrier and the pass it was recorded ahead of.
type placedTransition struct {
	gfx.TextureTransition
	beforePass int
}

// Present records the implicit present pass and how many declared passes had
// already run, so a test can assert both that it ran and that it ran last.
func (b *fakeBackend) Present() {
	b.presents++
	b.presentAfter = len(b.lastPasses)
}

func (b *fakeBackend) SetPipeline(gfx.PipelineID) {}
func (b *fakeBackend) SetParams(params []byte) {
	b.lastOps = append(b.lastOps, backendOp{kind: opSetParams, data: params})
}

// SetTexture records the binding so a test can assert which texture reached the
// GPU, which is the only way to tell a sampled render target from a draw that
// was silently dropped before it ever bound one.
func (b *fakeBackend) SetTexture(texture gfx.TextureID, group, binding int) {
	b.lastOps = append(b.lastOps, backendOp{kind: opSetTexture, texture: texture, group: group, binding: binding})
	b.boundTextures = append(b.boundTextures, texture)
}

// boundTexture reports whether the texture was bound at any point this frame.
func (b *fakeBackend) boundTexture(id gfx.TextureID) bool {
	return slices.Contains(b.boundTextures, id)
}
func (b *fakeBackend) SetSampler(_ gfx.SamplerID, group, binding int) {
	b.lastOps = append(b.lastOps, backendOp{kind: opSetSampler, group: group, binding: binding})
}
func (b *fakeBackend) SetVertexBuffer(gfx.BufferID, int) {}
func (b *fakeBackend) SetIndexBuffer(buffer gfx.BufferID, offset int, width gfx.IndexWidth) {
	b.indexBinds = append(b.indexBinds, indexBind{buffer: buffer, offset: offset, width: width})
}
func (b *fakeBackend) SetBuffer(group, binding int, buffer gfx.BufferID, offset, size int) {
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
	buffer gfx.BufferID
	offset int
	width  gfx.IndexWidth
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
type testGfxBackend kernel.Adapter[gfx.BackendPort]

// Name is the gfx plugin's, not this fixture's: the fixture locks gfx resources.
func (testPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{gfx.Name} }
func (testPlugin) Register(registrar *kernel.Registrar, _ any) error {
	adapter := &testAdapter{}
	registrar.ProvideAdapter[testGfxBackend](gfx.Backend(adapter))
	registrar.HandleCommand[attachBackendCmd](adapter.attachBackendCmdImpl)
	registrar.HandleCommand[recordCmd](recordCmdImpl)
	registrar.HandleCommand[recordResourcesCmd](recordResourcesCmdImpl)
	return nil
}

func recordCmdImpl() (kernel.Lock, kernel.Execute[recordRequest, recordResponse]) {
	var queue kernel.Write[*gfx.OpQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*gfx.OpQueue]()
		}, func(_ kernel.Kernel, req recordRequest) recordResponse {
			req.fn(queue.Get())
			return recordResponse{}
		}
}

func recordResourcesCmdImpl() (kernel.Lock, kernel.Execute[recordResourcesRequest, recordResourcesResponse]) {
	var queue kernel.Write[*gfx.ResourceQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*gfx.ResourceQueue]()
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
	config := map[kernel.PluginName]any{
		storage.Name: storage.Config{}.WithReadFS("test", 10, filesystem),
	}
	engine := kernel.New(config).Handler(handler).WithPlugins(storageplugin.New(), permanentAdapter{}, appplugin.New(), mainLoopAdapter{}, p, testPlugin{})
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

func triangle() gfx.MeshDescr {
	const stride = 28 // vec3 position + vec4 color
	return gfx.Mesh(
		gfx.BufferWithBytes(make([]byte, 3*stride), true),
		gfx.TopologyTriangleList,
		gfx.Attr(0, gfx.Float32x3),
		gfx.Attr(12, gfx.Float32x4),
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
	layout := gfx.ShaderLayout{
		UniformSize: 96, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []gfx.UniformMember{
			{Name: "mvp", Offset: 0},
			{Name: "tint", Offset: 64},
			{Name: "time", Offset: 80},
			{Name: "scale", Offset: 84},
		},
		Resources: []gfx.ShaderResource{
			{Name: "MainSampler", Sampler: true, Group: 1, Binding: 0},
			{Name: "MainTexture", Group: 1, Binding: 1},
			{Name: "Data", StorageBuffer: true, Group: 1, Binding: 2},
		},
	}
	backend := &fakeBackend{layout: &layout}
	translator := newTranslator()
	queue := testOpQueue(backend)
	mesh := gfx.Mesh(
		types.BakedBuffer(1, 3*28),
		gfx.TopologyTriangleList,
		gfx.Attr(0, gfx.Float32x3), gfx.Attr(12, gfx.Float32x4),
	)
	material := testMaterial(
		gfx.ColorParam("tint", m.Color{R: 1, G: 1, B: 1, A: 1}),
		gfx.FloatParam("scale", 1),
		gfx.TextureParam("MainTexture", types.BakedTexture(2, 0, 0)),
		gfx.SamplerParam("MainSampler", gfx.SamplerDesc{}),
		gfx.BufferParam("Data", types.BakedBuffer(3, 64)),
	)
	for range 100 {
		queue.Draw(mesh, material,
			gfx.MatParam("mvp", m.NewMat4()),
			gfx.FloatParam("time", 1),
			gfx.ColorParam("tint", m.Color{R: 0.5, A: 1}),
		)
	}
	// The zero Kernel is legal here because nothing this frame reaches it: every
	// texture in the material is already baked, so no cache loads and no failure
	// is reported. A kernel is only ever touched on a miss.
	translator.translate(kernel.Kernel{}, &queue, nil, backend, noFiles, gfx.CaptureDesc{}, false)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		translator.translate(kernel.Kernel{}, &queue, nil, backend, noFiles, gfx.CaptureDesc{}, false)
	}
}

type benchmarkGpuSink struct{}

func (benchmarkGpuSink) BakeBuffer(gfx.BufferID, gfx.BufferKind, int, []byte)                 {}
func (benchmarkGpuSink) BakeTexture(gfx.TextureID, int, int, gfx.TextureFormat, []byte, bool) {}
func (benchmarkGpuSink) AllocateTexture(gfx.TextureID, gfx.TextureDesc)                       {}
func (benchmarkGpuSink) UpdateTexture(gfx.TextureID, int, gfx.Region, []byte)                 {}
func (benchmarkGpuSink) SetPipeline(gfx.PipelineID)                                           {}
func (benchmarkGpuSink) SetParams([]byte)                                                     {}
func (benchmarkGpuSink) SetTexture(gfx.TextureID, int, int)                                   {}

func (benchmarkGpuSink) SetSampler(gfx.SamplerID, int, int)               {}
func (benchmarkGpuSink) SetVertexBuffer(gfx.BufferID, int)                {}
func (benchmarkGpuSink) SetIndexBuffer(gfx.BufferID, int, gfx.IndexWidth) {}
func (benchmarkGpuSink) SetBuffer(int, int, gfx.BufferID, int, int)       {}
func (benchmarkGpuSink) Draw(int, int, int, int, bool)                    {}
func (benchmarkGpuSink) ReleaseBuffer(gfx.BufferID)                       {}
func (benchmarkGpuSink) ReleaseTexture(gfx.TextureID)                     {}

func (benchmarkGpuSink) BeginPass(gfx.PassDesc) gfx.RenderPass      { return benchmarkGpuSink{} }
func (benchmarkGpuSink) EndPass(gfx.RenderPass)                     {}
func (benchmarkGpuSink) TransitionTextures([]gfx.TextureTransition) {}
func (benchmarkGpuSink) Present()                                   {}
func (benchmarkGpuSink) Capture(gfx.CaptureDesc)                    {}

func BenchmarkGpuQueueReplaySteadyState(b *testing.B) {
	var queue gfx.Queue
	queue.Reset()
	queue.BeginPass(gfx.PassDesc{Screen: true, DepthAuto: true})
	for i := range 100 {
		queue.BakeBuffer(gfx.BufferID(i+1), gfx.BufferVertex, 64, []byte{1})
		queue.SetPipeline(1)
		queue.SetParams([]byte{1})
		queue.SetVertexBuffer(gfx.BufferID(i+1), 0)
		queue.Draw(0, 3, 1, 0, false)
		queue.ReleaseBuffer(gfx.BufferID(i + 1))
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
	if len(types.OpQueuePasses(&queue)) != 0 || types.OpQueueCurrent(&queue) != -1 {
		t.Fatalf("after reset: %d passes, current %d, want none declared or selected", len(types.OpQueuePasses(&queue)), types.OpQueueCurrent(&queue))
	}
	first := queue.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), Load: gfx.LoadClear})
	second := queue.Pass(gfx.PassDescr{Order: 1, Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto()})
	if types.OpQueueSelectedPass(&queue) != 1 {
		t.Errorf("selected pass = %d, want the one just declared", types.OpQueueSelectedPass(&queue))
	}
	queue.SetPass(first)
	if types.OpQueueSelectedPass(&queue) != 0 {
		t.Errorf("selected pass = %d, want the re-selected first", types.OpQueueSelectedPass(&queue))
	}
	// An unknown reference leaves the selection alone rather than guessing.
	queue.SetPass(gfx.PassRef(99))
	if types.OpQueueSelectedPass(&queue) != 0 {
		t.Errorf("selected pass = %d, want the selection unchanged by an unknown ref", types.OpQueueSelectedPass(&queue))
	}
	_ = second

	queue.Reset()
	if len(types.OpQueuePasses(&queue)) != 0 || types.OpQueueCurrent(&queue) != -1 {
		t.Errorf("after reset: %d passes, current %d, want none selected", len(types.OpQueuePasses(&queue)), types.OpQueueCurrent(&queue))
	}
}

func testOpQueue(backend gfx.Backend) gfx.OpQueue {
	return *types.NewOpQueue(idsOf(backend))
}

// idsOf is the id source of a queue built outside a composition, which has no
// adapter handle to read.
func idsOf(backend types.IDMinter) types.IDSource {
	return func() types.IDMinter { return backend }
}

func TestBakeOpsAllocateBakedResourceIDs(t *testing.T) {
	backend := &fakeBackend{}
	queue := *types.NewResourceQueue(idsOf(backend))
	pixels := []byte{1, 2, 3, 4}
	buffer := queue.BakeBuffer(pixels, true)
	texture := queue.BakeTexture(1, 1, gfx.FormatRGBA8, pixels, true, false)
	rebakedBuffer := queue.ReBakeBuffer(buffer, pixels, true)
	rebakedTexture := queue.ReBakeTexture(texture, 1, 1, gfx.FormatRGBA8, pixels, true, false)

	if buffer.ID() == 0 || texture.ID() == 0 {
		t.Fatalf("baked handles = (%d, %d), want nonzero", buffer.ID(), texture.ID())
	}
	if rebakedBuffer.ID() != buffer.ID() || rebakedTexture.ID() != texture.ID() {
		t.Fatalf("rebaked handles = (%d, %d), want (%d, %d)", rebakedBuffer.ID(), rebakedTexture.ID(), buffer.ID(), texture.ID())
	}
	pixels[0] = 99
	for i := range types.ResourceQueueOps(&queue) {
		if types.ResourceQueueOps(&queue)[i].Bytes[0] != 1 {
			t.Fatalf("op %d did not copy caller data", i)
		}
	}
}

func TestBakeBufferCopyDataControlsOwnership(t *testing.T) {
	queue := *types.NewResourceQueue(idsOf(&fakeBackend{}))
	copied := []byte{1, 2, 3, 4}
	borrowed := []byte{5, 6, 7, 8}
	queue.BakeBuffer(copied, true)
	queue.BakeBuffer(borrowed, false)

	copied[0] = 9
	borrowed[0] = 10
	if got := types.ResourceQueueOps(&queue)[0].Bytes[0]; got != 1 {
		t.Fatalf("copied buffer byte = %d, want 1", got)
	}
	if got := types.ResourceQueueOps(&queue)[1].Bytes[0]; got != 10 {
		t.Fatalf("borrowed buffer byte = %d, want 10", got)
	}
	retainedOps := types.ResourceQueueOps(&queue)
	types.ResourceQueueReset(&queue)
	if retainedOps[1].Bytes != nil {
		t.Fatal("reset retained borrowed buffer bytes")
	}
}

func TestBakeTextureCopyDataControlsOwnership(t *testing.T) {
	queue := *types.NewResourceQueue(idsOf(&fakeBackend{}))
	copied := []byte{1, 2, 3, 4}
	borrowed := []byte{5, 6, 7, 8}
	queue.BakeTexture(1, 1, gfx.FormatRGBA8, copied, true, false)
	queue.BakeTexture(1, 1, gfx.FormatRGBA8, borrowed, false, false)

	copied[0] = 9
	borrowed[0] = 10
	if got := types.ResourceQueueOps(&queue)[0].Bytes[0]; got != 1 {
		t.Fatalf("copied texture byte = %d, want 1", got)
	}
	if got := types.ResourceQueueOps(&queue)[1].Bytes[0]; got != 10 {
		t.Fatalf("borrowed texture byte = %d, want 10", got)
	}
	retainedOps := types.ResourceQueueOps(&queue)
	types.ResourceQueueReset(&queue)
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
		texture := resources.AllocateTexture(64, 32, 4, gfx.FormatRGBA8)
		resources.UpdateTexture(texture, 2, gfx.Region{X: 5, Y: 7, Width: 1, Height: 1}, pixels, true)
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
			if op.width != 64 || op.height != 32 || op.layers != 4 || op.format != gfx.FormatRGBA8 {
				t.Fatalf("allocation metadata = (%d,%d,%d,%d)", op.width, op.height, op.layers, op.format)
			}
		case opUpdateTexture:
			if op.layer != 2 || op.region != (gfx.Region{X: 5, Y: 7, Width: 1, Height: 1}) || op.data[0] != 1 {
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

	var baked, bound gfx.TextureID
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
		texture = resources.BakeTexture(1, 1, gfx.FormatRGBA8, []byte{1, 2, 3, 4}, true, false)
	})
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})

	withResourceQueue(t, k, func(resources *gfx.ResourceQueue) {
		resources.ReBakeBuffer(buffer, []byte{5, 6, 7, 8}, true)
		resources.ReBakeTexture(texture, 1, 1, gfx.FormatRGBA8, []byte{5, 6, 7, 8}, true, false)
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
	w.Draw(triangle(), testMaterial(gfx.TextureParam("MainTexture", gfx.TextureWithBytes(1, 1, gfx.FormatRGBA8, []byte{1, 2, 3, 4}, false, false))), gfx.MatParam("mvp", m.NewMat4()))
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
	smallBuffer := types.OpQueueTemporaryBuffer(&queue, gfx.BufferVertex, small, true)
	largeBuffer := types.OpQueueTemporaryBuffer(&queue, gfx.BufferVertex, large, true)
	small[0] = 99
	if types.OpQueueOps(&queue)[0].Bytes[0] != 1 {
		t.Fatal("temporary bake op aliases caller data")
	}

	queue.Reset()
	fit := types.OpQueueTemporaryBuffer(&queue, gfx.BufferVertex, make([]byte, 12), true)
	if fit.ID() != largeBuffer.ID() {
		t.Errorf("best-fit buffer = %d, want %d", fit.ID(), largeBuffer.ID())
	}
	queue.Reset()
	resized := types.OpQueueTemporaryBuffer(&queue, gfx.BufferVertex, make([]byte, 32), true)
	if resized.ID() != largeBuffer.ID() {
		t.Errorf("resized buffer ID = %d, want reused %d", resized.ID(), largeBuffer.ID())
	}
	if types.OpQueueTemporaryBuffers(&queue)[1].Size < 32 {
		t.Errorf("resized size = %d, want at least 32", types.OpQueueTemporaryBuffers(&queue)[1].Size)
	}
	if smallBuffer.ID() == largeBuffer.ID() {
		t.Fatal("simultaneously used temporary buffers share an ID")
	}
}

func TestDrawStoresTemporaryBufferIDsWithoutInlineGeometry(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	mesh := triangle()
	queue.Draw(mesh, testMaterial(), gfx.MatParam("mvp", m.NewMat4()))

	if types.OpQueueOps(&queue)[0].Kind != types.OpBakeBuffer || types.OpQueueOps(&queue)[0].BufferKind != gfx.BufferVertex {
		t.Fatal("draw did not populate a vertex bake op first")
	}
	draw := &types.OpQueueOps(&queue)[1]
	if types.MeshVertices(&draw.Mesh).ID() == 0 || draw.Mesh.VertexCount() != 3 {
		t.Fatalf("draw vertex resource = (%d, %d), want nonzero ID and 3 vertices", types.MeshVertices(&draw.Mesh).ID(), draw.Mesh.VertexCount())
	}
	if types.BufferBytes(types.MeshVerticesRef(&draw.Mesh)).Len() != 0 {
		t.Fatalf("draw retained %d inline vertex bytes", types.BufferBytes(types.MeshVerticesRef(&draw.Mesh)).Len())
	}
	if len(types.OpQueueTemporaryBuffers(&queue)) != 1 || !types.OpQueueTemporaryBuffers(&queue)[0].Used {
		t.Fatal("draw did not lease one temporary vertex buffer")
	}
}

func TestOpQueueArenasPreserveCallerDataIsolation(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	vertices := make([]byte, 3*28)
	vertices[0] = 1
	layout := []gfx.VertexAttr{gfx.Attr(0, gfx.Float32x3), gfx.Attr(12, gfx.Float32x4)}
	materialParams := []gfx.ParameterDescr{gfx.ColorParam("tint", m.Color{R: 1})}
	drawParams := []gfx.ParameterDescr{gfx.FloatParam("time", 1)}

	queue.Draw(
		gfx.Mesh(gfx.BufferWithBytes(vertices, true), gfx.TopologyTriangleList, layout...),
		gfx.Material(gfx.ShaderWithText("//test"), materialParams...),
		drawParams...,
	)
	vertices[0] = 9
	layout[0] = gfx.Attr(4, gfx.Float32x2)
	materialParams[0] = gfx.ColorParam("tint", m.Color{G: 1})
	drawParams[0] = gfx.FloatParam("time", 9)

	draw := &types.OpQueueOps(&queue)[len(types.OpQueueOps(&queue))-1]
	if types.OpQueueOps(&queue)[0].Bytes[0] != 1 {
		t.Fatalf("recorded vertex byte = %d, want 1", types.OpQueueOps(&queue)[0].Bytes[0])
	}
	if types.MeshLayout(&draw.Mesh)[0] != (gfx.Attr(0, gfx.Float32x3)) {
		t.Fatalf("recorded layout = %+v, want original", types.MeshLayout(&draw.Mesh))
	}
	if types.ParameterColor(&(draw.Material.Params()[0])) != (m.Color{R: 1}) {
		t.Fatalf("recorded material color = %+v, want red", types.ParameterColor(&(draw.Material.Params()[0])))
	}
	if types.ParameterNum(&(draw.Params[0])) != 1 {
		t.Fatalf("recorded draw parameter = %v, want 1", types.ParameterNum(&(draw.Params[0])))
	}
}

func TestBufferWithBytesCopyDataControlsOwnership(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	copied := make([]byte, 12)
	borrowed := make([]byte, 12)
	copied[0] = 1
	borrowed[0] = 2
	layout := []gfx.VertexAttr{gfx.Attr(0, gfx.Float32x3)}

	queue.Draw(gfx.Mesh(gfx.BufferWithBytes(copied, true), gfx.TopologyTriangleList, layout...), testMaterial())
	queue.Draw(gfx.Mesh(gfx.BufferWithBytes(borrowed, false), gfx.TopologyTriangleList, layout...), testMaterial())
	copied[0] = 9
	borrowed[0] = 10

	if got := types.OpQueueOps(&queue)[0].Bytes[0]; got != 1 {
		t.Fatalf("copied mesh byte = %d, want 1", got)
	}
	if got := types.OpQueueOps(&queue)[2].Bytes[0]; got != 10 {
		t.Fatalf("borrowed mesh byte = %d, want 10", got)
	}
	retainedOps := types.OpQueueOps(&queue)
	queue.Reset()
	if retainedOps[2].Bytes != nil {
		t.Fatal("reset retained borrowed mesh bytes")
	}
}

func TestTextureWithBytesCopyDataControlsOwnership(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	copied := []byte{1, 2, 3, 4}
	borrowed := []byte{5, 6, 7, 8}
	types.OpQueueBakeTextureIfNeeded(&queue, gfx.TextureWithBytes(1, 1, gfx.FormatRGBA8, copied, true, false))
	types.OpQueueBakeTextureIfNeeded(&queue, gfx.TextureWithBytes(1, 1, gfx.FormatRGBA8, borrowed, false, false))

	copied[0] = 9
	borrowed[0] = 10
	if got := types.OpQueueOps(&queue)[0].Bytes[0]; got != 1 {
		t.Fatalf("copied temporary texture byte = %d, want 1", got)
	}
	if got := types.OpQueueOps(&queue)[1].Bytes[0]; got != 10 {
		t.Fatalf("borrowed temporary texture byte = %d, want 10", got)
	}
	retainedOps := types.OpQueueOps(&queue)
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
	drawTexture := gfx.TextureWithBytes(1, 1, gfx.FormatRGBA8, []byte{5, 6, 7, 8}, true, false)
	drawBuffer := gfx.BufferWithBytes([]byte{9, 10, 11, 12}, true)

	queue.Draw(triangle(), material,
		gfx.TextureParam("DrawTexture", drawTexture),
		gfx.BufferParam("DrawBuffer", drawBuffer),
	)
	draw := &types.OpQueueOps(queue)[len(types.OpQueueOps(queue))-1]
	for _, param := range append(draw.Material.Params(), draw.Params...) {
		switch types.ParameterKind(&param) {
		case types.ParamTexture:
			// An inline run is baked into a temporary texture at record time
			// and comes back as an id; a path is left alone for the render
			// thread's cache. Either way no pixels survive the recording.
			texture := types.ParameterTextureRef(&param)
			if texture.Blob.Len() != 0 || (texture.Path() == "" && texture.ID() == 0) {
				t.Errorf("texture param %q was not remapped to a baked ID", param.Name())
			}
		case types.ParamBuffer:
			if types.BufferSource(types.ParameterBufferRef(&param)) != gfx.BufferSourceBaked || types.ParameterBuffer(&param).ID() == 0 || types.BufferBytes(types.ParameterBufferRef(&param)).Len() != 0 {
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
	first := types.OpQueueBakeTextureIfNeeded(&queue, gfx.TextureWithBytes(1, 1, gfx.FormatRGBA8, []byte{1, 2, 3, 4}, true, false))
	second := types.OpQueueBakeTextureIfNeeded(&queue, gfx.TextureWithBytes(1, 1, gfx.FormatRGBA8, []byte{5, 6, 7, 8}, true, false))
	if first.ID() == second.ID() {
		t.Fatal("simultaneously used temporary textures share an ID")
	}

	queue.Reset()
	reused := types.OpQueueBakeTextureIfNeeded(&queue, gfx.TextureWithBytes(1, 1, gfx.FormatRGBA8, []byte{9, 10, 11, 12}, true, false))
	if reused.ID() != first.ID() {
		t.Errorf("reused temporary texture ID = %d, want %d", reused.ID(), first.ID())
	}
	if len(types.OpQueueOps(&queue)) != 1 || types.OpQueueOps(&queue)[0].Kind != types.OpBakeTexture {
		t.Fatal("temporary texture did not populate one bake op")
	}
}

func TestBakedResourcesTranslateToBakedBindings(t *testing.T) {
	p := newPlugin()
	layout := gfx.ShaderLayout{
		UniformSize: 80, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []gfx.UniformMember{{Name: "mvp", Offset: 0}},
		Resources: []gfx.ShaderResource{
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
		texture = resources.BakeTexture(1, 1, gfx.FormatRGBA8, []byte{255, 255, 255, 255}, true, false)
		buffer = resources.BakeBuffer([]byte{1, 2, 3, 4}, true)
	})
	w := recordList(t, k)
	material := testMaterial(
		gfx.TextureParam("MainTexture", texture),
		gfx.SamplerParam("MainSampler", gfx.SamplerDesc{}),
		gfx.BufferParam("Data", buffer),
	)
	w.Draw(triangle(), material, gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	var gotTexture gfx.TextureID
	var gotBuffer gfx.BufferID
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
		if got := resources.ReBakeTexture(texture, 2, 1, gfx.FormatRGBA8, make([]byte, 8), true, false); got.ID() != texture.ID() {
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

	var rebakedTexture gfx.TextureID
	var rebakedBuffer gfx.BufferID
	for i := range backend.lastOps {
		op := &backend.lastOps[i]
		switch op.kind {
		case opBakeTexture:
			rebakedTexture = op.texture
		case opBakeBuffer:
			if op.bufferKind == gfx.BufferStorage {
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

	var releasedTexture gfx.TextureID
	var releasedBuffer gfx.BufferID
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
		Load: gfx.LoadClear, Clear: m.Color{A: 1},
		DepthLoad: gfx.LoadClear, DepthClear: 0.5,
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
	if pass.Clear != (m.Color{A: 1}) || pass.DepthClear != 0.5 || pass.Load != gfx.LoadClear || pass.DepthLoad != gfx.LoadClear {
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
	w.Draw(gfx.Mesh(vertices, gfx.TopologyTriangleList,
		gfx.Attr(0, gfx.Float32x3), gfx.Attr(12, gfx.Float32x4),
	), testMaterial())
	w.Draw(gfx.Mesh(vertices, gfx.TopologyTriangleList,
		gfx.Attr(0, gfx.Float32x2), gfx.Attr(12, gfx.Float32x4),
	), testMaterial())
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.pipes != 2 {
		t.Fatalf("pipelines for distinct equal-stride layouts = %d, want 2", backend.pipes)
	}
}

func TestVertexLayoutKeyUsesZeroOnlyForMissingAttributes(t *testing.T) {
	key, ok := types.VertexLayoutKeyOf([]gfx.VertexAttr{
		gfx.Attr(0, gfx.Float32),
		gfx.Attr(256, gfx.Float32x4),
		gfx.Attr(types.MaxVertexStride-4, gfx.Unorm1010102),
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
	tooMany := make([]gfx.VertexAttr, types.MaxVertexAttributes+1)
	for i := range tooMany {
		tooMany[i] = gfx.Attr(0, gfx.Float32)
	}
	tests := []struct {
		name   string
		layout []gfx.VertexAttr
	}{
		{name: "unknown type", layout: []gfx.VertexAttr{gfx.Attr(0, gfx.UnknownVertexType)}},
		{name: "type count sentinel", layout: []gfx.VertexAttr{gfx.Attr(0, types.VertexTypeCount)}},
		{name: "negative offset", layout: []gfx.VertexAttr{gfx.Attr(-1, gfx.Float32)}},
		{name: "attribute exceeds stride limit", layout: []gfx.VertexAttr{gfx.Attr(types.MaxVertexStride-2, gfx.Float32)}},
		{name: "too many attributes", layout: tooMany},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, ok := types.VertexLayoutKeyOf(test.layout); ok {
				t.Fatal("unsupported vertex layout was accepted")
			}
		})
	}
}

func TestDrawParamsPackByNameAndOverrideMaterial(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	layout := gfx.ShaderLayout{
		UniformSize: 80, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []gfx.UniformMember{{Name: "camera", Offset: 0}, {Name: "tint", Offset: 64}},
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
		mat := testMaterial(gfx.TextureParam("MainTexture", gfx.TextureWithResource("hero.png")), gfx.SamplerParam("MainSampler", gfx.SamplerDesc{}))
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
	// Both caches are asked about through what a game can see - one upload and
	// one module built for one path each - rather than by reaching into the
	// tables that hold them.
	if backend.uploads != 1 || backend.shaders != 1 || len(p.translator.pipelines) != 1 {
		t.Fatalf("initial uploads/shaders/pipelines = (%d, %d, %d), want 1 each", backend.uploads, backend.shaders, len(p.translator.pipelines))
	}
	k.ExecuteCommand[gfx.ReleaseCachedResourceCmd](gfx.ReleaseCachedResourceRequest{})
	k.ExecuteCommand[gfx.ReleaseCachedResourceCmd](gfx.ReleaseCachedResourceRequest{Path: "hero.png"})
	k.ExecuteCommand[gfx.ReleaseCachedResourceCmd](gfx.ReleaseCachedResourceRequest{Path: "shader.wgsl"})
	w = recordList(t, k)
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(p.translator.pipelines) != 0 || len(p.translator.layouts) != 0 || len(p.translator.parameterPlans) != 0 {
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
		explicit = resources.BakeTexture(1, 1, gfx.FormatRGBA8, []byte{1, 2, 3, 4}, true, false)
	})
	w := recordList(t, k)
	w.Draw(triangle(), testMaterial(
		gfx.TextureParam("MainTexture", gfx.TextureWithResource("hero.png")),
		gfx.SamplerParam("MainSampler", gfx.SamplerDesc{}),
	), gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	// The cached texture is observable as the bake this frame emitted that the
	// game did not ask for by hand, which is all the test needs to name it.
	cachedTexture := gfx.TextureID(0)
	for i := range backend.lastOps {
		if backend.lastOps[i].kind == opBakeTexture && backend.lastOps[i].texture != explicit.ID() {
			cachedTexture = backend.lastOps[i].texture
		}
	}
	if cachedTexture == 0 {
		t.Fatalf("no cached texture bake beside the explicit one (%d)", explicit.ID())
	}

	k.ExecuteCommand[gfx.FreeCachedResourcesCmd](gfx.FreeCachedResourcesRequest{})
	w = recordList(t, k)
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if len(p.translator.pipelines) != 0 || len(p.translator.samplers) != 0 || len(p.translator.layouts) != 0 || len(p.translator.parameterPlans) != 0 {
		t.Fatal("global cleanup retained translator-owned caches")
	}
	if len(backend.freedShaders) != 1 || len(backend.freedPipelines) != 1 || len(backend.freedSamplers) != 1 {
		t.Fatalf("global cleanup freed shader/pipeline/sampler = (%d, %d, %d), want (1, 1, 1)", len(backend.freedShaders), len(backend.freedPipelines), len(backend.freedSamplers))
	}
	if countOps(backend.lastOps, opReleaseTexture) != 1 {
		t.Fatal("global cached cleanup did not release exactly one cached texture")
	}
	for i := range backend.lastOps {
		if backend.lastOps[i].kind == opReleaseTexture && backend.lastOps[i].texture != cachedTexture {
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
	material := testMaterial(gfx.TextureParam("MainTexture", gfx.TextureWithResource("later.png")))
	draw := func() {
		w := recordList(t, k)
		w.Draw(triangle(), material, gfx.MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
	}
	release := func() {
		k.ExecuteCommand[gfx.ReleaseCachedResourceCmd](gfx.ReleaseCachedResourceRequest{Path: "later.png"})
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
	config := map[kernel.PluginName]any{
		storage.Name: storage.Config{}.WithReadFS("test", 10, filesystem),
	}
	engine := kernel.New(config).Handler(func(err error) error {
		errorsReported++
		return nil
	}).WithPlugins(storageplugin.New(), permanentAdapter{}, appplugin.New(), mainLoopAdapter{}, p, testPlugin{})
	go engine.Run()
	t.Cleanup(engine.Quit)
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
	k.ExecuteCommand[gfx.ReleaseCachedResourceCmd](gfx.ReleaseCachedResourceRequest{Path: "later.wgsl"})
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
	if backend.shaders != 3 {
		t.Fatalf("modules built = %d, want 3", backend.shaders)
	}

	k.ExecuteCommand[gfx.ReleaseCachedResourceCmd](gfx.ReleaseCachedResourceRequest{Path: "shared.wgsl"})
	w = recordList(t, k)
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
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
	w.Draw(triangle(), gfx.Material(gfx.ShaderWithResource("root.wgsl")))
	w.Draw(triangle(), gfx.Material(gfx.ShaderWithText("const inline = 1;")))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if backend.shaders != 2 {
		t.Fatalf("modules built = %d, want 2", backend.shaders)
	}

	// The one path in the frame evicts the module rooted at it and leaves the
	// inline one, which no path reaches.
	k.ExecuteCommand[gfx.ReleaseCachedResourceCmd](gfx.ReleaseCachedResourceRequest{Path: "root.wgsl"})
	w = recordList(t, k)
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if len(backend.freedShaders) != 1 {
		t.Fatalf("releasing a path freed %d modules, want 1", len(backend.freedShaders))
	}

	k.ExecuteCommand[gfx.FreeCachedResourcesCmd](gfx.FreeCachedResourcesRequest{})
	w = recordList(t, k)
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
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
	texture := gfx.TextureWithBytes(1, 1, gfx.FormatRGBA8, pixels, false, false)
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
	layout := gfx.ShaderLayout{
		UniformSize: 80, UniformGroup: 0, UniformBinding: 0,
		Uniforms:  []gfx.UniformMember{{Name: "mvp", Offset: 0}},
		Resources: []gfx.ShaderResource{{Name: "Data", StorageBuffer: true, Group: 1, Binding: 0}},
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
			if backend.lastOps[i].bufferKind != gfx.BufferStorage {
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
	arena := make([]byte, 3*gfx.StorageAlignment)
	arena[0] = 7

	buffer := queue.TemporaryBuffer(arena, true)
	arena[0] = 9
	for i := range 3 {
		queue.Draw(triangle(), testMaterial(),
			gfx.BufferRangeParam("records", buffer, i*gfx.StorageAlignment, gfx.StorageAlignment))
	}

	bakes := 0
	for i := range types.OpQueueOps(&queue) {
		if types.OpQueueOps(&queue)[i].Kind == types.OpBakeBuffer && types.OpQueueOps(&queue)[i].BufferKind == gfx.BufferStorage {
			bakes++
			if types.OpQueueOps(&queue)[i].Bytes[0] != 7 {
				t.Fatal("the temporary arena aliases caller data past the call")
			}
		}
	}
	if bakes != 1 {
		t.Fatalf("the arena uploaded %d times, want once for the whole frame", bakes)
	}
	for i := range types.OpQueueOps(&queue) {
		if types.OpQueueOps(&queue)[i].Kind != types.OpDraw {
			continue
		}
		param := types.OpQueueOps(&queue)[i].Params[0]
		if types.ParameterBuffer(&param).ID() != buffer.ID() || types.BufferSource(types.ParameterBufferRef(&param)) != gfx.BufferSourceBaked {
			t.Fatalf("draw bound %+v, want the one baked arena %+v", types.ParameterBuffer(&param), buffer)
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
	backend := &fakeBackend{layout: &gfx.ShaderLayout{Resources: []gfx.ShaderResource{
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
