package internal

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"math"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/assets"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"

	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// fakeBackend records the calls the translator makes and captures the last
// executed op stream so tests can assert the translation without a GPU.
type fakeBackend struct {
	// uniforms is the frame's uniform arena as BakeUniforms handed it over,
	// which SetUniformBlock reads its block back out of.
	uniforms []byte
	// formats is what each texture was allocated or baked in, the fake's stand
	// in for gogpu's bakedTextureDescs: written when a bake is replayed and
	// dropped on release, so a texture is unknown until its frame's Execute.
	formats        map[types.TextureID]types.TextureFormat
	nextID         uint32
	nextTex        uint32
	nextBuf        uint32
	pipes          int
	textures       int
	samplers       int
	uploads        int
	pipelineErr    error
	freedSamplers  []types.SamplerID
	freedShaders   []types.ShaderID
	freedPipelines []types.PipelineID
	lastPipelines  []PipelineDesc

	// Shaders: ids reserved from their own counter, every module CreateShader
	// was handed, and the reflection port's hand-written answer - reflection
	// when set, then layout, then defaultLayout - with every source it was
	// asked about.
	nextShader     uint32
	createdShaders []createdShader
	createErr      error
	reflection     *shader.ShaderLayout
	reflectErr     error
	reflected      []string

	// lastOps is every call the last Execute's replay made, in replay order:
	// bakes, then each pass's render commands, then releases.
	lastOps    []backendOp
	lastPasses []types.PassDescr
	passDraws  []int
	draws      []drawCall
	// indexBinds records every index buffer the pass bound and the width it was
	// bound at, which is the only place the width is observable: a draw call
	// carries a count, not a format.
	indexBinds    []indexBind
	execCount     int
	layout        *shader.ShaderLayout
	presents      int
	presentAfter  int
	boundTextures []types.TextureID
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
	captureDescs   []types.CaptureDesc
	captureAfter   int
	captureLabels  [][]string
	takeCalls      int
	captureResult  func(types.CaptureDesc) Capture
	capturePending *Capture
	captureReady   *Capture
}

// Capture records the readback and prepares its result for the drain after the
// next Execute.
func (b *fakeBackend) Capture(desc types.CaptureDesc) {
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
		Format: types.FrameBufferFormat, BytesPerRow: rowBytes,
	}
}

func (b *fakeBackend) id() uint32 { b.nextID++; return b.nextID }

func (b *fakeBackend) Ready() bool { return true }

func (b *fakeBackend) NewTexture() types.TextureID { b.nextTex++; return types.TextureID(b.nextTex) }
func (b *fakeBackend) NewBuffer() types.BufferID   { b.nextBuf++; return types.BufferID(b.nextBuf) }

func (b *fakeBackend) NewSampler(types.SamplerDesc) (types.SamplerID, error) {
	b.samplers++
	return types.SamplerID(b.id()), nil
}
func (b *fakeBackend) FreeSampler(id types.SamplerID) { b.freedSamplers = append(b.freedSamplers, id) }
func (b *fakeBackend) FreeShader(id types.ShaderID)   { b.freedShaders = append(b.freedShaders, id) }

// createdShader is one module CreateShader was handed.
type createdShader struct {
	id    types.ShaderID
	label string
	code  string
}

func (b *fakeBackend) ReserveShader() types.ShaderID {
	b.nextShader++
	return types.ShaderID(b.nextShader)
}

func (b *fakeBackend) CreateShader(id types.ShaderID, desc shader.ShaderDesc) error {
	if b.createErr != nil {
		return b.createErr
	}
	b.createdShaders = append(b.createdShaders, createdShader{id: id, label: desc.Label, code: string(desc.Code)})
	return nil
}

func (b *fakeBackend) ReflectShader(code []byte) (shader.ShaderLayout, error) {
	b.reflected = append(b.reflected, string(code))
	if b.reflectErr != nil {
		return shader.ShaderLayout{}, b.reflectErr
	}
	if b.reflection != nil {
		return *b.reflection, nil
	}
	if b.layout != nil {
		return *b.layout, nil
	}
	return defaultLayout(), nil
}

// defaultLayout is what the fake reflects when a test names no layout: an mvp
// and a tint uniform in group 0, and a texture with its sampler in group 1.
func defaultLayout() shader.ShaderLayout {
	return shader.ShaderLayout{
		Resources: []shader.ShaderResource{
			{Name: "mvp", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 64},
			{Name: "tint", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 1, Size: 16},
			{Name: "MainSampler", Kind: shader.ResourceSampler, Group: 1, Binding: 0},
			{Name: "MainTexture", Group: 1, Binding: 1},
		},
	}
}
func (b *fakeBackend) NewPipeline(desc PipelineDesc) (types.PipelineID, error) {
	if b.pipelineErr != nil {
		return 0, b.pipelineErr
	}
	b.pipes++
	b.lastPipelines = append(b.lastPipelines, desc)
	return types.PipelineID(b.id()), nil
}
func (b *fakeBackend) FreePipeline(id types.PipelineID) {
	b.freedPipelines = append(b.freedPipelines, id)
}
func (b *fakeBackend) ScreenFramebuffer() (types.TextureViewID, int, int) {
	return 1, 100, 100
}

// Limits reports what a desktop adapter typically allows, which is far above
// the web floor gfx measures shaders against.
func (b *fakeBackend) Limits() types.PipelineLimits {
	return types.PipelineLimits{
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
func (b *fakeBackend) TextureFormat(id types.TextureID) (types.TextureFormat, bool) {
	format, ok := b.formats[id]
	return format, ok
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
	texture               types.TextureID
	buffer                types.BufferID
	bufferKind            types.BufferKind
	format                types.TextureFormat
	width, height, layers int
	layer                 int
	region                m.Recti
	renderable            bool
	group, binding        int
	offset, size          int
	first, count          int
	indexed               bool
	data                  []byte
}

func (b *fakeBackend) BakeBuffer(id types.BufferID, kind types.BufferKind, size int, data []byte) {
	b.lastOps = append(b.lastOps, backendOp{kind: testOpBakeBuffer, buffer: id, bufferKind: kind, size: size, data: data})
}

func (b *fakeBackend) BakeTexture(id types.TextureID, width, height int, format types.TextureFormat, pixels []byte, mipmaps bool) {
	b.textures++
	b.uploads++
	b.recordFormat(id, format)
	b.lastOps = append(b.lastOps, backendOp{
		kind: testOpBakeTexture, texture: id, width: width, height: height, format: format, data: pixels,
	})
}

func (b *fakeBackend) AllocateTexture(id types.TextureID, desc TextureDesc) {
	b.recordFormat(id, desc.Format)
	b.lastOps = append(b.lastOps, backendOp{
		kind: testOpAllocateTexture, texture: id, width: desc.Width, height: desc.Height, layers: desc.Layers,
		format: desc.Format, renderable: desc.Renderable,
	})
}

func (b *fakeBackend) UpdateTexture(id types.TextureID, layer int, region m.Recti, pixels []byte) {
	b.lastOps = append(b.lastOps, backendOp{kind: testOpUpdateTexture, texture: id, layer: layer, region: region, data: pixels})
}

func (b *fakeBackend) ReleaseBuffer(id types.BufferID) {
	b.lastOps = append(b.lastOps, backendOp{kind: testOpReleaseBuffer, buffer: id})
}

func (b *fakeBackend) recordFormat(id types.TextureID, format types.TextureFormat) {
	if b.formats == nil {
		b.formats = map[types.TextureID]types.TextureFormat{}
	}
	b.formats[id] = format
}

func (b *fakeBackend) ReleaseTexture(id types.TextureID) {
	delete(b.formats, id)
	b.lastOps = append(b.lastOps, backendOp{kind: testOpReleaseTexture, texture: id})
}

// BeginPass records the pass and returns the backend itself as its RenderPass,
// which counts the draws that land in it.
func (b *fakeBackend) BeginPass(desc types.PassDescr) RenderPass {
	b.lastPasses = append(b.lastPasses, desc)
	b.passDraws = append(b.passDraws, 0)
	return b
}

func (b *fakeBackend) EndPass(RenderPass) {}

// TransitionTextures records each barrier against the pass it precedes, so a
// test can assert not just that a transition happened but that it happened
// before the pass whose hazard it fixes.
func (b *fakeBackend) TransitionTextures(transitions []types.TextureTransition) {
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
func (b *fakeBackend) transitionBefore(want types.TextureTransition) (int, bool) {
	for _, placed := range b.transitions {
		if placed.TextureTransition == want {
			return placed.beforePass, true
		}
	}
	return -1, false
}

// placedTransition is one barrier and the pass it was recorded ahead of.
type placedTransition struct {
	types.TextureTransition
	beforePass int
}

// Present records the implicit present pass and how many declared passes had
// already run, so a test can assert both that it ran and that it ran last.
func (b *fakeBackend) Present() {
	b.presents++
	b.presentAfter = len(b.lastPasses)
}

func (b *fakeBackend) SetPipeline(types.PipelineID) {}
func (b *fakeBackend) BakeUniforms(arena []byte)    { b.uniforms = arena }
func (b *fakeBackend) SetUniformBlock(group, binding, offset, size int) {
	b.lastOps = append(b.lastOps, backendOp{
		kind: testOpSetUniformBlock, group: group, binding: binding, offset: offset, size: size,
		data: bytes.Clone(b.uniforms[offset : offset+size]),
	})
}

// SetTexture records the binding so a test can assert which texture reached the
// GPU, which is the only way to tell a sampled render target from a draw that
// was silently dropped before it ever bound one.
func (b *fakeBackend) SetTexture(texture types.TextureID, group, binding int) {
	b.lastOps = append(b.lastOps, backendOp{kind: testOpSetTexture, texture: texture, group: group, binding: binding})
	b.boundTextures = append(b.boundTextures, texture)
}

// boundTexture reports whether the texture was bound at any point this frame.
func (b *fakeBackend) boundTexture(id types.TextureID) bool {
	return slices.Contains(b.boundTextures, id)
}
func (b *fakeBackend) SetSampler(_ types.SamplerID, group, binding int) {
	b.lastOps = append(b.lastOps, backendOp{kind: testOpSetSampler, group: group, binding: binding})
}
func (b *fakeBackend) SetVertexBuffer(types.BufferID, int) {}
func (b *fakeBackend) SetIndexBuffer(buffer types.BufferID, offset int, width types.IndexWidth) {
	b.indexBinds = append(b.indexBinds, indexBind{buffer: buffer, offset: offset, width: width})
}
func (b *fakeBackend) SetBuffer(group, binding int, buffer types.BufferID, offset, size int) {
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
	buffer types.BufferID
	offset int
	width  types.IndexWidth
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
		}, func(k kernel.Kernel, req recordRequest) recordResponse {
			if req.withKernel != nil {
				req.withKernel(k, queue.Get())
				return recordResponse{}
			}
			req.fn(queue.Get())
			return recordResponse{}
		}
}

func recordResourcesCmdImpl() (kernel.Lock, kernel.Execute[recordResourcesRequest, recordResourcesResponse]) {
	var queue kernel.Write[*ResourceQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*ResourceQueue]()
		}, func(k kernel.Kernel, req recordResourcesRequest) recordResourcesResponse {
			if req.withKernel != nil {
				req.withKernel(k, queue.Get())
				return recordResourcesResponse{}
			}
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

func triangle() types.MeshDescr {
	const stride = 28 // vec3 position + vec4 color
	return types.MeshDescrWithVertices(
		types.BufferDescrWithBlob(assets.NewBlob(make([]byte, 3*stride)), true),
		types.TopologyTriangleList,
		types.VertexAttribute{Offset: 0, Type: types.Float32x3},
		types.VertexAttribute{Offset: 12, Type: types.Float32x4},
	)
}

type benchmarkGpuSink struct{}

func (benchmarkGpuSink) BakeUniforms([]byte)                                      {}
func (benchmarkGpuSink) BakeBuffer(types.BufferID, types.BufferKind, int, []byte) {}
func (benchmarkGpuSink) BakeTexture(types.TextureID, int, int, types.TextureFormat, []byte, bool) {
}
func (benchmarkGpuSink) AllocateTexture(types.TextureID, TextureDesc)        {}
func (benchmarkGpuSink) UpdateTexture(types.TextureID, int, m.Recti, []byte) {}
func (benchmarkGpuSink) SetPipeline(types.PipelineID)                        {}
func (benchmarkGpuSink) SetUniformBlock(int, int, int, int)                  {}
func (benchmarkGpuSink) SetTexture(types.TextureID, int, int)                {}

func (benchmarkGpuSink) SetSampler(types.SamplerID, int, int)                 {}
func (benchmarkGpuSink) SetVertexBuffer(types.BufferID, int)                  {}
func (benchmarkGpuSink) SetIndexBuffer(types.BufferID, int, types.IndexWidth) {}
func (benchmarkGpuSink) SetBuffer(int, int, types.BufferID, int, int)         {}
func (benchmarkGpuSink) Draw(int, int, int, int, bool)                        {}
func (benchmarkGpuSink) ReleaseBuffer(types.BufferID)                         {}
func (benchmarkGpuSink) ReleaseTexture(types.TextureID)                       {}

func (benchmarkGpuSink) BeginPass(types.PassDescr) RenderPass         { return benchmarkGpuSink{} }
func (benchmarkGpuSink) EndPass(RenderPass)                           {}
func (benchmarkGpuSink) TransitionTextures([]types.TextureTransition) {}
func (benchmarkGpuSink) Present()                                     {}
func (benchmarkGpuSink) Capture(types.CaptureDesc)                    {}

func BenchmarkGpuQueueReplaySteadyState(b *testing.B) {
	var queue Queue
	queue.Reset()
	queue.BeginPass(types.PassDescr{Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto()})
	for i := range 100 {
		queue.BakeBuffer(types.BufferID(i+1), types.BufferVertex, 64, []byte{1})
		queue.SetPipeline(1)
		queue.SetUniformBlock(0, 0, 16)[0] = 1
		queue.SetVertexBuffer(types.BufferID(i+1), 0)
		queue.Draw(0, 3, 1, 0, false)
		queue.ReleaseBuffer(types.BufferID(i + 1))
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

func TestPassRefsAreFrameLocal(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	queue.reset()
	first := queue.NewPass(types.PassDescr{Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), Load: types.LoadClear})
	second := queue.NewPass(types.PassDescr{Order: 1, Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto()})
	queue.Draw(second, triangle(), types.DrawStateID(0), 1, 0)
	queue.Draw(first, triangle(), types.DrawStateID(0), 1, 0)
	// An unknown reference names no pass rather than guessing one.
	queue.Draw(types.PassID(99), triangle(), types.DrawStateID(0), 1, 0)
	if got := drawCounts(&queue); !slices.Equal(got, []int{1, 1, 1}) {
		t.Errorf("draws per pass, then stray = %v, want [1 1 1]", got)
	}

	// A reference outlives nothing: after reset it names no pass until the new
	// frame declares one.
	queue.reset()
	if len(OpQueuePasses(&queue)) != 0 {
		t.Fatalf("after reset: %d passes, want none declared", len(OpQueuePasses(&queue)))
	}
	queue.Draw(first, triangle(), types.DrawStateID(0), 1, 0)
	if got := drawCounts(&queue); !slices.Equal(got, []int{1}) {
		t.Errorf("last frame's reference drew %v, want only a stray", got)
	}

	// A pass declared in a later frame reuses an earlier record, and starts
	// with none of that record's draws.
	queue.reset()
	queue.NewPass(types.PassDescr{Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto()})
	if got := drawCounts(&queue); !slices.Equal(got, []int{0, 0}) {
		t.Errorf("a reused pass record holds %v, want no draws", got)
	}
}

// drawCounts lists how many draws each declared pass holds, then the frame's
// stray draws.
func drawCounts(q *OpQueue) []int {
	var counts []int
	for _, pass := range q.passes {
		counts = append(counts, len(pass.Draws))
	}
	return append(counts, q.strayDraws)
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
	buffer := queue.UploadBuffer(queue.NewBuffer(), pixels, true)
	texture := queue.UploadTexture(queue.NewTexture(1, 1, 1, types.FormatRGBA8, false), 0, m.Recti{}, pixels, true)
	rebakedBuffer := queue.UploadBuffer(buffer, pixels, true)
	rebakedTexture := queue.UploadTexture(texture, 0, m.Recti{}, pixels, true)

	if buffer.ID == 0 || texture.Params.ID == 0 {
		t.Fatalf("baked handles = (%d, %d), want nonzero", buffer.ID, texture.Params.ID)
	}
	if rebakedBuffer.ID != buffer.ID || rebakedTexture.Params.ID != texture.Params.ID {
		t.Fatalf("rebaked handles = (%d, %d), want (%d, %d)", rebakedBuffer.ID, rebakedTexture.Params.ID, buffer.ID, texture.Params.ID)
	}
	pixels[0] = 99
	for i, op := range ResourceQueueOps(&queue) {
		if op.Kind != OpAllocateTexture && op.Bytes[0] != 1 {
			t.Fatalf("op %d did not copy caller data", i)
		}
	}
}

func TestBakeBufferCopyDataControlsOwnership(t *testing.T) {
	queue := *NewResourceQueue(idsOf(&fakeBackend{}))
	copied := []byte{1, 2, 3, 4}
	borrowed := []byte{5, 6, 7, 8}
	queue.UploadBuffer(queue.NewBuffer(), copied, true)
	queue.UploadBuffer(queue.NewBuffer(), borrowed, false)

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
	queue.UploadTexture(queue.NewTexture(1, 1, 1, types.FormatRGBA8, false), 0, m.Recti{}, copied, true)
	queue.UploadTexture(queue.NewTexture(1, 1, 1, types.FormatRGBA8, false), 0, m.Recti{}, borrowed, false)

	// Each upload is two ops, the allocation and then the pixels.
	copied[0] = 9
	borrowed[0] = 10
	if got := ResourceQueueOps(&queue)[1].Bytes[0]; got != 1 {
		t.Fatalf("copied texture byte = %d, want 1", got)
	}
	if got := ResourceQueueOps(&queue)[3].Bytes[0]; got != 10 {
		t.Fatalf("borrowed texture byte = %d, want 10", got)
	}
	retainedOps := ResourceQueueOps(&queue)
	ResourceQueueReset(&queue)
	if retainedOps[3].Bytes != nil {
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
		texture := resources.NewTexture(64, 32, 4, types.FormatRGBA8, false)
		resources.UploadTexture(texture, 2, m.Recti{X: 5, Y: 7, Width: 1, Height: 1}, pixels, true)
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
			if op.width != 64 || op.height != 32 || op.layers != 4 || op.format != types.FormatRGBA8 {
				t.Fatalf("allocation metadata = (%d,%d,%d,%d)", op.width, op.height, op.layers, op.format)
			}
		case testOpUpdateTexture:
			if op.layer != 2 || op.region != (m.Recti{X: 5, Y: 7, Width: 1, Height: 1}) || op.data[0] != 1 {
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
		w, ref := recordList(t, k)
		w.Draw(ref, triangle(), testSet(t, k, types.ShaderParameterTexture("MainTexture", types.TextureWithResource("persistent.png"))), 1, 0)
		k.ExecuteCommand[PresentCmd](PresentRequest{})
	}
	k.PublishEvent(app.RenderEvent{}).Wait()

	var baked, bound types.TextureID
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

	var buffer types.BufferDescr
	var texture types.TextureDescr
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		buffer = resources.UploadBuffer(resources.NewBuffer(), []byte{1, 2, 3, 4}, true)
		texture = resources.UploadTexture(resources.NewTexture(1, 1, 1, types.FormatRGBA8, false), 0, m.Recti{}, []byte{1, 2, 3, 4}, true)
	})
	k.ExecuteCommand[PresentCmd](PresentRequest{})

	withResourceQueue(t, k, func(resources *ResourceQueue) {
		resources.UploadBuffer(buffer, []byte{5, 6, 7, 8}, true)
		resources.UploadTexture(texture, 0, m.Recti{}, []byte{5, 6, 7, 8}, true)
		resources.ReleaseBuffer(buffer)
		resources.ReleaseTexture(texture)
	})
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if got := countOps(backend.lastOps, testOpBakeBuffer); got != 2 {
		t.Errorf("persistent buffer bakes = %d, want 2", got)
	}
	if got := countOps(backend.lastOps, testOpUpdateTexture); got != 2 {
		t.Errorf("persistent texture uploads = %d, want 2", got)
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

	set := testSet(t, k)
	w, ref := recordList(t, k)
	w.SetDrawParams(kernel.Kernel{}, set, types.ShaderParameterTexture("MainTexture", types.TextureWithBytes(1, 1, types.FormatRGBA8, []byte{1, 2, 3, 4}, false, false)))
	w.Draw(ref, triangle(), set, 1, 0)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	w, ref = recordList(t, k)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if countOps(backend.lastOps, testOpUpdateTexture) != 0 || countOps(backend.lastOps, testOpBakeBuffer) != 0 {
		t.Fatal("dropped frame retained temporary texture or geometry uploads")
	}
}

func TestOpQueueTemporaryBufferPool(t *testing.T) {
	backend := &fakeBackend{}
	queue := testOpQueue(backend)

	small := []byte{1, 2, 3, 4}
	large := make([]byte, 16)
	smallBuffer := OpQueueTemporaryBuffer(&queue, types.BufferVertex, small, true)
	largeBuffer := OpQueueTemporaryBuffer(&queue, types.BufferVertex, large, true)
	small[0] = 99
	if OpQueueResources(&queue)[0].Bytes[0] != 1 {
		t.Fatal("temporary bake op aliases caller data")
	}

	queue.reset()
	fit := OpQueueTemporaryBuffer(&queue, types.BufferVertex, make([]byte, 12), true)
	if fit.ID != largeBuffer.ID {
		t.Errorf("best-fit buffer = %d, want %d", fit.ID, largeBuffer.ID)
	}
	queue.reset()
	resized := OpQueueTemporaryBuffer(&queue, types.BufferVertex, make([]byte, 32), true)
	if resized.ID != largeBuffer.ID {
		t.Errorf("resized buffer ID = %d, want reused %d", resized.ID, largeBuffer.ID)
	}
	if OpQueueTemporaryBuffers(&queue)[1].Size < 32 {
		t.Errorf("resized size = %d, want at least 32", OpQueueTemporaryBuffers(&queue)[1].Size)
	}
	if smallBuffer.ID == largeBuffer.ID {
		t.Fatal("simultaneously used temporary buffers share an ID")
	}
}

func TestDrawStoresTemporaryBufferIDsWithoutInlineGeometry(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	ref := queue.NewPass(types.PassDescr{})
	mesh := triangle()
	queue.Draw(ref, mesh, types.DrawStateID(0), 1, 0)

	if bake := OpQueueResources(&queue); len(bake) != 1 || bake[0].Kind != OpBakeBuffer || bake[0].BufferKind != types.BufferVertex {
		t.Fatal("draw did not record one vertex bake")
	}
	draw := &queue.passes[0].Draws[0]
	if draw.Mesh.Vertices.ID == 0 || draw.Mesh.VertexCount != 3 {
		t.Fatalf("draw vertex resource = (%d, %d), want nonzero ID and 3 vertices", draw.Mesh.Vertices.ID, draw.Mesh.VertexCount)
	}
	if (&draw.Mesh.Vertices).Bytes.Len() != 0 {
		t.Fatalf("draw retained %d inline vertex bytes", (&draw.Mesh.Vertices).Bytes.Len())
	}
	if len(OpQueueTemporaryBuffers(&queue)) != 1 || !OpQueueTemporaryBuffers(&queue)[0].Used {
		t.Fatal("draw did not lease one temporary vertex buffer")
	}
}

func TestOpQueueArenasPreserveCallerDataIsolation(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	vertices := make([]byte, 3*28)
	vertices[0] = 1
	layout := []types.VertexAttribute{{Offset: 0, Type: types.Float32x3}, {Offset: 12, Type: types.Float32x4}}

	ref := queue.NewPass(types.PassDescr{})
	queue.Draw(ref,
		types.MeshDescrWithVertices(types.BufferDescrWithBlob(assets.NewBlob(vertices), true), types.TopologyTriangleList, layout...),
		types.DrawStateID(0), 1, 0,
	)
	vertices[0] = 9
	layout[0] = types.VertexAttribute{Offset: 4, Type: types.Float32x2}

	draw := &queue.passes[0].Draws[0]
	if OpQueueResources(&queue)[0].Bytes[0] != 1 {
		t.Fatalf("recorded vertex byte = %d, want 1", OpQueueResources(&queue)[0].Bytes[0])
	}
	if draw.Mesh.Layout[0] != (types.VertexAttribute{Offset: 0, Type: types.Float32x3}) {
		t.Fatalf("recorded layout = %+v, want original", draw.Mesh.Layout)
	}
}

func TestABlobBufferCopyDataControlsOwnership(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	copied := make([]byte, 12)
	borrowed := make([]byte, 12)
	copied[0] = 1
	borrowed[0] = 2
	layout := []types.VertexAttribute{{Offset: 0, Type: types.Float32x3}}

	ref := queue.NewPass(types.PassDescr{})
	queue.Draw(ref, types.MeshDescrWithVertices(types.BufferDescrWithBlob(assets.NewBlob(copied), true), types.TopologyTriangleList, layout...), types.DrawStateID(0), 1, 0)
	queue.Draw(ref, types.MeshDescrWithVertices(types.BufferDescrWithBlob(assets.NewBlob(borrowed), false), types.TopologyTriangleList, layout...), types.DrawStateID(0), 1, 0)
	copied[0] = 9
	borrowed[0] = 10

	if got := OpQueueResources(&queue)[0].Bytes[0]; got != 1 {
		t.Fatalf("copied mesh byte = %d, want 1", got)
	}
	if got := OpQueueResources(&queue)[1].Bytes[0]; got != 10 {
		t.Fatalf("borrowed mesh byte = %d, want 10", got)
	}
	retainedOps := OpQueueResources(&queue)
	queue.reset()
	if retainedOps[1].Bytes != nil {
		t.Fatal("reset retained borrowed mesh bytes")
	}
}

func TestTextureWithBytesCopyDataControlsOwnership(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	copied := []byte{1, 2, 3, 4}
	borrowed := []byte{5, 6, 7, 8}
	OpQueueBakeTextureIfNeeded(&queue, types.TextureWithBytes(1, 1, types.FormatRGBA8, copied, true, false))
	OpQueueBakeTextureIfNeeded(&queue, types.TextureWithBytes(1, 1, types.FormatRGBA8, borrowed, false, false))

	copied[0] = 9
	borrowed[0] = 10
	if got := OpQueueResources(&queue)[1].Bytes[0]; got != 1 {
		t.Fatalf("copied temporary texture byte = %d, want 1", got)
	}
	if got := OpQueueResources(&queue)[3].Bytes[0]; got != 10 {
		t.Fatalf("borrowed temporary texture byte = %d, want 10", got)
	}
	retainedOps := OpQueueResources(&queue)
	queue.reset()
	if retainedOps[3].Bytes != nil {
		t.Fatal("reset retained borrowed temporary texture pixels")
	}
}

func TestOpQueueTemporaryTexturePool(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	first := OpQueueBakeTextureIfNeeded(&queue, types.TextureWithBytes(1, 1, types.FormatRGBA8, []byte{1, 2, 3, 4}, true, false))
	second := OpQueueBakeTextureIfNeeded(&queue, types.TextureWithBytes(1, 1, types.FormatRGBA8, []byte{5, 6, 7, 8}, true, false))
	if first.Params.ID == second.Params.ID {
		t.Fatal("simultaneously used temporary textures share an ID")
	}

	queue.reset()
	reused := OpQueueBakeTextureIfNeeded(&queue, types.TextureWithBytes(1, 1, types.FormatRGBA8, []byte{9, 10, 11, 12}, true, false))
	if reused.Params.ID != first.Params.ID {
		t.Errorf("reused temporary texture ID = %d, want %d", reused.Params.ID, first.Params.ID)
	}
	ops := OpQueueResources(&queue)
	if len(ops) != 2 || ops[0].Kind != OpAllocateTexture || ops[1].Kind != OpUpdateTexture {
		t.Fatal("temporary texture did not record one allocation and one upload")
	}
	if ops[1].Region != (m.Recti{Width: 1, Height: 1}) {
		t.Errorf("temporary texture uploaded region %+v, want its whole layer", ops[1].Region)
	}
}

func TestBakedResourcesTranslateToBakedBindings(t *testing.T) {
	p := newPlugin()
	layout := shader.ShaderLayout{
		Resources: []shader.ShaderResource{
			{Name: "mvp", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 64},
			{Name: "MainSampler", Kind: shader.ResourceSampler, Group: 1, Binding: 0},
			{Name: "MainTexture", Group: 1, Binding: 1},
			{Name: "Data", Kind: shader.ResourceStorageBuffer, Group: 1, Binding: 2},
		},
	}
	backend := &fakeBackend{layout: &layout}
	k := newTestKernel(t, p)
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	var texture types.TextureDescr
	var buffer types.BufferDescr
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		texture = resources.UploadTexture(resources.NewTexture(1, 1, 1, types.FormatRGBA8, false), 0, m.Recti{}, []byte{255, 255, 255, 255}, true)
		buffer = resources.UploadBuffer(resources.NewBuffer(), []byte{1, 2, 3, 4}, true)
	})
	w, ref := recordList(t, k)
	set := testSet(t, k,
		types.ShaderParameterTexture("MainTexture", texture),
		types.ShaderParameterSampler("MainSampler", types.SamplerDesc{}),
		types.ShaderParameterBuffer("Data", buffer),
	)
	w.Draw(ref, triangle(), set, 1, 0)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	var gotTexture types.TextureID
	var gotBuffer types.BufferID
	for i := range backend.lastOps {
		op := &backend.lastOps[i]
		switch op.kind {
		case testOpSetTexture:
			gotTexture = op.texture
		case testOpSetBuffer:
			gotBuffer = op.buffer
		}
	}
	if gotTexture != texture.Params.ID {
		t.Errorf("bound baked texture = %d, want %d", gotTexture, texture.Params.ID)
	}
	if gotBuffer != buffer.ID {
		t.Errorf("bound baked buffer = %d, want %d", gotBuffer, buffer.ID)
	}

	withResourceQueue(t, k, func(resources *ResourceQueue) {
		if got := resources.UploadTexture(texture, 0, m.Recti{}, make([]byte, 4), true); got.Params.ID != texture.Params.ID {
			t.Errorf("rebaked texture = %d, want %d", got.Params.ID, texture.Params.ID)
		}
		if got := resources.UploadBuffer(buffer, []byte{5, 6, 7, 8}, true); got.ID != buffer.ID {
			t.Errorf("rebaked buffer = %d, want %d", got.ID, buffer.ID)
		}
	})
	w, ref = recordList(t, k)
	w.Draw(ref, triangle(), set, 1, 0)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	var rebakedTexture types.TextureID
	var rebakedBuffer types.BufferID
	for i := range backend.lastOps {
		op := &backend.lastOps[i]
		switch op.kind {
		case testOpUpdateTexture:
			rebakedTexture = op.texture
		case testOpBakeBuffer:
			if op.bufferKind == types.BufferStorage {
				rebakedBuffer = op.buffer
			}
		}
	}
	if rebakedTexture != texture.Params.ID || rebakedBuffer != buffer.ID {
		t.Errorf("rebaked resources = (%d, %d), want (%d, %d)", rebakedTexture, rebakedBuffer, texture.Params.ID, buffer.ID)
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

	var releasedTexture types.TextureID
	var releasedBuffer types.BufferID
	for i := range backend.lastOps {
		op := &backend.lastOps[i]
		switch op.kind {
		case testOpReleaseTexture:
			releasedTexture = op.texture
		case testOpReleaseBuffer:
			releasedBuffer = op.buffer
		}
	}
	if releasedTexture != texture.Params.ID || releasedBuffer != buffer.ID {
		t.Errorf("released resources = (%d, %d), want (%d, %d)", releasedTexture, releasedBuffer, texture.Params.ID, buffer.ID)
	}
}

func TestConsumeTranslatesDraws(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	// Record a frame: clear + one triangle through a set with a tint.
	k.ExecuteCommand[PresentCmd](PresentRequest{}) // ensure clean start
	w := recordRaw(t, k)
	ref := w.NewPass(types.PassDescr{
		Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(),
		Load: types.LoadClear, Clear: m.Color{A: 1},
		DepthLoad: types.LoadClear, DepthClear: 0.5,
	})
	mat := testSet(t, k, types.ShaderParameterColor("tint", m.Color{R: 1, G: 1, B: 1, A: 1}))
	w.Draw(ref, triangle(), mat, 1, 0)

	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.execCount == 0 {
		t.Fatal("render did not execute the recorded list")
	}
	if backend.pipes != 1 {
		t.Errorf("pipelines created = %d, want 1", backend.pipes)
	}
	if len(backend.lastPasses) != 1 {
		t.Fatalf("passes = %d, want 1", len(backend.lastPasses))
	}
	pass := backend.lastPasses[0]
	if pass.Clear != (m.Color{A: 1}) || pass.DepthClear != 0.5 || pass.Load != types.LoadClear || pass.DepthLoad != types.LoadClear {
		t.Errorf("clear state = (%+v, %v, %v, %v), want (black, 0.5, clear, clear)", pass.Clear, pass.DepthClear, pass.Load, pass.DepthLoad)
	}
	if got := countOps(backend.lastOps, testOpDraw); got != 1 {
		t.Errorf("draw ops = %d, want 1", got)
	}
	// One per uniform binding the shader declares: mvp, and the tint the set
	// supplies.
	if got := countOps(backend.lastOps, testOpSetUniformBlock); got != 2 {
		t.Errorf("uniform ops = %d, want 2", got)
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

func TestPipelineCacheDistinguishesEqualStrideLayouts(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	const stride = 28
	vertices := types.BufferDescrWithBlob(assets.NewBlob(make([]byte, 3*stride)), false)
	set := testSet(t, k)
	w, ref := recordList(t, k)
	w.Draw(ref, types.MeshDescrWithVertices(vertices, types.TopologyTriangleList,
		types.VertexAttribute{Offset: 0, Type: types.Float32x3}, types.VertexAttribute{Offset: 12, Type: types.Float32x4},
	), set, 1, 0)
	w.Draw(ref, types.MeshDescrWithVertices(vertices, types.TopologyTriangleList,
		types.VertexAttribute{Offset: 0, Type: types.Float32x2}, types.VertexAttribute{Offset: 12, Type: types.Float32x4},
	), set, 1, 0)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.pipes != 2 {
		t.Fatalf("pipelines for distinct equal-stride layouts = %d, want 2", backend.pipes)
	}
}

func TestVertexLayoutKeyUsesZeroOnlyForMissingAttributes(t *testing.T) {
	key, ok := VertexLayoutKeyOf([]types.VertexAttribute{
		{Offset: 0, Type: types.Float32},
		{Offset: 256, Type: types.Float32x4},
		{Offset: types.MaxVertexStride - 4, Type: types.Unorm1010102},
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
	tooMany := make([]types.VertexAttribute, types.MaxVertexAttributes+1)
	for i := range tooMany {
		tooMany[i] = types.VertexAttribute{Offset: 0, Type: types.Float32}
	}
	tests := []struct {
		name   string
		layout []types.VertexAttribute
	}{
		{name: "unknown type", layout: []types.VertexAttribute{{Offset: 0, Type: types.UnknownVertexType}}},
		{name: "type count sentinel", layout: []types.VertexAttribute{{Offset: 0, Type: types.VertexTypeCount__}}},
		{name: "negative offset", layout: []types.VertexAttribute{{Offset: -1, Type: types.Float32}}},
		{name: "attribute exceeds stride limit", layout: []types.VertexAttribute{{Offset: types.MaxVertexStride - 2, Type: types.Float32}}},
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

// Each uniform binding the shader declares is uploaded into its own span of the
// arena and bound at its own group and binding, whether the set or the frame's
// version supplied it.
func TestEveryUniformBlockPacksAndBindsOnItsOwn(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	layout := shader.ShaderLayout{
		Resources: []shader.ShaderResource{
			{Name: "view", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 64},
			{Name: "tint", Kind: shader.ResourceUniformBuffer, Group: 1, Binding: 2, Size: 16},
		},
	}
	backend := &fakeBackend{layout: &layout}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	green := m.Color{R: 0, G: 1, B: 0, A: 1}
	view := m.Translation4(3, 4, 5)
	set := testSet(t, k, types.ShaderParameterColor("tint", green))
	w, ref := recordList(t, k)
	w.SetDrawParams(kernel.Kernel{}, set, types.ShaderParameterMat4("view", view))
	w.Draw(ref, triangle(), set, 1, 0)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	var blocks []backendOp
	for _, op := range backend.lastOps {
		if op.kind == testOpSetUniformBlock {
			blocks = append(blocks, op)
		}
	}
	if len(blocks) != 2 {
		t.Fatalf("uniform blocks bound = %d, want 2", len(blocks))
	}
	// Bindings are bound in the order the program tables them, by name.
	surface, camera := blocks[0], blocks[1]
	if camera.group != 0 || camera.binding != 0 || len(camera.data) != 64 {
		t.Errorf("camera = %d/%d, %d bytes, want 0/0, 64 bytes", camera.group, camera.binding, len(camera.data))
	}
	if surface.group != 1 || surface.binding != 2 || len(surface.data) != 16 {
		t.Errorf("surface = %d/%d, %d bytes, want 1/2, 16 bytes", surface.group, surface.binding, len(surface.data))
	}
	if tx := math.Float32frombits(binary.LittleEndian.Uint32(camera.data[48:])); tx != view[12] {
		t.Errorf("view[12] = %v, want %v", tx, view[12])
	}
	if g := math.Float32frombits(binary.LittleEndian.Uint32(surface.data[4:])); g != 1 {
		t.Errorf("tint.g = %v, want 1", g)
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

	mat := testSet(t, k, types.ShaderParameterTexture("MainTexture", types.TextureWithResource("hero.png")), types.ShaderParameterSampler("MainSampler", types.SamplerDesc{}))
	for frame := 0; frame < 2; frame++ {
		w, ref := recordList(t, k)
		w.Draw(ref, triangle(), mat, 1, 0)
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

func TestReleaseCachedResourceReleasesPathAndAllowsReload(t *testing.T) {
	filesystem := &countingFS{FS: fstest.MapFS{
		"hero.png": &fstest.MapFile{Data: testPNG(t)},
	}}
	p := newPlugin()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	set := testSet(t, k, types.ShaderParameterTexture("MainTexture", types.TextureWithResource("hero.png")))

	w, ref := recordList(t, k)
	w.Draw(ref, triangle(), set, 1, 0)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	// The cache is asked about through what a game can see - one upload for
	// one path - rather than by reaching into the table that holds it.
	if backend.uploads != 1 || len(p.translator.pipelines) != 1 {
		t.Fatalf("initial uploads/pipelines = (%d, %d), want 1 each", backend.uploads, len(p.translator.pipelines))
	}
	k.ExecuteCommand[ReleaseCachedResourceCmd](ReleaseCachedResourceRequest{})
	k.ExecuteCommand[ReleaseCachedResourceCmd](ReleaseCachedResourceRequest{Path: "hero.png"})
	w, ref = recordList(t, k)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	// A shader is the caller's, created and released through the ResourceQueue,
	// so a path names no module and the pipelines built on one stay.
	if len(backend.freedShaders) != 0 || len(backend.freedPipelines) != 0 {
		t.Fatalf("path release freed shaders/pipelines = (%d, %d), want neither", len(backend.freedShaders), len(backend.freedPipelines))
	}
	if countOps(backend.lastOps, testOpReleaseTexture) != 1 {
		t.Fatal("path release did not emit one texture release")
	}

	w, ref = recordList(t, k)
	w.Draw(ref, triangle(), set, 1, 0)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if filesystem.opens != 2 || backend.uploads != 2 {
		t.Fatalf("reload opens/uploads = (%d, %d), want (2, 2)", filesystem.opens, backend.uploads)
	}
}

func TestFreeCachedResourcesClearsTranslatorOwnedCachesOnly(t *testing.T) {
	filesystem := fstest.MapFS{"hero.png": &fstest.MapFile{Data: testPNG(t)}}
	p := newPlugin()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	var explicit types.TextureDescr
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		explicit = resources.UploadTexture(resources.NewTexture(1, 1, 1, types.FormatRGBA8, false), 0, m.Recti{}, []byte{1, 2, 3, 4}, true)
	})
	set := testSet(t, k,
		types.ShaderParameterTexture("MainTexture", types.TextureWithResource("hero.png")),
		types.ShaderParameterSampler("MainSampler", types.SamplerDesc{}),
	)
	w, ref := recordList(t, k)
	w.Draw(ref, triangle(), set, 1, 0)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	// The cached texture is observable as the bake this frame emitted that the
	// game did not ask for by hand, which is all the test needs to name it.
	cachedTexture := types.TextureID(0)
	for i := range backend.lastOps {
		if backend.lastOps[i].kind == testOpBakeTexture && backend.lastOps[i].texture != explicit.Params.ID {
			cachedTexture = backend.lastOps[i].texture
		}
	}
	if cachedTexture == 0 {
		t.Fatalf("no cached texture bake beside the explicit one (%d)", explicit.Params.ID)
	}

	k.ExecuteCommand[FreeCachedResourcesCmd](FreeCachedResourcesRequest{})
	w, ref = recordList(t, k)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if len(p.translator.pipelines) != 0 || len(p.translator.samplers) != 0 {
		t.Fatal("global cleanup retained translator-owned caches")
	}
	// The shader is the caller's, and stays.
	if len(backend.freedShaders) != 0 || len(backend.freedPipelines) != 1 || len(backend.freedSamplers) != 1 {
		t.Fatalf("global cleanup freed shader/pipeline/sampler = (%d, %d, %d), want (0, 1, 1)", len(backend.freedShaders), len(backend.freedPipelines), len(backend.freedSamplers))
	}
	if countOps(backend.lastOps, testOpReleaseTexture) != 1 {
		t.Fatal("global cached cleanup did not release exactly one cached texture")
	}
	for i := range backend.lastOps {
		if backend.lastOps[i].kind == testOpReleaseTexture && backend.lastOps[i].texture != cachedTexture {
			t.Fatalf("global cleanup released texture %d, want cached texture %d (explicit %d)", backend.lastOps[i].texture, cachedTexture, explicit.Params.ID)
		}
	}
}

// A failed texture read is cached as failed and reported once, which is the
// behaviour the old shader cache had and the texture cache never did: a
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
	set := testSet(t, k, types.ShaderParameterTexture("MainTexture", types.TextureWithResource("later.png")))
	draw := func() {
		w, ref := recordList(t, k)
		w.Draw(ref, triangle(), set, 1, 0)
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

func TestTextureWithBytesReuploadsEveryFrame(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	pixels := []byte{255, 255, 255, 255}
	texture := types.TextureWithBytes(1, 1, types.FormatRGBA8, pixels, false, false)
	// In a set the pixels are baked once, into a texture the set owns; a
	// frame's version is where a run that changes every frame goes.
	set := testSet(t, k)
	for frame := 0; frame < 2; frame++ {
		w, ref := recordList(t, k)
		w.SetDrawParams(kernel.Kernel{}, set, types.ShaderParameterTexture("MainTexture", texture))
		w.Draw(ref, triangle(), set, 1, 0)
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
		uploads := 0
		for i := range backend.lastOps {
			if backend.lastOps[i].kind == testOpUpdateTexture {
				uploads++
				if backend.lastOps[i].texture == 0 {
					t.Errorf("frame %d uploaded texture has zero ID", frame)
				}
			}
		}
		if uploads != 1 {
			t.Errorf("frame %d texture uploads = %d, want 1", frame, uploads)
		}
	}
}

func TestABlobBufferReuploadsEveryFrame(t *testing.T) {
	p := newPlugin()
	layout := shader.ShaderLayout{
		Resources: []shader.ShaderResource{
			{Name: "mvp", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 64},
			{Name: "Data", Kind: shader.ResourceStorageBuffer, Group: 1, Binding: 0},
		},
	}
	k := newTestKernel(t, p)
	backend := &fakeBackend{layout: &layout}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	buffer := types.BufferDescrWithBlob(assets.NewBlob([]byte{1, 2, 3, 4}), false)
	set := testSet(t, k)
	for frame := 0; frame < 2; frame++ {
		w, ref := recordList(t, k)
		w.SetDrawParams(kernel.Kernel{}, set, types.ShaderParameterBuffer("Data", buffer))
		w.Draw(ref, triangle(), set, 1, 0)
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
		storageBakes := 0
		for i := range backend.lastOps {
			if backend.lastOps[i].kind != testOpBakeBuffer {
				continue
			}
			id := backend.lastOps[i].buffer
			if backend.lastOps[i].bufferKind != types.BufferStorage {
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
// recordList opens the frame's queue with a screen pass already declared, and
// returns it for the draws, since every draw names a pass and most tests do not
// care which one.
func recordList(t *testing.T, k kernel.Executioner) (*OpQueue, types.PassID) {
	t.Helper()
	queue := recordRaw(t, k)
	return queue, queue.NewPass(types.PassDescr{Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), Label: "test"})
}

// recordRaw opens the frame's queue with no pass declared.
func recordRaw(t *testing.T, k kernel.Executioner) *OpQueue {
	t.Helper()
	var captured *OpQueue
	k.ExecuteCommand[recordCmd](recordRequest{fn: func(l *OpQueue) { captured = l }})
	return captured
}

type recordCmd kernel.Command[recordRequest, recordResponse]

// recordRequest runs fn, or withKernel for the calls that report through the
// dispatch's Kernel.
type recordRequest struct {
	fn         func(*OpQueue)
	withKernel func(kernel.Kernel, *OpQueue)
}
type recordResponse struct{}

func withResourceQueue(t *testing.T, k kernel.Executioner, use func(*ResourceQueue)) {
	t.Helper()
	k.ExecuteCommand[recordResourcesCmd](recordResourcesRequest{fn: use})
}

type recordResourcesCmd kernel.Command[recordResourcesRequest, recordResourcesResponse]

// recordResourcesRequest runs fn, or withKernel for the calls that report
// through the dispatch's Kernel.
type recordResourcesRequest struct {
	fn         func(*ResourceQueue)
	withKernel func(kernel.Kernel, *ResourceQueue)
}
type recordResourcesResponse struct{}

// A shader that declares no uniform block must get no uniform binding. Scene
// declares none — all of its numeric data is storage — and emitting one anyway
// puts an entry in group 0 that the pipeline layout does not have, which fails
// CreateBindGroup and takes the whole frame's command buffer down.
func TestAShaderWithoutAUniformBlockGetsNoUniformBinding(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{layout: &shader.ShaderLayout{Resources: []shader.ShaderResource{
		{Name: "records", Kind: shader.ResourceStorageBuffer, Group: 0, Binding: 0},
	}}}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	set := testSet(t, k, types.ShaderParameterBuffer("records", types.BufferDescrWithBlob(assets.NewBlob(make([]byte, 64)), true)))
	w := recordRaw(t, k)
	ref := w.NewPass(types.PassDescr{Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto()})
	w.Draw(ref, triangle(), set, 1, 0)

	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if got := countOps(backend.lastOps, testOpDraw); got != 1 {
		t.Fatalf("draw ops = %d, want 1", got)
	}
	if got := countOps(backend.lastOps, testOpSetUniformBlock); got != 0 {
		t.Fatalf("uniform ops = %d, want none: the shader declares no uniform block", got)
	}
}
