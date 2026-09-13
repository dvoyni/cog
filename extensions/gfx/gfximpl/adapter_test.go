package gfximpl

import (
	"errors"
	"io/fs"
	"reflect"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/storage"
	"github.com/dvoyni/cog/extensions/storage/storageimpl"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// testAdapter is the Backend adapter the gfx tests compose. It is provided at
// registration, as a driver's is, and holds no backend until a test attaches
// one - which is the shape of a driver whose device arrives after the engine
// has started, and what lets a test render a frame before it is ready.
type testAdapter struct {
	backend atomic.Pointer[gfx.Backend]
}

// attachBackendCmd hands the adapter the fake a test renders through.
type attachBackendCmd kernel.Command[attachBackendRequest, attachBackendResponse]
type attachBackendRequest struct{ Backend gfx.Backend }
type attachBackendResponse struct{}

func (a *testAdapter) attachBackendCmdImpl() (kernel.Lock, kernel.Execute[attachBackendRequest, attachBackendResponse]) {
	return nil, func(_ kernel.Kernel, request attachBackendRequest) (attachBackendResponse, error) {
		a.backend.Store(&request.Backend)
		return attachBackendResponse{}, nil
	}
}

func (a *testAdapter) get() gfx.Backend {
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

func (a *testAdapter) NewTexture() gfx.TextureID {
	if backend := a.get(); backend != nil {
		return backend.NewTexture()
	}
	return gfx.TextureID(detachedIDs.Add(1))
}

func (a *testAdapter) NewBuffer() gfx.BufferID {
	if backend := a.get(); backend != nil {
		return backend.NewBuffer()
	}
	return gfx.BufferID(detachedIDs.Add(1))
}

func (a *testAdapter) NewSampler(desc gfx.SamplerDesc) (gfx.SamplerID, error) {
	return a.get().NewSampler(desc)
}
func (a *testAdapter) FreeSampler(id gfx.SamplerID) { a.get().FreeSampler(id) }
func (a *testAdapter) NewShader(desc gfx.ShaderDesc) (gfx.ShaderID, error) {
	return a.get().NewShader(desc)
}
func (a *testAdapter) FreeShader(id gfx.ShaderID)                    { a.get().FreeShader(id) }
func (a *testAdapter) ShaderLayout(id gfx.ShaderID) gfx.ShaderLayout { return a.get().ShaderLayout(id) }
func (a *testAdapter) FreePipeline(id gfx.PipelineID)                { a.get().FreePipeline(id) }
func (a *testAdapter) Limits() gfx.Limits                            { return a.get().Limits() }
func (a *testAdapter) Execute(queue *gfx.GpuQueue)                   { a.get().Execute(queue) }
func (a *testAdapter) TakeCapture() (gfx.GpuCapture, bool)           { return a.get().TakeCapture() }
func (a *testAdapter) ScreenFramebuffer() (gfx.TextureViewID, int, int) {
	return a.get().ScreenFramebuffer()
}
func (a *testAdapter) NewPipeline(desc gfx.PipelineDesc) (gfx.PipelineID, error) {
	return a.get().NewPipeline(desc)
}
func (a *testAdapter) TextureView(texture gfx.TextureID, mip, layer int) gfx.TextureViewID {
	return a.get().TextureView(texture, mip, layer)
}

// noFiles is the filesystem of a translation that loads nothing.
func noFiles() fs.FS { return fstest.MapFS{} }

// gfx is a Port: it requires exactly one Backend adapter, so a composition
// that has none fails before anything starts, rather than rendering nothing
// and reporting it a frame later.
func TestACompositionWithoutABackendAdapterFails(t *testing.T) {
	var reported []error
	kernel.New(map[kernel.PluginName]any{
		storage.Name: storageimpl.DefaultConfig(),
	}).Handler(func(err error) bool {
		reported = append(reported, err)
		return true
	}).WithPlugins(storageimpl.New(), permanentAdapter{}, newPlugin())

	var missing kernel.ErrMissingAdapter
	if !errors.As(errors.Join(reported...), &missing) {
		t.Fatalf("composition reported %v, want ErrMissingAdapter", reported)
	}
	if missing.Port != gfx.Name || missing.Interface != reflect.TypeFor[gfx.Backend]() {
		t.Fatalf("missing adapter = %+v, want gfx's Backend", missing)
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
	w.Draw(triangle(), testMaterial())
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(reported) != 1 || !errors.Is(reported[0], gfx.ErrBackendNotReady{}) {
		t.Fatalf("two frames before the backend was ready reported %v, want one ErrBackendNotReady", reported)
	}

	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	w = recordList(t, k)
	w.Draw(triangle(), testMaterial())
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.execCount != 1 {
		t.Fatalf("the backend executed %d frames once ready, want 1", backend.execCount)
	}
	if len(reported) != 1 {
		t.Fatalf("the ready frame reported %v", reported[1:])
	}
}
