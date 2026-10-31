package mcp

import (
	bundlesmcp "github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx/internal"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// observer is what gfx is given to watch its frames with: the one capture slot
// and the one frame-snapshot slot, the commands that arm them, and the
// Provider that offers them to an agent.
type observer struct {
	captures captureState
	// snapshots is separate from captures because the two are separate kinds:
	// a capture and a snapshot may be in flight together, and refusing them
	// would destroy the pairing they exist for.
	snapshots snapshotState
}

// New creates the observer gfxplugin hands to the gfx plugin.
func New() internal.FrameObserver { return &observer{} }

// Register adds the arm commands, the two start-of-tick subscriptions and the
// Provider to gfx's registration.
func (o *observer) Register(registrar *kernel.Registrar) {
	registrar.HandleCommand[ArmCaptureCmd](o.armCaptureCmdImpl)
	registrar.HandleCommand[ArmFrameCmd](o.armFrameCmdImpl)
	registrar.Subscribe[captureOnUpdate](o.admitCapture).First()
	registrar.Subscribe[frameOnUpdate](o.admitFrame).First()
	registrar.ProvideAdapter[internal.McpProvider](bundlesmcp.Provider(provider{}))
}

// Presenting takes the frame snapshot, then binds the capture admitted this
// tick beside the queue swap, so the capture rides the ready slot rather than
// one particular queue.
func (o *observer) Presenting(queue *internal.OpQueue, resources *internal.ResourceQueue, tick int64) {
	o.snapshots.record(queue, resources, tick)
	o.captures.endTick()
}

func (o *observer) Capturing() (types.CaptureDesc, bool) { return o.captures.target() }
func (o *observer) Captured(capture internal.Capture)    { o.captures.deliver(capture) }
func (o *observer) Encoded()                             { o.captures.encoded() }

// Abandon completes any capture or snapshot the engine walked away from. A
// capture armed in frame N resolves in N+1; if the window closes between them
// no further submit happens, and a waiter left alone learns nothing at all.
func (o *observer) Abandon() {
	o.captures.abandon()
	o.snapshots.abandon()
}

// admitCapture admits a waiting capture to the tick that has just begun. It
// declares no resources: the capture slot carries its own lock.
func (o *observer) admitCapture() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	return nil, func(kernel.Kernel, app.UpdateEvent) {
		o.captures.beginTick()
	}
}

// admitFrame admits a waiting frame snapshot to the tick that has just
// begun. It declares no resources: the snapshot slot carries its own lock.
func (o *observer) admitFrame() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	return nil, func(kernel.Kernel, app.UpdateEvent) {
		o.snapshots.beginTick()
	}
}

// armCaptureCmdImpl installs the frame's one capture request and hands back
// the channel its stills arrive on, plus the window size the caller cannot
// read for itself. The Viewport read is the only lock it needs: the capture
// slot carries its own.
func (o *observer) armCaptureCmdImpl() (kernel.Lock, kernel.Execute[ArmCaptureRequest, ArmCaptureResponse]) {
	var viewport kernel.Read[*types.Viewport]
	return func(access kernel.ResourceAccess) {
			viewport = access.GetRead[*types.Viewport]()
		}, func(_ kernel.Kernel, request ArmCaptureRequest) ArmCaptureResponse {
			live, err := o.captures.arm(request)
			if err != nil {
				return ArmCaptureResponse{Err: err}
			}
			return ArmCaptureResponse{Done: live.done, Viewport: *viewport.Get()}
		}
}

// armFrameCmdImpl installs the frame's one snapshot request and hands back
// the channel the result arrives on, plus the viewport the caller cannot read
// for itself. The Viewport read is the only lock it needs: the snapshot slot
// carries its own.
func (o *observer) armFrameCmdImpl() (kernel.Lock, kernel.Execute[ArmFrameRequest, ArmFrameResponse]) {
	var viewport kernel.Read[*types.Viewport]
	return func(access kernel.ResourceAccess) {
			viewport = access.GetRead[*types.Viewport]()
		}, func(_ kernel.Kernel, request ArmFrameRequest) ArmFrameResponse {
			live, err := o.snapshots.arm(request)
			if err != nil {
				return ArmFrameResponse{Err: err}
			}
			return ArmFrameResponse{Done: live.done, Viewport: *viewport.Get()}
		}
}
