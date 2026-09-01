package gfx

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"math"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/m"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/storage"
)

// testMaterial builds a material with an inline (fake-compiled) shader.
func testMaterial(params ...ParameterDescr) MaterialDescr {
	return Material(ShaderWithText("//test"), params...)
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
	freedSamplers  []SamplerID
	freedShaders   []ShaderID
	freedPipelines []PipelineID

	lastOps        []gpuOp
	lastClear      m.Color
	lastDepth      float32
	lastClearColor bool
	lastClearDepth bool
	execCount      int
	target         TextureViewID
	layout         *ShaderLayout
}

func (b *fakeBackend) id() uint32 { b.nextID++; return b.nextID }

func (b *fakeBackend) NewTexture() TextureID { b.nextTex++; return TextureID(b.nextTex) }
func (b *fakeBackend) NewBuffer() BufferID   { b.nextBuf++; return BufferID(b.nextBuf) }

func (b *fakeBackend) NewSampler(SamplerDesc) (SamplerID, error) {
	b.samplers++
	return SamplerID(b.id()), nil
}
func (b *fakeBackend) FreeSampler(id SamplerID) { b.freedSamplers = append(b.freedSamplers, id) }
func (b *fakeBackend) NewShader(desc ShaderDesc) (ShaderID, error) {
	b.shaders++
	b.shaderCode = append(b.shaderCode[:0], desc.Code...)
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
func (b *fakeBackend) NewPipeline(PipelineDesc) (PipelineID, error) {
	b.pipes++
	return PipelineID(b.id()), nil
}
func (b *fakeBackend) FreePipeline(id PipelineID) { b.freedPipelines = append(b.freedPipelines, id) }
func (b *fakeBackend) ScreenFramebuffer() (TextureViewID, int, int) {
	return 1, 100, 100
}
func (b *fakeBackend) Execute(target TextureViewID, queue *GpuQueue) {
	b.execCount++
	b.target = target
	b.lastOps = append(b.lastOps[:0], queue.bakes...)
	b.lastOps = append(b.lastOps, queue.render...)
	b.lastOps = append(b.lastOps, queue.releases...)
	b.lastClear = queue.ClearColor()
	b.lastDepth = queue.ClearDepthValue()
	b.lastClearColor, b.lastClearDepth = queue.Clears()
	for i := range queue.bakes {
		if queue.bakes[i].kind == gpuBakeTexture {
			b.textures++
			b.uploads++
		}
	}
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
func (testPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }
func (testPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[recordCmd](func() (kernel.Lock, kernel.Execute[recordReq, struct{}]) {
		var queue kernel.Write[*OpQueue]
		return func(access kernel.ResourceAccess) {
				queue = access.GetWrite[*OpQueue]()
			}, func(_ kernel.Kernel, req recordReq) (struct{}, error) {
				req.fn(queue.Get())
				return struct{}{}, nil
			}
	})
	registrar.HandleCommand[recordResourcesCmd](func() (kernel.Lock, kernel.Execute[recordResourcesReq, struct{}]) {
		var queue kernel.Write[*ResourceQueue]
		return func(access kernel.ResourceAccess) {
				queue = access.GetWrite[*ResourceQueue]()
			}, func(_ kernel.Kernel, req recordResourcesReq) (struct{}, error) {
				req.fn(queue.Get())
				return struct{}{}, nil
			}
	})
	return nil
}
func newTestKernel(t *testing.T, p *Plugin) kernel.Executioner {
	return newTestKernelWithFS(t, p, fstest.MapFS{})
}

func newTestKernelWithFS(t *testing.T, p *Plugin, filesystem fs.FS) kernel.Executioner {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	config := map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("gfx-test").WithReadFS("test", 10, filesystem),
	}
	engine := kernel.New(config).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(storage.New(), p, testPlugin{})
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
	mesh := Mesh(
		BufferDescr{source: BufferSourceBaked, id: 1, size: 3 * 28},
		TopologyTriangleList,
		Attr(0, Float32x3), Attr(12, Float32x4),
	)
	material := testMaterial(
		ColorParam("tint", m.Color{R: 1, G: 1, B: 1, A: 1}),
		FloatParam("scale", 1),
		TextureParam("MainTexture", TextureDescr{source: TextureSourceBaked, id: 2}),
		SamplerParam("MainSampler", AddressClamp, FilterLinear),
		BufferParam("Data", BufferDescr{source: BufferSourceBaked, id: 3, size: 64}),
	)
	for range 100 {
		queue.Draw(mesh, material,
			MatParam("mvp", m.NewMat4()),
			FloatParam("time", 1),
			ColorParam("tint", m.Color{R: 0.5, A: 1}),
		)
	}
	translator.translate(&queue, nil, backend, storage.ReadFS{})

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		translator.translate(&queue, nil, backend, storage.ReadFS{})
	}
}

type benchmarkGpuSink struct{}

func (benchmarkGpuSink) BakeBuffer(BufferID, BufferKind, int, []byte)                 {}
func (benchmarkGpuSink) BakeTexture(TextureID, int, int, TextureFormat, []byte, bool) {}
func (benchmarkGpuSink) AllocateTexture(TextureID, TextureDesc)                       {}
func (benchmarkGpuSink) UpdateTexture(TextureID, int, Region, []byte)                 {}
func (benchmarkGpuSink) SetPipeline(PipelineID)                                       {}
func (benchmarkGpuSink) SetParams([]byte)                                             {}
func (benchmarkGpuSink) SetTexture(TextureID, SamplerID, int, int, int)               {}
func (benchmarkGpuSink) SetVertexBuffer(BufferID, int)                                {}
func (benchmarkGpuSink) SetIndexBuffer(BufferID, int)                                 {}
func (benchmarkGpuSink) SetBuffer(int, int, BufferID, int, int)                       {}
func (benchmarkGpuSink) Draw(int, int, int, bool)                                     {}
func (benchmarkGpuSink) ReleaseBuffer(BufferID)                                       {}
func (benchmarkGpuSink) ReleaseTexture(TextureID)                                     {}

func BenchmarkGpuQueueReplaySteadyState(b *testing.B) {
	var queue GpuQueue
	queue.Reset()
	for i := range 100 {
		queue.BakeBuffer(BufferID(i+1), BufferVertex, 64, []byte{1})
		queue.SetPipeline(1)
		queue.SetParams([]byte{1})
		queue.SetVertexBuffer(BufferID(i+1), 0)
		queue.Draw(0, 3, 1, false)
		queue.ReleaseBuffer(BufferID(i + 1))
	}
	sink := benchmarkGpuSink{}
	b.ReportAllocs()
	for b.Loop() {
		queue.ReplayBakes(sink)
		queue.ReplayRenderPass(sink)
		queue.ReplayReleases(sink)
	}
}

func countOps(ops []gpuOp, kind gpuOpKind) int {
	n := 0
	for i := range ops {
		if ops[i].kind == kind {
			n++
		}
	}
	return n
}

func TestGpuQueueClearValuesAreDirectLastWinsState(t *testing.T) {
	var queue GpuQueue
	queue.Reset()
	if color, depth := queue.Clears(); color || depth {
		t.Fatalf("default clear mask = (%v, %v), want neither", color, depth)
	}
	queue.Clear(m.Color{R: 1})
	queue.Clear(m.Color{G: 1, A: 1})
	queue.ClearDepth(0.75)
	queue.ClearDepth(0.25)
	if got := queue.ClearColor(); got != (m.Color{G: 1, A: 1}) {
		t.Fatalf("clear color = %+v, want last color", got)
	}
	if got := queue.ClearDepthValue(); got != 0.25 {
		t.Fatalf("clear depth = %v, want 0.25", got)
	}
	if color, depth := queue.Clears(); !color || !depth {
		t.Fatalf("clear mask = (%v, %v), want both", color, depth)
	}
	if len(queue.bakes) != 0 || len(queue.render) != 0 || len(queue.releases) != 0 {
		t.Fatal("clear values were recorded as phase operations")
	}
	queue.Reset()
	queue.Clear(m.Color{})
	if color, depth := queue.Clears(); !color || depth {
		t.Fatalf("color-only clear mask = (%v, %v), want (true, false)", color, depth)
	}
	queue.Reset()
	queue.ClearDepth(0)
	if color, depth := queue.Clears(); color || !depth {
		t.Fatalf("depth-only clear mask = (%v, %v), want (false, true)", color, depth)
	}
	queue.Reset()
	if color, depth := queue.Clears(); color || depth {
		t.Fatalf("reset clear mask = (%v, %v), want neither", color, depth)
	}
}

func testOpQueue(backend Backend) OpQueue {
	return OpQueue{backend: backend}
}

func TestBakeOpsAllocateBakedResourceIDs(t *testing.T) {
	backend := &fakeBackend{}
	queue := ResourceQueue{backend: backend}
	pixels := []byte{1, 2, 3, 4}
	buffer := queue.BakeBuffer(pixels, true)
	texture := queue.BakeTexture(1, 1, FormatRGBA8, pixels, true, false)
	rebakedBuffer := queue.ReBakeBuffer(buffer, pixels, true)
	rebakedTexture := queue.ReBakeTexture(texture, 1, 1, FormatRGBA8, pixels, true, false)

	if buffer.id == 0 || texture.id == 0 {
		t.Fatalf("baked handles = (%d, %d), want nonzero", buffer.id, texture.id)
	}
	if rebakedBuffer.id != buffer.id || rebakedTexture.id != texture.id {
		t.Fatalf("rebaked handles = (%d, %d), want (%d, %d)", rebakedBuffer.id, rebakedTexture.id, buffer.id, texture.id)
	}
	pixels[0] = 99
	for i := range queue.ops {
		if queue.ops[i].bytes[0] != 1 {
			t.Fatalf("op %d did not copy caller data", i)
		}
	}
}

func TestBakeBufferCopyDataControlsOwnership(t *testing.T) {
	queue := ResourceQueue{backend: &fakeBackend{}}
	copied := []byte{1, 2, 3, 4}
	borrowed := []byte{5, 6, 7, 8}
	queue.BakeBuffer(copied, true)
	queue.BakeBuffer(borrowed, false)

	copied[0] = 9
	borrowed[0] = 10
	if got := queue.ops[0].bytes[0]; got != 1 {
		t.Fatalf("copied buffer byte = %d, want 1", got)
	}
	if got := queue.ops[1].bytes[0]; got != 10 {
		t.Fatalf("borrowed buffer byte = %d, want 10", got)
	}
	retainedOps := queue.ops
	queue.reset()
	if retainedOps[1].bytes != nil {
		t.Fatal("reset retained borrowed buffer bytes")
	}
}

func TestBakeTextureCopyDataControlsOwnership(t *testing.T) {
	queue := ResourceQueue{backend: &fakeBackend{}}
	copied := []byte{1, 2, 3, 4}
	borrowed := []byte{5, 6, 7, 8}
	queue.BakeTexture(1, 1, FormatRGBA8, copied, true, false)
	queue.BakeTexture(1, 1, FormatRGBA8, borrowed, false, false)

	copied[0] = 9
	borrowed[0] = 10
	if got := queue.ops[0].bytes[0]; got != 1 {
		t.Fatalf("copied texture byte = %d, want 1", got)
	}
	if got := queue.ops[1].bytes[0]; got != 10 {
		t.Fatalf("borrowed texture byte = %d, want 10", got)
	}
	retainedOps := queue.ops
	queue.reset()
	if retainedOps[1].bytes != nil {
		t.Fatal("reset retained borrowed texture pixels")
	}
}

func TestTextureArrayAllocationAndLayerUpdateTranslate(t *testing.T) {
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})
	pixels := []byte{1, 2, 3, 4}
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		texture := resources.AllocateTexture(64, 32, 4, FormatRGBA8)
		resources.UpdateTexture(texture, 2, Region{X: 5, Y: 7, Width: 1, Height: 1}, pixels, true)
	})
	pixels[0] = 9
	k.PublishEvent(app.RenderEvent{}).Wait()

	if got := countOps(backend.lastOps, gpuAllocateTexture); got != 1 {
		t.Fatalf("texture allocations = %d, want 1", got)
	}
	if got := countOps(backend.lastOps, gpuUpdateTexture); got != 1 {
		t.Fatalf("texture updates = %d, want 1", got)
	}
	for i := range backend.lastOps {
		op := &backend.lastOps[i]
		switch op.kind {
		case gpuAllocateTexture:
			if op.arg0 != 64 || op.arg1 != 32 || op.arg2 != 4 || TextureFormat(op.arg3) != FormatRGBA8 {
				t.Fatalf("allocation metadata = (%d,%d,%d,%d)", op.arg0, op.arg1, op.arg2, op.arg3)
			}
		case gpuUpdateTexture:
			if op.arg0 != 2 || op.arg1 != 5 || op.arg2 != 7 || op.arg3 != 1 || op.arg4 != 1 || op.params[0] != 1 {
				t.Fatalf("update metadata = layer/region/data (%d,%d,%d,%d,%d,%d)", op.arg0, op.arg1, op.arg2, op.arg3, op.arg4, op.params[0])
			}
		}
	}
}

func TestPersistentResourceTextureSurvivesDroppedFrame(t *testing.T) {
	filesystem := &countingFS{FS: fstest.MapFS{
		"persistent.png": &fstest.MapFile{Data: testPNG(t)},
	}}
	p := New()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

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
		case gpuBakeTexture:
			baked = TextureID(op.res0)
		case gpuSetBakedTexture:
			bound = TextureID(op.res0)
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
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

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

	if got := countOps(backend.lastOps, gpuBakeBuffer); got != 2 {
		t.Errorf("persistent buffer bakes = %d, want 2", got)
	}
	if got := countOps(backend.lastOps, gpuBakeTexture); got != 2 {
		t.Errorf("persistent texture bakes = %d, want 2", got)
	}
	if got := countOps(backend.lastOps, gpuReleaseBuffer); got != 1 {
		t.Errorf("persistent buffer releases = %d, want 1", got)
	}
	if got := countOps(backend.lastOps, gpuReleaseTexture); got != 1 {
		t.Errorf("persistent texture releases = %d, want 1", got)
	}
}

func TestDroppedFrameDiscardsTemporaryUploads(t *testing.T) {
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	w := recordList(t, k)
	w.Draw(triangle(), testMaterial(TextureParam("MainTexture", TextureWithBytes(1, 1, FormatRGBA8, []byte{1, 2, 3, 4}, false, false))), MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	w = recordList(t, k)
	w.Clear(m.Color{})
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if countOps(backend.lastOps, gpuBakeTexture) != 0 || countOps(backend.lastOps, gpuBakeBuffer) != 0 {
		t.Fatal("dropped frame retained temporary texture or geometry uploads")
	}
}

func TestOpQueueTemporaryBufferPool(t *testing.T) {
	backend := &fakeBackend{}
	queue := testOpQueue(backend)

	small := []byte{1, 2, 3, 4}
	large := make([]byte, 16)
	smallBuffer := queue.temporaryBuffer(BufferVertex, small, true)
	largeBuffer := queue.temporaryBuffer(BufferVertex, large, true)
	small[0] = 99
	if queue.ops[0].bytes[0] != 1 {
		t.Fatal("temporary bake op aliases caller data")
	}

	queue.Reset()
	fit := queue.temporaryBuffer(BufferVertex, make([]byte, 12), true)
	if fit.id != largeBuffer.id {
		t.Errorf("best-fit buffer = %d, want %d", fit.id, largeBuffer.id)
	}
	queue.Reset()
	resized := queue.temporaryBuffer(BufferVertex, make([]byte, 32), true)
	if resized.id != largeBuffer.id {
		t.Errorf("resized buffer ID = %d, want reused %d", resized.id, largeBuffer.id)
	}
	if queue.temporaryBuffers[1].size < 32 {
		t.Errorf("resized size = %d, want at least 32", queue.temporaryBuffers[1].size)
	}
	if smallBuffer.id == largeBuffer.id {
		t.Fatal("simultaneously used temporary buffers share an ID")
	}
}

func TestDrawStoresTemporaryBufferIDsWithoutInlineGeometry(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	mesh := triangle()
	queue.Draw(mesh, testMaterial(), MatParam("mvp", m.NewMat4()))

	if queue.ops[0].kind != opBakeBuffer || queue.ops[0].bufferKind != BufferVertex {
		t.Fatal("draw did not populate a vertex bake op first")
	}
	draw := &queue.ops[1]
	if draw.mesh.vertices.id == 0 || draw.mesh.vertexCount != 3 {
		t.Fatalf("draw vertex resource = (%d, %d), want nonzero ID and 3 vertices", draw.mesh.vertices.id, draw.mesh.vertexCount)
	}
	if len(draw.mesh.vertices.bytes) != 0 {
		t.Fatalf("draw retained %d inline vertex bytes", len(draw.mesh.vertices.bytes))
	}
	if len(queue.temporaryBuffers) != 1 || !queue.temporaryBuffers[0].used {
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

	draw := &queue.ops[len(queue.ops)-1]
	if queue.ops[0].bytes[0] != 1 {
		t.Fatalf("recorded vertex byte = %d, want 1", queue.ops[0].bytes[0])
	}
	if draw.mesh.layout[0] != (VertexAttr{offset: 0, typ: Float32x3}) {
		t.Fatalf("recorded layout = %+v, want original", draw.mesh.layout)
	}
	if draw.material.params[0].color != (m.Color{R: 1}) {
		t.Fatalf("recorded material color = %+v, want red", draw.material.params[0].color)
	}
	if draw.params[0].num != 1 {
		t.Fatalf("recorded draw parameter = %v, want 1", draw.params[0].num)
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

	if got := queue.ops[0].bytes[0]; got != 1 {
		t.Fatalf("copied mesh byte = %d, want 1", got)
	}
	if got := queue.ops[2].bytes[0]; got != 10 {
		t.Fatalf("borrowed mesh byte = %d, want 10", got)
	}
	retainedOps := queue.ops
	queue.Reset()
	if retainedOps[2].bytes != nil {
		t.Fatal("reset retained borrowed mesh bytes")
	}
}

func TestTextureWithBytesCopyDataControlsOwnership(t *testing.T) {
	queue := testOpQueue(&fakeBackend{})
	copied := []byte{1, 2, 3, 4}
	borrowed := []byte{5, 6, 7, 8}
	queue.bakeTextureIfNeeded(TextureWithBytes(1, 1, FormatRGBA8, copied, true, false))
	queue.bakeTextureIfNeeded(TextureWithBytes(1, 1, FormatRGBA8, borrowed, false, false))

	copied[0] = 9
	borrowed[0] = 10
	if got := queue.ops[0].bytes[0]; got != 1 {
		t.Fatalf("copied temporary texture byte = %d, want 1", got)
	}
	if got := queue.ops[1].bytes[0]; got != 10 {
		t.Fatalf("borrowed temporary texture byte = %d, want 10", got)
	}
	retainedOps := queue.ops
	queue.Reset()
	if retainedOps[1].bytes != nil {
		t.Fatal("reset retained borrowed temporary texture pixels")
	}
}

func TestOpQueueBakesInlineMaterialAndDrawParameters(t *testing.T) {
	filesystem := &countingFS{FS: fstest.MapFS{
		"shared.png": &fstest.MapFile{Data: testPNG(t)},
	}}
	p := New()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})
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
	draw := &queue.ops[len(queue.ops)-1]
	for _, param := range append(draw.material.params, draw.params...) {
		switch param.kind {
		case paramTexture:
			if param.texture.source == TextureSourceBytes || len(param.texture.pixels) != 0 {
				t.Errorf("texture param %q was not remapped to a baked ID", param.name)
			}
		case paramBuffer:
			if param.buffer.source != BufferSourceBaked || param.buffer.id == 0 || len(param.buffer.bytes) != 0 {
				t.Errorf("buffer param %q was not remapped to a baked ID", param.name)
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
	first := queue.bakeTextureIfNeeded(TextureWithBytes(1, 1, FormatRGBA8, []byte{1, 2, 3, 4}, true, false))
	second := queue.bakeTextureIfNeeded(TextureWithBytes(1, 1, FormatRGBA8, []byte{5, 6, 7, 8}, true, false))
	if first.id == second.id {
		t.Fatal("simultaneously used temporary textures share an ID")
	}

	queue.Reset()
	reused := queue.bakeTextureIfNeeded(TextureWithBytes(1, 1, FormatRGBA8, []byte{9, 10, 11, 12}, true, false))
	if reused.id != first.id {
		t.Errorf("reused temporary texture ID = %d, want %d", reused.id, first.id)
	}
	if len(queue.ops) != 1 || queue.ops[0].kind != opBakeTexture {
		t.Fatal("temporary texture did not populate one bake op")
	}
}

func TestBakedResourcesTranslateToBakedBindings(t *testing.T) {
	p := New()
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
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	var texture TextureDescr
	var buffer BufferDescr
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		texture = resources.BakeTexture(1, 1, FormatRGBA8, []byte{255, 255, 255, 255}, true, false)
		buffer = resources.BakeBuffer([]byte{1, 2, 3, 4}, true)
	})
	w := recordList(t, k)
	material := testMaterial(
		TextureParam("MainTexture", texture),
		SamplerParam("MainSampler", AddressClamp, FilterLinear),
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
		case gpuSetBakedTexture:
			gotTexture = TextureID(op.res0)
		case gpuSetBakedBuffer:
			gotBuffer = BufferID(op.res0)
		}
	}
	if gotTexture != texture.id {
		t.Errorf("bound baked texture = %d, want %d", gotTexture, texture.id)
	}
	if gotBuffer != buffer.id {
		t.Errorf("bound baked buffer = %d, want %d", gotBuffer, buffer.id)
	}

	withResourceQueue(t, k, func(resources *ResourceQueue) {
		if got := resources.ReBakeTexture(texture, 2, 1, FormatRGBA8, make([]byte, 8), true, false); got.id != texture.id {
			t.Errorf("rebaked texture = %d, want %d", got.id, texture.id)
		}
		if got := resources.ReBakeBuffer(buffer, []byte{5, 6, 7, 8}, true); got.id != buffer.id {
			t.Errorf("rebaked buffer = %d, want %d", got.id, buffer.id)
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
		case gpuBakeTexture:
			rebakedTexture = TextureID(op.res0)
		case gpuBakeBuffer:
			if BufferKind(op.arg0) == BufferStorage {
				rebakedBuffer = BufferID(op.res0)
			}
		}
	}
	if rebakedTexture != texture.id || rebakedBuffer != buffer.id {
		t.Errorf("rebaked resources = (%d, %d), want (%d, %d)", rebakedTexture, rebakedBuffer, texture.id, buffer.id)
	}
	if countOps(backend.lastOps, gpuSetBakedTexture) != 1 || countOps(backend.lastOps, gpuSetBakedBuffer) != 1 {
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
		case gpuReleaseTexture:
			releasedTexture = TextureID(op.res0)
		case gpuReleaseBuffer:
			releasedBuffer = BufferID(op.res0)
		}
	}
	if releasedTexture != texture.id || releasedBuffer != buffer.id {
		t.Errorf("released resources = (%d, %d), want (%d, %d)", releasedTexture, releasedBuffer, texture.id, buffer.id)
	}
}

func TestConsumeTranslatesDraws(t *testing.T) {
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	// Record a frame: clear + one triangle with a tint material.
	k.ExecuteCommand[PresentCmd](PresentRequest{}) // ensure clean start
	w := recordList(t, k)
	w.Clear(m.Color{A: 1})
	w.ClearDepth(0.5)
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
	if backend.lastClear != (m.Color{A: 1}) || backend.lastDepth != 0.5 || !backend.lastClearColor || !backend.lastClearDepth {
		t.Errorf("clear state = (%+v, %v, %v, %v), want (black, 0.5, true, true)", backend.lastClear, backend.lastDepth, backend.lastClearColor, backend.lastClearDepth)
	}
	if got := countOps(backend.lastOps, gpuDraw); got != 1 {
		t.Errorf("draw ops = %d, want 1", got)
	}
	if got := countOps(backend.lastOps, gpuSetParams); got != 1 {
		t.Errorf("uniform ops = %d, want 1", got)
	}
	// The single draw is non-indexed with 3 vertices.
	for i := range backend.lastOps {
		if backend.lastOps[i].kind == gpuDraw {
			first := int(backend.lastOps[i].arg0)
			count := int(backend.lastOps[i].arg1)
			indexed := backend.lastOps[i].arg2 != 0
			if first != 0 || count != 3 || indexed {
				t.Errorf("draw = (first %d, count %d, indexed %v), want (0, 3, false)", first, count, indexed)
			}
		}
	}
}

func TestConsumeCachesShaderAndPipeline(t *testing.T) {
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	for frame := 0; frame < 3; frame++ {
		w := recordList(t, k)
		w.Clear(m.Color{})
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
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

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
	key, ok := vertexLayoutKeyOf([]VertexAttr{
		Attr(0, Float32),
		Attr(256, Float32x4),
		Attr(maxVertexStride-4, Unorm1010102),
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
	tooMany := make([]VertexAttr, maxVertexAttributes+1)
	for i := range tooMany {
		tooMany[i] = Attr(0, Float32)
	}
	tests := []struct {
		name   string
		layout []VertexAttr
	}{
		{name: "unknown type", layout: []VertexAttr{Attr(0, UnknownVertexType)}},
		{name: "type count sentinel", layout: []VertexAttr{Attr(0, vertexTypeCount)}},
		{name: "negative offset", layout: []VertexAttr{Attr(-1, Float32)}},
		{name: "attribute exceeds stride limit", layout: []VertexAttr{Attr(maxVertexStride-2, Float32)}},
		{name: "too many attributes", layout: tooMany},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, ok := vertexLayoutKeyOf(test.layout); ok {
				t.Fatal("unsupported vertex layout was accepted")
			}
		})
	}
}

func TestDrawParamsPackByNameAndOverrideMaterial(t *testing.T) {
	p := New()
	k := newTestKernel(t, p)
	layout := ShaderLayout{
		UniformSize: 80, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []UniformMember{{Name: "camera", Offset: 0}, {Name: "tint", Offset: 64}},
	}
	backend := &fakeBackend{layout: &layout}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

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
		if backend.lastOps[i].kind == gpuSetParams {
			params = backend.lastOps[i].params
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
	p := New()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	for frame := 0; frame < 2; frame++ {
		w := recordList(t, k)
		mat := testMaterial(TextureParam("MainTexture", TextureWithResource("hero.png")), SamplerParam("MainSampler", AddressClamp, FilterLinear))
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
	const source = "// storage shader"
	filesystem := &countingFS{FS: fstest.MapFS{
		"shader.wgsl": &fstest.MapFile{Data: []byte(source)},
	}}
	p := New()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	for range 2 {
		w := recordList(t, k)
		w.Draw(triangle(), Material(ShaderWithResource("shader.wgsl")), MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
	}
	if filesystem.opens != 1 {
		t.Errorf("shader resource opened %d times, want 1", filesystem.opens)
	}
	if string(backend.shaderCode) != source {
		t.Errorf("shader source = %q, want %q", backend.shaderCode, source)
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
	p := New()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})
	material := Material(
		ShaderWithResource("shader.wgsl"),
		TextureParam("MainTexture", TextureWithResource("hero.png")),
	)

	w := recordList(t, k)
	w.Draw(triangle(), material, MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if len(p.translator.textures) != 1 || len(p.translator.shaders) != 1 || len(p.translator.pipelines) != 1 {
		t.Fatalf("initial caches = textures %d shaders %d pipelines %d, want 1 each", len(p.translator.textures), len(p.translator.shaders), len(p.translator.pipelines))
	}
	k.ExecuteCommand[ReleaseCachedResourceCmd](ReleaseCachedResourceRequest{})
	k.ExecuteCommand[ReleaseCachedResourceCmd](ReleaseCachedResourceRequest{Path: "hero.png"})
	k.ExecuteCommand[ReleaseCachedResourceCmd](ReleaseCachedResourceRequest{Path: "shader.wgsl"})
	w = recordList(t, k)
	w.Clear(m.Color{})
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(p.translator.textures) != 0 || len(p.translator.shaders) != 0 || len(p.translator.pipelines) != 0 || len(p.translator.layouts) != 0 || len(p.translator.parameterPlans) != 0 {
		t.Fatal("path release retained translator cache entries")
	}
	if len(backend.freedShaders) != 1 || len(backend.freedPipelines) != 1 {
		t.Fatalf("path release freed shaders/pipelines = (%d, %d), want (1, 1)", len(backend.freedShaders), len(backend.freedPipelines))
	}
	if countOps(backend.lastOps, gpuReleaseTexture) != 1 {
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
	p := New()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})
	var explicit TextureDescr
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		explicit = resources.BakeTexture(1, 1, FormatRGBA8, []byte{1, 2, 3, 4}, true, false)
	})
	w := recordList(t, k)
	w.Draw(triangle(), testMaterial(
		TextureParam("MainTexture", TextureWithResource("hero.png")),
		SamplerParam("MainSampler", AddressClamp, FilterLinear),
	), MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	cachedTexture := p.translator.textures[textureKey("hero.png")]
	if cachedTexture.id == 0 || cachedTexture.id == explicit.id {
		t.Fatalf("cached/explicit texture IDs = (%d, %d), want distinct nonzero IDs", cachedTexture.id, explicit.id)
	}

	k.ExecuteCommand[FreeCachedResourcesCmd](FreeCachedResourcesRequest{})
	w = recordList(t, k)
	w.Clear(m.Color{})
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if len(p.translator.shaders) != 0 || len(p.translator.pipelines) != 0 || len(p.translator.samplers) != 0 || len(p.translator.layouts) != 0 || len(p.translator.parameterPlans) != 0 {
		t.Fatal("global cleanup retained translator-owned caches")
	}
	if len(backend.freedShaders) != 1 || len(backend.freedPipelines) != 1 || len(backend.freedSamplers) != 1 {
		t.Fatalf("global cleanup freed shader/pipeline/sampler = (%d, %d, %d), want (1, 1, 1)", len(backend.freedShaders), len(backend.freedPipelines), len(backend.freedSamplers))
	}
	if countOps(backend.lastOps, gpuReleaseTexture) != 1 {
		t.Fatal("global cached cleanup did not release exactly one cached texture")
	}
	for i := range backend.lastOps {
		if backend.lastOps[i].kind == gpuReleaseTexture && TextureID(backend.lastOps[i].res0) != cachedTexture.id {
			t.Fatalf("global cleanup released texture %d, want cached texture %d (explicit %d)", backend.lastOps[i].res0, cachedTexture.id, explicit.id)
		}
	}
}

func TestFailedTextureResourceLoadIsRetried(t *testing.T) {
	files := fstest.MapFS{}
	filesystem := &countingFS{FS: files}
	p := New()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})
	material := testMaterial(TextureParam("MainTexture", TextureWithResource("later.png")))

	for attempt := range 2 {
		w := recordList(t, k)
		w.Draw(triangle(), material, MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
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

func TestFailedShaderResourceLoadIsRetried(t *testing.T) {
	files := fstest.MapFS{}
	filesystem := &countingFS{FS: files}
	p := New()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errorsReported := 0
	config := map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("gfx-test").WithReadFS("test", 10, filesystem),
	}
	engine := kernel.New(config).Handler(func(error) bool {
		errorsReported++
		return false
	}).WithPlugins(storage.New(), p, testPlugin{})
	go engine.Run(ctx)
	<-engine.Ready()
	k := engine.Executioner()
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})
	material := Material(ShaderWithResource("later.wgsl"))

	for attempt := range 2 {
		w := recordList(t, k)
		w.Draw(triangle(), material)
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
		if attempt == 0 {
			if len(p.translator.shaders) != 0 {
				t.Fatal("failed shader load was cached")
			}
			files["later.wgsl"] = &fstest.MapFile{Data: []byte("// shader")}
		}
	}
	if filesystem.opens != 2 || len(p.translator.shaders) != 1 || backend.shaders != 1 || errorsReported != 1 {
		t.Fatalf("retry opens/cache/shaders/errors = (%d, %d, %d, %d), want (2, 1, 1, 1)", filesystem.opens, len(p.translator.shaders), backend.shaders, errorsReported)
	}
}

func TestTextureWithBytesReuploadsEveryFrame(t *testing.T) {
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	pixels := []byte{255, 255, 255, 255}
	texture := TextureWithBytes(1, 1, FormatRGBA8, pixels, false, false)
	for frame := 0; frame < 2; frame++ {
		w := recordList(t, k)
		w.Draw(triangle(), testMaterial(TextureParam("MainTexture", texture)), MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
		for i := range backend.lastOps {
			if backend.lastOps[i].kind == gpuBakeTexture {
				id := TextureID(backend.lastOps[i].res0)
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
	p := New()
	layout := ShaderLayout{
		UniformSize: 80, UniformGroup: 0, UniformBinding: 0,
		Uniforms:  []UniformMember{{Name: "mvp", Offset: 0}},
		Resources: []ShaderResource{{Name: "Data", StorageBuffer: true, Group: 1, Binding: 0}},
	}
	k := newTestKernel(t, p)
	backend := &fakeBackend{layout: &layout}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	buffer := BufferWithBytes([]byte{1, 2, 3, 4}, false)
	for frame := 0; frame < 2; frame++ {
		w := recordList(t, k)
		w.Draw(triangle(), testMaterial(BufferParam("Data", buffer)), MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
		storageBakes := 0
		for i := range backend.lastOps {
			if backend.lastOps[i].kind != gpuBakeBuffer {
				continue
			}
			id := BufferID(backend.lastOps[i].res0)
			if BufferKind(backend.lastOps[i].arg0) != BufferStorage {
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
func recordList(t *testing.T, k kernel.Executioner) *OpQueue {
	t.Helper()
	var captured *OpQueue
	k.ExecuteCommand[recordCmd](recordReq{fn: func(l *OpQueue) { captured = l }})
	return captured
}

type recordReq struct{ fn func(*OpQueue) }
type recordCmd kernel.Command[recordReq, struct{}]

func withResourceQueue(t *testing.T, k kernel.Executioner, use func(*ResourceQueue)) {
	t.Helper()
	k.ExecuteCommand[recordResourcesCmd](recordResourcesReq{fn: use})
}

type recordResourcesReq struct{ fn func(*ResourceQueue) }
type recordResourcesCmd kernel.Command[recordResourcesReq, struct{}]
