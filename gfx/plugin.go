package gfx

import (
	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/storage"
)

// Name is the gfx plugin's name.
const Name kernel.PluginName = "gfx"

// UpdateEventHandler is the subscription type of the plugin's end-of-tick present
// handler on app.UpdateEvent; it runs last so gameplay has finished recording.
type UpdateEventHandler kernel.Subscription[app.UpdateEvent]

// CaptureUpdateEventHandler is the subscription type of the plugin's
// start-of-tick capture handler on app.UpdateEvent. It is ordered First - ahead
// of every other subscriber, not merely in the first phase - because that is
// what makes "a tick that began after the request" decidable: a capture armed
// while a tick is already running has to wait for the next one, and nothing
// later in the publication can tell the two apart. Its whole body is a
// mutex-guarded no-op when no capture is waiting.
type CaptureUpdateEventHandler kernel.Subscription[app.UpdateEvent]

// RenderEventHandler is the subscription type of the plugin's per-frame render
// handler on app.RenderEvent. A driver publishes app.RenderEvent synchronously on
// its render thread (after making the surface current), so this handler acquires
// the latest list, translates it, and executes it into the backend's screen
// framebuffer — no explicit render command needed.
type RenderEventHandler kernel.Subscription[app.RenderEvent]

// readyList is the internal third buffer of the triple buffer: it parks the
// latest completed OpQueue between Present and Acquire/Consume.
type readyList struct {
	queue   *OpQueue
	pending bool
}

// Plugin implements renderer v2: a triple-buffered OpQueue pipeline plus a
// translator that turns high-level draw commands into a backend-agnostic GpuQueue
// stream. It owns the translator (render-thread-only caches and dynamic buffers);
// the three queues live in kernel resources.
type Plugin struct {
	translator *translator
	// reportedMissingBackend is render-thread-only, like the translator caches.
	reportedMissingBackend bool
	// captures is gfx's one capture slot, plugin-owned and self-synchronizing.
	// See captureState for why it is not a kernel resource.
	captures captureState
}

// New creates the gfx plugin.
func New() *Plugin { return &Plugin{translator: newTranslator()} }

// Name reports the plugin name.
func (p *Plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins gfx requires: storage, from which it loads
// shader and texture resources.
func (p *Plugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{storage.Name} }

// Register registers the three command-list buffers, the Present/Acquire/Consume
// commands, and the end-of-tick present subscription on app.UpdateEvent.
func (p *Plugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(&OpQueue{})
	registrar.InitResource(&readList{OpQueue: &OpQueue{}})
	registrar.InitResource(&readyList{queue: &OpQueue{}})
	registrar.InitResource(&ResourceQueue{})
	registrar.InitResource(&app.Viewport{})
	registrar.InitResource(&desiredViewport{})
	registrar.HandleCommand[PresentCmd](p.presentCmdImpl)
	registrar.HandleCommand[AcquireCmd](p.acquireCmdImpl)
	registrar.HandleCommand[SetBackendCmd](p.setBackendCmdImpl)
	registrar.HandleCommand[ReleaseCachedResourceCmd](p.releaseCachedResourceCmdImpl)
	registrar.HandleCommand[FreeCachedResourcesCmd](p.freeCachedResourcesCmdImpl)
	registrar.HandleCommand[app.SetViewportCmd](setViewportCmdImpl)
	registrar.HandleCommand[app.SetDesiredViewportCmd](setDesiredViewportCmdImpl)
	registrar.HandleCommand[ArmCaptureCmd](p.armCaptureCmdImpl)
	registrar.Subscribe[CaptureUpdateEventHandler](p.captureOnUpdate).First()
	registrar.Subscribe[UpdateEventHandler](p.presentOnUpdate).Last()
	registrar.Subscribe[RenderEventHandler](p.renderOnRender)
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
	return nil
}

// captureOnUpdate admits a waiting capture to the tick that has just begun. It
// declares no resources: the capture slot carries its own lock.
func (p *Plugin) captureOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	return nil, func(kernel.Kernel, app.UpdateEvent) error {
		p.captures.beginTick()
		return nil
	}
}

func (p *Plugin) presentOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var write kernel.Write[*OpQueue]
	var ready kernel.Write[*readyList]
	return func(access kernel.ResourceAccess) {
			write = access.GetWrite[*OpQueue]()
			ready = access.GetWrite[*readyList]()
		}, func(kernel.Kernel, app.UpdateEvent) error {
			present(write, ready)
			// Bound beside the queue swap, so the capture rides the ready slot
			// rather than one particular queue.
			p.captures.endTick()
			return nil
		}
}

// present swaps the recorded OpQueue into the ready slot and installs the queue
// previously parked there as the reset writable resource (latest-wins).
func present(write kernel.Write[*OpQueue], ready kernel.Write[*readyList]) {
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
	var resources kernel.Write[*ResourceQueue]
	var filesystem kernel.Read[storage.FileSystem]
	return func(access kernel.ResourceAccess) {
			read = access.GetWrite[*readList]()
			ready = access.GetWrite[*readyList]()
			resources = access.GetWrite[*ResourceQueue]()
			filesystem = access.GetRead[storage.FileSystem]()
		}, func(k kernel.Kernel, _ app.RenderEvent) error {
			acquire(read, ready)
			list := read.Get()
			if list.backend == nil {
				if !p.reportedMissingBackend {
					p.reportedMissingBackend = true
					return ErrBackendMissing{}
				}
				return nil
			}
			queue := resources.Get()
			capture, capturing := p.captures.target()
			ops, err := p.translator.translate(
				list.OpQueue, queue.ops, list.backend, filesystem.Get(), capture, capturing)
			if err != nil {
				k.ReportError(err)
			}
			list.backend.Execute(ops)
			// Drained before this frame's own copy is promoted, because what a
			// backend has ready now is the copy the previous frame encoded: its
			// map resolved on the submit Execute just made.
			if done, ready := list.backend.TakeCapture(); ready {
				p.captures.deliver(done)
			}
			if capturing {
				p.captures.encoded()
			}
			queue.reset()
			return nil
		}
}
