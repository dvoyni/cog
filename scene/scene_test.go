package scene

import (
	"context"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/storage"
)

// testBackend is a Backend that mints ids and records the passes it was asked
// to encode. Everything scene decides is decided before this is reached, which
// is what makes the whole ticket assertable with no GPU.
type testBackend struct {
	nextTexture gfx.TextureID
	nextBuffer  gfx.BufferID
	nextID      uint32
	passes      []gfx.GpuPassDesc
	presents    int
	// draws, bindings and bakes are what the frame actually asked the GPU to
	// do, which is where the pass-relative instance slices and the one upload
	// per arena become assertable.
	draws    []drawCall
	bindings []bufferBinding
	textures []textureBinding
	samplers []samplerBinding
	bakes    int
	// baked keeps every uploaded buffer's bytes, so a test can read back the
	// records scene packed rather than only their offsets.
	baked map[gfx.BufferID][]byte
}

// drawCall is one recorded draw, so a test can assert the arguments that reach
// the backend rather than an op encoding.
type drawCall struct {
	first, count, instances, firstInstance int
	indexed                                bool
}

// bufferBinding is one storage range bound into a bind group.
type bufferBinding struct {
	group, binding int
	buffer         gfx.BufferID
	offset, size   int
}

// textureBinding and samplerBinding are the material-group resources one draw
// bound, which is what makes "all five slots, always" assertable with no GPU.
type textureBinding struct {
	group, binding int
	texture        gfx.TextureID
}

type samplerBinding struct {
	group, binding int
	sampler        gfx.SamplerID
}

// testShaderLayout is what reflection reports for the bundled scene shader. The
// real source is reflected and asserted in the wgpu package, the only tree with
// a WGSL front end; here it stands in so that scene's bindings reach the
// backend and can be read back by name.
var testShaderLayout = gfx.ShaderLayout{Resources: []gfx.ShaderResource{
	{Name: "sceneFrame", StorageBuffer: true, Group: 0, Binding: 0},
	{Name: "sceneInstances", StorageBuffer: true, Group: 0, Binding: 1},
	{Name: "sceneAnim", StorageBuffer: true, Group: 0, Binding: 2},
	{Name: "scenePbrMaterial", StorageBuffer: true, Group: 1, Binding: 0},
	{Name: "baseColorTexture", Group: 1, Binding: 1},
	{Name: "baseColorSampler", Sampler: true, Group: 1, Binding: 2},
	{Name: "metallicRoughnessTexture", Group: 1, Binding: 3},
	{Name: "metallicRoughnessSampler", Sampler: true, Group: 1, Binding: 4},
	{Name: "normalTexture", Group: 1, Binding: 5},
	{Name: "normalSampler", Sampler: true, Group: 1, Binding: 6},
	{Name: "occlusionTexture", Group: 1, Binding: 7},
	{Name: "occlusionSampler", Sampler: true, Group: 1, Binding: 8},
	{Name: "emissiveTexture", Group: 1, Binding: 9},
	{Name: "emissiveSampler", Sampler: true, Group: 1, Binding: 10},
	{Name: "scenePoses", StorageBuffer: true, Group: 2, Binding: 0},
	{Name: "sceneSkinJoints", StorageBuffer: true, Group: 2, Binding: 1},
	{Name: "sceneMorphDeltas", StorageBuffer: true, Group: 2, Binding: 2},
}}

// texturesBoundTo reports the texture bound to one reflected binding on each
// draw, in the order the frame bound them. A slot nobody bound reports nothing,
// which is the failure that takes the whole frame's command buffer down.
func (b *testBackend) texturesBoundTo(name string) []gfx.TextureID {
	group, binding := bindingOf(name)
	var found []gfx.TextureID
	for _, bound := range b.textures {
		if bound.group == group && bound.binding == binding {
			found = append(found, bound.texture)
		}
	}
	return found
}

// samplersBoundTo reports the sampler bound to one reflected binding per draw.
func (b *testBackend) samplersBoundTo(name string) []gfx.SamplerID {
	group, binding := bindingOf(name)
	var found []gfx.SamplerID
	for _, bound := range b.samplers {
		if bound.group == group && bound.binding == binding {
			found = append(found, bound.sampler)
		}
	}
	return found
}

func bindingOf(name string) (group, binding int) {
	for _, resource := range testShaderLayout.Resources {
		if resource.Name == name {
			return resource.Group, resource.Binding
		}
	}
	return -1, -1
}

// buffersBoundTo reports every range bound to one reflected binding, in the
// order the frame bound them.
func (b *testBackend) buffersBoundTo(name string) []bufferBinding {
	group, binding := -1, -1
	for _, resource := range testShaderLayout.Resources {
		if resource.Name == name {
			group, binding = resource.Group, resource.Binding
		}
	}
	var found []bufferBinding
	for _, bound := range b.bindings {
		if bound.group == group && bound.binding == binding {
			found = append(found, bound)
		}
	}
	return found
}

func (b *testBackend) NewTexture() gfx.TextureID { b.nextTexture++; return b.nextTexture }
func (b *testBackend) NewBuffer() gfx.BufferID   { b.nextBuffer++; return b.nextBuffer }
func (b *testBackend) NewSampler(gfx.SamplerDesc) (gfx.SamplerID, error) {
	b.nextID++
	return gfx.SamplerID(b.nextID), nil
}
func (b *testBackend) FreeSampler(gfx.SamplerID) {}
func (b *testBackend) NewShader(gfx.ShaderDesc) (gfx.ShaderID, error) {
	b.nextID++
	return gfx.ShaderID(b.nextID), nil
}
func (b *testBackend) FreeShader(gfx.ShaderID)                    {}
func (b *testBackend) ShaderLayout(gfx.ShaderID) gfx.ShaderLayout { return testShaderLayout }
func (b *testBackend) NewPipeline(gfx.PipelineDesc) (gfx.PipelineID, error) {
	b.nextID++
	return gfx.PipelineID(b.nextID), nil
}
func (b *testBackend) FreePipeline(gfx.PipelineID) {}
func (b *testBackend) ScreenFramebuffer() (gfx.TextureViewID, int, int) {
	return 1, 1600, 1200
}
func (b *testBackend) Limits() gfx.Limits { return gfx.DefaultLimits }
func (b *testBackend) TextureView(gfx.TextureID, int, int) gfx.TextureViewID {
	b.nextID++
	return gfx.TextureViewID(b.nextID)
}
func (b *testBackend) Execute(queue *gfx.GpuQueue) {
	queue.ReplayBakes(b)
	queue.ReplayPasses(b)
	queue.ReplayReleases(b)
}
func (b *testBackend) BeginPass(desc gfx.GpuPassDesc) gfx.RenderPass {
	b.passes = append(b.passes, desc)
	return b
}
func (b *testBackend) EndPass(gfx.RenderPass) {}
func (b *testBackend) Present()               { b.presents++ }

// BakeBuffer keeps the bytes as well as counting the upload, because the
// records scene packs are only readable here: everything downstream of the
// arena is an offset and a size, and a flag written into the wrong instance is
// exactly the failure that reads as a plausible wrong picture.
func (b *testBackend) BakeBuffer(id gfx.BufferID, _ gfx.BufferKind, _ int, data []byte) {
	b.bakes++
	if b.baked == nil {
		b.baked = map[gfx.BufferID][]byte{}
	}
	b.baked[id] = append([]byte(nil), data...)
}
func (b *testBackend) BakeTexture(gfx.TextureID, int, int, gfx.TextureFormat, []byte, bool) {}
func (b *testBackend) AllocateTexture(gfx.TextureID, gfx.TextureDesc)                       {}
func (b *testBackend) UpdateTexture(gfx.TextureID, int, gfx.Region, []byte)                 {}
func (b *testBackend) SetPipeline(gfx.PipelineID)                                           {}
func (b *testBackend) SetParams([]byte)                                                     {}
func (b *testBackend) SetTexture(texture gfx.TextureID, group, binding int) {
	b.textures = append(b.textures, textureBinding{group: group, binding: binding, texture: texture})
}

func (b *testBackend) SetSampler(sampler gfx.SamplerID, group, binding int) {
	b.samplers = append(b.samplers, samplerBinding{group: group, binding: binding, sampler: sampler})
}
func (b *testBackend) SetVertexBuffer(gfx.BufferID, int) {}
func (b *testBackend) SetIndexBuffer(gfx.BufferID, int)  {}
func (b *testBackend) SetBuffer(group, binding int, buffer gfx.BufferID, offset, size int) {
	b.bindings = append(b.bindings, bufferBinding{
		group: group, binding: binding, buffer: buffer, offset: offset, size: size,
	})
}

func (b *testBackend) Draw(first, count, instances, firstInstance int, indexed bool) {
	b.draws = append(b.draws, drawCall{
		first: first, count: count, instances: instances, firstInstance: firstInstance, indexed: indexed,
	})
}
func (b *testBackend) ReleaseBuffer(gfx.BufferID)   {}
func (b *testBackend) ReleaseTexture(gfx.TextureID) {}

// recordPlugin is the gameplay side of the harness: a separate plugin that
// locks scene's OpQueue, exactly as a real recorder does. It holds the gfx
// queue as well, ordered ahead of scene's flush, so a recorder that allocates a
// temporary target allocates it in the frame the pass using it is emitted into.
type recordPlugin struct{ record func(*OpQueue, *gfx.OpQueue) }
type recordHandler kernel.Subscription[app.UpdateEvent]

// inspectCmd runs a callback inside a handler holding scene's OpQueue, so a
// test reads Ops and Passes the way a real caller would.
type inspectCmd kernel.Command[inspectRequest, inspectResponse]
type inspectRequest struct{ run func(*OpQueue) }
type inspectResponse struct{}

// lookupProbeCmd runs a callback with a valid scoped LookupAccess.
type lookupProbeCmd kernel.Command[lookupProbeRequest, lookupProbeResponse]
type lookupProbeRequest struct {
	run func(LookupAccess)
	// files is the separate probe readFile needs. LookupAccess carries no
	// filesystem any more - that is the facade's whole "two dependencies, not
	// three" - so a test that wants to read a mounted file asks for one here.
	files func(storage.FileSystem)
}
type lookupProbeResponse struct{}

func (p recordPlugin) Name() kernel.PluginName { return "scene-test-recorder" }
func (p recordPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{Name, storage.Name}
}

func (p recordPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[recordHandler](func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
		var queue kernel.Write[*OpQueue]
		var gfxQueue kernel.Write[*gfx.OpQueue]
		return func(access kernel.ResourceAccess) {
				queue = access.GetWrite[*OpQueue]()
				gfxQueue = access.GetWrite[*gfx.OpQueue]()
			}, func(kernel.Kernel, app.UpdateEvent) error {
				p.record(queue.Get(), gfxQueue.Get())
				return nil
			}
	}).Before[UpdateEventHandler]()
	registrar.HandleCommand[inspectCmd](inspectCmdImpl)
	registrar.HandleCommand[lookupProbeCmd](lookupProbeCmdImpl)
	return nil
}

func inspectCmdImpl() (kernel.Lock, kernel.Execute[inspectRequest, inspectResponse]) {
	var queue kernel.Write[*OpQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*OpQueue]()
		}, func(_ kernel.Kernel, req inspectRequest) (inspectResponse, error) {
			req.run(queue.Get())
			return inspectResponse{}, nil
		}
}

func lookupProbeCmdImpl() (kernel.Lock, kernel.Execute[lookupProbeRequest, lookupProbeResponse]) {
	var lookup kernel.Write[*Lookup]
	var filesystem kernel.Read[storage.FileSystem]
	return func(access kernel.ResourceAccess) {
			lookup = access.GetWrite[*Lookup]()
			filesystem = access.GetRead[storage.FileSystem]()
		}, func(k kernel.Kernel, req lookupProbeRequest) (lookupProbeResponse, error) {
			if req.files != nil {
				req.files(filesystem.Get())
			}
			if req.run != nil {
				req.run(NewLookupAccess(k, lookup.Get()))
			}
			return lookupProbeResponse{}, nil
		}
}

// errorSink collects reports under a mutex. A model load reports from its own
// goroutine rather than from the flush the test drives, which is the whole
// point of "an error can outlive the draw call that caused it" - so the sink
// that reads them has to be safe to read from a different one.
type errorSink struct {
	mu   sync.Mutex
	errs []error
}

func (s *errorSink) add(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, err)
}

func (s *errorSink) snapshot() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]error(nil), s.errs...)
}

type harness struct {
	kernel   kernel.Executioner
	backend  *testBackend
	reported *[]error
	sink     *errorSink
}

// errors reports what the engine has reported so far, safely to read while a
// load goroutine may still be running.
func (h *harness) errors() []error { return h.sink.snapshot() }

func newHarness(t testing.TB, record func(*OpQueue)) *harness {
	t.Helper()
	var reported []error
	return newHarnessWithErrors(t, record, &reported)
}

// newHarnessWithGfx is newHarness for a recorder that also allocates from the
// gfx queue, the way an app rendering a camera into a temporary target does.
func newHarnessWithGfx(t testing.TB, record func(*OpQueue, *gfx.OpQueue)) *harness {
	t.Helper()
	var reported []error
	return newHarnessRecording(t, record, &reported)
}

func newHarnessWithErrors(t testing.TB, record func(*OpQueue), reported *[]error) *harness {
	t.Helper()
	return newHarnessRecording(t, func(q *OpQueue, _ *gfx.OpQueue) { record(q) }, reported)
}

// newHarnessWithFiles is newHarness over a filesystem holding the given
// files, which is how a model test hands scene a glTF file to load without
// this package growing a testdata directory.
func newHarnessWithFiles(t testing.TB, files fstest.MapFS, record func(*OpQueue)) *harness {
	t.Helper()
	var reported []error
	return newHarnessOver(t, files, func(q *OpQueue, _ *gfx.OpQueue) { record(q) }, &reported)
}

func newHarnessRecording(t testing.TB, record func(*OpQueue, *gfx.OpQueue), reported *[]error) *harness {
	return newHarnessOver(t, fstest.MapFS{}, record, reported)
}

func newHarnessOver(
	t testing.TB, files fstest.MapFS,
	record func(*OpQueue, *gfx.OpQueue), reported *[]error,
) *harness {
	t.Helper()
	backend := &testBackend{}
	sink := &errorSink{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	configs := map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("scene-test").WithReadFS("test", 10, fs.FS(files)),
		Name:         DefaultConfig(),
	}
	engine := kernel.New(configs).
		Handler(func(err error) bool { sink.add(err); *reported = append(*reported, err); return false }).
		WithPlugins(storage.New(), gfx.New(), New(), recordPlugin{record: record})
	go engine.Run(ctx)
	<-engine.Ready()
	k := engine.Executioner()
	k.PublishEvent(app.InitEvent{}).Wait()
	k.ExecuteCommand[gfx.SetBackendCmd](gfx.SetBackendRequest{Backend: backend})
	k.ExecuteCommand[app.SetViewportCmd](app.SetViewportRequest{
		Width: 800, Height: 600, FramebufferWidth: 1600, FramebufferHeight: 1200,
	})
	return &harness{kernel: k, backend: backend, reported: reported, sink: sink}
}

func (h *harness) frame() {
	h.kernel.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()
	h.kernel.PublishEvent(app.RenderEvent{}).Wait()
}

// frameUntil runs frames until ready, which is how a test waits on an
// asynchronous load: the parse and the upload are two commands on their own
// goroutines, so residency lands some frames after the draw that asked for it.
func (h *harness) frameUntil(t testing.TB, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.frame()
		if ready() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// inspect runs fn inside a handler holding scene's OpQueue write lock.
func (h *harness) inspect(fn func(*OpQueue)) {
	h.kernel.ExecuteCommand[inspectCmd](inspectRequest{run: fn})
}

// passes reads the flush result back out of scene's queue.
func (h *harness) passes() []PassView {
	var out []PassView
	h.inspect(func(q *OpQueue) { out = q.Passes(nil) })
	return out
}

func (h *harness) ops() []Op {
	var out []Op
	h.inspect(func(q *OpQueue) { out = q.Ops(nil) })
	return out
}

// readFile reads one path out of the engine's storage, which is how a test
// checks that a plugin's builtin mount is in place.
func (h *harness) readFile(t testing.TB, path string) []byte {
	t.Helper()
	var data []byte
	var err error
	h.kernel.ExecuteCommand[lookupProbeCmd](lookupProbeRequest{files: func(files storage.FileSystem) {
		data, err = fs.ReadFile(files, path)
	}})
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return data
}
