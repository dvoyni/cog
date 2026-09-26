package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"

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
		Model Model
	}
	meshQuery struct {
		Place m.Transform
		Mesh  Mesh
	}
	lightQuery struct {
		Place m.Transform
		Light Light
	}
	cameraQuery struct {
		Place  m.Transform
		Camera Camera
	}
)

// errorReporter is what the frame reports through: the kernel, or a sink.
type errorReporter interface{ ReportError(error) }

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
	// params are the Entity's Params, which the frame's version of each
	// pass's set lays over what the set binds.
	params []gfx.ParameterDescr
	// interned is the Batch's material's index in the frame's material table.
	interned int32
}

// cameraRecord is one Camera Entity the frame draws for, with its passes as a
// span of scratch.passes.
type cameraRecord struct {
	camera       Camera
	place        m.Transform
	first, count int
}

// frameInputs is one frame's handles, bundled so bucketing an Entity is one
// call.
type frameInputs struct {
	read       model.LookupReadAccess
	keys       *keyScratch
	animations *ecs.Get[Animation]
	params     *ecs.Get[Params]
	materials  *ecs.Get[Material]
}

// sortClass is one of the two classes a pass emits, with whether its entries
// batch.
type sortClass struct {
	entries []sortEntry
	batched bool
}

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
	passes        []Pass
	defaultPasses [1]Pass

	// forward is the arena a model's own set is wrapped in as a material
	// serving the forward pass, and params the arena a Params Component is
	// copied into. Each copy is a full-slice window, so no later copy can
	// grow into it.
	forward []materialTag
	params  []gfx.ParameterDescr

	// The animation resolution: plays is one Entity's used
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

// begin starts a frame: the arenas truncate, and the lists and tables empty.
func (s *scratch) begin(k kernel.Kernel) {
	s.k = k
	s.build.reset()
	s.forward = s.forward[:0]
	s.params = s.params[:0]
	s.materials.reset()
	s.entries = s.entries[:0]
	s.batches = s.batches[:0]
	clear(s.bucket)
	s.preparedLights = s.preparedLights[:0]
	s.cameras = s.cameras[:0]
	s.passes = s.passes[:0]
	s.modelMorphOffsets = s.modelMorphOffsets[:0]
	clear(s.meshReported)
}

// addCamera records one Camera Entity, unless an earlier one this frame holds
// its ID. Its passes are copied out of their List into the frame's arena.
func (s *scratch) addCamera(k kernel.Kernel, it *cameraQuery) {
	for i := range s.cameras {
		if s.cameras[i].camera.ID == it.Camera.ID {
			k.ReportError(ErrCameraAlreadyRecorded{Camera: it.Camera.ID})
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
	viewMatrix, err := cameraView(&record.camera, record.place)
	if err != nil {
		k.ReportError(err)
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
	k kernel.Kernel, view *gfx.Viewport, record *cameraRecord, viewMatrix m.Mat4, pass *Pass,
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

// forwardMaterial wraps one of model's own sets as a material serving only
// the forward pass, in the frame's own arena. The result is a one-entry
// window, so a later append can never grow into it.
func (s *scratch) forwardMaterial(tag materialTag) material {
	start := len(s.forward)
	s.forward = append(s.forward, tag)
	return s.forward[start : start+1 : start+1]
}

// release zeroes every backing that held a copy of some Entity's Components
// once the frame is emitted - names, and Blob bytes that may be a texture's
// pixels - and the program handles a material tag carries, so a backing does not keep a despawned Entity's data reachable
// until a later frame happens to overwrite the same slots. gfx copied what it
// keeps as the draws were recorded.
func (s *scratch) release() {
	clear(s.batches)
	clear(s.cameras)
	clear(s.passes)
	clear(s.forward)
	clear(s.params)
	clear(s.plays[:cap(s.plays)])
	for i := range s.materials.interned {
		s.materials.interned[i].material = nil
	}
	s.k = kernel.Kernel{}
}
