package internal

import (
	"io/fs"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"

	"github.com/dvoyni/cog/slots/storage"
)

// readList is the read side of the triple buffer: the queue the render
// handler translates.
type readList struct{ *OpQueue }

// readyList is the internal third buffer of the triple buffer: it parks the
// latest completed OpQueue between Present and Acquire/Consume.
type readyList struct {
	queue   *OpQueue
	pending bool
}

// backendNotReadyKey names the one condition gfx reports once per engine: no
// backend ever installed. It is its own type rather than a bare struct{}
// because the kernel's report-once table is keyed across plugins by the boxed
// key's dynamic type, and every singleton condition sharing struct{} would be
// one condition.
type backendNotReadyKey struct{}

// plugin implements renderer v2: a triple-buffered OpQueue pipeline plus a
// translator that turns high-level draw commands into a backend-agnostic gfx.Queue
// stream. It owns the translator (render-thread-only caches and dynamic buffers);
// the three queues live in kernel resources.
type plugin struct {
	translator *translator
	// backend is the bound Backend adapter, valid from Start onwards.
	backend kernel.RequiredAdapter[Backend]
	// observer watches the frames from outside gfx. See FrameObserver.
	observer FrameObserver
}

// New creates the gfx plugin, watched by observer.
func New(observer FrameObserver) kernel.Plugin {
	p := newPlugin()
	p.observer = observer
	return p
}

// newPlugin creates the gfx plugin as its own type, unobserved, for the tests
// that reach its translator.
func newPlugin() *plugin { return &plugin{translator: newTranslator(), observer: noObserver{}} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins gfx requires: storage, from which it loads
// shader and texture resources, and app, whose events drive it and whose
// TimeCmd the observer's capabilities dispatch.
func (p *plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{app.Name, storage.Name}
}

// Register requires the Backend adapter, registers the three command-list
// buffers, the Present/Acquire/Consume commands and the end-of-tick present
// subscription on app.UpdateEvent, and lets the observer register its own.
func (p *plugin) Register(registrar *kernel.Registrar, _ any) error {
	p.backend = registrar.RequireAdapter[BackendPort]()
	ids := func() IDMinter { return p.backend.Get() }
	// The three frame queues share the ResourceQueue's registry of sets, which
	// is how SetDrawParams reads a set without the ResourceQueue's lock.
	resources := NewResourceQueue(ids)
	sets := resources.drawParams.registry
	registrar.InitResource(newOpQueue(ids, sets))
	registrar.InitResource(&readList{OpQueue: newOpQueue(ids, sets)})
	registrar.InitResource(&readyList{queue: newOpQueue(ids, sets)})
	registrar.InitResource(resources)
	registrar.InitResource(&types.Viewport{})
	registrar.InitResource(&types.DesiredViewport{})
	registrar.HandleCommand[PresentCmd](p.presentCmdImpl)
	registrar.HandleCommand[AcquireCmd](p.acquireCmdImpl)
	registrar.HandleCommand[ReleaseCachedResourceCmd](p.releaseCachedResourceCmdImpl)
	registrar.HandleCommand[FreeCachedResourcesCmd](p.freeCachedResourcesCmdImpl)
	registrar.HandleCommand[CompileShaderCmd](p.compileShaderCmdImpl)
	registrar.HandleCommand[SetViewportCmd](setViewportCmdImpl)
	registrar.HandleCommand[SetDesiredViewportCmd](setDesiredViewportCmdImpl)
	registrar.Subscribe[PresentOnUpdate](p.presentOnUpdate).Last()
	registrar.Subscribe[RenderOnRender](p.renderOnRender)
	p.observer.Register(registrar)
	return nil
}

// Stop lets the observer complete whatever the engine walked away from. By
// Stop the scheduler has stopped and grants no locks, which is also why
// nothing else can be touching the observer's state: the host loop has
// returned and every handler is done.
func (p *plugin) Stop(kernel.Executioner) error {
	p.observer.Abandon()
	return nil
}

// presentOnUpdate swaps the recorded queue into the ready slot, and hands the
// observer the queue immediately before doing so.
//
// The observer is called here rather than from a subscriber of its own because
// this is the only place the ordering a frame snapshot needs can be expressed. It has to run
// after canvas has flushed into the queue and before present swaps it away -
// but canvas.FlushOnUpdate is a type gfx cannot name, since canvas
// imports gfx and not the other way round. A second Last subscriber would
// carry no order relative to canvas's flush at all, and would see the frame
// half recorded half the time. Present already sits exactly where the snapshot
// belongs: canvas orders its own flush Before gfx's present, so by the time
// this runs the frame is complete, and it has not been swapped away yet.
//
// The Read on ResourceQueue is what widens this handler's lock set, and it is
// not optional: most of the resource traffic a snapshot is asked about -
// durable bakes, allocations, uploads, releases - is recorded there and never
// reaches the frame queue. Read conflicts only with a writer, and every writer
// of it in a tick already orders itself before present.
func (p *plugin) presentOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var write kernel.Write[*OpQueue]
	var ready kernel.Write[*readyList]
	var resources kernel.Read[*ResourceQueue]
	return func(access kernel.ResourceAccess) {
			write = access.GetWrite[*OpQueue]()
			ready = access.GetWrite[*readyList]()
			resources = access.GetRead[*ResourceQueue]()
		}, func(_ kernel.Kernel, event app.UpdateEvent) {
			p.observer.Presenting(write.Get(), resources.Get(), event.Tick)
			present(write, ready)
		}
}

// renderOnRender is the app.RenderEvent handler: it acquires the latest list,
// translates it against the installed Backend, and executes the resulting op
// stream into the backend's screen framebuffer. It runs on the MainLoop's render
// thread (where app publishes app.RenderEvent).
func (p *plugin) renderOnRender() (kernel.Lock, kernel.Observe[app.RenderEvent]) {
	var read kernel.Write[*readList]
	var ready kernel.Write[*readyList]
	var resources kernel.Write[*ResourceQueue]
	var files func() fs.FS
	return func(access kernel.ResourceAccess) {
			read = access.GetWrite[*readList]()
			ready = access.GetWrite[*readyList]()
			resources = access.GetWrite[*ResourceQueue]()
			files = readFiles(access)
		}, func(k kernel.Kernel, _ app.RenderEvent) {
			acquire(read, ready)
			list := read.Get()
			backend := p.backend.Get()
			if !backend.Ready() {
				k.ReportErrorOnce(backendNotReadyKey{}, types.ErrBackendNotReady{})
				return
			}
			queue := resources.Get()
			capture, capturing := p.observer.Capturing()
			ops, err := p.translator.translate(
				k, list.OpQueue, queue, backend, files, capture, capturing)
			if err != nil {
				k.ReportError(err)
			}
			backend.Execute(ops)
			// Drained before this frame's own copy is promoted, because what a
			// backend has ready now is the copy the previous frame encoded: its
			// map resolved on the submit Execute just made.
			if done, ready := backend.TakeCapture(); ready {
				p.observer.Captured(done)
			}
			if capturing {
				p.observer.Encoded()
			}
			queue.reset()
		}
}

// present swaps the recorded OpQueue into the ready slot and installs the queue
// previously parked there as the reset writable resource (latest-wins).
func present(write kernel.Write[*OpQueue], ready kernel.Write[*readyList]) {
	rd := ready.Get()
	recycled := rd.queue
	recycled.reset()
	rd.queue = write.Get()
	write.Set(recycled)
	rd.pending = true
}

// acquire advances the read list to the latest completed list if one is pending,
// recycling the previous read list into the ready slot.
func acquire(read kernel.Write[*readList], ready kernel.Write[*readyList]) bool {
	rd := ready.Get()
	if !rd.pending {
		return false
	}
	current := read.Get()
	current.OpQueue, rd.queue = rd.queue, current.OpQueue
	rd.pending = false
	return true
}

// readFiles declares a read lock on storage's FileSystem inside a handler's
// lock, and returns what reads it.
//
// The box it returns is not free - handing storage.FileSystem out as an fs.FS
// costs 32 bytes, measured - which is why translate calls this exactly once, at
// the top of the frame, and threads the result down. It used to be called behind
// each cache's own miss probe, and a frame that loaded nothing boxed nothing;
// the caches now want the filesystem in hand before every lookup, so leaving it
// there would have moved the cost from once a miss to once a hit.
func readFiles(access kernel.ResourceAccess) func() fs.FS {
	filesystem := access.GetRead[storage.FileSystem]()
	return func() fs.FS { return filesystem.Get() }
}
