package canvas

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"math"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/m"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/storage"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/wgsl"
)

type testFS struct {
	fs.FS
	opens int
}

func (f *testFS) Open(name string) (fs.File, error) {
	f.opens++
	return f.FS.Open(name)
}

type testBackend struct {
	nextTexture      gfx.TextureID
	nextBuffer       gfx.BufferID
	nextID           uint32
	allocations      []textureAllocation
	updates          []textureUpdate
	drawParams       [][]byte
	draws            int
	pipelines        []gfx.PipelineDesc
	releasedTextures []gfx.TextureID
	releasedBuffers  []gfx.BufferID
	buffers          []bufferBake
	samplers         []gfx.SamplerDesc
	capture          bool
}

type textureAllocation struct {
	id   gfx.TextureID
	desc gfx.TextureDesc
}

type textureUpdate struct {
	id     gfx.TextureID
	layer  int
	region gfx.Region
	pixels []byte
}

type bufferBake struct {
	kind gfx.BufferKind
	data []byte
}

type customTriangleVertex struct {
	Position m.Vec2
	Data     m.Vec4
}

var customTriangleVertexLayout = [...]gfx.VertexAttr{
	gfx.Attr(0, gfx.Float32x2),
	gfx.Attr(8, gfx.Float32x4),
}

func (customTriangleVertex) VertexLayout() []gfx.VertexAttr {
	return customTriangleVertexLayout[:]
}

func (b *testBackend) NewTexture() gfx.TextureID { b.nextTexture++; return b.nextTexture }
func (b *testBackend) NewBuffer() gfx.BufferID   { b.nextBuffer++; return b.nextBuffer }
func (b *testBackend) NewSampler(desc gfx.SamplerDesc) (gfx.SamplerID, error) {
	b.nextID++
	if b.capture {
		b.samplers = append(b.samplers, desc)
	}
	return gfx.SamplerID(b.nextID), nil
}
func (b *testBackend) FreeSampler(gfx.SamplerID) {}
func (b *testBackend) NewShader(gfx.ShaderDesc) (gfx.ShaderID, error) {
	b.nextID++
	return gfx.ShaderID(b.nextID), nil
}
func (b *testBackend) FreeShader(gfx.ShaderID) {}
func (b *testBackend) ShaderLayout(gfx.ShaderID) gfx.ShaderLayout {
	return gfx.ShaderLayout{
		UniformSize: 192, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []gfx.UniformMember{
			{Name: "canvasTransform0", Offset: 0},
			{Name: "canvasTransform1", Offset: 16},
			{Name: "canvasFrame", Offset: 32},
			{Name: "canvasViewport", Offset: 48},
			{Name: "atlasLayer", Offset: 56},
			{Name: "clipEnabled", Offset: 60},
			{Name: "canvasLayer", Offset: 64},
			{Name: "canvasClip", Offset: 128},
			{Name: "tint", Offset: 144},
			{Name: "keyColor", Offset: 160},
			{Name: "customValue", Offset: 180},
		},
		Resources: []gfx.ShaderResource{
			{Name: "canvasSampler", Sampler: true, Group: 1, Binding: 0},
			{Name: "canvasTexture", TextureView: gfx.TextureView2DArray, Group: 1, Binding: 1},
			{Name: "instances", StorageBuffer: true, Group: 2, Binding: 0},
		},
	}
}
func (b *testBackend) NewPipeline(desc gfx.PipelineDesc) (gfx.PipelineID, error) {
	b.nextID++
	if b.capture {
		b.pipelines = append(b.pipelines, desc)
	}
	return gfx.PipelineID(b.nextID), nil
}
func (b *testBackend) FreePipeline(gfx.PipelineID) {}
func (b *testBackend) ScreenFramebuffer() (gfx.TextureViewID, int, int) {
	return 1, 100, 100
}
func (b *testBackend) Execute(_ gfx.TextureViewID, queue *gfx.GpuQueue) {
	queue.ReplayBakes(b)
	queue.ReplayRenderPass(b)
	queue.ReplayReleases(b)
}
func (b *testBackend) BakeBuffer(_ gfx.BufferID, kind gfx.BufferKind, _ int, data []byte) {
	if b.capture {
		b.buffers = append(b.buffers, bufferBake{kind: kind, data: append([]byte(nil), data...)})
	}
}
func (b *testBackend) BakeTexture(gfx.TextureID, int, int, gfx.TextureFormat, []byte, bool) {
}
func (b *testBackend) AllocateTexture(id gfx.TextureID, desc gfx.TextureDesc) {
	b.allocations = append(b.allocations, textureAllocation{id: id, desc: desc})
}
func (b *testBackend) UpdateTexture(id gfx.TextureID, layer int, region gfx.Region, pixels []byte) {
	b.updates = append(b.updates, textureUpdate{id: id, layer: layer, region: region, pixels: append([]byte(nil), pixels...)})
}
func (b *testBackend) SetPipeline(gfx.PipelineID) {}
func (b *testBackend) SetParams(params []byte) {
	if b.capture {
		b.drawParams = append(b.drawParams, append([]byte(nil), params...))
	}
}
func (b *testBackend) SetTexture(gfx.TextureID, gfx.SamplerID, int, int, int) {}
func (b *testBackend) SetVertexBuffer(gfx.BufferID, int)                      {}
func (b *testBackend) SetIndexBuffer(gfx.BufferID, int)                       {}
func (b *testBackend) SetBuffer(int, int, gfx.BufferID, int, int)             {}
func (b *testBackend) Draw(_, _, _ int, _ bool)                               { b.draws++ }
func (b *testBackend) ReleaseBuffer(id gfx.BufferID) {
	b.releasedBuffers = append(b.releasedBuffers, id)
}
func (b *testBackend) ReleaseTexture(id gfx.TextureID) {
	b.releasedTextures = append(b.releasedTextures, id)
}

type recordCanvasPlugin struct{ record func(*OpQueue) }
type recordCanvasHandler kernel.Subscription[app.UpdateEvent]

// lookupProbeCmd runs a callback inside a handler that holds the Lookup and
// filesystem locks, giving tests a valid scoped LookupAccess to exercise the
// public query and unload API the way real callers do.
type lookupProbeCmd kernel.Command[lookupProbeReq, lookupProbeResp]
type lookupProbeReq struct{ run func(LookupAccess) }
type lookupProbeResp struct{}

// readFileProbeCmd reads a path from storage.ReadFS under its read lock,
// standing in for production code that reads the resource directly.
type readFileProbeCmd kernel.Command[readFileProbeReq, readFileProbeResp]
type readFileProbeReq struct{ Name string }
type readFileProbeResp struct{ Data []byte }

func (p recordCanvasPlugin) Name() kernel.PluginName { return "canvas-test-recorder" }

// Name is the canvas plugin's, not this fixture's: the recorder is a separate
// plugin that locks canvas resources and storage.ReadFS.
func (p recordCanvasPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{Name, storage.Name}
}
func (p recordCanvasPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[recordCanvasHandler](func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
		var queue kernel.Write[*OpQueue]
		return func(access kernel.ResourceAccess) {
				queue = access.GetWrite[*OpQueue]()
			}, func(_ kernel.Kernel, _ app.UpdateEvent) error {
				p.record(queue.Get())
				return nil
			}
	})
	registrar.HandleCommand[lookupProbeCmd](func() (kernel.Lock, kernel.Execute[lookupProbeReq, lookupProbeResp]) {
		var lookup kernel.Write[*Lookup]
		var filesystem kernel.Read[storage.ReadFS]
		return func(access kernel.ResourceAccess) {
				lookup = access.GetWrite[*Lookup]()
				filesystem = access.GetRead[storage.ReadFS]()
			}, func(k kernel.Kernel, req lookupProbeReq) (lookupProbeResp, error) {
				req.run(NewLookupAccess(k, lookup.Get(), filesystem.Get()))
				return lookupProbeResp{}, nil
			}
	})
	registrar.HandleCommand[readFileProbeCmd](func() (kernel.Lock, kernel.Execute[readFileProbeReq, readFileProbeResp]) {
		var filesystem kernel.Read[storage.ReadFS]
		return func(access kernel.ResourceAccess) {
				filesystem = access.GetRead[storage.ReadFS]()
			}, func(_ kernel.Kernel, req readFileProbeReq) (readFileProbeResp, error) {
				data, err := fs.ReadFile(filesystem.Get(), req.Name)
				return readFileProbeResp{Data: data}, err
			}
	})
	return nil
}

// probeLookup executes fn with a scoped LookupAccess inside a canvas handler.
func probeLookup(k kernel.Executioner, fn func(LookupAccess)) {
	k.ExecuteCommand[lookupProbeCmd](lookupProbeReq{run: fn})
}
func testKernel(t testing.TB, filesystem fs.FS, config Config, record func(*OpQueue)) (kernel.Executioner, *Plugin, *testBackend) {
	return testKernelHandler(t, filesystem, config, record, func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	})
}

// testKernelCapturing builds a harness whose error handler records reported
// errors instead of failing, so tests can assert the report-once behavior of the
// Lookup query API.
func testKernelCapturing(t testing.TB, filesystem fs.FS, config Config, record func(*OpQueue)) (kernel.Executioner, *[]error) {
	var errs []error
	k, _, _ := testKernelHandler(t, filesystem, config, record, func(err error) bool {
		errs = append(errs, err)
		return false
	})
	return k, &errs
}

func testKernelHandler(t testing.TB, filesystem fs.FS, config Config, record func(*OpQueue), onError func(error) bool) (kernel.Executioner, *Plugin, *testBackend) {
	t.Helper()
	canvasPlugin := New()
	backend := &testBackend{capture: true}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	configs := map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("canvas-test").WithReadFS("test", 10, filesystem),
		Name:         config,
	}
	engine := kernel.New(configs).Handler(onError).WithPlugins(storage.New(), gfx.New(), canvasPlugin, recordCanvasPlugin{record: record})
	go engine.Run(ctx)
	<-engine.Ready()
	k := engine.Executioner()
	k.PublishEvent(app.InitEvent{}).Wait()
	k.ExecuteCommand[gfx.SetBackendCmd](gfx.SetBackendRequest{Backend: backend})
	k.ExecuteCommand[app.SetViewportCmd](app.SetViewportRequest{
		Width: 100, Height: 100, FramebufferWidth: 200, FramebufferHeight: 200,
	})
	return k, canvasPlugin, backend
}

func runFrame(k kernel.Executioner) {
	k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()
	k.PublishEvent(app.RenderEvent{}).Wait()
}

func TestPluginMountsBuiltInShaders(t *testing.T) {
	k, _, _ := testKernel(t, fstest.MapFS{}, DefaultConfig(), func(*OpQueue) {})
	for _, path := range []string{spriteShaderPath, trianglesShaderPath} {
		want, err := fs.ReadFile(shaderFS, path)
		if err != nil {
			t.Fatalf("read embedded shader %q: %v", path, err)
		}
		got, _ := k.ExecuteCommand[readFileProbeCmd](readFileProbeReq{Name: path})
		if !bytes.Equal(got.Data, want) {
			t.Fatalf("mounted shader %q differs from embedded source", path)
		}
	}
}

func TestDefaultAtlasUsesTwoLayerArrays(t *testing.T) {
	config := DefaultConfig()
	if config.LayersPerArray != 2 {
		t.Fatalf("default atlas layers = %d, want 2", config.LayersPerArray)
	}
}

func BenchmarkCanvasRecordSteadyState(b *testing.B) {
	var list opQueue
	params := []gfx.ParameterDescr{gfx.ColorParam("tint", m.Color{R: 1, A: 1})}
	transform := SpriteTransform{Position: m.Vec2{X: 10, Y: 20}, Size: m.Vec2{X: 32, Y: 32}}
	list.Sprite(1, "images/sprite.png", transform, nil, params...)
	list.reset()
	b.ReportAllocs()
	for b.Loop() {
		list.Sprite(1, "images/sprite.png", transform, nil, params...)
		list.reset()
	}
}

func BenchmarkCanvasRecordCustomMaterial(b *testing.B) {
	var list opQueue
	material := gfx.Material(gfx.ShaderWithText("// custom"), gfx.FloatParam("base", 1))
	params := []gfx.ParameterDescr{gfx.ColorParam("tint", m.Color{R: 1, A: 1})}
	transform := SpriteTransform{Position: m.Vec2{X: 10, Y: 20}, Size: m.Vec2{X: 32, Y: 32}}
	list.Sprite(1, "images/sprite.png", transform, &material, params...)
	list.reset()
	b.ReportAllocs()
	for b.Loop() {
		list.Sprite(1, "images/sprite.png", transform, &material, params...)
		list.reset()
	}
}

func BenchmarkCanvasRecordTriangles(b *testing.B) {
	var list opQueue
	vertices := []Vertex{
		{Position: m.Vec2{}, Color: m.Color{R: 1, A: 1}},
		{Position: m.Vec2{X: 10}, Color: m.Color{G: 1, A: 1}},
		{Position: m.Vec2{Y: 10}, Color: m.Color{B: 1, A: 1}},
	}
	list.DrawTriangles(1, vertices, nil)
	list.reset()
	b.ReportAllocs()
	for b.Loop() {
		list.DrawTriangles(1, vertices, nil)
		list.reset()
	}
}

func BenchmarkCanvasFlushSprites(b *testing.B) {
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernel(b, fstest.MapFS{}, config, func(write *OpQueue) {
		for i := range 100 {
			write.Sprite(Layer(i%4), "", SpriteTransform{
				Position: m.Vec2{X: float32(i), Y: float32(i)}, Size: m.Vec2{X: 8, Y: 8},
			}, nil)
		}
	})
	runFrame(k)
	backend.capture = false
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		runFrame(k)
	}
}

func BenchmarkCanvasFlushTexturedTriangles(b *testing.B) {
	filesystem := fstest.MapFS{}
	paths := []string{"a.png", "b.png", "c.png", "d.png", "e.png", "f.png", "g.png", "h.png"}
	for _, p := range paths {
		filesystem[p] = &fstest.MapFile{Data: pngBytes(b, 8, 8)}
	}
	config := Config{AtlasSize: 64, LayersPerArray: 2, MaxAtlasBytes: 64 * 64 * 4 * 2}
	white := m.Color{R: 1, G: 1, B: 1, A: 1}
	verts := []Vertex{
		{Position: m.Vec2{X: 0, Y: 0}, Color: white}, {Position: m.Vec2{X: 8, Y: 0}, UV: m.Vec2{X: 1}, Color: white}, {Position: m.Vec2{X: 8, Y: 8}, UV: m.Vec2{X: 1, Y: 1}, Color: white},
		{Position: m.Vec2{X: 0, Y: 0}, Color: white}, {Position: m.Vec2{X: 8, Y: 8}, UV: m.Vec2{X: 1, Y: 1}, Color: white}, {Position: m.Vec2{X: 0, Y: 8}, UV: m.Vec2{Y: 1}, Color: white},
	}
	k, _, backend := testKernel(b, filesystem, config, func(write *OpQueue) {
		for i := 0; i < 300; i++ {
			write.DrawTriangles(Layer(i%6), verts, nil,
				gfx.TextureParam(TextureSlot, gfx.TextureWithResource(paths[i%len(paths)])),
				gfx.SamplerParam(SamplerSlot, gfx.AddressClamp, gfx.FilterLinear))
		}
	})
	runFrame(k)
	backend.capture = false
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		runFrame(k)
	}
}

func TestPrimitiveHelpersUseWhiteSpriteAndNormalizePaths(t *testing.T) {
	var list opQueue
	list.FillRect(1, m.Rect{X: 1, Y: 2, Width: 3, Height: 4}, m.Color{R: 1})
	list.Line(1, m.Vec2{}, m.Vec2{X: 10}, 2, m.Color{G: 1})
	list.Sprite(1, `images\units\..\hero.png`, SpriteTransform{Size: m.Vec2{X: 1, Y: 1}}, nil)
	ops := list.ops[1].ops
	if len(ops) != 3 || ops[0].sprite.path != "" || ops[1].sprite.path != "" {
		t.Fatalf("primitive paths = (%q,%q), want empty", ops[0].sprite.path, ops[1].sprite.path)
	}
	if got := ops[2].sprite.path; got != "images/hero.png" {
		t.Fatalf("normalized path = %q, want images/hero.png", got)
	}
}

func TestEmptyPathUsesWhiteAtlasAndLayersAreOrdered(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{}}
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(5, "", SpriteTransform{Position: m.Vec2{X: 50}, Size: m.Vec2{X: 10, Y: 10}}, nil)
		write.Sprite(1, "", SpriteTransform{Position: m.Vec2{X: 10}, Size: m.Vec2{X: 10, Y: 10}}, nil)
	})
	runFrame(k)

	if filesystem.opens != 0 {
		t.Fatalf("empty-path storage opens = %d, want 0", filesystem.opens)
	}
	if len(backend.allocations) != 1 || backend.allocations[0].desc.Width != 16 || backend.allocations[0].desc.Height != 16 || backend.allocations[0].desc.Layers != 2 {
		t.Fatalf("atlas allocations = %+v, want one 16x16x2 texture array", backend.allocations)
	}
	if len(backend.updates) != 1 || backend.updates[0].pixels[0] != 255 {
		t.Fatalf("white updates = %+v, want one opaque-white upload", backend.updates)
	}
	if backend.draws != 2 || len(backend.drawParams) != 2 {
		t.Fatalf("draws/params = (%d,%d), want 2", backend.draws, len(backend.drawParams))
	}
	instances := spriteInstances(backend)
	if len(instances) != 2 {
		t.Fatalf("instance buffers = %d, want 2 (one per layer batch)", len(instances))
	}
	first := instanceAt(instances[0], 0)
	if firstX := floatAt(first, 0); firstX != 10 {
		t.Fatalf("first layered draw X = %v, want lower-layer X 10", firstX)
	}
	if u0, v0, u1, v1 := floatAt(first, 32), floatAt(first, 36), floatAt(first, 40), floatAt(first, 44); u0 != 0.5/16 || v0 != 0.5/16 || u1 != 0.5/16 || v1 != 0.5/16 {
		t.Fatalf("white sprite UV = (%v,%v,%v,%v), want texel center", u0, v0, u1, v1)
	}
	if width, height := floatAt(backend.drawParams[0], 48), floatAt(backend.drawParams[0], 52); width != 100 || height != 100 {
		t.Fatalf("Canvas viewport = %vx%v, want logical 100x100", width, height)
	}
	if len(backend.pipelines) != 1 || backend.pipelines[0].DepthTest || backend.pipelines[0].Blend != gfx.BlendAlpha {
		t.Fatalf("default pipeline state = %+v, want alpha with depth disabled", backend.pipelines)
	}
}

func TestLogicalAtlasPagesShareOneTextureArrayAndBatch(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{
		"a.png": &fstest.MapFile{Data: pngBytes(t, 10, 10)},
		"b.png": &fstest.MapFile{Data: pngBytes(t, 10, 10)},
	}}
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "a.png", SpriteTransform{}, nil)
		write.Sprite(0, "b.png", SpriteTransform{}, nil)
	})
	runFrame(k)

	if len(backend.allocations) != 1 || backend.allocations[0].desc != (gfx.TextureDesc{Width: 16, Height: 16, Layers: 2, Format: gfx.FormatRGBA8}) {
		t.Fatalf("atlas allocations = %+v, want one 16x16x2 texture array", backend.allocations)
	}
	if len(backend.updates) != 3 {
		t.Fatalf("atlas updates = %d, want white texel and two sprites", len(backend.updates))
	}
	if update := backend.updates[2]; update.id != backend.updates[1].id || update.layer != 1 || update.region.Y != 0 {
		t.Fatalf("second-page update = %+v, want same texture at layer 1, Y 0", update)
	}
	if backend.draws != 1 {
		t.Fatalf("draws = %d, want sprites from both logical pages in one batch", backend.draws)
	}
	instances := spriteInstances(backend)
	if len(instances) != 1 {
		t.Fatalf("instance buffers = %d, want one batch", len(instances))
	}
	second := instanceAt(instances[0], 1)
	if v0, v1, layer := floatAt(second, 36), floatAt(second, 44), floatAt(second, 64); v0 != 2.0/16 || v1 != 12.0/16 || layer != 1 {
		t.Fatalf("second-page V coordinates/layer = (%v,%v,%v), want (2/16,12/16,1)", v0, v1, layer)
	}
}

func TestLayerTransformFinalStateAndActiveClipSnapshot(t *testing.T) {
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernel(t, fstest.MapFS{}, config, func(write *OpQueue) {
		write.SetClip(m.Rect{X: 1, Y: 2, Width: 3, Height: 4})
		write.Sprite(1, "", SpriteTransform{Size: m.Vec2{X: 10, Y: 10}}, nil)
		write.Sprite(2, "", SpriteTransform{Size: m.Vec2{X: 10, Y: 10}}, nil)
		write.SetLayerTransform(1, m.Rect{X: 10, Y: 20, Width: 50, Height: 25}, AspectInscribe)
	})
	runFrame(k)
	if len(backend.drawParams) != 2 {
		t.Fatalf("draw params = %d, want 2", len(backend.drawParams))
	}
	if scaleX, scaleY := floatAt(backend.drawParams[0], 64), floatAt(backend.drawParams[0], 84); scaleX != 2 || scaleY != 2 {
		t.Fatalf("layer transform scale = (%v,%v), want inscribed (2,2)", scaleX, scaleY)
	}
	if x, y := floatAt(backend.drawParams[0], 112), floatAt(backend.drawParams[0], 116); x != -20 || y != -15 {
		t.Fatalf("layer transform translation = (%v,%v), want centered (-20,-15)", x, y)
	}
	for i, params := range backend.drawParams {
		if enabled := floatAt(params, 56); enabled != 1 {
			t.Fatalf("draw %d clip enabled = %v, want 1", i, enabled)
		}
		if left, top, right, bottom := floatAt(params, 128), floatAt(params, 132), floatAt(params, 136), floatAt(params, 140); left != 1 || top != 2 || right != 4 || bottom != 6 {
			t.Fatalf("draw %d clip = (%v,%v,%v,%v), want (1,2,4,6)", i, left, top, right, bottom)
		}
	}
}

func TestClipSnapshotIsPerOperation(t *testing.T) {
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernel(t, fstest.MapFS{}, config, func(write *OpQueue) {
		write.Sprite(0, "", SpriteTransform{Size: m.Vec2{X: 10, Y: 10}}, nil)
		write.SetClip(m.Rect{X: 1, Y: 2, Width: 3, Height: 4})
		write.Sprite(0, "", SpriteTransform{Size: m.Vec2{X: 10, Y: 10}}, nil)
		write.RemoveClip()
		write.Sprite(0, "", SpriteTransform{Size: m.Vec2{X: 10, Y: 10}}, nil)
	})
	runFrame(k)
	if len(backend.drawParams) != 3 {
		t.Fatalf("draw params = %d, want 3 in record order", len(backend.drawParams))
	}
	if enabled := floatAt(backend.drawParams[0], 56); enabled != 0 {
		t.Fatalf("draw before SetClip clip enabled = %v, want 0", enabled)
	}
	if enabled := floatAt(backend.drawParams[1], 56); enabled != 1 {
		t.Fatalf("draw under clip enabled = %v, want 1", enabled)
	}
	if left, top, right, bottom := floatAt(backend.drawParams[1], 128), floatAt(backend.drawParams[1], 132), floatAt(backend.drawParams[1], 136), floatAt(backend.drawParams[1], 140); left != 1 || top != 2 || right != 4 || bottom != 6 {
		t.Fatalf("clipped draw clip = (%v,%v,%v,%v), want (1,2,4,6)", left, top, right, bottom)
	}
	if enabled := floatAt(backend.drawParams[2], 56); enabled != 0 {
		t.Fatalf("draw after RemoveClip enabled = %v, want 0", enabled)
	}
}

func TestLayerTransformAspectModes(t *testing.T) {
	view := &app.Viewport{Width: 100, Height: 100}
	tests := []struct {
		name             string
		aspect           AspectMode
		scaleX, scaleY   float32
		offsetX, offsetY float32
	}{
		{name: "inscribe", aspect: AspectInscribe, scaleX: 2, scaleY: 2, offsetX: -20, offsetY: -15},
		{name: "overlap", aspect: AspectOverlap, scaleX: 4, scaleY: 4, offsetX: -90, offsetY: -80},
		{name: "stretch", aspect: AspectStretch, scaleX: 2, scaleY: 4, offsetX: -20, offsetY: -80},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var list opQueue
			list.SetLayerTransform(1, m.Rect{X: 10, Y: 20, Width: 50, Height: 25}, test.aspect)
			transform := resolveLayerTransform(list.ops[1], view)
			if transform[0] != test.scaleX || transform[5] != test.scaleY || transform[12] != test.offsetX || transform[13] != test.offsetY {
				t.Fatalf("transform scale=(%v,%v) offset=(%v,%v), want scale=(%v,%v) offset=(%v,%v)",
					transform[0], transform[5], transform[12], transform[13], test.scaleX, test.scaleY, test.offsetX, test.offsetY)
			}
		})
	}
}

func TestLayerTransformHelpersInvert(t *testing.T) {
	window := m.Rect{X: 10, Y: 20, Width: 50, Height: 25}
	viewport := m.Vec2{X: 100, Y: 100}
	scale, offset := LayerTransform(window, AspectInscribe, viewport)
	if scale != (m.Vec2{X: 2, Y: 2}) || offset != (m.Vec2{X: -20, Y: -15}) {
		t.Fatalf("transform = scale %+v offset %+v, want (2,2)/(-20,-15)", scale, offset)
	}
	world := m.Vec2{X: 30, Y: 25}
	if screen := WorldToScreen(window, AspectInscribe, viewport, world); screen != (m.Vec2{X: 40, Y: 35}) {
		t.Fatalf("world->screen = %+v, want (40,35)", screen)
	}
	if back := ScreenToWorld(window, AspectInscribe, viewport, m.Vec2{X: 40, Y: 35}); back != world {
		t.Fatalf("screen->world = %+v, want %+v", back, world)
	}
	if scale, offset := LayerTransform(m.Rect{}, AspectStretch, viewport); scale != (m.Vec2{X: 1, Y: 1}) || offset != (m.Vec2{}) {
		t.Fatalf("zero window = scale %+v offset %+v, want identity", scale, offset)
	}
}

func TestDrawTrianglesSnapshotsStandardVerticesAndUsesLayerTransform(t *testing.T) {
	vertices := []Vertex{
		{Position: m.Vec2{X: 1, Y: 2}, Color: m.Color{R: 1, A: 1}, UV: m.Vec2{}},
		{Position: m.Vec2{X: 11, Y: 2}, Color: m.Color{G: 1, A: 1}, UV: m.Vec2{X: 1}},
		{Position: m.Vec2{X: 1, Y: 12}, Color: m.Color{B: 1, A: 1}, UV: m.Vec2{Y: 1}},
	}
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernel(t, fstest.MapFS{}, config, func(write *OpQueue) {
		write.DrawTriangles(1, vertices, nil)
		vertices[0].Position.X = 99
		write.SetLayerTransform(1, m.Rect{Width: 50, Height: 50}, AspectStretch)
	})
	runFrame(k)
	if backend.draws != 1 || len(backend.pipelines) != 1 {
		t.Fatalf("draws/pipelines = (%d,%d), want (1,1)", backend.draws, len(backend.pipelines))
	}
	pipeline := backend.pipelines[0]
	if pipeline.Stride != 32 || len(pipeline.Attributes) != 3 ||
		pipeline.Attributes[0] != (gfx.VertexAttribute{Offset: 0, Type: gfx.Float32x2, Location: 0}) ||
		pipeline.Attributes[1] != (gfx.VertexAttribute{Offset: 8, Type: gfx.Float32x4, Location: 1}) ||
		pipeline.Attributes[2] != (gfx.VertexAttribute{Offset: 24, Type: gfx.Float32x2, Location: 2}) {
		t.Fatalf("triangle vertex pipeline = %+v", pipeline)
	}
	var uploaded []byte
	for _, buffer := range backend.buffers {
		if buffer.kind == gfx.BufferVertex && len(buffer.data) == len(vertices)*32 {
			uploaded = buffer.data
		}
	}
	if uploaded == nil || floatAt(uploaded, 0) != 1 || floatAt(uploaded, 8) != 1 || floatAt(uploaded, 24) != 0 {
		t.Fatalf("triangle vertex upload = %v, want snapshotted position/color/uv", uploaded)
	}
	params := backend.drawParams[0]
	if scaleX, scaleY := floatAt(params, 64), floatAt(params, 84); scaleX != 2 || scaleY != 2 {
		t.Fatalf("triangle layer scale = (%v,%v), want (2,2)", scaleX, scaleY)
	}
}

func TestDrawTrianglesBindsTextureViaSlotParams(t *testing.T) {
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	white := m.Color{R: 1, G: 1, B: 1, A: 1}
	k, _, backend := testKernel(t, fstest.MapFS{}, config, func(write *OpQueue) {
		write.DrawTriangles(0, []Vertex{
			{Position: m.Vec2{}, Color: white},
			{Position: m.Vec2{X: 4}, Color: white, UV: m.Vec2{X: 2}},
			{Position: m.Vec2{Y: 4}, Color: white, UV: m.Vec2{Y: 2}},
		}, nil,
			gfx.TextureParam(TextureSlot, gfx.TextureWithBytes(2, 2, gfx.FormatRGBA8, make([]byte, 16), true, false)),
			gfx.SamplerParam(SamplerSlot, gfx.AddressRepeat, gfx.FilterNearest),
		)
	})
	runFrame(k)
	if backend.draws != 1 || len(backend.pipelines) != 1 {
		t.Fatalf("draws/pipelines = (%d,%d), want 1 texture-slot triangle", backend.draws, len(backend.pipelines))
	}
}

func TestDrawTrianglesSupportsCustomVertexLayout(t *testing.T) {
	vertices := []customTriangleVertex{
		{Position: m.Vec2{X: 1, Y: 2}, Data: m.Vec4{X: 3, Y: 4, Z: 5, W: 6}},
		{Position: m.Vec2{X: 7, Y: 8}},
		{Position: m.Vec2{X: 9, Y: 10}},
	}
	material := gfx.MaterialWithState(
		gfx.ShaderWithText("// custom triangle shader"),
		gfx.MaterialState{Blend: gfx.BlendOpaque, DepthTest: false},
	)
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernel(t, fstest.MapFS{}, config, func(write *OpQueue) {
		write.DrawTriangles(1, vertices, &material)
		vertices[0].Position.X = 99
		customTriangleVertexLayout[0] = gfx.Attr(4, gfx.Float32)
	})
	t.Cleanup(func() { customTriangleVertexLayout[0] = gfx.Attr(0, gfx.Float32x2) })
	runFrame(k)
	if len(backend.pipelines) != 1 {
		t.Fatalf("pipelines = %d, want 1", len(backend.pipelines))
	}
	pipeline := backend.pipelines[0]
	if pipeline.Stride != 24 || len(pipeline.Attributes) != 2 ||
		pipeline.Attributes[0] != (gfx.VertexAttribute{Offset: 0, Type: gfx.Float32x2, Location: 0}) ||
		pipeline.Attributes[1] != (gfx.VertexAttribute{Offset: 8, Type: gfx.Float32x4, Location: 1}) ||
		pipeline.Blend != gfx.BlendOpaque {
		t.Fatalf("custom triangle pipeline = %+v", pipeline)
	}
	var uploaded []byte
	for _, buffer := range backend.buffers {
		if buffer.kind == gfx.BufferVertex && len(buffer.data) == len(vertices)*24 {
			uploaded = buffer.data
		}
	}
	if uploaded == nil || floatAt(uploaded, 0) != 1 || floatAt(uploaded, 8) != 3 {
		t.Fatalf("custom triangle upload = %v, want snapshotted custom vertices", uploaded)
	}
}

func TestCustomMaterialAndParametersPassThrough(t *testing.T) {
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	custom := gfx.MaterialWithState(
		gfx.ShaderWithText("// custom canvas shader"),
		gfx.MaterialState{Blend: gfx.BlendOpaque, DepthTest: false},
		gfx.FloatParam("customValue", 1),
	)
	k, _, backend := testKernel(t, fstest.MapFS{}, config, func(write *OpQueue) {
		write.Sprite(0, "", SpriteTransform{Position: m.Vec2{X: 12}, Size: m.Vec2{X: 8, Y: 8}}, &custom,
			gfx.FloatParam("customValue", 7),
			gfx.VecParam("canvasTransform0", m.Vec4{X: 999}),
		)
	})
	runFrame(k)
	if len(backend.drawParams) != 1 {
		t.Fatalf("draw params = %d, want 1", len(backend.drawParams))
	}
	if got := floatAt(backend.drawParams[0], 0); got != 12 {
		t.Fatalf("reserved transform X = %v, want 12", got)
	}
	if got := floatAt(backend.drawParams[0], 180); got != 7 {
		t.Fatalf("custom value = %v, want draw override 7", got)
	}
	if len(backend.pipelines) != 1 || backend.pipelines[0].Blend != gfx.BlendOpaque || backend.pipelines[0].DepthTest {
		t.Fatalf("custom pipeline state = %+v", backend.pipelines)
	}
}

func TestSpriteLoadsOnceAndAppliesFramePadding(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{"sprite.png": &fstest.MapFile{Data: pngBytes(t, 4, 3)}}}
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "sprite.png", SpriteTransform{
			Size: m.Vec2{X: 10, Y: 10}, Frame: SpriteFrame{Left: 1, Top: 1, Right: 1},
		}, nil)
	})
	runFrame(k)
	runFrame(k)
	if filesystem.opens != 1 {
		t.Fatalf("sprite opens = %d, want one lazy load", filesystem.opens)
	}
	if len(backend.updates) != 2 {
		t.Fatalf("atlas uploads = %d, want white texel and sprite", len(backend.updates))
	}
	inst := instanceAt(spriteInstances(backend)[0], 0)
	u0, v0 := floatAt(inst, 32), floatAt(inst, 36)
	u1, v1 := floatAt(inst, 40), floatAt(inst, 44)
	if u0 != 4.0/16 || v0 != 3.0/16 || u1 != 6.0/16 || v1 != 5.0/16 {
		t.Fatalf("frame UV = (%v,%v,%v,%v)", u0, v0, u1, v1)
	}
}

func TestSpriteNineSliceExpandsAfterTextureDimensionsResolve(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{"sprite.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)}}}
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "sprite.png", SpriteTransform{
			Size: m.Vec2{X: 12, Y: 12}, NineSlice: SpriteFrame{Left: 1, Top: 1, Right: 1, Bottom: 1},
		}, nil)
	})
	runFrame(k)
	instances := spriteInstances(backend)
	if len(instances) != 1 || len(instances[0])/96 != 9 {
		t.Fatalf("nine-slice instances = %d batches, %d instances; want 1, 9", len(instances), len(instances[0])/96)
	}
}

func TestUnloadSpriteReloadsOnNextFrame(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{"sprite.png": &fstest.MapFile{Data: pngBytes(t, 2, 2)}}}
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "sprite.png", SpriteTransform{Size: m.Vec2{X: 8, Y: 8}}, nil)
	})
	runFrame(k)
	probeLookup(k, func(la LookupAccess) { la.UnloadSprite("sprite.png") })
	runFrame(k)
	if filesystem.opens != 2 || len(backend.updates) != 3 {
		t.Fatalf("path reload opens/updates = (%d,%d), want (2,3)", filesystem.opens, len(backend.updates))
	}
}

func TestSpriteSnapshotsMaterialAndParametersWhileLayerTransformIsFinal(t *testing.T) {
	var list opQueue
	materialParams := []gfx.ParameterDescr{gfx.FloatParam("base", 1)}
	material := gfx.Material(gfx.ShaderWithText("// custom"), materialParams...)
	params := []gfx.ParameterDescr{gfx.FloatParam("value", 2)}
	window := m.Rect{X: 3, Y: 4, Width: 20, Height: 10}
	list.SetLayerTransform(2, window, AspectStretch)
	list.Sprite(2, "image.png", SpriteTransform{Size: m.Vec2{X: 1, Y: 1}}, &material, params...)
	params[0] = gfx.FloatParam("other", 9)
	materialParams[0] = gfx.FloatParam("mutated", 9)
	list.SetLayerTransform(2, m.Rect{Width: 30, Height: 15}, AspectOverlap)
	op := list.ops[2].ops[0].sprite
	if !op.hasMaterial || reflect.DeepEqual(op.params[0], params[0]) || reflect.DeepEqual(op.material, material) {
		t.Fatal("sprite did not snapshot material and parameters")
	}
	if got := list.ops[2]; got.window != (m.Rect{Width: 30, Height: 15}) || got.aspect != AspectOverlap {
		t.Fatalf("layer window = %+v mode %v, want final frame window", got.window, got.aspect)
	}
}

func TestSpriteSizeReturnsPixelDimensions(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{"sprite.png": &fstest.MapFile{Data: pngBytes(t, 6, 4)}}}
	config := Config{AtlasSize: 32, LayersPerArray: 2, MaxAtlasBytes: 32 * 32 * 4 * 2}
	k, _, _ := testKernel(t, filesystem, config, func(*OpQueue) {})

	var size, again m.Vec2
	probeLookup(k, func(la LookupAccess) {
		size = la.SpriteSize("sprite.png")
	})
	if size != (m.Vec2{X: 6, Y: 4}) {
		t.Fatalf("size = %+v, want 6x4", size)
	}
	if filesystem.opens != 1 {
		t.Fatalf("opens = %d, want one header read", filesystem.opens)
	}

	probeLookup(k, func(la LookupAccess) {
		again = la.SpriteSize("sprite.png")
	})
	if again != (m.Vec2{X: 6, Y: 4}) || filesystem.opens != 1 {
		t.Fatalf("second query should reuse metadata: %+v opens %d", again, filesystem.opens)
	}
}

func TestSpriteScaleRendersTextureSizeTimesScale(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{"sprite.png": &fstest.MapFile{Data: pngBytes(t, 6, 4)}}}
	config := Config{AtlasSize: 32, LayersPerArray: 2, MaxAtlasBytes: 32 * 32 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "sprite.png", SpriteTransform{Scale: 2}, nil)
	})
	runFrame(k)
	if backend.draws != 1 || len(backend.drawParams) != 1 {
		t.Fatalf("draws/params = (%d,%d), want 1 scaled sprite", backend.draws, len(backend.drawParams))
	}
	inst := instanceAt(spriteInstances(backend)[0], 0)
	if w, h := floatAt(inst, 8), floatAt(inst, 12); w != 12 || h != 8 {
		t.Fatalf("scaled size = (%v,%v), want (12,8) = 6x4 texture times 2", w, h)
	}
}

func TestSpriteSizeAspectFitAndDefault(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{"sprite.png": &fstest.MapFile{Data: pngBytes(t, 6, 4)}}}
	config := Config{AtlasSize: 32, LayersPerArray: 2, MaxAtlasBytes: 32 * 32 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "sprite.png", SpriteTransform{Size: m.Vec2{X: 12}}, nil)
		write.Sprite(0, "sprite.png", SpriteTransform{Size: m.Vec2{Y: 8}}, nil)
		write.Sprite(0, "sprite.png", SpriteTransform{}, nil)
	})
	runFrame(k)
	if len(backend.drawParams) != 1 {
		t.Fatalf("draw params = %d, want 1 batched draw", len(backend.drawParams))
	}
	buffer := spriteInstances(backend)[0]
	if w, h := floatAt(instanceAt(buffer, 0), 8), floatAt(instanceAt(buffer, 0), 12); w != 12 || h != 8 {
		t.Fatalf("aspect from width = (%v,%v), want (12,8) preserving 6x4 ratio", w, h)
	}
	if w, h := floatAt(instanceAt(buffer, 1), 8), floatAt(instanceAt(buffer, 1), 12); w != 12 || h != 8 {
		t.Fatalf("aspect from height = (%v,%v), want (12,8) preserving 6x4 ratio", w, h)
	}
	if w, h := floatAt(instanceAt(buffer, 2), 8), floatAt(instanceAt(buffer, 2), 12); w != 6 || h != 4 {
		t.Fatalf("unset size = (%v,%v), want natural texture 6x4 (Scale defaults to 1)", w, h)
	}
}

func TestSpriteFlipSwapsUV(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{"sprite.png": &fstest.MapFile{Data: pngBytes(t, 6, 4)}}}
	config := Config{AtlasSize: 32, LayersPerArray: 2, MaxAtlasBytes: 32 * 32 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "sprite.png", SpriteTransform{Size: m.Vec2{X: 6, Y: 4}}, nil)
		write.Sprite(0, "sprite.png", SpriteTransform{Size: m.Vec2{X: 6, Y: 4}, FlipX: true}, nil)
		write.Sprite(0, "sprite.png", SpriteTransform{Size: m.Vec2{X: 6, Y: 4}, FlipY: true}, nil)
	})
	runFrame(k)
	if len(backend.drawParams) != 1 {
		t.Fatalf("draw params = %d, want 1 batched draw", len(backend.drawParams))
	}
	buffer := spriteInstances(backend)[0]
	// frame uv packs at instance offset 32: X=32, Y=36, Z=40, W=44.
	base := instanceAt(buffer, 0)
	baseX, baseY, baseZ, baseW := floatAt(base, 32), floatAt(base, 36), floatAt(base, 40), floatAt(base, 44)
	flipX := instanceAt(buffer, 1)
	if floatAt(flipX, 32) != baseZ || floatAt(flipX, 40) != baseX || floatAt(flipX, 36) != baseY || floatAt(flipX, 44) != baseW {
		t.Fatalf("FlipX should swap U only: got X=%v Z=%v, want X=%v Z=%v", floatAt(flipX, 32), floatAt(flipX, 40), baseZ, baseX)
	}
	flipY := instanceAt(buffer, 2)
	if floatAt(flipY, 36) != baseW || floatAt(flipY, 44) != baseY || floatAt(flipY, 32) != baseX || floatAt(flipY, 40) != baseZ {
		t.Fatalf("FlipY should swap V only: got Y=%v W=%v, want Y=%v W=%v", floatAt(flipY, 36), floatAt(flipY, 44), baseW, baseY)
	}
}

func TestTiledSpriteRepeatsAcrossSizeViaStandaloneTexture(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{"wave.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)}}}
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	tint := m.Color{R: 1, G: 0, B: 0, A: 1}
	k, _, backend := testKernel(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "wave.png", SpriteTransform{Size: m.Vec2{X: 12, Y: 4}, TileX: true}, nil,
			gfx.ColorParam("tint", tint))
	})
	runFrame(k)
	runFrame(k)
	if backend.draws != 2 {
		t.Fatalf("draws = %d, want 2 tiled draws", backend.draws)
	}
	if filesystem.opens != 1 {
		t.Fatalf("opens = %d, want one cached standalone decode", filesystem.opens)
	}
	var vertices []byte
	for _, buffer := range backend.buffers {
		if buffer.kind == gfx.BufferVertex && len(buffer.data) == 6*32 {
			vertices = buffer.data
		}
	}
	if vertices == nil {
		t.Fatal("tiled sprite vertex buffer was not uploaded")
	}
	// Vertex 1 is the top-right corner: position (12,0), uv (12/4, 0) = 3 repeats.
	if px := floatAt(vertices, 32); px != 12 {
		t.Fatalf("corner x = %v, want 12", px)
	}
	if u := floatAt(vertices, 32+24); u != 3 {
		t.Fatalf("tiled u = %v, want 12/4 = 3 repeats", u)
	}
	if r, g := floatAt(vertices, 8), floatAt(vertices, 12); r != 1 || g != 0 {
		t.Fatalf("vertex color = (%v,%v), want tint baked into color", r, g)
	}
}

func TestTiledSpriteRepeatsOnlyTiledAxes(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{"wave.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)}}}
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "wave.png", SpriteTransform{Size: m.Vec2{X: 12, Y: 4}, TileX: true, Filter: gfx.FilterNearest}, nil)
	})
	runFrame(k)
	found := false
	for _, sampler := range backend.samplers {
		if sampler.Address == gfx.AddressRepeatX && sampler.Filter == gfx.FilterNearest {
			found = true
		}
	}
	if !found {
		t.Fatalf("samplers = %+v, want one with AddressRepeatX and FilterNearest", backend.samplers)
	}
}

func TestDefaultShaderParses(t *testing.T) {
	shader, err := fs.ReadFile(shaderFS, spriteShaderPath)
	if err != nil {
		t.Fatalf("read embedded default shader: %v", err)
	}
	parsed, err := naga.Parse(string(shader))
	if err != nil {
		t.Fatalf("parse default shader: %v", err)
	}
	module, err := wgsl.Lower(parsed)
	if err != nil {
		t.Fatalf("lower default shader: %v", err)
	}
	want := map[string]uint32{
		"canvasTransform0": 0, "canvasTransform1": 16, "canvasFrame": 32,
		"canvasViewport": 48, "atlasLayer": 56, "clipEnabled": 60,
		"canvasLayer": 64, "canvasClip": 128,
		"tint": 144, "keyColor": 160,
	}
	found := false
	for _, variable := range module.GlobalVariables {
		if variable.Name != "u" || variable.Space != ir.SpaceUniform {
			continue
		}
		structure, ok := module.Types[variable.Type].Inner.(ir.StructType)
		if !ok {
			t.Fatal("Canvas uniform is not a struct")
		}
		found = true
		for _, member := range structure.Members {
			if offset, exists := want[member.Name]; !exists || member.Offset != offset {
				t.Fatalf("uniform %q offset = %d, want %d", member.Name, member.Offset, offset)
			}
		}
	}
	if !found {
		t.Fatal("Canvas uniform block was not reflected")
	}
}

func TestTrianglesShaderParses(t *testing.T) {
	shader, err := fs.ReadFile(shaderFS, trianglesShaderPath)
	if err != nil {
		t.Fatalf("read embedded triangles shader: %v", err)
	}
	parsed, err := naga.Parse(string(shader))
	if err != nil {
		t.Fatalf("parse triangles shader: %v", err)
	}
	if _, err := wgsl.Lower(parsed); err != nil {
		t.Fatalf("lower triangles shader: %v", err)
	}
}

func TestSpriteSizeReadsHeaderWithoutGPUUpload(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{"sprite.png": &fstest.MapFile{Data: pngBytes(t, 6, 4)}}}
	config := Config{AtlasSize: 32, LayersPerArray: 2, MaxAtlasBytes: 32 * 32 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(*OpQueue) {})
	var size m.Vec2
	probeLookup(k, func(la LookupAccess) { size = la.SpriteSize("sprite.png") })
	if size != (m.Vec2{X: 6, Y: 4}) {
		t.Fatalf("size = %+v, want 6x4 from header", size)
	}
	if len(backend.allocations) != 0 || len(backend.updates) != 0 {
		t.Fatalf("header sizing uploaded to GPU: allocs %d updates %d", len(backend.allocations), len(backend.updates))
	}
}

func TestLookupReportsMissingAndInvalidPathsOncePerEpisode(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{}}
	config := Config{AtlasSize: 32, LayersPerArray: 2, MaxAtlasBytes: 32 * 32 * 4 * 2}
	k, errs := testKernelCapturing(t, filesystem, config, func(*OpQueue) {})
	var missing, invalid m.Vec2
	probeLookup(k, func(la LookupAccess) {
		missing = la.SpriteSize("gone.png")
		_ = la.SpriteSize("gone.png") // repeat: must not report again
		invalid = la.SpriteSize("../escape.png")
	})
	if missing != (m.Vec2{}) || invalid != (m.Vec2{}) {
		t.Fatalf("failed lookups returned %+v and %+v, want zero", missing, invalid)
	}
	if len(*errs) != 2 {
		t.Fatalf("reported errors = %d, want one per episode (missing + invalid)", len(*errs))
	}
}

func TestPaddedRGBAExtrudesSpriteEdges(t *testing.T) {
	pixels := []byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
	}
	got := paddedRGBA(pixels, 2, 2, 1, true)
	if len(got) != 4*4*4 {
		t.Fatalf("padded bytes = %d, want 64", len(got))
	}
	if !bytes.Equal(got[:4], pixels[:4]) || !bytes.Equal(got[len(got)-4:], pixels[len(pixels)-4:]) {
		t.Fatal("sprite edges were not extruded into padding")
	}
}

func pngBytes(t testing.TB, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x + 1), G: uint8(y + 1), B: 1, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return out.Bytes()
}

func floatAt(data []byte, offset int) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(data[offset:]))
}

// testInstanceSize is the byte size of one batched sprite/glyph instance record.
const testInstanceSize = 96

// spriteInstances returns the per-draw instance storage buffers captured during
// the frame, in draw (flush) order. Instance buffers are storage buffers whose
// size is a whole number of 96-byte instance records, which distinguishes them
// from the small persistent quad vertex/index buffers.
func spriteInstances(b *testBackend) [][]byte {
	var out [][]byte
	for i := range b.buffers {
		if b.buffers[i].kind == gfx.BufferStorage && len(b.buffers[i].data) > 0 && len(b.buffers[i].data)%testInstanceSize == 0 {
			out = append(out, b.buffers[i].data)
		}
	}
	return out
}

// instanceAt returns the i-th instance record within one storage buffer. Field
// offsets: transform0@0 (pos.xy,size.xy), transform1@16 (origin.xy,sin,cos),
// frame@32 (uv), tint@48, misc@64 (atlasLayer), keyColor@80.
func instanceAt(buffer []byte, i int) []byte {
	return buffer[i*testInstanceSize : (i+1)*testInstanceSize]
}
