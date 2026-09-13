package gfximpl

import (
	"io/fs"

	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/internal"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// captureOnUpdate is the subscription type of the plugin's
// start-of-tick capture handler, admitCapture, on app.UpdateEvent. It is ordered First - ahead
// of every other subscriber, not merely in the first phase - because that is
// what makes "a tick that began after the request" decidable: a capture armed
// while a tick is already running has to wait for the next one, and nothing
// later in the publication can tell the two apart. Its whole body is a
// mutex-guarded no-op when no capture is waiting.
type captureOnUpdate kernel.Subscription[app.UpdateEvent]

// readList is the read side of the triple buffer: the queue the render
// handler translates.
type readList struct{ *gfx.OpQueue }

// readyList is the internal third buffer of the triple buffer: it parks the
// latest completed OpQueue between Present and Acquire/Consume.
type readyList struct {
	queue   *gfx.OpQueue
	pending bool
}

// Plugin implements renderer v2: a triple-buffered OpQueue pipeline plus a
// translator that turns high-level draw commands into a backend-agnostic GpuQueue
// stream. It owns the translator (render-thread-only caches and dynamic buffers);
// the three queues live in kernel resources.
type Plugin struct {
	// PluginEdges carries what the plugin still reaches in storage and mcp:
	// its dependency on storage, the read lock on storage's FileSystem, and
	// its mcp capabilities.
	gfx.PluginEdges

	translator *translator
	// backend is the bound Backend adapter, valid from Start onwards.
	backend kernel.RequiredAdapter[gfx.Backend]
	// reportedNotReady is render-thread-only, like the translator caches.
	reportedNotReady bool
	// captures is gfx's one capture slot, plugin-owned and self-synchronizing.
	// See captureState for why it is not a kernel resource.
	captures captureState
	// snapshots is gfx's one frame-snapshot slot, plugin-owned for the same
	// reason and separate from captures because the two are separate kinds: a
	// capture and a snapshot may be in flight together, and refusing them
	// would destroy the pairing they exist for.
	snapshots snapshotState
}

// New creates the gfx plugin.
func New() *Plugin { return &Plugin{translator: newTranslator()} }

// Name reports the plugin name.
func (p *Plugin) Name() kernel.PluginName { return gfx.Name }

// Register requires the Backend adapter, and registers the three command-list
// buffers, the Present/Acquire/Consume commands, and the end-of-tick present
// subscription on app.UpdateEvent.
func (p *Plugin) Register(registrar *kernel.Registrar, _ any) error {
	p.backend = registrar.RequireAdapter[gfx.Backend]()
	ids := func() internal.IDMinter { return p.backend.Get() }
	registrar.InitResource(internal.NewOpQueue(ids))
	registrar.InitResource(&readList{OpQueue: internal.NewOpQueue(ids)})
	registrar.InitResource(&readyList{queue: internal.NewOpQueue(ids)})
	registrar.InitResource(internal.NewResourceQueue(ids))
	registrar.InitResource(&gfx.Viewport{})
	registrar.InitResource(&desiredViewport{})
	registrar.HandleCommand[gfx.PresentCmd](p.presentCmdImpl)
	registrar.HandleCommand[gfx.AcquireCmd](p.acquireCmdImpl)
	registrar.HandleCommand[gfx.ReleaseCachedResourceCmd](p.releaseCachedResourceCmdImpl)
	registrar.HandleCommand[gfx.FreeCachedResourcesCmd](p.freeCachedResourcesCmdImpl)
	registrar.HandleCommand[gfx.SetViewportCmd](setViewportCmdImpl)
	registrar.HandleCommand[gfx.SetDesiredViewportCmd](setDesiredViewportCmdImpl)
	registrar.HandleCommand[gfx.ArmCaptureCmd](p.armCaptureCmdImpl)
	registrar.HandleCommand[gfx.ArmFrameCmd](p.armFrameCmdImpl)
	registrar.Subscribe[captureOnUpdate](p.admitCapture).First()
	registrar.Subscribe[frameOnUpdate](p.admitFrame).First()
	registrar.Subscribe[gfx.PresentOnUpdate](p.presentOnUpdate).Last()
	registrar.Subscribe[gfx.RenderOnRender](p.renderOnRender)
	return nil
}

// Stop completes any capture the engine walked away from. A capture armed in
// frame N resolves in N+1; if the window closes between them no further submit
// happens, the pending map never resolves, and a waiter left alone learns
// nothing at all. Abandonment is delivered rather than merely true.
//
// It touches the capture slot directly because by Stop the scheduler has
// stopped and grants no locks, which is also why nothing else can be touching
// it: the host loop has returned and every handler is done.
func (p *Plugin) Stop(kernel.Executioner) error {
	p.captures.abandon()
	p.snapshots.abandon()
	return nil
}

// admitCapture admits a waiting capture to the tick that has just begun. It
// declares no resources: the capture slot carries its own lock.
func (p *Plugin) admitCapture() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	return nil, func(kernel.Kernel, app.UpdateEvent) error {
		p.captures.beginTick()
		return nil
	}
}

// admitFrame admits a waiting frame snapshot to the tick that has just
// begun. It declares no resources: the snapshot slot carries its own lock.
func (p *Plugin) admitFrame() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	return nil, func(kernel.Kernel, app.UpdateEvent) error {
		p.snapshots.beginTick()
		return nil
	}
}

// presentOnUpdate swaps the recorded queue into the ready slot, and takes the
// frame snapshot immediately before doing so.
//
// The snapshot is taken here rather than from a subscriber of its own because
// this is the only place the ordering it needs can be expressed. It has to run
// after canvas has flushed into the queue and before present swaps it away -
// but canvas.UpdateEventHandler is a type gfx cannot name, since canvas
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
func (p *Plugin) presentOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var write kernel.Write[*gfx.OpQueue]
	var ready kernel.Write[*readyList]
	var resources kernel.Read[*gfx.ResourceQueue]
	return func(access kernel.ResourceAccess) {
			write = access.GetWrite[*gfx.OpQueue]()
			ready = access.GetWrite[*readyList]()
			resources = access.GetRead[*gfx.ResourceQueue]()
		}, func(_ kernel.Kernel, event app.UpdateEvent) error {
			p.snapshots.record(write.Get(), resources.Get(), event.Tick)
			present(write, ready)
			// Bound beside the queue swap, so the capture rides the ready slot
			// rather than one particular queue.
			p.captures.endTick()
			return nil
		}
}

// present swaps the recorded OpQueue into the ready slot and installs the queue
// previously parked there as the reset writable resource (latest-wins).
func present(write kernel.Write[*gfx.OpQueue], ready kernel.Write[*readyList]) {
	rd := ready.Get()
	recycled := rd.queue
	recycled.Reset()
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

// renderOnRender is the app.RenderEvent handler: it acquires the latest list,
// translates it against the installed Backend, and executes the resulting op
// stream into the backend's screen framebuffer. It runs on the driver's render
// thread (where the driver publishes app.RenderEvent).
func (p *Plugin) renderOnRender() (kernel.Lock, kernel.Observe[app.RenderEvent]) {
	var read kernel.Write[*readList]
	var ready kernel.Write[*readyList]
	var resources kernel.Write[*gfx.ResourceQueue]
	var files func() fs.FS
	return func(access kernel.ResourceAccess) {
			read = access.GetWrite[*readList]()
			ready = access.GetWrite[*readyList]()
			resources = access.GetWrite[*gfx.ResourceQueue]()
			files = p.ReadFiles(access)
		}, func(k kernel.Kernel, _ app.RenderEvent) error {
			acquire(read, ready)
			list := read.Get()
			backend := p.backend.Get()
			if !backend.Ready() {
				if !p.reportedNotReady {
					p.reportedNotReady = true
					return gfx.ErrBackendNotReady{}
				}
				return nil
			}
			queue := resources.Get()
			capture, capturing := p.captures.target()
			ops, err := p.translator.translate(
				list.OpQueue, internal.ResourceQueueOps(queue), backend, files, capture, capturing)
			if err != nil {
				k.ReportError(err)
			}
			backend.Execute(ops)
			// Drained before this frame's own copy is promoted, because what a
			// backend has ready now is the copy the previous frame encoded: its
			// map resolved on the submit Execute just made.
			if done, ready := backend.TakeCapture(); ready {
				p.captures.deliver(done)
			}
			if capturing {
				p.captures.encoded()
			}
			internal.ResourceQueueReset(queue)
			return nil
		}
}
