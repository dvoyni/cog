package types

import (
	"errors"
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// ModelDescrParams is empty, and that is the whole statement: a model's path is
// its only cache key.
//
// The pose sample rate used to ride the load request, for the stated reason
// that the parse held no Lookup. The load runs inside the cache now and the
// Lookup is in hand, so the rate comes off the configuration and never enters
// the key - which is right, because it is startup configuration fixed for the
// Lookup's life and a key carrying it would have exactly one value forever.
type ModelDescrParams struct{}

// modelDescr names one model file: the path, and nothing else there is to say.
type modelDescr = assets.Descr[ModelDescrParams]

// modelUserData is what the model loader needs that a handler holds: the Lookup it
// mints mesh slots and texture entries in, and the resource queue it uploads
// through. A loader is stateless and long-lived, so both arrive per call and
// neither is retained.
//
// path rides here too, and it is the one member that is not a handler's. The
// Library hands Load the bytes and the parameters but not the descriptor, and
// the parse needs the path three times over - to resolve a .gltf file's
// relative buffer and image URIs against its own directory, to key the textures
// it decodes, and to name the model in every error it produces. So the caller
// that spells the descriptor spells the path here as well; Free and Default
// never read it.
type modelUserData struct {
	lookup    *Lookup
	resources *gfx.ResourceQueue
	path      string
}

// modelLoader decodes one glTF file into a resident model, uploads it, and
// releases one. It is stateless: everything it touches arrives through the
// kernel, the filesystem and the user data the cache hands it.
type modelLoader struct{}

// residentModel is one loaded model file. It is what the cache stores, and it
// is immutable once Load returns.
//
// A model that failed to parse is an otherwise-zero value carrying err, cached
// like any other so the parse runs one time per path until a free. A model
// whose file could not be read at all is not this type but the loader's nil
// default, because the Library owns that read and its failure.
type residentModel struct {
	// err is the load's own failure, and non-nil makes every other member
	// meaningless. It is what State returns, so a HUD prints the reason rather
	// than a state word.
	err error
	// primitives is the flattened, subtree-contiguous list a draw expands into,
	// and materials the records they bind, both owned by this model and shared
	// by every draw of the path.
	primitives []modelPrimitive
	Materials  []modelMaterial
	lights     []ModelLight
	// boxes are the primitives' own local-space axis-aligned boxes, parallel to
	// primitives, and are what AABB reports and Bounds is derived from. They sit
	// beside modelPrimitive rather than inside it because a primitive is read
	// once per instance per frame on the expansion path and these are read only
	// by the cold facade.
	boxes []modelBox
	// meshes are the distinct mesh slots the load claimed, one per glTF
	// primitive rather than one per placement. A free releases these, and it has
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
	// animation is the model's baked animation: the clip table the packer reads
	// and the two group 2 buffers its draws bind.
	animation ResidentAnimation
}

// modelPrimitive is one flattened primitive as the flush draws it.
type modelPrimitive struct {
	Mesh MeshRef
	// Local places the primitive under the scene root. A draw's own Transform
	// multiplies it, and the product is the draw record's world matrix.
	Local    m.Mat4
	Material int
	// Bounds is the primitive's own local-space sphere, from the POSITION
	// accessor's declared min/max, before local is applied - the world matrix
	// carries that, and applying it here as well would place the sphere twice.
	Bounds m.Sphere
	// Skinned reports whether the primitive draws through the pose buffer. A
	// skinned primitive is culled by its bind-pose sphere or not at all: where
	// the joints put it this frame is not knowable without replaying the
	// blend, so it is marked never-cull and the sphere is kept only for the
	// blend sort's depth.
	//
	// It is the placement's answer, not the mesh's. The same converted mesh is
	// plain-bound under one node and static under another, so this is the only
	// place either reader - the no-skin flag and the cull exemption - can ask.
	Skinned bool
	// Joint is the model joint a plain-bound placement rides at full weight,
	// and plain says it is one. The instance record carries it, so the mesh
	// under it is the mesh every other node referencing it draws.
	Joint uint32
	Plain bool
	// Morph is where the primitive's delta block sits and which of the model's
	// weight slots feed it, empty for a primitive with no targets. Morphing
	// needs no cull exemption of its own: the bounds were expanded at load by
	// the reach the deltas can pull a vertex.
	Morph MorphBinding
}

// modelMaterial is one converted glTF material: the forward gfx material a draw
// binds, once per shader variant, and the per-batch record that carries its
// numbers. It names no pass: a renderer wraps the forward material under its
// own tag.
//
// One glTF material serves whatever primitives reference it, and what a
// primitive deforms is not the material's business - so the variant is picked
// per primitive, from the same bindings that primitive's draw supplies, and the
// material carries all four rather than deciding.
type modelMaterial struct {
	Forward [VariantCount]gfx.MaterialDescr
	Record  ScenePbrRecord
}

// modelReportKey and textureReportKey are the keys the load's report-once calls
// name, cleared on unload. The table they key lives on the kernel and is shared
// with every other plugin that keys by string, which is what the two prefixes
// separate. The Library's own read failure is keyed by the descriptor instead,
// which is a type of its own and therefore a namespace of its own.
const (
	modelReportPrefix   = "model:"
	textureReportPrefix = "texture:"
)

func modelReportKey(path string) string   { return modelReportPrefix + path }
func textureReportKey(path string) string { return textureReportPrefix + path }

// errBackendNotReady is the one non-terminal reason a model is not resident: a
// draw or a query arrived before the graphics backend existed, so nothing was
// read, nothing was uploaded and nothing was cached. The next frame asks again,
// which is why it is neither reported nor remembered.
var errBackendNotReady = errors.New("the graphics backend is not ready yet")

// errModelNotRead is why a model whose entry is the loader's nil default is not
// resident. The Library performs the read for a path-named asset and reports
// its own failure once, under the descriptor, and it does not tell the loader
// what went wrong - so what scene can say is that the file was not read, and
// the report the engine already has carries the reason.
var errModelNotRead = errors.New("the file could not be read")

// errLookupUnavailable is what a query answers with when its facade is backed
// by no Lookup at all: the (value, false) every other query returns, spelled as
// the error the one query that returns an error has to return instead.
var errLookupUnavailable = errors.New("the scene lookup is not available")

// model resolves one path to the model drawn for it, loading it if the cache
// holds no entry, and says why there is none when there is none.
//
// This is the one way a path enters residency, so a draw, a query and Preload
// all reach the same load and Preload is an optimisation rather than a step a
// caller can forget. The load is synchronous: the read, the parse, the image
// decodes and every upload finish before this returns, which is the cost this
// design accepts and which Preload is the lever for.
//
// An invalid path never reaches the cache. It is refused here, reported once,
// and leaves no entry and no tombstone behind, so a typo is permanently a typo
// and unloading the string the caller passed is what clears the report.
//
// A backend that is not up yet is the other refusal, and it is the one that
// does not stick: nothing is cached, so the next frame loads for real. Baking
// against an absent backend would panic, and caching the failure would make a
// startup race terminal.
func (l *Lookup) model(
	k kernel.Kernel, fsys fs.FS, resources *gfx.ResourceQueue, modelPath string,
) (*residentModel, error) {
	key, valid := ModelKey(modelPath)
	if !valid {
		err := ErrModelPathInvalid{Model: modelPath}
		k.ReportErrorOnce(modelReportKey(key), err)
		return nil, err
	}
	if resources == nil || !resources.Ready() {
		return nil, ErrModelUnavailable{Model: modelPath, Err: errBackendNotReady}
	}
	loaded := l.models.Get(k, modelDescr{Name: key}, fsys,
		modelUserData{lookup: l, resources: resources, path: key})
	if loaded == nil {
		return nil, ErrModelUnavailable{Model: modelPath, Err: errModelNotRead}
	}
	if loaded.err != nil {
		return nil, loaded.err
	}
	return loaded, nil
}

// ModelView resolves one draw of the model at path under its scene and node
// selectors, loading the model if the cache holds no entry. ok is false when
// no model is resident for the path, and why is State's to report; a selector
// that matches nothing is err, which the caller reports under its ReportKey.
//
// It is the load facade's read for a renderer's flush, which holds the Lookup
// for writing, the filesystem and the resource queue.
func (l *Lookup) ModelView(
	k kernel.Kernel, fsys fs.FS, resources *gfx.ResourceQueue, path, scene, node string,
) (view ModelView, err ModelSelectorError, ok bool) {
	model, loadErr := l.model(k, fsys, resources, path)
	if loadErr != nil {
		return ModelView{}, nil, false
	}
	view, err = model.View(path, scene, node)
	return view, err, true
}

// Load parses one file and takes it into residency, or records the failure that
// ended it. The Library has already read the bytes, so data is the file whole
// and no second open happens here.
//
// Residency is atomic: geometry, material records, every one of the model's
// textures and both animation buffers are uploaded before this returns, so
// there is no frame in which half a model is drawn.
//
// Whatever it returns is cached, a failure included, so a file that does not
// parse is parsed one time until a free - which is the whole of "never
// retries", and UnloadModel followed by Preload is the way back.
func (modelLoader) Load(
	k kernel.Kernel, data assets.Blob, _ ModelDescrParams, fsys fs.FS, userData modelUserData,
) *residentModel {
	loaded, err := parseModel(data, userData.path, fsys, userData.lookup.config.PoseSampleRate)
	if err != nil {
		failure := ErrModelUnavailable{Model: userData.path, Err: err}
		k.ReportErrorOnce(modelReportKey(userData.path), failure)
		return &residentModel{err: failure}
	}
	return userData.lookup.installModel(k, userData.path, loaded, fsys, userData.resources)
}

// Default is nil, and a nil model expands into no primitives, so "skip, never
// substitute" survives as a null object rather than as a special case. Nothing
// is stood in for; the loudness comes from the report the Library has already
// made, not from the pixels.
func (modelLoader) Default(modelDescr, modelUserData) *residentModel { return nil }

// Free gives up one model's geometry, baked poses and morph deltas. The mesh
// slots retire at once, so a ref to one goes stale immediately; their buffers
// join the pending releases the flush drains at the frame boundary, which is
// the same queue ReleaseMesh uses and the reason nothing the frame has already
// recorded draws from a dead buffer.
//
// It does not cascade to textures. With no refcount the Lookup cannot know
// whether another resident model binds the same image by path, and freeing one
// that is still bound is a dead texture in a live bind group rather than a
// missing picture. UnloadTexture is the separate, deliberate lever.
func (modelLoader) Free(value *residentModel, userData modelUserData) {
	if value == nil {
		return
	}
	l := userData.lookup
	for _, ref := range value.meshes {
		l.releaseMesh(ref)
	}
	animation := &value.animation
	if animation.poseBytes > 0 {
		l.pendingReleases = append(l.pendingReleases, animation.poses, animation.skinJoints)
	}
	if animation.morphBytes > 0 {
		l.pendingReleases = append(l.pendingReleases, animation.morphDeltas)
	}
	l.poseBytes -= animation.poseBytes
	l.morphBytes -= animation.morphBytes
}

// parseModel decodes and converts one file's bytes. Everything it returns is
// scene's own types: model's decoder drops the glTF document before it
// returns, so the document never appears in scene's API and never outlives the
// load that read it.
//
// The decoder resolves a .gltf file's external buffers against the model's own
// directory, which is what glTF's relative URIs are relative to. Images are not
// the decoder's business, so the texture loader resolves those itself, against
// the whole filesystem and the full storage path.
func parseModel(
	data assets.Blob, modelPath string, fsys fs.FS, sampleRate int,
) (*LoadedModel, error) {
	decoded, err := DecodeModel(data.Data(), modelPath, fsys)
	if err != nil {
		return nil, err
	}
	return convertDecoded(decoded, modelPath, sampleRate)
}

// installModel uploads one parsed model and builds the value the cache keeps.
//
// It runs inside Load, holding whatever the handler that called Get holds -
// the Lookup and the resource queue, because those are what a device facade
// carries. Nothing here is deferred: the uploads are what make residency
// atomic.
func (l *Lookup) installModel(
	k kernel.Kernel, modelPath string, loaded *LoadedModel,
	fsys fs.FS, resources *gfx.ResourceQueue,
) *residentModel {
	defaults := l.ensureDefaults(resources)
	// The pictures resolve through the texture cache, which is where the
	// double decode went: the parse named them and this is the first thing
	// that asks for them, so an image another model already uploaded is
	// neither read nor decoded here. A Get takes the payload the parse
	// supplied beside an embedded name and ignores it on a hit.
	textures := make([]gfx.TextureDescr, len(loaded.textures))
	for i, descr := range loaded.textures {
		textures[i] = l.textures.Get(k, descr, fsys, textureUserData{
			resources: resources, model: modelPath, name: descr.Name,
		})
	}
	model := &residentModel{
		Materials:    make([]modelMaterial, 0, len(loaded.materials)),
		lights:       loaded.lights,
		scenes:       loaded.scenes,
		defaultScene: loaded.defaultScene,
		neverCull:    loaded.neverCull,
	}
	for i := range loaded.materials {
		model.Materials = append(model.Materials,
			bindModelMaterial(&loaded.materials[i], textures, defaults))
	}
	// Geometry is baked once per distinct glTF primitive and placed once per
	// node that referenced it, so a mesh two nodes share is one upload and one
	// mesh id - which also means the two placements sort together.
	meshes := make([]MeshRef, len(loaded.geometries))
	for i := range loaded.geometries {
		meshes[i] = l.bakeModelGeometry(&loaded.geometries[i], resources)
	}
	model.meshes = meshes
	model.primitives = make([]modelPrimitive, 0, len(loaded.primitives))
	model.boxes = make([]modelBox, 0, len(loaded.primitives))
	for i := range loaded.primitives {
		primitive := &loaded.primitives[i]
		geometry := &loaded.geometries[primitive.geometry]
		placed := modelPrimitive{
			Mesh: meshes[primitive.geometry], Local: primitive.local, Material: primitive.material,
			Skinned: primitive.skinned, Joint: primitive.joint,
			Plain: primitive.plain, Morph: primitive.morph,
		}
		model.boxes = append(model.boxes,
			modelBox{box: geometry.box, rest: primitive.rest, known: geometry.hasBox})
		if geometry.hasBox {
			// The sphere stays in the primitive's own space, unflattened,
			// because the draw record's world matrix already folds localMatrix
			// in - so the culler applies it exactly once, through the same
			// resolveBounds path a buffer-built mesh takes.
			placed.Bounds = geometry.box.Sphere()
		}
		model.primitives = append(model.primitives, placed)
	}
	model.animation = l.residentAnimation(loaded, resources)
	// The two totals are running counters rather than a walk of the table,
	// which is what makes TotalPoseBytes and TotalMorphBytes O(1) and what
	// removes their need for a residency test that no longer exists.
	l.poseBytes += model.animation.poseBytes
	l.morphBytes += model.animation.morphBytes
	l.reportLoad(k, modelPath, loaded.reports)
	return model
}

// reportLoad fires one load's non-fatal reports under the key each belongs to.
// A texture keys on the texture rather than on the model, so two models naming
// one broken image report it once between them - which is the whole reason the
// keys are two namespaces rather than one.
func (l *Lookup) reportLoad(k kernel.Kernel, path string, reports []error) {
	var model []error
	for _, err := range reports {
		var texture ErrModelTextureUnavailable
		if errors.As(err, &texture) {
			k.ReportErrorOnce(textureReportKey(texture.Texture), err)
			continue
		}
		model = append(model, err)
	}
	// The model's own faults go as one burst, gated as a whole: a load that
	// reports a missing texture and an unbounded primitive reports both facts,
	// while a second load of the same path repeats neither. An empty burst
	// claims nothing, so a clean load leaves the key free.
	k.ReportErrorOnce(modelReportKey(path), model...)
}

// bindModelMaterial builds the forward materials one converted glTF material
// draws with: the bundled shader, the pipeline state its alphaMode, doubleSided and
// winding produced, and all ten of the shader's texture and sampler bindings.
//
// All ten, always: WGSL requires every declared binding bound and gfx does no
// preprocessing, so an empty slot binds a default rather than being omitted.
// That is the same rule the bundled PBR follows, and the reason there are only
// two default textures rather than five.
func bindModelMaterial(
	loaded *loadedMaterial, textures []gfx.TextureDescr, defaults PbrDefaults,
) modelMaterial {
	params := make([]gfx.ParameterDescr, 0, 2*pbrSlotCount)
	for slot, name := range PbrSlots {
		texture := defaults.White
		if slot == NormalSlot {
			texture = defaults.FlatNormal
		}
		// A slot whose image did not arrive at all takes the cache's zero
		// entry, which is the placeholder for a data slot: the picture slots
		// get magenta and these keep the white texel and the flat normal,
		// because magenta as a normal map is a surface lit from nowhere.
		if index := loaded.slots[slot]; index >= 0 && index < len(textures) &&
			textures[index].ID() != 0 {
			texture = textures[index]
		}
		params = append(params,
			gfx.TextureParam(name.Texture, texture),
			gfx.SamplerParam(name.Sampler, loaded.samplers[slot]),
		)
	}
	built := modelMaterial{Record: loaded.record}
	for variant := range built.Forward {
		// One params slice serves all four: only the shader differs.
		built.Forward[variant] = gfx.MaterialWithState(
			ShaderVariant(variant).shader(), loaded.state, params...)
	}
	return built
}

// bakeModelGeometry uploads one converted primitive and claims a durable mesh
// slot for it.
//
// Model geometry lives in the same mesh table as BakeMesh's, rather than in a
// table of its own, because the sort key is a dense meshID and BatchView
// reports one: a second id space would either collide in the key or need a
// third source bit. It also means freeing a model releases its meshes through
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
	record := MeshRecord{
		Vertices: resources.BakeBuffer(
			packOverAuthored(geometry.vertices, uv, geometry.skinnedLayout), false),
		VertexCount: len(geometry.vertices),
		indexCount:  len(geometry.indices),
		Topology:    geometry.topology,
		IndexWidth:  indexWidthFor(len(geometry.vertices)),
		Layout:      layout,
		layoutID:    layoutID,
		Standard:    true,
		baked:       true,
		UV:          uv,
	}
	// The narrowing is the one O(n) pass this path adds, and it replaces no
	// walk: the width itself costs a comparison, because a glTF accessor's
	// indices are below the vertex count by construction and need no scan.
	if record.indexCount > 0 {
		record.Indices = resources.BakeBuffer(indexBytes(geometry.indices, record.IndexWidth), false)
		record.Indexed = true
	}
	return l.claimMesh(record)
}

// ensureDefaults bakes the two 1x1 textures every empty slot binds, once. It is
// the half of EnsureBundled a model load needs: a model builds its own
// materials but shares those defaults.
func (l *Lookup) ensureDefaults(resources *gfx.ResourceQueue) PbrDefaults {
	if l.hasDefaults {
		return l.defaults
	}
	l.defaults = PbrDefaults{
		White:      resources.BakeTexture(1, 1, gfx.FormatRGBA8, []byte{0xff, 0xff, 0xff, 0xff}, true, false),
		FlatNormal: resources.BakeTexture(1, 1, gfx.FormatRGBA8, []byte{0x80, 0x80, 0xff, 0xff}, true, false),
	}
	l.hasDefaults = true
	return l.defaults
}

// ModelLights appends the KHR_lights_punctual lights the model at path
// declares, in its own space, and reports whether they are real. A path that
// does not load returns dst untouched.
//
// Nothing converts a light automatically. This is the read an app makes once at
// startup to place a file's lamps as its own PointLight and SpotLight calls,
// which is why it is a cold dst-append rather than something on the per-frame
// path.
func (la LookupDeviceAccess) ModelLights(path string, dst []ModelLight) ([]ModelLight, bool) {
	model, ok := la.resolve(path)
	if !ok {
		return dst, false
	}
	return append(dst, model.lights...), true
}

// Preload loads the model at path without drawing it, so an app can move the
// hitch a synchronous load costs into a loading screen it controls.
//
// It is the same load a draw fires, fired without one, and it is idempotent:
// preloading a resident or a failed path does nothing, because the entry is the
// record that the load already ran.
func (la LookupDeviceAccess) Preload(path string) {
	if la.Valid() {
		la.lookup.model(la.kernel, la.fsys, la.resources, path)
	}
}

// ResidentAnimation uploads a model's baked poses and joint records and keeps
// the clip table the packer reads every frame.
//
// A model with no joints uploads nothing and every one of its draws declares no
// pose bindings to fill. That is the common case - a static prop - and it is why
// the two buffers are per model rather than a shared arena everything indexes
// into.
func (l *Lookup) residentAnimation(
	loaded *LoadedModel, resources *gfx.ResourceQueue,
) ResidentAnimation {
	baked := &loaded.animation
	resident := ResidentAnimation{
		JointCount:  baked.jointCount,
		SampleRate:  l.config.PoseSampleRate,
		Clips:       baked.clips,
		JointNames:  baked.jointNames,
		SlotCount:   baked.slotCount,
		Weights:     baked.weights,
		targetNames: baked.targetNames,
	}
	// The delta buffer is uploaded whether or not the model has joints: a
	// morph-only face has no pose row anywhere and still binds its own shapes.
	if len(loaded.morphDeltas) > 0 {
		resident.morphBytes = len(loaded.morphDeltas) * MorphWordSize
		resident.morphDeltas = resources.BakeBuffer(recordSliceBytes(loaded.morphDeltas), false)
	}
	if baked.jointCount == 0 {
		return resident
	}
	resident.poseBytes = len(baked.poses) * PoseSize
	resident.skinJointBytes = len(baked.joints) * SkinJointSize
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
func needsAnimatedReroot(loaded *LoadedModel) bool {
	for i := range loaded.scenes {
		for _, node := range loaded.scenes[i].Nodes {
			if len(node.Animated) > 0 {
				return true
			}
		}
	}
	return false
}

// Joints appends the names of the model's joints, in the model's own joint
// order, and reports whether they are real. A path that does not load returns
// dst untouched.
//
// Names only, no hierarchy. A joint's parent is a question about the file's
// node graph, which scene drops at load: the pose buffer holds bone world
// transforms, so nothing downstream needs the tree, and exposing one would
// mean keeping it resident for a reader that does not exist yet.
//
// An unnamed joint contributes an empty string rather than being skipped, so
// the slice stays indexed by joint rather than searched.
func (la LookupDeviceAccess) Joints(path string, dst []string) ([]string, bool) {
	model, ok := la.resolve(path)
	if !ok {
		return dst, false
	}
	return append(dst, model.animation.JointNames...), true
}

// Clips appends the model's animation clips, name and duration, and reports
// whether they are real.
//
// Duration is here rather than left to the caller because a caller needs it to
// know when a one-shot play has ended, and that is the one piece of clip state
// gameplay cannot compute for itself: a play carries a time the caller
// advanced, and only the clip knows how long it runs.
func (la LookupDeviceAccess) Clips(path string, dst []ClipInfo) ([]ClipInfo, bool) {
	model, ok := la.resolve(path)
	if !ok {
		return dst, false
	}
	for i := range model.animation.Clips {
		dst = append(dst, ClipInfo{
			Name:     model.animation.Clips[i].Name,
			Duration: model.animation.Clips[i].Duration,
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
func (la LookupDeviceAccess) PoseBytes(path string) (int, bool) {
	model, ok := la.resolve(path)
	if !ok {
		return 0, false
	}
	return model.animation.poseBytes, true
}

// TotalPoseBytes reports the baked pose memory of every resident model. It
// triggers no load and has no ok: it is a sum over what is resident now, and
// zero is a true answer when nothing is.
//
// It is a counter the load adds to and the free subtracts from rather than a
// walk of the table, which is what makes it O(1) - and, because it needs no
// device and no filesystem to answer, what keeps it on the facade an ECS System
// can build.
func (la LookupAccess) TotalPoseBytes() int {
	if !la.Valid() {
		return 0
	}
	return la.lookup.poseBytes
}

// MorphTargets appends the names of the model's morph targets, in the flattened
// order MorphWeights is positional over, and reports whether they are real. A
// path that does not load returns dst untouched.
//
// The list is one entry per target of every morphed node in depth-first node
// order, so a file whose head mesh hangs off two nodes appears here as two runs
// of the same names - which is exactly what MorphWeights addresses, because the
// weights are the node's while the deltas behind them stay shared.
//
// An unnamed target contributes an empty string rather than being skipped: glTF
// carries target names only as the extras convention, so a file that names none
// still has to keep the slice indexed by slot rather than searched.
func (la LookupDeviceAccess) MorphTargets(path string, dst []string) ([]string, bool) {
	model, ok := la.resolve(path)
	if !ok {
		return dst, false
	}
	return append(dst, model.animation.targetNames...), true
}

// MorphBytes reports how much GPU memory one model's morph deltas occupy, and
// whether the answer is real.
//
// It is the number to look at when a morphed model's storage surprises you: it
// is targets x vertices x 16 x popcount(mask), so it scales with the shapes a
// file carries and with how many attributes each deforms, not with what is
// playing. The weight grid is not in it - that never reaches the GPU.
func (la LookupDeviceAccess) MorphBytes(path string) (int, bool) {
	model, ok := la.resolve(path)
	if !ok {
		return 0, false
	}
	return model.animation.morphBytes, true
}

// TotalMorphBytes reports the morph delta memory of every resident model, under
// the same counter rule TotalPoseBytes follows.
func (la LookupAccess) TotalMorphBytes() int {
	if !la.Valid() {
		return 0
	}
	return la.lookup.morphBytes
}
