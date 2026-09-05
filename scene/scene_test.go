package scene

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

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
func (b *testBackend) ShaderLayout(gfx.ShaderID) gfx.ShaderLayout { return gfx.ShaderLayout{} }
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
func (b *testBackend) EndPass(gfx.RenderPass)                                               {}
func (b *testBackend) Present()                                                             { b.presents++ }
func (b *testBackend) BakeBuffer(gfx.BufferID, gfx.BufferKind, int, []byte)                 {}
func (b *testBackend) BakeTexture(gfx.TextureID, int, int, gfx.TextureFormat, []byte, bool) {}
func (b *testBackend) AllocateTexture(gfx.TextureID, gfx.TextureDesc)                       {}
func (b *testBackend) UpdateTexture(gfx.TextureID, int, gfx.Region, []byte)                 {}
func (b *testBackend) SetPipeline(gfx.PipelineID)                                           {}
func (b *testBackend) SetParams([]byte)                                                     {}
func (b *testBackend) SetTexture(gfx.TextureID, int, int)                                   {}
func (b *testBackend) SetSampler(gfx.SamplerID, int, int)                                   {}
func (b *testBackend) SetVertexBuffer(gfx.BufferID, int)                                    {}
func (b *testBackend) SetIndexBuffer(gfx.BufferID, int)                                     {}
func (b *testBackend) SetBuffer(int, int, gfx.BufferID, int, int)                           {}
func (b *testBackend) Draw(int, int, int, int, bool)                                        {}
func (b *testBackend) ReleaseBuffer(gfx.BufferID)                                           {}
func (b *testBackend) ReleaseTexture(gfx.TextureID)                                         {}

// recordPlugin is the gameplay side of the harness: a separate plugin that
// locks scene's OpQueue, exactly as a real recorder does.
type recordPlugin struct{ record func(*OpQueue) }
type recordHandler kernel.Subscription[app.UpdateEvent]

// inspectCmd runs a callback inside a handler holding scene's OpQueue, so a
// test reads Ops and Passes the way a real caller would.
type inspectCmd kernel.Command[inspectRequest, inspectResponse]
type inspectRequest struct{ run func(*OpQueue) }
type inspectResponse struct{}

// lookupProbeCmd runs a callback with a valid scoped LookupAccess.
type lookupProbeCmd kernel.Command[lookupProbeRequest, lookupProbeResponse]
type lookupProbeRequest struct{ run func(LookupAccess) }
type lookupProbeResponse struct{}

func (p recordPlugin) Name() kernel.PluginName { return "scene-test-recorder" }
func (p recordPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{Name, storage.Name}
}

func (p recordPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[recordHandler](func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
		var queue kernel.Write[*OpQueue]
		return func(access kernel.ResourceAccess) {
				queue = access.GetWrite[*OpQueue]()
			}, func(kernel.Kernel, app.UpdateEvent) error {
				p.record(queue.Get())
				return nil
			}
	})
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
			req.run(NewLookupAccess(k, lookup.Get(), filesystem.Get()))
			return lookupProbeResponse{}, nil
		}
}

type harness struct {
	kernel   kernel.Executioner
	backend  *testBackend
	reported *[]error
}

func newHarness(t testing.TB, record func(*OpQueue)) *harness {
	t.Helper()
	var reported []error
	return newHarnessWithErrors(t, record, &reported)
}

func newHarnessWithErrors(t testing.TB, record func(*OpQueue), reported *[]error) *harness {
	t.Helper()
	backend := &testBackend{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	configs := map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("scene-test").WithReadFS("test", 10, fs.FS(fstest.MapFS{})),
		Name:         DefaultConfig(),
	}
	engine := kernel.New(configs).
		Handler(func(err error) bool { *reported = append(*reported, err); return false }).
		WithPlugins(storage.New(), gfx.New(), New(), recordPlugin{record: record})
	go engine.Run(ctx)
	<-engine.Ready()
	k := engine.Executioner()
	k.PublishEvent(app.InitEvent{}).Wait()
	k.ExecuteCommand[gfx.SetBackendCmd](gfx.SetBackendRequest{Backend: backend})
	k.ExecuteCommand[app.SetViewportCmd](app.SetViewportRequest{
		Width: 800, Height: 600, FramebufferWidth: 1600, FramebufferHeight: 1200,
	})
	return &harness{kernel: k, backend: backend, reported: reported}
}

func (h *harness) frame() {
	h.kernel.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()
	h.kernel.PublishEvent(app.RenderEvent{}).Wait()
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
