package types

import (
	"cmp"
	"slices"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// CameraRecord is one registered camera. Its descriptor's Passes slice aliases
// the queue's pass arena, never the caller's array.
type CameraRecord struct {
	ID    CameraID
	Descr CameraDescr
}

// OpQueue is scene's frame-local recording surface.
//
// It double-buffers: the flush publishes the frame it just consumed and starts
// recording into the buffer the previous publication vacated, so Ops and Passes
// describe the frame that was actually flushed and stay valid until the next
// one. Nothing is copied to achieve that — the two halves swap.
type OpQueue struct {
	cameras    []CameraRecord
	draws      []DrawRecord
	lights     []LightRecord
	passArena  []Pass
	duplicates []CameraID
	// calls is the frame's draw calls as the recorder made them, one Op each,
	// kept apart from the draw records the flush consumes: a WireBox is one
	// call and twelve draws, and Ops reports calls.
	calls []Op
	// meshes is everything TemporaryMesh and Mesh recorded into this frame.
	meshes MeshRecording
	// models is the frame's Model calls. They are kept apart from the draws
	// because a model draw expands into one draw per primitive at flush time,
	// and the expansion has to resolve residency first: a non-resident model
	// contributes no draws at all.
	models []ModelDrawRecord
	// plays backs every model draw's ClipPlay slice, so a record never aliases
	// the caller's array and a caller may reuse its own the moment the call
	// returns.
	plays []model.ClipPlay
	// morphWeights backs every model draw's MorphWeights slice, for the same
	// reason plays does.
	morphWeights []float32
	// frame counts recordings, and stamps every temporary MeshRef minted into
	// this one. It is what makes a temporary ref used in a later frame
	// detectable rather than a draw of whatever now holds its slot.
	frame uint32

	// published is the recording the last flush consumed, kept readable.
	published        []CameraRecord
	publishedDraws   []DrawRecord
	publishedLights  []LightRecord
	publishedArena   []Pass
	publishedCalls   []Op
	publishedMeshes  MeshRecording
	publishedModels  []ModelDrawRecord
	publishedPlays   []model.ClipPlay
	publishedWeights []float32
	publishedFrame   uint32
	// cameraOps is the published frame's camera registrations as Ops, in id
	// order, which Ops reports ahead of the draw calls.
	cameraOps []Op
	passViews []PassView
	// batchArena backs every published pass's Batches slice, and passBatches
	// is the span each pass claimed while the arena was still growing.
	batchArena  []BatchView
	passBatches [][2]int
}

// Camera registers a camera for this frame and gives it its passes. It is a
// registration rather than a free parameter, so recording the same id twice
// keeps the first record and reports the second: a repeat means two systems
// each believe they own that camera.
func (q *OpQueue) Camera(id CameraID, descr CameraDescr) {
	for i := range q.cameras {
		if q.cameras[i].ID == id {
			q.duplicates = append(q.duplicates, id)
			return
		}
	}
	start := len(q.passArena)
	q.passArena = append(q.passArena, descr.Passes...)
	descr.Passes = q.passArena[start:len(q.passArena):len(q.passArena)]
	q.cameras = append(q.cameras, CameraRecord{ID: id, Descr: descr})
}

// OpCount reports how many operations have been recorded into this frame so
// far. It reads the recording in progress, not the published frame Ops returns.
func (q *OpQueue) OpCount() int { return len(q.cameras) + len(q.calls) }

// Reset abandons everything recorded into the frame in progress. It does not
// disturb the published frame.
//
// Temporary MeshRefs minted into the abandoned recording do not survive it:
// their slots are reissued to whatever is recorded next, and the frame stamp
// they carry cannot tell the difference. Re-record the geometry after a Reset.
func (q *OpQueue) Reset() {
	clear(q.cameras)
	q.cameras = q.cameras[:0]
	clear(q.draws)
	q.draws = q.draws[:0]
	q.lights = q.lights[:0]
	q.passArena = q.passArena[:0]
	q.duplicates = q.duplicates[:0]
	clear(q.calls)
	q.calls = q.calls[:0]
	q.meshes.reset()
	clear(q.models)
	q.models = q.models[:0]
	clear(q.plays)
	q.plays = q.plays[:0]
	q.morphWeights = q.morphWeights[:0]
}

// Ops appends the published frame's recorded operations to dst, in flush order:
// every camera by id, then every draw call in the order it was recorded. A call
// is one Op whatever it flushes to — a WireBox is one op and twelve draws. The
// returned slice and every slice inside it alias the queue's storage and stay
// valid until the next flush.
func (q *OpQueue) Ops(dst []Op) []Op { return append(append(dst, q.cameraOps...), q.publishedCalls...) }

// Passes appends the published frame's pass results to dst, in emission order:
// cameras by id ascending, then each camera's passes as declared. The returned
// slices alias flush storage and stay valid until the next flush.
//
// Within a pass, recording order is not preserved. Every pass sorts what it
// draws - opaque and alpha-masked by material key, then blended back to front
// by view depth - so a pass's Batches are in emission order, which is the
// sort's order, not the recorder's. Ops keeps the recording order.
//
// There is no Config knob gating this. The lists are built during the flush
// regardless, so retaining them costs a slice header — and a knob would mean
// tests exercise a code path production does not.
func (q *OpQueue) Passes(dst []PassView) []PassView { return append(dst, q.passViews...) }

// beginFlush publishes the frame just recorded and starts a new recording in
// the buffer the previous publication vacated. The published records keep their
// own arena, so their borrowed Pass slices stay valid while the next frame
// records over the other one.
//
// It sorts the published cameras by id, which is the frame's emission order and
// therefore the order Ops reports. Cameras are collected into a slice and
// sorted rather than ranged over as a map, because a Go map range would make
// frame output nondeterministic.
func (q *OpQueue) beginFlush() []CameraRecord {
	q.cameras, q.published = q.published, q.cameras
	q.draws, q.publishedDraws = q.publishedDraws, q.draws
	q.lights, q.publishedLights = q.publishedLights, q.lights
	q.passArena, q.publishedArena = q.publishedArena, q.passArena
	q.calls, q.publishedCalls = q.publishedCalls, q.calls
	// The layout cache is interning, not recording, so it stays with the half
	// that keeps recording rather than travelling with the published frame.
	q.meshes, q.publishedMeshes = q.publishedMeshes, q.meshes
	q.models, q.publishedModels = q.publishedModels, q.models
	q.plays, q.publishedPlays = q.publishedPlays, q.plays
	q.morphWeights, q.publishedWeights = q.publishedWeights, q.morphWeights
	q.meshes.layouts, q.publishedMeshes.layouts = q.publishedMeshes.layouts, q.meshes.layouts
	q.publishedFrame, q.frame = q.frame, q.frame+1
	clear(q.cameras)
	q.cameras = q.cameras[:0]
	clear(q.draws)
	q.draws = q.draws[:0]
	q.lights = q.lights[:0]
	q.passArena = q.passArena[:0]
	clear(q.calls)
	q.calls = q.calls[:0]
	q.meshes.reset()
	clear(q.models)
	q.models = q.models[:0]
	clear(q.plays)
	q.plays = q.plays[:0]
	q.morphWeights = q.morphWeights[:0]

	slices.SortFunc(q.published, func(a, b CameraRecord) int { return cmp.Compare(a.ID, b.ID) })
	q.passViews = q.passViews[:0]
	q.passBatches = q.passBatches[:0]
	q.batchArena = q.batchArena[:0]
	q.cameraOps = q.cameraOps[:0]
	for i := range q.published {
		q.cameraOps = append(q.cameraOps, Op{
			Kind: OpCamera, Camera: q.published[i].ID, Descr: q.published[i].Descr,
		})
	}
	return q.published
}

// publishPass records what the flush decided about one pass.
func (q *OpQueue) publishPass(view PassView) { q.passViews = append(q.passViews, view) }

// recordedDuplicates lists the camera ids the frame recorded more than once.
func (q *OpQueue) recordedDuplicates() []CameraID { return q.duplicates }

// endFlush drops the duplicate reports the frame accumulated, after the flush
// has had its chance to report them.
func (q *OpQueue) endFlush() {
	q.duplicates = q.duplicates[:0]
	q.resolveBatches()
}

// DrawRecord is one recorded draw: what the flush consumes, as distinct from
// the call that produced it, which the queue keeps as an Op. The debug
// vocabulary is sugar over scene's own unit meshes, so a box needs nothing
// beyond where it stands and what colour it is; the mesh it draws is scene's,
// and the material is the bundled one.
type DrawRecord struct {
	Layers    LayerMask
	Transform m.Transform
	// Stretch is the non-uniform scale scene applied itself, on top of the
	// transform's own Scale, to turn a unit mesh into a line, an edge or a
	// plane of the requested size. Zero means none.
	Stretch m.Vec3
	// Matrix, when non-nil, is the whole world matrix and the transform is not
	// consulted. It is how a model expansion places a primitive at its
	// flattened node world, which no position, rotation and scale can spell in
	// general; it points into the flush's world arena, which is sized before
	// the first record points into it, so it never moves under one. Nothing a
	// caller records sets it.
	Matrix *m.Mat4
	Color  m.Color
	// SelfLit draws the shape as black with color as its emissive, so it
	// reads the same in a frame with no lights at all.
	SelfLit bool
	// Shape names which of scene's own meshes the draw renders. Its zero value
	// is ShapeNone, which means the draw names its own mesh instead - and a
	// zero mesh there is a draw of nothing, which is what a rejected mint
	// yields.
	Shape unitShape
	// Material is the scene material the draw named, or nil for the bundled
	// PBR. Every debug shape leaves it nil, which is what makes a draw literal
	// that omits the field untouched by the field existing.
	Material Material
	// MaterialKey is Material's content key when the recording took it, so the
	// flush interns without fingerprinting again. Zero means nothing keyed it:
	// the bundled PBR, a debug shape, a model's own material.
	MaterialKey MaterialKey
	// Mesh is the mesh the draw renders, for a draw that names one. The debug
	// vocabulary leaves it zero and names a shape instead: scene's unit meshes
	// are baked lazily on first use, so their refs cannot be known at record
	// time.
	Mesh model.MeshRef
	// Params are the extra gfx parameters a MeshDraw asked to bind, or a model
	// draw's OverrideParams, both aliasing the recording's parameter arena.
	// Either way gfx resolves them over the entry's own material parameters by
	// name, against its reflected layout, and drops what it does not declare.
	Params []gfx.ParameterDescr
	// Bounds is the draw's explicit local-space sphere, and neverCull exempts
	// it from culling outright. Both are zero for a debug shape, which culls
	// by its mesh's baked sphere.
	Bounds    m.Sphere
	NeverCull bool
	// Anim is what the draw's instances say about animation: the group 2
	// buffers, the sceneAnim offset and whether the geometry skins. A model
	// expansion fills it; everything else leaves it zero, which is also what
	// picks the variant that declares no group 2 at all - so a draw literal
	// that never heard of animation binds nothing there and declares nothing.
	Anim AnimBinding
	// Group ties together the records one instanced call expanded into, so the
	// flush can pack their survivors as a single batch without comparing them.
	// It is the recording ordinal of the group's first record plus one - unique
	// within a frame with no counter to reset - and zero for a record that
	// stands alone.
	//
	// A debug shape leaves it zero even where a call makes several draws: a
	// wire box's twelve edges still pack as one batch, but because the flush
	// merges equal draws recorded separately, not because they share a group.
	Group uint32
}

// World resolves the draw's model matrix: its transform, with the internal
// stretch folded into the scale when there is one.
func (r DrawRecord) World() m.Mat4 {
	if r.Matrix != nil {
		return *r.Matrix
	}
	if r.Stretch == (m.Vec3{}) {
		return r.Transform.Mat4()
	}
	return r.Transform.ScaledMat4(r.Stretch)
}

// AppendPaint appends what a debug shape's colour is to the bundled material:
// the params that turn its white paint into paint of the draw's colour, lit or
// self-lit, as model spells them. Every other draw appends nothing: a model
// primitive's numbers are its material's own params, and a mesh draw is white
// paint unless its params say otherwise.
//
// Everything that takes the bundled PBR is paint, not metal: glTF defaults to
// a fully metallic surface, and a metal with no image-based lighting to reflect
// is a dark mirror, the opposite of visible - which is the one thing a draw
// with no material of its own has to be. A self-lit shape is black paint that
// glows.
func (r DrawRecord) AppendPaint(dst []gfx.ParameterDescr) []gfx.ParameterDescr {
	if r.Shape == ShapeNone {
		return dst
	}
	return model.PaintParams(dst, r.Color, r.SelfLit)
}

// draw records one draw of any kind. Every recording call is sugar over it.
func (q *OpQueue) draw(record DrawRecord) { q.draws = append(q.draws, record) }

// appendFlushDraw adds one draw to the frame the flush is consuming. It is how
// a model draw expands: the records land in the same list culling, sorting and
// packing already walk, so nothing downstream can tell an expanded draw from a
// recorded one. Appending here rather than into the recording half is what
// keeps the expansion out of the next frame.
func (q *OpQueue) appendFlushDraw(record DrawRecord) {
	q.publishedDraws = append(q.publishedDraws, record)
}

// drawCount reports how many draws the flush has to consume so far, which is
// the recording ordinal the next appended record takes. Model expansion needs
// it to stamp a group: the ordinal is what makes a group unique within a frame
// with no counter to reset.
func (q *OpQueue) drawCount() int { return len(q.publishedDraws) }

// publishedDraws lists the draws the flush is consuming, in recording order.
func (q *OpQueue) flushDraws() []DrawRecord { return q.publishedDraws }

// flushMeshes hands the flush the mesh recording it is consuming.
func (q *OpQueue) flushMeshes() *MeshRecording { return &q.publishedMeshes }

// publishBatches copies one pass's batches into the frame's batch arena and
// records the span, which endFlush resolves into the published PassView.
//
// The span is resolved late because appending to the arena may move it, and a
// PassView published early would then alias a backing nobody writes to again.
func (q *OpQueue) publishBatches(batches []BatchView) {
	start := len(q.batchArena)
	q.batchArena = append(q.batchArena, batches...)
	q.passBatches = append(q.passBatches, [2]int{start, len(batches)})
}

// resolveBatches points every published pass at its own span of the batch
// arena, now that the arena has stopped moving.
func (q *OpQueue) resolveBatches() {
	for i := range q.passViews {
		if i >= len(q.passBatches) {
			return
		}
		span := q.passBatches[i]
		q.passViews[i].Batches = q.batchArena[span[0] : span[0]+span[1] : span[0]+span[1]]
	}
}
