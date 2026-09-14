package internal

import (
	"context"
	"errors"
	"io/fs"
	"sync"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// flattenShader resolves one shader through gfx's preprocessor, with
// filesystem as storage's only read mount under mount. The preprocessor is
// internal to gfx, so the module is read where it leaves gfx: one draw with the
// shader, and the source the backend is handed for it. A shader the
// preprocessor refuses is the error gfx reports for it.
func flattenShader(t testing.TB, mount storage.MountId, filesystem fs.FS, shader gfx.ShaderDescr) (string, error) {
	t.Helper()
	backend := &flattenBackend{}
	var (
		mu       sync.Mutex
		refusals []error
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine := kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.Config{}.WithReadFS(mount, 0, filesystem),
	}).Handler(func(err error) bool {
		var source gfx.ErrShaderSource
		var missing gfx.ErrShaderNotFound
		if errors.As(err, &source) || errors.As(err, &missing) {
			mu.Lock()
			refusals = append(refusals, err)
			mu.Unlock()
		}
		return true
	}).WithPlugins(storageplugin.New(), permanentAdapter{}, appplugin.New(), mainLoopAdapter{}, gfxplugin.New(), flattenRecorder{backend: backend, shader: shader})
	go engine.Run(ctx)
	<-engine.Ready()
	k := engine.Executioner()
	k.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{
		Width: 16, Height: 16, FramebufferWidth: 16, FramebufferHeight: 16,
	})
	k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()
	k.PublishEvent(app.RenderEvent{}).Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(refusals) > 0 {
		return "", refusals[0]
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.modules) != 1 {
		t.Fatalf("the draw handed the backend %d shader modules, want 1", len(backend.modules))
	}
	return backend.modules[0], nil
}

// flattenRecorder provides flattenShader's backend and records its one draw.
type flattenRecorder struct {
	backend *flattenBackend
	shader  gfx.ShaderDescr
}

// flattenGfxBackend is the Adapter flattenRecorder fills gfx's backend Port as.
type flattenGfxBackend kernel.Adapter[gfx.BackendPort]

type flattenOnUpdate kernel.Subscription[app.UpdateEvent]

func (flattenRecorder) Name() kernel.PluginName           { return "flatten-test-recorder" }
func (flattenRecorder) Dependencies() []kernel.PluginName { return []kernel.PluginName{gfx.Name} }

func (r flattenRecorder) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[flattenGfxBackend](gfx.Backend(r.backend))
	registrar.Subscribe[flattenOnUpdate](func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
		var queue kernel.Write[*gfx.OpQueue]
		return func(access kernel.ResourceAccess) {
				queue = access.GetWrite[*gfx.OpQueue]()
			}, func(kernel.Kernel, app.UpdateEvent) error {
				q := queue.Get()
				q.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthNone()})
				vertices := gfx.BufferWithBytes(make([]byte, 12), true)
				q.Draw(gfx.Mesh(vertices, gfx.TopologyTriangleList, gfx.Attr(0, gfx.Float32x3)), gfx.Material(r.shader))
				return nil
			}
	})
	return nil
}

// flattenBackend is the least backend a draw reaches: every handle is a
// counter, nothing is encoded, and every shader module it is handed is kept.
type flattenBackend struct {
	mu      sync.Mutex
	next    uint32
	modules []string
}

func (b *flattenBackend) id() gfx.ResourceID {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	return gfx.ResourceID(b.next)
}

func (b *flattenBackend) NewShader(desc gfx.ShaderDesc) (gfx.ShaderID, error) {
	b.mu.Lock()
	b.modules = append(b.modules, string(desc.Code))
	b.mu.Unlock()
	return gfx.ShaderID(b.id()), nil
}

func (b *flattenBackend) Ready() bool               { return true }
func (b *flattenBackend) NewTexture() gfx.TextureID { return gfx.TextureID(b.id()) }
func (b *flattenBackend) NewBuffer() gfx.BufferID   { return gfx.BufferID(b.id()) }
func (b *flattenBackend) NewSampler(gfx.SamplerDesc) (gfx.SamplerID, error) {
	return gfx.SamplerID(b.id()), nil
}
func (b *flattenBackend) FreeSampler(gfx.SamplerID)                  {}
func (b *flattenBackend) FreeShader(gfx.ShaderID)                    {}
func (b *flattenBackend) ShaderLayout(gfx.ShaderID) gfx.ShaderLayout { return gfx.ShaderLayout{} }
func (b *flattenBackend) FreePipeline(gfx.PipelineID)                {}
func (b *flattenBackend) Limits() gfx.Limits                         { return gfx.DefaultLimits() }
func (b *flattenBackend) Execute(*gfx.Queue)                         {}
func (b *flattenBackend) TakeCapture() (gfx.Capture, bool)           { return gfx.Capture{}, false }
func (b *flattenBackend) TextureView(gfx.TextureID, int, int) gfx.TextureViewID {
	return gfx.TextureViewID(b.id())
}
func (b *flattenBackend) NewPipeline(gfx.PipelineDesc) (gfx.PipelineID, error) {
	return gfx.PipelineID(b.id()), nil
}
func (b *flattenBackend) ScreenFramebuffer() (gfx.TextureViewID, int, int) {
	return gfx.TextureViewID(1), 16, 16
}
