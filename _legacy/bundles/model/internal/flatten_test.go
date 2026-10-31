package internal

import (
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
// internal to gfx, so the module is read where it leaves gfx: the shader is
// compiled through CompileShaderCmd and uploaded, and the source the backend
// is handed when the upload is replayed is the flattened module. A shader the
// compile refuses is the error the compile returns.
func flattenShader(t testing.TB, mount storage.MountId, filesystem fs.FS, shader gfx.ShaderDescr) (string, error) {
	t.Helper()
	backend := &flattenBackend{}
	recorder := &flattenRecorder{backend: backend, shader: shader}
	engine := kernel.New(nil).Handler(func(err error) error {
		t.Errorf("unexpected kernel error: %v", err)
		return err
	}).WithPlugins(storageplugin.New(), permanentAdapter{}, readMountAdapter{storage.ReadMount{Id: mount, Priority: 0, FS: filesystem}}, appplugin.New(), mainLoopAdapter{}, gfxplugin.New(), recorder)
	go engine.Run()
	t.Cleanup(engine.Quit)
	<-engine.Ready()
	k := engine.Executioner()
	k.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{
		Width: 16, Height: 16, FramebufferWidth: 16, FramebufferHeight: 16,
	})
	k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()
	k.PublishEvent(app.RenderEvent{}).Wait()

	recorder.mu.Lock()
	err := recorder.err
	recorder.mu.Unlock()
	if err != nil {
		return "", err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.modules) != 1 {
		t.Fatalf("the upload handed the backend %d shader modules, want 1", len(backend.modules))
	}
	return backend.modules[0], nil
}

// flattenRecorder provides flattenShader's backend, and compiles and uploads
// its one shader on the first tick.
type flattenRecorder struct {
	backend *flattenBackend
	shader  gfx.ShaderDescr
	mu      sync.Mutex
	err     error
}

// flattenGfxBackend is the Adapter flattenRecorder fills gfx's backend Port as.
type flattenGfxBackend kernel.Adapter[gfx.BackendPort]

type flattenOnUpdate kernel.Subscription[app.UpdateEvent]

func (*flattenRecorder) Name() kernel.PluginName           { return "flatten-test-recorder" }
func (*flattenRecorder) Dependencies() []kernel.PluginName { return []kernel.PluginName{gfx.Name} }

func (r *flattenRecorder) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[flattenGfxBackend](gfx.Backend(r.backend))
	registrar.Subscribe[flattenOnUpdate](func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
		var (
			resources kernel.Write[*gfx.ResourceQueue]
			files     kernel.Read[storage.FileSystem]
			compile   gfx.ShaderCompiler
		)
		return func(access kernel.ResourceAccess) {
				resources = access.GetWrite[*gfx.ResourceQueue]()
				files = access.GetRead[storage.FileSystem]()
				compile = access.Uses[gfx.CompileShaderCmd]()
			}, func(k kernel.Kernel, _ app.UpdateEvent) {
				compiled := compile(k, gfx.CompileShaderRequest{FS: fs.FS(files.Get()), Descr: r.shader})
				if compiled.Err != nil {
					r.mu.Lock()
					r.err = compiled.Err
					r.mu.Unlock()
					return
				}
				q := resources.Get()
				q.UploadProgram(k, q.NewShader(), compiled.Program)
			}
	})
	return nil
}

// flattenBackend is the least backend an upload reaches: every handle is a
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

func (b *flattenBackend) CreateShader(_ gfx.ShaderID, desc gfx.ShaderDesc) error {
	b.mu.Lock()
	b.modules = append(b.modules, string(desc.Code))
	b.mu.Unlock()
	return nil
}

func (b *flattenBackend) Ready() bool               { return true }
func (b *flattenBackend) NewTexture() gfx.TextureID { return gfx.TextureID(b.id()) }
func (b *flattenBackend) NewBuffer() gfx.BufferID   { return gfx.BufferID(b.id()) }
func (b *flattenBackend) NewSampler(gfx.SamplerDesc) (gfx.SamplerID, error) {
	return gfx.SamplerID(b.id()), nil
}
func (b *flattenBackend) FreeSampler(gfx.SamplerID)   {}
func (b *flattenBackend) FreeShader(gfx.ShaderID)     {}
func (b *flattenBackend) ReserveShader() gfx.ShaderID { return gfx.ShaderID(b.id()) }
func (b *flattenBackend) ReflectShader([]byte) (gfx.ShaderLayout, error) {
	return gfx.ShaderLayout{}, nil
}
func (b *flattenBackend) FreePipeline(gfx.PipelineID)      {}
func (b *flattenBackend) Limits() gfx.PipelineLimits       { return gfx.DefaultLimits() }
func (b *flattenBackend) Execute(*gfx.Queue)               {}
func (b *flattenBackend) TakeCapture() (gfx.Capture, bool) { return gfx.Capture{}, false }

// TextureFormat answers for no texture: this double keeps no descriptors, and
// gfx falls back to the frame buffer's format for a target it cannot place.
func (b *flattenBackend) TextureFormat(gfx.TextureID) (gfx.TextureFormat, bool) {
	return 0, false
}

func (b *flattenBackend) NewPipeline(gfx.PipelineDesc) (gfx.PipelineID, error) {
	return gfx.PipelineID(b.id()), nil
}
func (b *flattenBackend) ScreenFramebuffer() (gfx.TextureViewID, int, int) {
	return gfx.TextureViewID(1), 16, 16
}
