package internal

import (
	"errors"
	"io/fs"
	"reflect"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// testAdapter is the Backend adapter the gfx tests compose. It is provided at
// registration, as a driver's is, and holds no backend until a test attaches
// one - which is the shape of a driver whose device arrives after the engine
// has started, and what lets a test render a frame before it is ready.
type testAdapter struct {
	backend atomic.Pointer[Backend]
}

// attachBackendCmd hands the adapter the fake a test renders through.
type attachBackendCmd kernel.Command[attachBackendRequest, attachBackendResponse]
type attachBackendRequest struct{ Backend Backend }
type attachBackendResponse struct{}

func (a *testAdapter) attachBackendCmdImpl() (kernel.Lock, kernel.Execute[attachBackendRequest, attachBackendResponse]) {
	return nil, func(_ kernel.Kernel, request attachBackendRequest) attachBackendResponse {
		a.backend.Store(&request.Backend)
		return attachBackendResponse{}
	}
}

func (a *testAdapter) get() Backend {
	if backend := a.backend.Load(); backend != nil {
		return *backend
	}
	return nil
}

func (a *testAdapter) Ready() bool {
	backend := a.get()
	return backend != nil && backend.Ready()
}

// ids keep counting before a backend is attached, so recording can reserve
// resource ids the moment the engine starts, as a driver's stable backend does.
var detachedIDs atomic.Uint32

func (a *testAdapter) NewTexture() types.TextureID {
	if backend := a.get(); backend != nil {
		return backend.NewTexture()
	}
	return types.TextureID(detachedIDs.Add(1))
}

func (a *testAdapter) NewBuffer() types.BufferID {
	if backend := a.get(); backend != nil {
		return backend.NewBuffer()
	}
	return types.BufferID(detachedIDs.Add(1))
}

func (a *testAdapter) NewSampler(desc types.SamplerDesc) (types.SamplerID, error) {
	return a.get().NewSampler(desc)
}
func (a *testAdapter) FreeSampler(id types.SamplerID) { a.get().FreeSampler(id) }
func (a *testAdapter) NewShader(desc shader.ShaderDesc) (types.ShaderID, error) {
	return a.get().NewShader(desc)
}
func (a *testAdapter) FreeShader(id types.ShaderID) { a.get().FreeShader(id) }
func (a *testAdapter) ShaderLayout(id types.ShaderID) shader.ShaderLayout {
	return a.get().ShaderLayout(id)
}
func (a *testAdapter) FreePipeline(id types.PipelineID) { a.get().FreePipeline(id) }
func (a *testAdapter) Limits() types.Limits             { return a.get().Limits() }
func (a *testAdapter) Execute(queue *Queue)             { a.get().Execute(queue) }
func (a *testAdapter) TakeCapture() (Capture, bool)     { return a.get().TakeCapture() }
func (a *testAdapter) ScreenFramebuffer() (types.TextureViewID, int, int) {
	return a.get().ScreenFramebuffer()
}
func (a *testAdapter) NewPipeline(desc PipelineDesc) (types.PipelineID, error) {
	return a.get().NewPipeline(desc)
}
func (a *testAdapter) TextureFormat(texture types.TextureID) (descriptors.TextureFormat, bool) {
	return a.get().TextureFormat(texture)
}
func (a *testAdapter) TextureView(texture types.TextureID, mip, layer int) types.TextureViewID {
	return a.get().TextureView(texture, mip, layer)
}

// emptyFS is built once rather than per call, because translate now materialises
// the filesystem at the top of every frame: a fresh fstest.MapFS{} there would
// put its 48-byte map in every benchmark result and measure the fixture instead
// of the translation. The frame's real box is storage.FileSystem's 32 bytes, and
// it shows where a real engine pays it - canvas's flush benchmarks.
var emptyFS = fstest.MapFS{}

// noFiles is the filesystem of a translation that loads nothing.
func noFiles() fs.FS { return emptyFS }

// gfx is a Slot: it requires exactly one Backend adapter, so a composition
// that has none fails before anything starts, rather than rendering nothing
// and reporting it a frame later.
func TestACompositionWithoutABackendAdapterFails(t *testing.T) {
	var reported []error
	kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.Config{},
	}).Handler(func(err error) error {
		reported = append(reported, err)
		return err
	}).WithPlugins(storageplugin.New(), permanentAdapter{}, appplugin.New(), mainLoopAdapter{}, newPlugin())

	var missing kernel.ErrMissingAdapter
	if !errors.As(errors.Join(reported...), &missing) {
		t.Fatalf("composition reported %v, want ErrMissingAdapter", reported)
	}
	if missing.Plugin != Name || missing.Port != reflect.TypeFor[BackendPort]() {
		t.Fatalf("missing adapter = %+v, want gfx's BackendPort", missing)
	}
}

// A driver provides its Backend at registration, before its device exists. A
// frame rendered before the backend is ready is skipped: nothing is
// translated or executed, the condition is reported once rather than at frame
// rate, and the first frame after the backend becomes ready renders.
func TestAFrameBeforeTheBackendIsReadyIsSkipped(t *testing.T) {
	var reported []error
	k := newTestKernelWithErrors(t, newPlugin(), func(err error) { reported = append(reported, err) })
	backend := &fakeBackend{}

	w := recordList(t, k)
	w.Draw(triangle(), testMaterial(), 1, 0)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(reported) != 1 || !errors.Is(reported[0], types.ErrBackendNotReady{}) {
		t.Fatalf("two frames before the backend was ready reported %v, want one ErrBackendNotReady", reported)
	}

	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	w = recordList(t, k)
	w.Draw(triangle(), testMaterial(), 1, 0)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.execCount != 1 {
		t.Fatalf("the backend executed %d frames once ready, want 1", backend.execCount)
	}
	if len(reported) != 1 {
		t.Fatalf("the ready frame reported %v", reported[1:])
	}
}
