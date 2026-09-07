package scene

import (
	"errors"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
)

// modelState is one path's residency. It is a state rather than an absence
// precisely so that an in-flight load is distinguishable from a path nobody has
// asked for: a model drawn every frame while it loads must enqueue exactly one
// command.
type modelState uint8

const (
	// modelMissing is a path the table has no entry for at all. It is the zero
	// value so that a map miss reads as missing without a second test.
	modelMissing modelState = iota
	modelLoading
	modelResident
	// modelFailed is terminal. It never retries, and it clears only on unload -
	// a typo'd path must not spawn a load command every frame forever.
	modelFailed
)

// modelEntry is one path's slot in the model table.
type modelEntry struct {
	state modelState
	// generation makes an unload while a load is in flight discard the
	// completing load rather than let it become resident as a ghost.
	generation uint32
	// primitives is the flattened, subtree-contiguous list a draw expands into,
	// and materials the records they bind, both owned by this entry and shared
	// by every draw of the path.
	primitives []modelPrimitive
	materials  []modelMaterial
	lights     []ModelLight
	// scenes mirrors the file's scenes array, each entry naming the range of
	// primitives it flattened to and the nodes within it a selector can
	// address; defaultScene is the one an empty Scene selector takes. Both are
	// taken from the load whole, because neither holds a GPU handle.
	scenes       []loadedScene
	defaultScene int
	// neverCull is set when a primitive's POSITION accessor declared no
	// min/max, which leaves the whole model with no bound to cull against.
	neverCull bool
	// animation is the model's baked animation once it is resident: the clip
	// table the packer reads and the two group 2 buffers its draws bind.
	animation residentAnimation
}

// modelPrimitive is one flattened primitive as the flush draws it.
type modelPrimitive struct {
	mesh MeshRef
	// local places the primitive under the scene root. A draw's own Transform
	// multiplies it, and the product is the draw record's world matrix.
	local    m.Mat4
	material int
	// bounds is the primitive's own local-space sphere, from the POSITION
	// accessor's declared min/max, before local is applied - the world matrix
	// carries that, and applying it here as well would place the sphere twice.
	bounds m.Sphere
	// skinned reports whether the primitive draws through the pose buffer. A
	// skinned primitive is culled by its bind-pose sphere or not at all: where
	// the joints put it this frame is not knowable without replaying the
	// blend, so it is marked never-cull and the sphere is kept only for the
	// blend sort's depth.
	skinned bool
}

// modelMaterial is one converted glTF material: the scene material a draw binds
// and the per-batch record that carries its numbers.
type modelMaterial struct {
	material Material
	record   scenePbrRecord
}

// modelReportKey and textureReportKey are the report-once keys the load fires
// under, cleared on a successful load and on unload - canvas's precedent.
func modelReportKey(path string) string   { return "model:" + path }
func textureReportKey(path string) string { return "texture:" + path }

// requestModel returns one path's entry when it is resident and enqueues a load
// when it is not. It is the one way a path enters residency, so a draw and a
// query reach the same command and Preload is an optimisation rather than a
// step a caller can forget.
//
// A loading path enqueues nothing: the entry is the record that a command is
// already in flight. A failed path enqueues nothing either, and that is the
// whole of "never retries".
func (l *Lookup) requestModel(k kernel.Kernel, path string) (*modelEntry, bool) {
	entry, ok := l.models[path]
	if !ok {
		if l.models == nil {
			l.models = map[string]*modelEntry{}
		}
		entry = &modelEntry{generation: 1}
		l.models[path] = entry
	}
	switch entry.state {
	case modelResident:
		return entry, true
	case modelLoading, modelFailed:
		return nil, false
	}
	entry.state = modelLoading
	k.ExecuteCommandAsync[loadModelCmd](loadModelRequest{
		Path: path, Generation: entry.generation, SampleRate: l.config.PoseSampleRate,
	})
	return nil, false
}

// installModel takes one completed load into residency, or records the failure
// that ended it. It runs holding the Lookup and the resource queue, and it is
// the only place either is touched by a load.
//
// Residency is atomic: geometry, material records and every one of the model's
// textures are uploaded in this one call, so there is no frame in which half a
// model is drawn.
func (l *Lookup) installModel(
	report func(error), path string, generation uint32,
	loaded *loadedModel, failure error, resources *gfx.ResourceQueue,
) {
	entry, ok := l.models[path]
	// An unload while the load was in flight bumped the generation, so this
	// result belongs to a model nobody asked for any more and is dropped rather
	// than installed as a ghost.
	if !ok || entry.generation != generation || entry.state != modelLoading {
		return
	}
	if failure != nil {
		entry.state = modelFailed
		l.reportOnce(report, modelReportKey(path), ErrModelUnavailable{Model: path, Err: failure})
		return
	}
	// A load that completes before the backend is installed goes back to
	// missing rather than to failed: there is nothing wrong with the file, and
	// the next draw should try again.
	if !resources.Ready() {
		entry.state = modelMissing
		return
	}
	defaults := l.ensureDefaults(resources)
	textures := l.residentTextures(loaded, resources)
	entry.materials = entry.materials[:0]
	for i := range loaded.materials {
		entry.materials = append(entry.materials,
			bindModelMaterial(&loaded.materials[i], textures, defaults))
	}
	// Geometry is baked once per distinct glTF primitive and placed once per
	// node that referenced it, so a mesh two nodes share is one upload and one
	// mesh id - which also means the two placements sort together.
	meshes := make([]MeshRef, len(loaded.geometries))
	for i := range loaded.geometries {
		meshes[i] = l.bakeModelGeometry(&loaded.geometries[i], resources)
	}
	entry.primitives = entry.primitives[:0]
	for i := range loaded.primitives {
		primitive := &loaded.primitives[i]
		geometry := &loaded.geometries[primitive.geometry]
		placed := modelPrimitive{
			mesh: meshes[primitive.geometry], local: primitive.local, material: primitive.material,
			skinned: primitive.skinned,
		}
		if geometry.hasBox {
			// The sphere stays in the primitive's own space, unflattened,
			// because the draw record's world matrix already folds localMatrix
			// in - so the culler applies it exactly once, through the same
			// resolveBounds path a buffer-built mesh takes.
			placed.bounds = geometry.box.Sphere()
		}
		entry.primitives = append(entry.primitives, placed)
	}
	entry.lights = loaded.lights
	entry.scenes, entry.defaultScene = loaded.scenes, loaded.defaultScene
	entry.neverCull = loaded.neverCull
	entry.animation = l.residentAnimation(loaded, resources)
	entry.state = modelResident
	// A successful load clears the model's report key, so a path that failed,
	// was unloaded and now loads reports again if it breaks again.
	delete(l.reported, modelReportKey(path))
	l.reportLoad(report, path, loaded.reports)
}

// reportLoad fires one load's non-fatal reports under the key each belongs to.
// A texture keys on the texture rather than on the model, so two models naming
// one broken image report it once between them - which is the whole reason the
// keys are two namespaces rather than one.
func (l *Lookup) reportLoad(report func(error), path string, reports []error) {
	var model []error
	for _, err := range reports {
		var texture ErrModelTextureUnavailable
		if errors.As(err, &texture) {
			l.reportOnce(report, textureReportKey(texture.Texture), err)
			continue
		}
		model = append(model, err)
	}
	l.reportOnce(report, modelReportKey(path), model...)
}

// residentTextures uploads the model's decoded images, skipping any key the
// cache already holds. The cache is consulted here rather than during the parse
// because the parse holds no Lookup lock; the cost is that two models sharing
// an external image path both decode it and only the first uploads it.
func (l *Lookup) residentTextures(
	loaded *loadedModel, resources *gfx.ResourceQueue,
) []gfx.TextureDescr {
	if l.textures == nil {
		l.textures = map[textureKey]gfx.TextureDescr{}
	}
	resident := make([]gfx.TextureDescr, len(loaded.textures))
	for i := range loaded.textures {
		texture := &loaded.textures[i]
		if existing, ok := l.textures[texture.key]; ok {
			resident[i] = existing
			continue
		}
		// A texture that loads clears its own report key, so an image that was
		// missing and has since been added reports again if it breaks again.
		delete(l.reported, textureReportKey(textureReportPath(texture.key)))
		// Mips are generated at bake by the backend's CPU box filter, which is
		// the whole of the no-mipmap-generation-API gap: a minified model
		// texture without them aliases into noise the moment the camera moves.
		descr := resources.BakeTexture(
			texture.width, texture.height, texture.format, texture.pixels, false, true)
		l.textures[texture.key] = descr
		resident[i] = descr
	}
	return resident
}

// bindModelMaterial builds the scene material one converted glTF material draws
// with: the bundled shader, the pipeline state its alphaMode, doubleSided and
// winding produced, and all ten of the shader's texture and sampler bindings.
//
// All ten, always: WGSL requires every declared binding bound and gfx does no
// preprocessing, so an empty slot binds a default rather than being omitted.
// That is the same rule the bundled PBR follows, and the reason there are only
// two default textures rather than five.
func bindModelMaterial(
	loaded *loadedMaterial, textures []gfx.TextureDescr, defaults pbrDefaults,
) modelMaterial {
	params := make([]gfx.ParameterDescr, 0, 2*pbrSlotCount)
	for slot, name := range pbrSlots {
		texture := defaults.white
		if slot == normalSlot {
			texture = defaults.flatNormal
		}
		if index := loaded.slots[slot]; index >= 0 && index < len(textures) {
			texture = textures[index]
		}
		params = append(params,
			gfx.TextureParam(name.texture, texture),
			gfx.SamplerParam(name.sampler, loaded.samplers[slot]),
		)
	}
	return modelMaterial{
		record: loaded.record,
		material: Material{{
			Tag: TagForward,
			Descr: gfx.MaterialWithState(
				gfx.ShaderWithResource(sceneShaderPath), loaded.state, params...),
		}},
	}
}

// bakeModelGeometry uploads one converted primitive and claims a durable mesh
// slot for it.
//
// Model geometry lives in the same mesh table as BakeMesh's, rather than in a
// table of its own, because the sort key is a dense meshID and BatchView
// reports one: a second id space would either collide in the key or need a
// third source bit. It also means unloading a model releases its meshes through
// the machinery that already exists.
//
// The bounding sphere is not recomputed from the vertices: glTF requires the
// POSITION accessor to declare min/max, and that box is both authoritative and
// already read. A pass over the vertices would produce the same answer at O(n).
func (l *Lookup) bakeModelGeometry(
	geometry *gltfGeometry, resources *gfx.ResourceQueue,
) MeshRef {
	layoutID, layout, _ := l.layouts.resolve[Vertex]()
	// The bytes are handed over rather than copied: the converted geometry is
	// this load's private copy and nothing reads it again, so a second copy
	// would double a model's peak memory for no reader.
	record := meshRecord{
		vertices:    resources.BakeBuffer(uploadBytes(geometry.vertices), false),
		vertexCount: len(geometry.vertices),
		indexCount:  len(geometry.indices),
		topology:    geometry.topology,
		layout:      layout,
		layoutID:    layoutID,
		standard:    true,
		baked:       true,
	}
	if record.indexCount > 0 {
		record.indices = resources.BakeBuffer(indexBytes(geometry.indices), false)
		record.indexed = true
	}
	return l.claimMesh(record)
}

// ensureDefaults bakes the two 1x1 textures every empty slot binds, once. It is
// the half of ensureBundled a model load needs: a model builds its own
// materials but shares those defaults.
func (l *Lookup) ensureDefaults(resources *gfx.ResourceQueue) pbrDefaults {
	if l.hasDefaults {
		return l.defaults
	}
	l.defaults = pbrDefaults{
		white:      resources.BakeTexture(1, 1, gfx.FormatRGBA8, []byte{0xff, 0xff, 0xff, 0xff}, true, false),
		flatNormal: resources.BakeTexture(1, 1, gfx.FormatRGBA8, []byte{0x80, 0x80, 0xff, 0xff}, true, false),
	}
	l.hasDefaults = true
	return l.defaults
}

// reportOnce fires a burst of reports under one key, or drops it because the
// key has already reported. The burst is gated as a whole rather than per
// error, so a model that reports a missing texture and an unbounded primitive
// reports both facts, while a second load of the same path repeats neither.
func (l *Lookup) reportOnce(report func(error), key string, errs ...error) {
	if len(errs) == 0 {
		return
	}
	if l.reported == nil {
		l.reported = map[string]struct{}{}
	}
	if _, done := l.reported[key]; done {
		return
	}
	l.reported[key] = struct{}{}
	for _, err := range errs {
		report(err)
	}
}

// ModelLights appends the KHR_lights_punctual lights the model at path
// declares, in its own space, and reports whether they are real. A path that is
// not resident yet returns dst untouched and triggers the same load a draw
// does, so a caller who polls this on successive frames eventually gets an
// answer rather than polling an empty list forever.
//
// Nothing converts a light automatically. This is the read an app makes once at
// startup to place a file's lamps as its own PointLight and SpotLight calls,
// which is why it is a cold dst-append rather than something on the per-frame
// path. It joins the rest of the lookup facade when that lands.
func (la LookupAccess) ModelLights(path string, dst []ModelLight) ([]ModelLight, bool) {
	if !la.Valid() {
		return dst, false
	}
	entry, ok := la.lookup.requestModel(la.kernel, path)
	if !ok {
		return dst, false
	}
	return append(dst, entry.lights...), true
}

// Preload loads the model at path without drawing it, so an app can move a
// decode into a loading screen it controls. It is the same idempotent command a
// draw fires: preloading a resident, loading or failed path does nothing.
func (la LookupAccess) Preload(path string) {
	if la.Valid() {
		la.lookup.requestModel(la.kernel, path)
	}
}

// residentAnimation uploads a model's baked poses and joint records and keeps
// the clip table the packer reads every frame.
//
// A model with no joints uploads nothing and every one of its draws binds the
// null skin. That is the common case - a static prop - and it is why the two
// buffers are per model rather than a shared arena everything indexes into.
func (l *Lookup) residentAnimation(
	loaded *loadedModel, resources *gfx.ResourceQueue,
) residentAnimation {
	baked := &loaded.animation
	resident := residentAnimation{
		jointCount: baked.jointCount,
		sampleRate: l.config.PoseSampleRate,
		clips:      baked.clips,
		jointNames: baked.jointNames,
	}
	if baked.jointCount == 0 {
		return resident
	}
	resident.poseBytes = len(baked.poses) * poseSize
	resident.skinJointBytes = len(baked.joints) * skinJointSize
	// The bytes are handed over rather than copied: this is the load's private
	// copy and nothing reads it again - except where a re-root has to follow a
	// moving bone, which is what poseRows is, and which almost no file needs.
	resident.poses = resources.BakeBuffer(recordSliceBytes(baked.poses), false)
	resident.skinJoints = resources.BakeBuffer(recordSliceBytes(baked.joints), false)
	if needsAnimatedReroot(loaded) {
		resident.poseRows = baked.poses
	}
	return resident
}

// needsAnimatedReroot reports whether any node a selector can address has an
// animated ancestor. Almost no file does, and the answer is what decides
// whether the model keeps a CPU copy of its pose rows at all.
func needsAnimatedReroot(loaded *loadedModel) bool {
	for i := range loaded.scenes {
		for _, node := range loaded.scenes[i].nodes {
			if len(node.animated) > 0 {
				return true
			}
		}
	}
	return false
}

// Joints appends the names of the model's joints, in the model's own joint
// order, and reports whether they are real. A path that is not resident yet
// returns dst untouched and triggers the same load a draw does.
//
// Names only, no hierarchy. A joint's parent is a question about the file's
// node graph, which scene drops at load: the pose buffer holds bone world
// transforms, so nothing downstream needs the tree, and exposing one would
// mean keeping it resident for a reader that does not exist yet.
//
// An unnamed joint contributes an empty string rather than being skipped, so
// the slice stays indexed by joint rather than searched.
func (la LookupAccess) Joints(path string, dst []string) ([]string, bool) {
	if !la.Valid() {
		return dst, false
	}
	entry, ok := la.lookup.requestModel(la.kernel, path)
	if !ok {
		return dst, false
	}
	return append(dst, entry.animation.jointNames...), true
}

// Clips appends the model's animation clips, name and duration, and reports
// whether they are real.
//
// Duration is here rather than left to the caller because a caller needs it to
// know when a one-shot play has ended, and that is the one piece of clip state
// gameplay cannot compute for itself: a play carries a time the caller
// advanced, and only the clip knows how long it runs.
func (la LookupAccess) Clips(path string, dst []ClipInfo) ([]ClipInfo, bool) {
	if !la.Valid() {
		return dst, false
	}
	entry, ok := la.lookup.requestModel(la.kernel, path)
	if !ok {
		return dst, false
	}
	for i := range entry.animation.clips {
		dst = append(dst, ClipInfo{
			Name:     entry.animation.clips[i].name,
			Duration: entry.animation.clips[i].duration,
		})
	}
	return dst, true
}

// PoseBytes reports how much GPU memory one model's baked poses occupy, and
// whether the answer is real. It is the number to look at when a rig's storage
// surprises you: it is joints x frames x 48 bytes, so it scales with the sample
// rate and with the clips a file carries, not with what is playing.
//
// The per-joint records are not in it. They are one small array per model
// rather than the per-frame cost this query exists to make visible.
func (la LookupAccess) PoseBytes(path string) (int, bool) {
	if !la.Valid() {
		return 0, false
	}
	entry, ok := la.lookup.requestModel(la.kernel, path)
	if !ok {
		return 0, false
	}
	return entry.animation.poseBytes, true
}

// TotalPoseBytes reports the baked pose memory of every resident model. It
// triggers no load and has no ok: it is a sum over what is resident now, and
// zero is a true answer when nothing is.
func (la LookupAccess) TotalPoseBytes() int {
	if !la.Valid() {
		return 0
	}
	total := 0
	for _, entry := range la.lookup.models {
		if entry.state == modelResident {
			total += entry.animation.poseBytes
		}
	}
	return total
}
