package scene

import (
	"math"
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
	// build is the frame under construction. It lives on the plugin so its
	// arenas keep their backing across frames.
	build frameBuild
	// materials interns the frame's pass tags and caller materials. Tags
	// outlive a frame because the tag set belongs to the passes an app
	// declares; the materials in it do not.
	materials materialTable
	// prepared is the frame's per-draw resolution, parallel to the flushed
	// draws, and culler the per-camera cull. Both keep their backing across
	// frames.
	prepared []preparedDraw
	culler   culler
	// preparedLights is the frame's per-light resolution, packed once before
	// any camera, and lights the per-pass selection. Both keep their backing
	// across frames.
	preparedLights []preparedLight
	lights         lightSelection
	// temporaries is the frame's temporary meshes as mesh records, built once
	// so that a temporary ref resolves exactly the way a durable one does and
	// everything downstream is blind to which kind it has.
	temporaries []meshRecord
	// meshReported is the set of mesh ids already reported this frame, so a
	// released mesh named by a hundred draws is one report rather than a
	// hundred.
	meshReported map[uint32]struct{}
}

// passLabel keys the synthesised debug label of one camera's pass. Labels are
// cached because the camera set is stable frame to frame, so building them
// costs nothing after the first frame that used them.
type passLabel struct {
	camera CameraID
	tag    PassTag
}

func New() *Plugin {
	return &Plugin{labels: map[passLabel]string{}, meshReported: map[uint32]struct{}{}}
}

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
// Start mounts the bundled shader filesystem. Startup runs after every plugin
// has registered and before the host loop, so the shader is in place for the
// first frame without depending on a driver publishing an event.
func (p *Plugin) Start(k kernel.Executioner) error {
	_, err := k.ExecuteCommand[storage.SetMountCmd](storage.SetMountRequest{Mount: storage.ReadMount{
		Id: shaderMountID, Priority: math.MaxInt, FS: shaderFS,
	}})
	return err
}

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
	k kernel.Kernel, write *OpQueue, lookup *Lookup,
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

	p.build.reset()
	// Durable geometry bakes through the resource queue this handler already
	// write-locks, which is why the Lookup never holds a gfx handle of its own:
	// a mesh baked and drawn in the same update uploads in that same frame.
	bake := func(data []byte) gfx.BufferDescr { return gfxResources.BakeBuffer(data, true) }
	bakeTexture := func(width, height int, format gfx.TextureFormat, pixels []byte) gfx.TextureDescr {
		return gfxResources.BakeTexture(width, height, format, pixels, true, false)
	}
	report := func(err error) { k.ReportError(err) }
	// The frame's meshes are settled before anything looks at a draw: the
	// callers' deferred bakes and releases drain, then the frame's temporaries
	// become records, so every ref a draw names resolves against final state.
	// The staged bytes go to gfx without a second copy: BakeMesh already copied
	// them out of its caller, and the arena they live in is handed over rather
	// than reused. That is the one copy the whole durable path costs.
	lookup.drainMeshes(meshBaker{
		bake: func(data []byte) gfx.BufferDescr { return gfxResources.BakeBuffer(data, false) },
		rebake: func(buffer gfx.BufferDescr, data []byte) gfx.BufferDescr {
			return gfxResources.ReBakeBuffer(buffer, data, false)
		},
		release: gfxResources.ReleaseBuffer,
	})
	p.buildTemporaries(report, write)
	p.materials.reset(lookup.ensureBundled(bakeTexture))
	p.prepareDraws(report, lookup, write, bake, write.flushDraws())
	p.preparedLights = prepareLights(report, p.preparedLights, write.flushLights())
	for i := range cameras {
		p.flushCamera(k, write, lookup, view, cameras[i])
	}
	p.build.emit(gfxWrite)
}

// prepareDraws resolves everything about a draw that no camera changes, once
// per frame before any camera walks the draws: its mesh, its material's
// interned index, its world matrix and its world-space bounding sphere. A draw's
// per-camera cost is then one sphere test, and its per-pass cost one array read.
func (p *Plugin) prepareDraws(
	report func(error), lookup *Lookup, write *OpQueue, bake bakeFunc, draws []drawRecord,
) {
	p.prepared = grow(p.prepared, len(draws))
	clear(p.meshReported)
	for i := range draws {
		record := &draws[i]
		ref := record.mesh
		if record.shape != shapeNone {
			ref = lookup.ensureUnit(record.shape, bake)
		}
		mesh, ok := p.resolveMesh(lookup, write, ref)
		switch {
		case !ok:
			// A ref that named a mesh and no longer resolves - released, stale,
			// or temporary and from an earlier frame - is reported, once per
			// ref. A ref that never named one was already reported at the mint
			// that rejected it, so it skips in silence.
			if ref.source != meshNone {
				p.reportMeshOnce(report, ref, ErrMeshUnavailable{Mesh: ref.ID()})
			}
			ref = MeshRef{}
		case !mesh.standard && record.material == nil:
			p.reportMeshOnce(report, ref, ErrMeshCustomLayoutNeedsMaterial{Mesh: ref.ID()})
			ref, mesh = MeshRef{}, meshRecord{}
		}
		p.prepared[i] = prepareDraw(*record, mesh)
		p.prepared[i].mesh = ref
		p.prepared[i].interned = p.materials.intern(report, record.material)
	}
}

// resolveMesh reads one ref, whichever surface minted it. A temporary is
// checked against the frame it was minted in, which is what stops a ref kept
// across a frame boundary from drawing whatever now holds its slot.
func (p *Plugin) resolveMesh(lookup *Lookup, write *OpQueue, ref MeshRef) (meshRecord, bool) {
	if ref.source == meshTemporary {
		if ref.generation != write.publishedFrame || ref.id == 0 || int(ref.id) > len(p.temporaries) {
			return meshRecord{}, false
		}
		return p.temporaries[ref.id-1], true
	}
	return lookup.mesh(ref)
}

// buildTemporaries materialises the frame's temporary meshes and reports the
// mints that were rejected, which the recording could not report itself: the
// queue holds no kernel.
func (p *Plugin) buildTemporaries(report func(error), write *OpQueue) {
	recording := write.flushMeshes()
	for _, err := range recording.reports {
		report(err)
	}
	p.temporaries = grow(p.temporaries, len(recording.temporaries))
	for i := range recording.temporaries {
		p.temporaries[i] = recording.temporaries[i].record(recording.arena)
	}
}

// reportMeshOnce reports one mesh's failure the first time a draw hits it this
// frame. Keyed by the public id, so a hundred draws of one released mesh are one
// report and two different meshes are two.
func (p *Plugin) reportMeshOnce(report func(error), ref MeshRef, err error) {
	if _, seen := p.meshReported[ref.ID()]; seen {
		return
	}
	p.meshReported[ref.ID()] = struct{}{}
	report(err)
}

// flushCamera emits one camera's passes. A camera missing a clip plane is
// skipped whole: the projection it would get instead is degenerate, and every
// pass built from it would cull against a volume nobody asked for.
func (p *Plugin) flushCamera(
	k kernel.Kernel, write *OpQueue, lookup *Lookup, view *app.Viewport, camera cameraRecord,
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
	p.culler.beginCamera()
	for _, pass := range passes {
		p.flushPass(k, write, lookup, view, camera, viewMatrix, pass)
	}
}

// flushPass decides one pass: which of the camera's survivors its tag admits,
// in what order, and packs them. Within a pass, recording order is not
// preserved - that is the trade the sort makes, and Passes documents it.
func (p *Plugin) flushPass(
	k kernel.Kernel, write *OpQueue, lookup *Lookup, view *app.Viewport,
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
	viewProjection := projectionMatrix.Mul(viewMatrix)
	draws := write.flushDraws()
	// One cull per distinct frustum: a second pass at the same aspect reuses
	// the first's survivors and filters them by its own tag.
	cull := p.culler.results[p.culler.cull(
		aspect, viewProjection, viewMatrix, camera.descr.CullMask, draws, p.prepared,
	)]
	survivors := p.culler.survivors[cull.first : cull.first+cull.count]
	eye := cameraPosition(camera.descr.Transform)
	// Lights are culled per pass, against this pass's frustum, and capped at
	// the eye, before anything is packed. A camera's shadow pass and screen
	// pass may end up with different light sets, which is correct.
	p.lights.selectLights(cull.frustum, eye.Vec3(), camera.descr.CullMask, p.preparedLights)
	block := packFrameLighting(sceneFrameBlock{
		View:           viewMatrix,
		Projection:     projectionMatrix,
		ViewProjection: viewProjection,
		CameraPosition: eye,
		LightCount:     uint32(p.lights.count),
		Lights:         p.lights.lights,
	}, camera.descr)
	pending := p.build.beginPass(p.passDescr(camera.id, pass, order), block)
	result := PassView{
		CameraID: camera.id,
		Order:    order,
		Tag:      pass.tag(),
		Frustum:  cull.frustum,
		Recorded: cull.recorded,
		Culled:   cull.culled,
		Lights:   p.lights.count,
	}
	// The tag interns once per pass, so no draw in it ever compares a string.
	tag := p.materials.internTag(pass.tag())
	p.build.opaque, p.build.blend = p.build.opaque[:0], p.build.blend[:0]
	for i := range survivors {
		prepared := &p.prepared[survivors[i].draw]
		if prepared.mesh.ID() == 0 {
			continue
		}
		// A material with no entry for this pass's tag skips it. Tag
		// participation is purely a material property: a draw gets no say in
		// which passes it appears in.
		entry, serves := p.materials.entry(prepared.interned, tag)
		if !serves {
			continue
		}
		if entry.blend {
			p.build.blend = append(p.build.blend, sortEntry{key: blendKey(survivors[i].depth), draw: uint32(i)})
		} else {
			p.build.opaque = append(p.build.opaque, sortEntry{key: opaqueKey(entry.materialID, prepared.mesh.ID()), draw: uint32(i)})
		}
	}
	// Opaque and blend are separate arrays emitted in that order, which is what
	// removes any class bit from the key.
	sortEntries(p.build.opaque)
	sortEntries(p.build.blend)
	for _, class := range [2][]sortEntry{p.build.opaque, p.build.blend} {
		for _, entry := range class {
			index := survivors[entry.draw].draw
			prepared := &p.prepared[index]
			material, _ := p.materials.entry(prepared.interned, tag)
			mesh, _ := p.resolveMesh(lookup, write, prepared.mesh)
			p.build.addDraw(pending, mesh, prepared.mesh.ID(), material,
				prepared.world, draws[index].pbrRecord(), draws[index].params)
			result.Instances++
		}
	}
	p.build.endPass(pending)
	write.publishPass(result)
	write.publishBatches(p.build.batches)
}

// cameraPosition reads the eye out of a camera's transform. Scale is ignored
// the way the view matrix ignores it: a scaled camera scales the world instead,
// and its position is unaffected either way.
func cameraPosition(transform Transform) m.Vec4 {
	eye := transform.Mat4().Translation()
	return m.Vec4{X: eye.X, Y: eye.Y, Z: eye.Z, W: 1}
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
