package scene

import (
	"strconv"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/storage"
)

// Name is the scene plugin's kernel name.
const Name kernel.PluginName = "scene"

// UpdateEventHandler is scene's flush subscription. It is exported so a
// recorder can order itself before scene, the way canvas's is.
type UpdateEventHandler kernel.Subscription[app.UpdateEvent]

// Plugin records declarative 3D draws and translates them into gfx passes and
// draws at the end of each simulation update. It is the sibling of canvas: the
// same frame-local OpQueue shape, the same persistent Lookup behind a scoped
// access facade.
//
// Register storage before scene. A typical order is storage, input, gfx,
// canvas, scene, then the system driver.
type Plugin struct {
	config Config
	// defaultPasses is the reused one-element slice a camera that declared no
	// passes of its own is flushed through.
	defaultPasses [1]Pass
	labels        map[passLabel]string
}

// passLabel keys the synthesised debug label of one camera's pass. Labels are
// cached because the camera set is stable frame to frame, so building them
// costs nothing after the first frame that used them.
type passLabel struct {
	camera CameraID
	tag    PassTag
}

func New() *Plugin { return &Plugin{labels: map[passLabel]string{}} }

func (p *Plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins scene requires: gfx, which it emits passes
// and draws into, and storage, which hosts its shader filesystem mount.
func (p *Plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{gfx.Name, storage.Name}
}

func (p *Plugin) Register(registrar *kernel.Registrar, value any) error {
	config, err := resolveConfig(value)
	if err != nil {
		return err
	}
	p.config = config
	registrar.InitResource(&opQueue{})
	registrar.InitResource(newLookup(config))
	registrar.Subscribe[UpdateEventHandler](p.flush).
		Last().Before[gfx.UpdateEventHandler]()
	return nil
}

// flush binds the resources the frame's decisions need. Everything scene
// decides happens here, on the update thread: projection resolve, culling,
// sorting, instance packing and buffer uploads. Scene never runs on the render
// thread — gfx renders from a latest-wins snapshot, so there is no mechanism
// for it and no need for one, because a frustum needs aspect, not pixel size,
// and app.Viewport already carries the exact aspect here.
func (p *Plugin) flush() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var writeQueue kernel.Write[*OpQueue]
	var lookupResource kernel.Write[*Lookup]
	var gfxQueue kernel.Write[*gfx.OpQueue]
	var gfxResourceQueue kernel.Write[*gfx.ResourceQueue]
	var viewport kernel.Read[*app.Viewport]
	return func(access kernel.ResourceAccess) {
			writeQueue = access.GetWrite[*OpQueue]()
			lookupResource = access.GetWrite[*Lookup]()
			gfxQueue = access.GetWrite[*gfx.OpQueue]()
			gfxResourceQueue = access.GetWrite[*gfx.ResourceQueue]()
			viewport = access.GetRead[*app.Viewport]()
		}, func(k kernel.Kernel, _ app.UpdateEvent) error {
			p.flushFrame(k, writeQueue.Get(), lookupResource.Get(),
				gfxQueue.Get(), gfxResourceQueue.Get(), viewport.Get())
			return nil
		}
}

func (p *Plugin) flushFrame(
	k kernel.Kernel, write *OpQueue, _ *Lookup,
	gfxWrite *gfx.OpQueue, gfxResources *gfx.ResourceQueue, view *app.Viewport,
) {
	cameras := write.beginFlush()
	defer write.endFlush()
	for _, id := range write.recordedDuplicates() {
		k.ReportError(ErrCameraAlreadyRecorded{Camera: id})
	}
	// A frame before the driver has reported a window, or while one is
	// minimised, is skipped whole rather than reported: every screen-targeted
	// pass in it would resolve an aspect of zero, and reporting that once per
	// camera per frame says nothing a caller can act on.
	if !gfxResources.Ready() || view.WindowWidth <= 0 || view.WindowHeight <= 0 {
		return
	}

	for i := range cameras {
		p.flushCamera(k, write, gfxWrite, view, cameras[i])
	}
}

// flushCamera emits one camera's passes. A camera missing a clip plane is
// skipped whole: the projection it would get instead is degenerate, and every
// pass built from it would cull against a volume nobody asked for.
func (p *Plugin) flushCamera(
	k kernel.Kernel, write *OpQueue, gfxWrite *gfx.OpQueue,
	view *app.Viewport, camera cameraRecord,
) {
	if camera.descr.Near == 0 || camera.descr.Far == 0 {
		k.ReportError(ErrCameraClipPlanesMissing{
			Camera: camera.id, Near: camera.descr.Near, Far: camera.descr.Far,
		})
		return
	}
	viewMatrix, ok := cameraView(camera.descr.Transform)
	if !ok {
		k.ReportError(ErrCameraProjectionDegenerate{
			Camera: camera.id, Reason: "the transform has no inverse",
		})
		return
	}
	passes := camera.descr.Passes
	if len(passes) == 0 {
		p.defaultPasses[0] = defaultPass()
		passes = p.defaultPasses[:]
	}
	for _, pass := range passes {
		p.flushPass(k, write, gfxWrite, view, camera, viewMatrix, pass)
	}
}

func (p *Plugin) flushPass(
	k kernel.Kernel, write *OpQueue, gfxWrite *gfx.OpQueue, view *app.Viewport,
	camera cameraRecord, viewMatrix m.Mat4, pass Pass,
) {
	aspect, err := passAspect(camera.id, pass, view)
	if err != nil {
		k.ReportError(err)
		return
	}
	projectionMatrix, err := projection(camera.id, camera.descr, aspect)
	if err != nil {
		k.ReportError(err)
		return
	}
	order := gfx.Order(camera.id) + pass.Order
	gfxWrite.Pass(p.passDescr(camera.id, pass, order))
	write.publishPass(PassView{
		CameraID: camera.id,
		Order:    order,
		Tag:      pass.tag(),
		Frustum:  m.FrustumFromMat4(projectionMatrix.Mul(viewMatrix)),
	})
}

// passDescr translates one scene pass into the gfx pass it emits.
//
// Store ops are inferred rather than exposed. Depth is kept iff the pass names
// an explicit depth texture — you allocated it, you mean to sample it — and
// discarded otherwise, so every forward pass gets the tiled-GPU depth-discard
// win for free and there is no knob to set wrong. Colour is always kept.
func (p *Plugin) passDescr(id CameraID, pass Pass, order gfx.Order) gfx.PassDescr {
	desc := gfx.PassDescr{
		Order:      order,
		Target:     pass.Target,
		Depth:      pass.Depth,
		DepthStore: gfx.StoreDiscard,
		Label:      p.label(id, pass.tag()),
	}
	if pass.Depth.IsTexture() {
		desc.DepthStore = gfx.StoreKeep
	}
	if pass.ClearColor != nil {
		desc.Load, desc.Clear = gfx.LoadClear, *pass.ClearColor
	}
	if pass.ClearDepth != nil {
		desc.DepthLoad, desc.DepthClear = gfx.LoadClear, *pass.ClearDepth
	}
	return desc
}

func (p *Plugin) label(id CameraID, tag PassTag) string {
	key := passLabel{camera: id, tag: tag}
	label, ok := p.labels[key]
	if !ok {
		label = "scene.camera" + strconv.Itoa(int(id)) + "." + string(tag)
		p.labels[key] = label
	}
	return label
}
