package scene

import (
	"errors"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
)

// ModelState is one path's residency. It is a state rather than an absence
// precisely so that an in-flight load is distinguishable from a path nobody has
// asked for: a model drawn every frame while it loads must enqueue exactly one
// command.
type ModelState uint8

const (
	// ModelMissing is a path the table has no entry for at all. It is the zero
	// value so that a map miss reads as missing without a second test.
	ModelMissing ModelState = iota
	ModelLoading
	ModelResident
	// ModelFailed is terminal. It never retries, and it clears only on unload -
	// a typo'd path must not spawn a load command every frame forever.
	ModelFailed
)

// modelEntry is one path's slot in the model table.
type modelEntry struct {
	state ModelState
	// generation makes an unload while a load is in flight discard the
	// completing load rather than let it become resident as a ghost.
	generation uint32
	// primitives is the flattened, subtree-contiguous list a draw expands into,
	// and materials the records they bind, both owned by this entry and shared
	// by every draw of the path.
	primitives []modelPrimitive
	materials  []modelMaterial
	lights     []ModelLight
	// boxes are the primitives' own local-space axis-aligned boxes, parallel to
	// primitives, and are what AABB reports and Bounds is derived from. They sit
	// beside modelPrimitive rather than inside it because a primitive is read
	// once per instance per frame on the expansion path and these are read only
	// by the cold facade.
	boxes []modelBox
	// meshes are the distinct mesh slots the load claimed, one per glTF
	// primitive rather than one per placement. An unload frees these, and it has
	// to be this list rather than a walk of primitives: two nodes sharing a mesh
	// hold the same ref, and releasing it twice would retire whatever slot the
	// first release handed back.
	meshes []MeshRef
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
	//
	// It is the placement's answer, not the mesh's. The same converted mesh is
	// plain-bound under one node and static under another, so this is the only
	// place either reader - the no-skin flag and the cull exemption - can ask.
	skinned bool
	// joint is the model joint a plain-bound placement rides at full weight,
	// and plain says it is one. The instance record carries it, so the mesh
	// under it is the mesh every other node referencing it draws.
	joint uint32
	plain bool
	// morph is where the primitive's delta block sits and which of the model's
	// weight slots feed it, empty for a primitive with no targets. Morphing
	// needs no cull exemption of its own: the bounds were expanded at load by
	// the reach the deltas can pull a vertex.
	morph morphBinding
}

// modelMaterial is one converted glTF material: the scene material a draw binds,
// once per shader variant, and the per-batch record that carries its numbers.
//
// One glTF material serves whatever primitives reference it, and what a
// primitive deforms is not the material's business - so the variant is picked
// per primitive, from the same bindings that primitive's draw supplies, and the
// material carries all four rather than deciding.
type modelMaterial struct {
	variants [variantCount]Material
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
//
// An invalid path is failed here, synchronously, rather than by the load. It
// never reaches a load command at all, so the report-from-the-goroutine rule
// cannot see it, and without this every query on it would return a silent false
// forever. One state machine, rather than a set of bad strings beside it.
func (l *Lookup) requestModel(k kernel.Kernel, path string) (*modelEntry, bool) {
	key, valid := modelKey(path)
	entry := l.modelEntry(key)
	if !valid {
		// Only a missing entry is failed, so the second query neither reports
		// again nor rewrites a state an unload has since reset.
		if entry.state == ModelMissing {
			entry.state = ModelFailed
			l.reportOnce(func(err error) { k.ReportError(err) },
				modelReportKey(key), ErrModelPathInvalid{Model: path})
		}
		return nil, false
	}
	switch entry.state {
	case ModelResident:
		return entry, true
	case ModelLoading, ModelFailed:
		return nil, false
	}
	entry.state = ModelLoading
	k.ExecuteCommandAsync[loadModelCmd](loadModelRequest{
		Path: key, Generation: entry.generation, SampleRate: l.config.PoseSampleRate,
	})
	return nil, false
}

// modelEntry returns the table slot for a key, minting a missing one. The
// generation starts at one so that zero stays "no entry ever existed", and it
// only ever climbs: an unload resets the slot rather than deleting it, because
// a fresh slot would restart the count and let a load still in flight from
// before the unload install into it as a ghost.
func (l *Lookup) modelEntry(key string) *modelEntry {
	if entry, ok := l.models[key]; ok {
		return entry
	}
	if l.models == nil {
		l.models = map[string]*modelEntry{}
	}
	entry := &modelEntry{generation: 1}
	l.models[key] = entry
	return entry
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
	if !ok || entry.generation != generation || entry.state != ModelLoading {
		return
	}
	if failure != nil {
		entry.state = ModelFailed
		l.reportOnce(report, modelReportKey(path), ErrModelUnavailable{Model: path, Err: failure})
		return
	}
	// A load that completes before the backend is installed goes back to
	// missing rather than to failed: there is nothing wrong with the file, and
	// the next draw should try again.
	if !resources.Ready() {
		entry.state = ModelMissing
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
	entry.meshes = append(entry.meshes[:0], meshes...)
	entry.primitives = entry.primitives[:0]
	entry.boxes = entry.boxes[:0]
	for i := range loaded.primitives {
		primitive := &loaded.primitives[i]
		geometry := &loaded.geometries[primitive.geometry]
		placed := modelPrimitive{
			mesh: meshes[primitive.geometry], local: primitive.local, material: primitive.material,
			skinned: primitive.skinned, joint: primitive.joint,
			plain: primitive.plain, morph: primitive.morph,
		}
		entry.boxes = append(entry.boxes,
			modelBox{box: geometry.box, rest: primitive.rest, known: geometry.hasBox})
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
	entry.state = ModelResident
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
	built := modelMaterial{record: loaded.record}
	for variant := range built.variants {
		// One params slice serves all four: only the shader differs.
		built.variants[variant] = Material{{
			Tag:   TagForward,
			Descr: gfx.MaterialWithState(shaderVariant(variant).shader(), loaded.state, params...),
		}}
	}
	return built
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
// The bounding sphere is not taken from the pack even though the pack computes
// one: glTF requires the POSITION accessor to declare min/max, and that box is
// both authoritative and already read, so a model's bounds come from the file
// on this path and from the walk on the buffer-built one.
func (l *Lookup) bakeModelGeometry(
	geometry *gltfGeometry, resources *gfx.ResourceQueue,
) MeshRef {
	// Which of the two named layouts this geometry stores in was decided by the
	// flattening walk, once per geometry: the skinned one iff some placement
	// draws it under SCENE_SKIN. Both are resolved through the same layout
	// cache every other mesh takes, so each gets a dense id of its own and an
	// UpdateMesh comparing ids still compares one integer.
	var layoutID int
	var layout []gfx.VertexAttr
	if geometry.skinnedLayout {
		layoutID, layout, _ = l.layouts.resolve[skinnedVertex]()
	} else {
		layoutID, layout, _ = l.layouts.resolve[Vertex]()
	}
	// The UV ranges were accumulated while the accessors were being read, so
	// the record is in hand before the pack needs it and the pack stays the one
	// walk this path makes over the vertices.
	uv := meshRecordFor(geometry.uv0, geometry.uv1)
	// The converted vertices are packed over themselves and the bytes are then
	// handed over rather than copied. This load's vertices are its private copy
	// and nothing reads them again, so packing into a second buffer would hold
	// both forms of every primitive at once - which is the peak memory the
	// reinterpret this replaced was avoiding. A storage vertex is smaller than
	// a converted one under either layout, so the pack fits inside the memory
	// it reads.
	record := meshRecord{
		vertices: resources.BakeBuffer(
			packOverAuthored(geometry.vertices, uv, geometry.skinnedLayout), false),
		vertexCount: len(geometry.vertices),
		indexCount:  len(geometry.indices),
		topology:    geometry.topology,
		indexWidth:  indexWidthFor(len(geometry.vertices)),
		layout:      layout,
		layoutID:    layoutID,
		standard:    true,
		baked:       true,
		uv:          uv,
	}
	// The narrowing is the one O(n) pass this path adds, and it replaces no
	// walk: the width itself costs a comparison, because a glTF accessor's
	// indices are below the vertex count by construction and need no scan.
	if record.indexCount > 0 {
		record.indices = resources.BakeBuffer(indexBytes(geometry.indices, record.indexWidth), false)
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
// A model with no joints uploads nothing and every one of its draws declares no
// pose bindings to fill. That is the common case - a static prop - and it is why
// the two buffers are per model rather than a shared arena everything indexes
// into.
func (l *Lookup) residentAnimation(
	loaded *loadedModel, resources *gfx.ResourceQueue,
) residentAnimation {
	baked := &loaded.animation
	resident := residentAnimation{
		jointCount:  baked.jointCount,
		sampleRate:  l.config.PoseSampleRate,
		clips:       baked.clips,
		jointNames:  baked.jointNames,
		slotCount:   baked.slotCount,
		weights:     baked.weights,
		targetNames: baked.targetNames,
	}
	// The delta buffer is uploaded whether or not the model has joints: a
	// morph-only face has no pose row anywhere and still binds its own shapes.
	if len(loaded.morphDeltas) > 0 {
		resident.morphBytes = len(loaded.morphDeltas) * morphRecordSize
		resident.morphDeltas = resources.BakeBuffer(recordSliceBytes(loaded.morphDeltas), false)
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
		if entry.state == ModelResident {
			total += entry.animation.poseBytes
		}
	}
	return total
}

// MorphTargets appends the names of the model's morph targets, in the flattened
// order MorphWeights is positional over, and reports whether they are real. A
// path that is not resident yet returns dst untouched and triggers the same load
// a draw does.
//
// The list is one entry per target of every morphed node in depth-first node
// order, so a file whose head mesh hangs off two nodes appears here as two runs
// of the same names - which is exactly what MorphWeights addresses, because the
// weights are the node's while the deltas behind them stay shared.
//
// An unnamed target contributes an empty string rather than being skipped: glTF
// carries target names only as the extras convention, so a file that names none
// still has to keep the slice indexed by slot rather than searched.
func (la LookupAccess) MorphTargets(path string, dst []string) ([]string, bool) {
	if !la.Valid() {
		return dst, false
	}
	entry, ok := la.lookup.requestModel(la.kernel, path)
	if !ok {
		return dst, false
	}
	return append(dst, entry.animation.targetNames...), true
}

// MorphBytes reports how much GPU memory one model's morph deltas occupy, and
// whether the answer is real.
//
// It is the number to look at when a morphed model's storage surprises you: it
// is targets x vertices x 16 x popcount(mask), so it scales with the shapes a
// file carries and with how many attributes each deforms, not with what is
// playing. The weight grid is not in it - that never reaches the GPU.
func (la LookupAccess) MorphBytes(path string) (int, bool) {
	if !la.Valid() {
		return 0, false
	}
	entry, ok := la.lookup.requestModel(la.kernel, path)
	if !ok {
		return 0, false
	}
	return entry.animation.morphBytes, true
}

// TotalMorphBytes reports the morph delta memory of every resident model. It
// triggers no load and has no ok: it is a sum over what is resident now, and
// zero is a true answer when nothing is.
func (la LookupAccess) TotalMorphBytes() int {
	if !la.Valid() {
		return 0
	}
	total := 0
	for _, entry := range la.lookup.models {
		if entry.state == ModelResident {
			total += entry.animation.morphBytes
		}
	}
	return total
}
