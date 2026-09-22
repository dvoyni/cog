package internal

import (
	"math"
	"strconv"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal/types"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

// plugin records declarative 3D draws and translates them into gfx passes and
// draws at the end of each simulation update. It is the sibling of canvas: the
// same frame-local OpQueue shape, the same persistent Lookup behind a scoped
// access facade.
type plugin struct {
	// defaultPasses is the reused one-element slice a camera that declared no
	// passes of its own is flushed through.
	defaultPasses [1]scene.Pass
	labels        map[passLabel]string
	// build is the frame under construction. It lives on the plugin so its
	// arenas keep their backing across frames.
	build frameBuild
	// materials interns the frame's pass tags and caller materials. Tags
	// outlive a frame because the tag set belongs to the passes an app
	// declares; the materials in it do not.
	materials materialTable
	// forward is the frame's arena of forward tag entries, one per model
	// material a draw names: model's materials name no pass, so the flush
	// wraps each as a scene material here. It keeps its backing across frames,
	// so a steady frame wraps without allocating.
	forward []scene.MaterialTag
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
	temporaries []model.MeshRecord
	// modelWorlds is the frame's expanded model instance matrices. A draw
	// record points into it, so it is sized once before any record is written
	// and never appended to while records already point at it.
	modelWorlds []m.Mat4
	// modelViews is what each model draw's selectors resolved to, one per
	// recorded draw, carried from the sizing pass to the expansion so the
	// resolution and its report happen once. modelAnims is the same shape for
	// the animation each draw resolved to, and modelPlays the scratch one
	// draw's play records are folded in. All three keep their backing across
	// frames.
	modelViews []model.ModelView
	modelAnims []types.AnimBinding
	modelPlays []model.ScenePlayRecord
	// The morph half of the same resolution. modelWeightFrames is parallel to
	// modelPlays, modelWeights the draw's blended vector over the model's
	// flattened slot list, and modelTargets the scratch one primitive's sparse
	// list is culled into. modelMorphOffsets is the frame's per-primitive
	// sceneAnim offsets, which a draw record reaches by index rather than by a
	// slice: it is still being appended to while the records are written.
	modelWeightFrames []model.WeightFrames
	modelWeights      []float32
	modelTargets      []model.SceneMorphWeight
	modelMorphOffsets []uint32
	// meshReported is the set of mesh ids already reported this frame, so a
	// released mesh named by a hundred draws is one report rather than a
	// hundred.
	meshReported map[uint32]struct{}
}

// passLabel keys the synthesised debug label of one camera's pass. Labels are
// cached because the camera set is stable frame to frame, so building them
// costs nothing after the first frame that used them.
type passLabel struct {
	camera scene.CameraID
	tag    scene.PassTag
}

// New returns the scene plugin. It declares scene.OpQueue, and requires gfx,
// storage and model, whose *model.Lookup it draws from.
//
// Register storage and model before scene. A typical order is storage, input,
// gfx, canvas, model, scene, then the system driver.
func New() kernel.Plugin {
	return &plugin{labels: map[passLabel]string{}, meshReported: map[uint32]struct{}{}}
}

func (p *plugin) Name() kernel.PluginName { return scene.Name }

// Dependencies reports the plugins scene requires: gfx, which it emits passes
// and draws into, storage, which hosts its shader filesystem mount, and model,
// which registers the *model.Lookup every draw resolves against.
func (p *plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{gfx.Name, storage.Name, model.Name}
}

// scene takes no configuration. The Lookup it draws from is model's, and so
// is the pose sample rate that sizes it, so a value handed to scene under its
// own name is ignored.
func (p *plugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(&scene.OpQueue{})
	registrar.Subscribe[scene.FlushOnUpdate](p.flush).
		Last().Before[gfx.PresentOnUpdate]()
	// storage installs the bundled shader mount at its Start, ahead of every
	// plugin that depends on it, so the shader is in place for the first frame.
	registrar.ProvideAdapter[scene.StorageReadMount](storage.ReadMount{
		Id: shaderMountID, Priority: math.MaxInt, FS: shaderFS,
	})
	return nil
}

// flush binds the resources the frame's decisions need. Everything scene
// decides happens here, on the update thread: projection resolve, culling,
// sorting, instance packing and buffer uploads. Scene never runs on the render
// thread — gfx renders from a latest-wins snapshot, so there is no mechanism
// for it and no need for one, because a frustum needs aspect, not pixel size,
// and gfx.Viewport already carries the exact aspect here.
//
// The filesystem read is here because a model draw loads the file it names, in
// the frame that named it. It is the one lock this plugin added for that, and
// it serialises against nothing a frame does: storage.FileSystem is write-locked
// only by storage's own three mount commands, and canvas's flush, ui's update
// and gfx's render already hold it as a read.
func (p *plugin) flush() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var writeQueue kernel.Write[*scene.OpQueue]
	var lookupResource kernel.Write[*scene.Lookup]
	var gfxQueue kernel.Write[*gfx.OpQueue]
	var gfxResourceQueue kernel.Write[*gfx.ResourceQueue]
	var viewport kernel.Read[*gfx.Viewport]
	var filesystem kernel.Read[storage.FileSystem]
	return func(access kernel.ResourceAccess) {
			writeQueue = access.GetWrite[*scene.OpQueue]()
			lookupResource = access.GetWrite[*scene.Lookup]()
			gfxQueue = access.GetWrite[*gfx.OpQueue]()
			gfxResourceQueue = access.GetWrite[*gfx.ResourceQueue]()
			viewport = access.GetRead[*gfx.Viewport]()
			filesystem = access.GetRead[storage.FileSystem]()
		}, func(k kernel.Kernel, _ app.UpdateEvent) {
			p.flushFrame(k, writeQueue.Get(), lookupResource.Get(),
				gfxQueue.Get(), gfxResourceQueue.Get(), viewport.Get(), filesystem.Get())
		}
}

func (p *plugin) flushFrame(
	k kernel.Kernel, write *scene.OpQueue, lookup *scene.Lookup,
	gfxWrite *gfx.OpQueue, gfxResources *gfx.ResourceQueue, view *gfx.Viewport,
	filesystem storage.FileSystem,
) {
	cameras := types.OpQueueBeginFlush(write)
	defer types.OpQueueEndFlush(write)
	for _, id := range types.OpQueueDuplicates(write) {
		k.ReportError(scene.ErrCameraAlreadyRecorded{Camera: id})
	}
	// A frame before the MainLoop has reported a window, or while one is
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
	lookup.DrainMeshes(model.MeshBaker{
		Bake: func(data []byte) gfx.BufferDescr { return gfxResources.BakeBuffer(data, false) },
		Rebake: func(buffer gfx.BufferDescr, data []byte) gfx.BufferDescr {
			return gfxResources.ReBakeBuffer(buffer, data, false)
		},
		Release: gfxResources.ReleaseBuffer,
	})
	p.buildTemporaries(report, write)
	// model's materials name no pass, so each one a frame draws is wrapped as a
	// forward scene material in the frame's own arena, starting with the
	// bundled PBR's four variants.
	clear(p.forward)
	p.forward = p.forward[:0]
	bundled := lookup.EnsureBundled(bakeTexture)
	var wrapped [model.VariantCount]scene.Material
	for variant := range bundled {
		wrapped[variant] = p.forwardMaterial(bundled[variant])
	}
	p.materials.reset(wrapped)
	// Model draws expand into ordinary draw records before anything looks at
	// one, so culling, sorting and packing are blind to where a draw came from.
	// Anything a draw names and the cache does not hold is read, parsed and
	// uploaded right here, which is the hitch Preload exists to move.
	p.expandModels(k, lookup, write, filesystem, gfxResources)
	p.prepareDraws(report, lookup, write, bake, types.OpQueueFlushDraws(write))
	p.preparedLights = prepareLights(report, p.preparedLights, types.OpQueueFlushLights(write))
	for i := range cameras {
		p.flushCamera(k, write, lookup, view, cameras[i])
	}
	p.build.emit(gfxWrite)
}

// prepareDraws resolves everything about a draw that no camera changes, once
// per frame before any camera walks the draws: its mesh, its material's
// interned index, its world matrix and its world-space bounding sphere. A draw's
// per-camera cost is then one sphere test, and its per-pass cost one array read.
func (p *plugin) prepareDraws(
	report func(error), lookup *scene.Lookup, write *scene.OpQueue,
	bake model.BakeFunc, draws []types.DrawRecord,
) {
	p.prepared = grow(p.prepared, len(draws))
	clear(p.meshReported)
	for i := range draws {
		record := &draws[i]
		ref := record.Mesh
		if record.Shape != types.ShapeNone {
			ref = lookup.EnsureUnit(record.Shape.UnitMesh(), bake)
		}
		mesh, ok := p.resolveMesh(lookup, write, ref)
		switch {
		case !ok:
			// A ref that named a mesh and no longer resolves - released, stale,
			// or temporary and from an earlier frame - is reported, once per
			// ref. A ref that never named one was already reported at the mint
			// that rejected it, so it skips in silence.
			if ref.Source() != model.MeshNone {
				p.reportMeshOnce(report, ref, scene.ErrMeshUnavailable{Mesh: ref.ID()})
			}
			ref = scene.MeshRef{}
		case !mesh.Standard && record.Material == nil:
			p.reportMeshOnce(report, ref, scene.ErrMeshCustomLayoutNeedsMaterial{Mesh: ref.ID()})
			ref, mesh = scene.MeshRef{}, model.MeshRecord{}
		}
		p.prepared[i] = prepareDraw(*record, mesh)
		p.prepared[i].mesh = ref
		// The variant is what the draw actually deforms, and it is decided here
		// so that the module a draw compiles and the bindings it supplies come
		// from one answer. A draw with neither half declares no group 2 at all,
		// which is what removed the shared identity pose it used to bind: the
		// rule was that a declared binding must be bound, and it now declares
		// nothing there.
		//
		// The two halves are read separately because a model may have either on
		// its own: a rigged prop has poses and no shapes, a morph-only face the
		// other way round. Only a draw with neither loses its animOffset - a
		// buffer-built draw's is the zero value, which is a valid offset rather
		// than an absent one.
		skin := &p.prepared[i].anim.Skin
		p.prepared[i].interned = p.materials.intern(report, record.Material, record.MaterialKey,
			model.VariantFor(skin.Bound, skin.Morphed))
		if !skin.Bound && !skin.Morphed {
			p.prepared[i].anim.Offset = types.SceneNoAnim
		}
	}
}

// resolveMesh reads one ref, whichever surface minted it. A temporary is
// checked against the frame it was minted in, which is what stops a ref kept
// across a frame boundary from drawing whatever now holds its slot.
func (p *plugin) resolveMesh(
	lookup *scene.Lookup, write *scene.OpQueue, ref scene.MeshRef,
) (model.MeshRecord, bool) {
	if ref.Source() == types.MeshTemporary {
		id := ref.Index()
		if ref.Generation() != types.OpQueuePublishedFrame(write) || id == 0 || int(id) > len(p.temporaries) {
			return model.MeshRecord{}, false
		}
		return p.temporaries[id-1], true
	}
	return lookup.Mesh(ref)
}

// buildTemporaries materialises the frame's temporary meshes and reports the
// mints that were rejected, which the recording could not report itself: the
// queue holds no kernel.
func (p *plugin) buildTemporaries(report func(error), write *scene.OpQueue) {
	recording := types.OpQueueFlushMeshes(write)
	for _, err := range recording.Reports {
		report(err)
	}
	p.temporaries = grow(p.temporaries, len(recording.Temporaries))
	for i := range recording.Temporaries {
		p.temporaries[i] = recording.Temporaries[i].InlineRecord(recording.Arena)
	}
}

// reportMeshOnce reports one mesh's failure the first time a draw hits it this
// frame. Keyed by the public id, so a hundred draws of one released mesh are one
// report and two different meshes are two.
//
// It is the plugin's own set rather than kernel.ReportErrorOnce because the
// quiet it wants lasts one frame, not one episode: the set is cleared at the
// top of every flush, and asking the kernel to forget a family per frame would
// scan the engine's whole table per frame for a condition that is already one
// map lookup here.
func (p *plugin) reportMeshOnce(report func(error), ref scene.MeshRef, err error) {
	if _, seen := p.meshReported[ref.ID()]; seen {
		return
	}
	p.meshReported[ref.ID()] = struct{}{}
	report(err)
}

// flushCamera emits one camera's passes. A camera missing a clip plane is
// skipped whole: the projection it would get instead is degenerate, and every
// pass built from it would cull against a volume nobody asked for.
func (p *plugin) flushCamera(
	k kernel.Kernel, write *scene.OpQueue, lookup *scene.Lookup, view *gfx.Viewport, camera types.CameraRecord,
) {
	if camera.Descr.Near == 0 || camera.Descr.Far == 0 {
		k.ReportError(scene.ErrCameraClipPlanesMissing{
			Camera: camera.ID, Near: camera.Descr.Near, Far: camera.Descr.Far,
		})
		return
	}
	viewMatrix, ok := types.CameraView(camera.Descr.Transform)
	if !ok {
		k.ReportError(scene.ErrCameraProjectionDegenerate{
			Camera: camera.ID, Reason: "the transform has no inverse",
		})
		return
	}
	passes := camera.Descr.Passes
	if len(passes) == 0 {
		p.defaultPasses[0] = types.DefaultPass()
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
func (p *plugin) flushPass(
	k kernel.Kernel, write *scene.OpQueue, lookup *scene.Lookup, view *gfx.Viewport,
	camera types.CameraRecord, viewMatrix m.Mat4, pass scene.Pass,
) {
	aspect, err := passAspect(camera.ID, pass, view)
	if err != nil {
		k.ReportError(err)
		return
	}
	projectionMatrix, err := types.Projection(camera.ID, camera.Descr, aspect)
	if err != nil {
		k.ReportError(err)
		return
	}
	order := gfx.Order(camera.ID) + pass.Order
	viewProjection := projectionMatrix.Mul(viewMatrix)
	draws := types.OpQueueFlushDraws(write)
	// One cull per distinct frustum: a second pass at the same aspect reuses
	// the first's survivors and filters them by its own tag.
	cull := p.culler.results[p.culler.cull(
		aspect, viewProjection, viewMatrix, camera.Descr.CullMask, draws, p.prepared,
	)]
	survivors := p.culler.survivors[cull.first : cull.first+cull.count]
	eye := cameraPosition(camera.Descr.Transform)
	// Lights are culled per pass, against this pass's frustum, and capped at
	// the eye, before anything is packed. A camera's shadow pass and screen
	// pass may end up with different light sets, which is correct.
	p.lights.selectLights(cull.frustum, eye.Vec3(), camera.Descr.CullMask, p.preparedLights)
	block := packFrameLighting(sceneFrameBlock{
		View:           viewMatrix,
		Projection:     projectionMatrix,
		ViewProjection: viewProjection,
		CameraPosition: eye,
		ViewDirection:  types.ViewDirection(camera.Descr),
		LightCount:     uint32(p.lights.count),
		Lights:         p.lights.lights,
	}, camera.Descr)
	pending := p.build.beginPass(p.passDescr(camera.ID, pass, order), block)
	result := scene.PassView{
		CameraID: camera.ID,
		Order:    order,
		Tag:      types.PassTagOf(pass),
		Frustum:  cull.frustum,
		Recorded: cull.recorded,
		Culled:   cull.culled,
		Lights:   p.lights.count,
	}
	// The tag interns once per pass, so no draw in it ever compares a string.
	tag := p.materials.internTag(types.PassTagOf(pass))
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
	// The opaque class packs an instanced call's survivors as one batch; the
	// blend class packs one batch per entry, which is what keeps its back-to-
	// front order intact.
	for _, class := range [2]sortClass{{p.build.opaque, true}, {p.build.blend, false}} {
		for i := 0; i < len(class.entries); {
			run := 1
			if class.batched {
				run = groupRun(class.entries[i:], survivors, draws)
			}
			p.build.worlds = p.build.worlds[:0]
			for _, entry := range class.entries[i : i+run] {
				p.build.worlds = append(p.build.worlds, p.prepared[survivors[entry.draw].draw].world)
			}
			index := survivors[class.entries[i].draw].draw
			prepared := &p.prepared[index]
			material, _ := p.materials.entry(prepared.interned, tag)
			mesh, _ := p.resolveMesh(lookup, write, prepared.mesh)
			p.build.addDraw(pending, mesh, prepared.mesh.ID(), material,
				p.build.worlds, draws[index].PbrRecord(), draws[index].Params, prepared.anim)
			result.Instances += run
			i += run
		}
	}
	p.build.endPass(pending)
	types.OpQueuePublishPass(write, result)
	types.OpQueuePublishBatches(write, p.build.batches)
}

// sortClass is one of the two classes a pass emits, with whether its entries
// batch. Only the opaque class does: a blended instanced draw is N entries
// precisely so that each lands at its own depth.
type sortClass struct {
	entries []sortEntry
	batched bool
}

// groupRun counts the entries at the head of a sorted class that came from one
// instanced call and so pack as a single batch. An ungrouped draw runs alone,
// which is what leaves the deferred collapse of consecutive equal draws
// deferred.
//
// A group's survivors reach here contiguous, and nothing else can land between
// them: the instances of one call share a mesh and a material and therefore a
// sort key, ties break on the recording ordinal, and every other draw was
// recorded wholly before or wholly after the call.
func groupRun(entries []sortEntry, survivors []survivor, draws []types.DrawRecord) int {
	group := draws[survivors[entries[0].draw].draw].Group
	if group == 0 {
		return 1
	}
	run := 1
	for run < len(entries) && draws[survivors[entries[run].draw].draw].Group == group {
		run++
	}
	return run
}

// cameraPosition reads the eye out of a camera's transform. Scale is ignored
// the way the view matrix ignores it: a scaled camera scales the world instead,
// and its position is unaffected either way.
func cameraPosition(transform m.Transform) m.Vec4 {
	eye := transform.Mat4().Translation()
	return m.Vec4{X: eye.X, Y: eye.Y, Z: eye.Z, W: 1}
}

// passDescr translates one scene pass into the gfx pass it emits.
//
// Store ops are inferred rather than exposed. Depth is kept iff the pass names
// an explicit depth texture — you allocated it, you mean to sample it — and
// discarded otherwise, so every forward pass gets the tiled-GPU depth-discard
// win for free and there is no knob to set wrong. Colour is always kept.
func (p *plugin) passDescr(id scene.CameraID, pass scene.Pass, order gfx.Order) gfx.PassDescr {
	desc := gfx.PassDescr{
		Order:      order,
		Target:     pass.Target,
		Depth:      pass.Depth,
		DepthStore: gfx.StoreDiscard,
		Label:      p.label(id, types.PassTagOf(pass)),
	}
	if pass.Depth.IsTexture() {
		desc.DepthStore = gfx.StoreKeep
	}
	if color, ok := pass.ClearColor.Get(); ok {
		desc.Load, desc.Clear = gfx.LoadClear, color
	}
	if depth, ok := pass.ClearDepth.Get(); ok {
		desc.DepthLoad, desc.DepthClear = gfx.LoadClear, depth
	}
	return desc
}

func (p *plugin) label(id scene.CameraID, tag scene.PassTag) string {
	key := passLabel{camera: id, tag: tag}
	label, ok := p.labels[key]
	if !ok {
		label = "scene.camera" + strconv.Itoa(int(id)) + "." + string(tag)
		p.labels[key] = label
	}
	return label
}

// forwardMaterial wraps one of model's forward gfx materials as a scene
// material serving only the forward pass, in the frame's own arena. The result
// is a one-entry window of the arena, so a later append can never grow into it.
func (p *plugin) forwardMaterial(descr gfx.MaterialDescr) scene.Material {
	start := len(p.forward)
	p.forward = append(p.forward, scene.MaterialTag{Tag: scene.TagForward, Descr: descr})
	return p.forward[start : start+1 : start+1]
}
