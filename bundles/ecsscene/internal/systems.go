package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The four Queries the recording System walks. Every field is a read — a value
// field yields a copy — because recording changes nothing about an Entity, and
// each names Transform because an Entity with nowhere to stand is not recorded.
//
// Animation, Params and Material are deliberately not fields. A Query matches
// an Entity having at least the Components it names, so naming an optional one
// would drop every Entity without it out of the walk; they are reached through
// accessors instead, at one probe each, and Params and Material only once per
// Batch.
type (
	modelQuery struct {
		Place m.Transform
		Model ecsscene.Model
	}
	meshQuery struct {
		Place m.Transform
		Mesh  ecsscene.Mesh
	}
	lightQuery struct {
		Place m.Transform
		Light ecsscene.Light
	}
	cameraQuery struct {
		Place  m.Transform
		Camera ecsscene.Camera
	}
)

// errorReporter is what the frame reports through: the kernel, or a sink.
type errorReporter interface{ ReportError(error) }

// scratch is the recording System's frame: every arena, table and list it
// builds a frame in. It is a resource the plugin owns, not something the
// System's closure captured: anything a System keeps between calls is in its
// lock set, so the kernel, not a comment, is what keeps two holders apart.
//
// Every slice keeps its backing across frames, so a steady frame allocates
// nothing after the first.
type scratch struct {
	build     frameBuild
	materials materialTable
	culler    culler
	lights    model.LightSelection
	labels    map[passLabel]string

	// entries is the frame's instances, one per Model primitive and one per
	// Mesh Entity, in walk order; batches is the frame's Batches, and bucket
	// the index of each Batch by its key. A key that cannot draw - a mesh
	// that does not resolve - maps to -1, so it is tried once a frame.
	entries []entry
	batches []batch
	bucket  map[bucketKey]int32

	preparedLights []preparedLight
	cameras        []cameraRecord
	// passes is the frame's Camera passes, which a cameraRecord indexes.
	passes        []ecsscene.Pass
	defaultPasses [1]ecsscene.Pass

	// bundled is the bundled PBR's four variants wrapped as forward
	// materials, and forward the arena they and a model's own materials are
	// wrapped in.
	bundled [model.VariantCount]material
	forward []materialTag
	// tags and tagParams are the arenas a Material Component is copied out
	// into, once per Batch, and params the arena a Params Component is. Each
	// copy is a full-slice window, so no later copy can grow into it.
	tags      []materialTag
	tagParams []gfx.ParameterDescr
	params    []gfx.ParameterDescr

	// The animation resolution, as scene's: plays is one Entity's used
	// Animation slots, modelPlays its folded play records, and the morph
	// half's frames, weights, targets and per-primitive block offsets.
	plays             []model.ClipPlay
	modelPlays        []model.ScenePlayRecord
	modelWeightFrames []model.WeightFrames
	modelWeights      []float32
	modelTargets      []model.SceneMorphWeight
	modelMorphOffsets []uint32

	// meshReported is the set of mesh ids already reported this frame.
	meshReported map[uint32]struct{}
	// k is the kernel of the frame being recorded, and once the report-once
	// callback model's animation resolution takes, built once over it.
	k    kernel.Kernel
	once model.ReportOnce
}

// bucketKey is what the frame buckets instances by: the Batch key the load
// System took, and the shader variant the primitive draws with. The variant
// follows from the key for everything a file draws with its own material; it
// is here so that an override Material over a skinned and an unskinned use of
// one mesh cannot share group 2 bindings only one of them declares.
type bucketKey struct {
	key     batchKey
	variant model.ShaderVariant
}

// batch is one Batch of the frame: everything its instances share, resolved
// once when its first instance is bucketed.
type batch struct {
	mesh model.MeshRecord
	skin model.SkinBuffers
	// pbr is the Batch's bundled-PBR record: the file's own, or white paint,
	// with a Model's Params merged over it.
	pbr    model.ScenePbrRecord
	params []gfx.ParameterDescr
	// interned is the Batch's material's index in the frame's material table.
	interned int32
}

// cameraRecord is one Camera Entity the frame draws for, with its passes as a
// span of scratch.passes.
type cameraRecord struct {
	camera       ecsscene.Camera
	place        m.Transform
	first, count int
}

func newScratch() *scratch {
	s := &scratch{
		labels:       map[passLabel]string{},
		bucket:       map[bucketKey]int32{},
		meshReported: map[uint32]struct{}{},
		plays:        make([]model.ClipPlay, 0, model.MaxClipPlays),
	}
	s.once = s.reportOnce
	return s
}

func (s *scratch) reportOnce(key string, err error) { s.k.ReportErrorOnce(key, err) }

// recordSystem is the recording System: every Entity with a Transform and a
// Model, Mesh, Light or Camera, drawn into gfx once a tick. It repeats scene's
// path over Entities: it buckets the frame's instances into Batches by the keys
// the load System wrote, and for each camera and pass culls, applies the layer
// mask and filters each Batch's instances, then sorts, and packs one properties
// record and one instanced draw per Batch through model's packers.
//
// Its signature is its whole lock set. It reads the Stores, the load System's
// keys, the Lookup through the read facade and the viewport; it writes only
// its own scratch and gfx's queue. It declares no ordering beyond the load
// System's Before: gfx presents Last, so an ordinary-phase System already runs
// before it, and a game System that moves Transforms orders itself
// Before[ecsscene.RecordOnUpdate].
func recordSystem(
	k kernel.Kernel,
	models *ecs.Query[modelQuery],
	meshes *ecs.Query[meshQuery],
	lights *ecs.Query[lightQuery],
	cameras *ecs.Query[cameraQuery],
	animations *ecs.Get[ecsscene.Animation],
	params *ecs.Get[ecsscene.Params],
	materials *ecs.Get[ecsscene.Material],
	keys *ecs.Read[*keyScratch],
	lookup *ecs.Read[*model.Lookup],
	viewport *ecs.Read[*gfx.Viewport],
	work *ecs.Write[*scratch],
	out *ecs.Write[*gfx.OpQueue],
) {
	s, keyed, view := work.Get(), keys.Get(), viewport.Get()
	// A frame before the backend is up, before the MainLoop has reported a
	// window, or while one is minimised, is skipped whole, as scene skips it:
	// every screen-targeted pass would resolve an aspect of zero.
	if !keyed.ready || view == nil || view.WindowWidth <= 0 || view.WindowHeight <= 0 {
		return
	}
	s.begin(k, keyed)
	frame := frameInputs{
		read: model.NewLookupReadAccess(lookup.Get()), keys: keyed,
		animations: animations, params: params, materials: materials,
	}
	for e, it := range models.All() {
		s.addModel(&frame, e, it)
	}
	for e, it := range meshes.All() {
		s.addMesh(&frame, e, it)
	}
	for _, it := range lights.All() {
		// The Component's Position and Direction are documented as ignored:
		// the Transform places the light.
		light := it.Light.Descr
		light.Position, light.Direction = it.Place.Position, m.Vec3{}
		if light.Kind == model.LightSpot {
			// m.Quat.Rotate reads the zero Quat as no rotation, so an
			// unrotated spot shines down -Z.
			light.Direction = it.Place.Rotation.Rotate(facing)
		}
		s.preparedLights = prepareLight(k, s.preparedLights, &light, it.Light.Layers)
	}
	for _, it := range cameras.All() {
		s.addCamera(k, it)
	}
	for i := range s.cameras {
		s.flushCamera(k, view, &s.cameras[i])
	}
	s.build.emit(out.Get())
	s.release()
}

// facing is the direction an unrotated Transform faces, which is the way
// m.LookAt aims.
var facing = m.Vec3{Z: -1}

// frameInputs is one frame's handles, bundled so bucketing an Entity is one
// call.
type frameInputs struct {
	read       model.LookupReadAccess
	keys       *keyScratch
	animations *ecs.Get[ecsscene.Animation]
	params     *ecs.Get[ecsscene.Params]
	materials  *ecs.Get[ecsscene.Material]
}

// begin starts a frame: the arenas truncate, the bundled PBR is wrapped as the
// frame's first four materials, and the lists and tables empty.
func (s *scratch) begin(k kernel.Kernel, keyed *keyScratch) {
	s.k = k
	s.build.reset()
	s.forward = s.forward[:0]
	for variant := range keyed.bundled {
		s.bundled[variant] = s.forwardMaterial(keyed.bundled[variant])
	}
	s.materials.reset(&s.bundled)
	s.entries = s.entries[:0]
	s.batches = s.batches[:0]
	clear(s.bucket)
	s.preparedLights = s.preparedLights[:0]
	s.cameras = s.cameras[:0]
	s.passes = s.passes[:0]
	s.tags, s.tagParams, s.params = s.tags[:0], s.tagParams[:0], s.params[:0]
	s.modelMorphOffsets = s.modelMorphOffsets[:0]
	clear(s.meshReported)
}

// addCamera records one Camera Entity, unless an earlier one this frame holds
// its ID. Its passes are copied out of their List into the frame's arena.
func (s *scratch) addCamera(k kernel.Kernel, it *cameraQuery) {
	for i := range s.cameras {
		if s.cameras[i].camera.ID == it.Camera.ID {
			k.ReportError(ecsscene.ErrCameraAlreadyRecorded{Camera: it.Camera.ID})
			return
		}
	}
	first := len(s.passes)
	for _, pass := range it.Camera.Passes.All() {
		s.passes = append(s.passes, pass)
	}
	s.cameras = append(s.cameras, cameraRecord{
		camera: it.Camera, place: it.Place, first: first, count: len(s.passes) - first,
	})
}

// flushCamera emits one camera's passes. A camera missing a clip plane is
// skipped whole: the projection it would get instead is degenerate, and every
// pass built from it would cull against a volume nobody asked for.
func (s *scratch) flushCamera(k kernel.Kernel, view *gfx.Viewport, record *cameraRecord) {
	camera := &record.camera
	if camera.Near == 0 || camera.Far == 0 {
		k.ReportError(ecsscene.ErrCameraClipPlanesMissing{Camera: camera.ID, Near: camera.Near, Far: camera.Far})
		return
	}
	viewMatrix, ok := m.CameraView(record.place)
	if !ok {
		k.ReportError(ecsscene.ErrCameraProjectionDegenerate{
			Camera: camera.ID, Reason: "the transform has no inverse",
		})
		return
	}
	passes := s.passes[record.first : record.first+record.count]
	if len(passes) == 0 {
		s.defaultPasses[0] = defaultPass()
		passes = s.defaultPasses[:]
	}
	s.culler.beginCamera()
	for i := range passes {
		s.flushPass(k, view, record, viewMatrix, &passes[i])
	}
}

// flushPass decides one pass: which of the camera's surviving instances its
// tag admits, in what order, and packs them - one instanced draw per opaque
// Batch, and one per blended instance, back to front.
func (s *scratch) flushPass(
	k kernel.Kernel, view *gfx.Viewport, record *cameraRecord, viewMatrix m.Mat4, pass *ecsscene.Pass,
) {
	camera := &record.camera
	aspect, err := passAspect(camera.ID, pass, view)
	if err != nil {
		k.ReportError(err)
		return
	}
	projectionMatrix, err := projection(camera, aspect)
	if err != nil {
		k.ReportError(err)
		return
	}
	order := gfx.Order(camera.ID) + pass.Order
	viewProjection := projectionMatrix.Mul(viewMatrix)
	// One cull per distinct frustum: a second pass at the same aspect reuses
	// the first's survivors and filters them by its own tag.
	cull := s.culler.results[s.culler.cull(aspect, viewProjection, viewMatrix, camera.CullMask, s.entries)]
	survivors := s.culler.survivors[cull.first : cull.first+cull.count]
	eye := cameraPosition(record.place)
	selectLights(&s.lights, cull.frustum, eye.Vec3(), camera.CullMask, s.preparedLights)
	block := model.PackFrameLighting(model.FrameBlock{
		View:           viewMatrix,
		Projection:     projectionMatrix,
		ViewProjection: viewProjection,
		CameraPosition: eye,
		ViewDirection:  viewDirection(camera, record.place),
	}, frameLighting(camera), &s.lights)
	pending := s.build.beginPass(passDescr(s.labels, camera.ID, pass, order), block)
	// The tag interns once per pass, so no instance in it compares a string.
	tag := s.materials.internTag(pass.Tag)
	s.build.opaque, s.build.blend = s.build.opaque[:0], s.build.blend[:0]
	for i := range survivors {
		b := s.entries[survivors[i].draw].batch
		// A material with no entry for this pass's tag skips it: tag
		// participation is purely a material property.
		entry, serves := s.materials.entry(s.batches[b].interned, tag)
		if !serves {
			continue
		}
		if entry.blend {
			s.build.blend = append(s.build.blend, sortEntry{key: blendKey(survivors[i].depth), draw: uint32(i)})
		} else {
			s.build.opaque = append(s.build.opaque, sortEntry{key: opaqueKey(entry.materialID, uint32(b)), draw: uint32(i)})
		}
	}
	// Opaque and blend are separate arrays emitted in that order, which is
	// what removes any class bit from the key.
	sortEntries(s.build.opaque)
	sortEntries(s.build.blend)
	// An opaque Batch's survivors sort together, because the Batch is in the
	// key, and pack as one draw. A blended instance is a draw of its own,
	// which is what keeps its back-to-front order intact.
	for _, class := range [2]sortClass{{s.build.opaque, true}, {s.build.blend, false}} {
		for i := 0; i < len(class.entries); {
			b := s.entries[survivors[class.entries[i].draw].draw].batch
			run := 1
			if class.batched {
				for i+run < len(class.entries) &&
					s.entries[survivors[class.entries[i+run].draw].draw].batch == b {
					run++
				}
			}
			material, _ := s.materials.entry(s.batches[b].interned, tag)
			s.build.addDraw(pending, &s.batches[b], material,
				class.entries[i:i+run], survivors, s.entries)
			i += run
		}
	}
	s.build.endPass(pending)
}

// sortClass is one of the two classes a pass emits, with whether its entries
// batch.
type sortClass struct {
	entries []sortEntry
	batched bool
}

// forwardMaterial wraps one of model's forward gfx materials as a material
// serving only the forward pass, in the frame's own arena. The result is a
// one-entry window, so a later append can never grow into it.
func (s *scratch) forwardMaterial(descr gfx.MaterialDescr) material {
	start := len(s.forward)
	s.forward = append(s.forward, materialTag{tag: ecsscene.TagForward, descr: descr})
	return s.forward[start : start+1 : start+1]
}

// release zeroes every backing that held a copy of some Entity's Components
// once the frame is emitted - names, and Blob bytes that may be a texture's
// pixels - so a backing does not keep a despawned Entity's data reachable
// until a later frame happens to overwrite the same slots. gfx copied what it
// keeps as the draws were recorded.
func (s *scratch) release() {
	clear(s.batches)
	clear(s.cameras)
	clear(s.passes)
	clear(s.forward)
	clear(s.tags)
	clear(s.tagParams)
	clear(s.params)
	clear(s.plays[:cap(s.plays)])
	for i := range s.bundled {
		s.bundled[i] = nil
	}
	for i := range s.materials.interned {
		s.materials.interned[i].material = nil
	}
	s.k = kernel.Kernel{}
}
