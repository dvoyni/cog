package scene

import (
	"cmp"
	"slices"

	"github.com/dvoyni/cog/m"
)

// cameraRecord is one registered camera. Its descriptor's Passes slice aliases
// the queue's pass arena, never the caller's array.
type cameraRecord struct {
	id    CameraID
	descr CameraDescr
}

// opQueue is scene's frame-local recording surface.
//
// It double-buffers: the flush publishes the frame it just consumed and starts
// recording into the buffer the previous publication vacated, so Ops and Passes
// describe the frame that was actually flushed and stay valid until the next
// one. Nothing is copied to achieve that — the two halves swap.
type opQueue struct {
	cameras    []cameraRecord
	draws      []drawRecord
	passArena  []Pass
	duplicates []CameraID

	// published is the recording the last flush consumed, kept readable.
	published      []cameraRecord
	publishedDraws []drawRecord
	publishedArena []Pass
	opViews        []Op
	passViews      []PassView
	// batchArena backs every published pass's Batches slice, and passBatches
	// is the span each pass claimed while the arena was still growing.
	batchArena  []BatchView
	passBatches [][2]int
}

// Camera registers a camera for this frame and gives it its passes. It is a
// registration rather than a free parameter, so recording the same id twice
// keeps the first record and reports the second: a repeat means two systems
// each believe they own that camera.
func (q *opQueue) Camera(id CameraID, descr CameraDescr) {
	for i := range q.cameras {
		if q.cameras[i].id == id {
			q.duplicates = append(q.duplicates, id)
			return
		}
	}
	start := len(q.passArena)
	q.passArena = append(q.passArena, descr.Passes...)
	descr.Passes = q.passArena[start:len(q.passArena):len(q.passArena)]
	q.cameras = append(q.cameras, cameraRecord{id: id, descr: descr})
}

// OpCount reports how many operations have been recorded into this frame so
// far. It reads the recording in progress, not the published frame Ops returns.
func (q *opQueue) OpCount() int { return len(q.cameras) + len(q.draws) }

// Reset abandons everything recorded into the frame in progress. It does not
// disturb the published frame.
func (q *opQueue) Reset() {
	clear(q.cameras)
	q.cameras = q.cameras[:0]
	clear(q.draws)
	q.draws = q.draws[:0]
	q.passArena = q.passArena[:0]
	q.duplicates = q.duplicates[:0]
}

// Ops appends the published frame's recorded operations to dst, in flush order.
// The returned slice and every slice inside it alias the queue's storage and
// stay valid until the next flush.
func (q *opQueue) Ops(dst []Op) []Op { return append(dst, q.opViews...) }

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
func (q *opQueue) Passes(dst []PassView) []PassView { return append(dst, q.passViews...) }

// beginFlush publishes the frame just recorded and starts a new recording in
// the buffer the previous publication vacated. The published records keep their
// own arena, so their borrowed Pass slices stay valid while the next frame
// records over the other one.
//
// It sorts the published cameras by id, which is the frame's emission order and
// therefore the order Ops reports. Cameras are collected into a slice and
// sorted rather than ranged over as a map, because a Go map range would make
// frame output nondeterministic.
func (q *opQueue) beginFlush() []cameraRecord {
	q.cameras, q.published = q.published, q.cameras
	q.draws, q.publishedDraws = q.publishedDraws, q.draws
	q.passArena, q.publishedArena = q.publishedArena, q.passArena
	clear(q.cameras)
	q.cameras = q.cameras[:0]
	clear(q.draws)
	q.draws = q.draws[:0]
	q.passArena = q.passArena[:0]

	slices.SortFunc(q.published, func(a, b cameraRecord) int { return cmp.Compare(a.id, b.id) })
	q.passViews = q.passViews[:0]
	q.passBatches = q.passBatches[:0]
	q.batchArena = q.batchArena[:0]
	q.opViews = q.opViews[:0]
	for i := range q.published {
		q.opViews = append(q.opViews, Op{
			Kind: OpCamera, Camera: q.published[i].id, Descr: q.published[i].descr,
		})
	}
	for i := range q.publishedDraws {
		q.opViews = append(q.opViews, Op{
			Kind:      OpBox,
			Layers:    q.publishedDraws[i].layers,
			Transform: q.publishedDraws[i].transform,
			Color:     q.publishedDraws[i].color,
		})
	}
	return q.published
}

// publishPass records what the flush decided about one pass.
func (q *opQueue) publishPass(view PassView) { q.passViews = append(q.passViews, view) }

// recordedDuplicates lists the camera ids the frame recorded more than once.
func (q *opQueue) recordedDuplicates() []CameraID { return q.duplicates }

// endFlush drops the duplicate reports the frame accumulated, after the flush
// has had its chance to report them.
func (q *opQueue) endFlush() {
	q.duplicates = q.duplicates[:0]
	q.resolveBatches()
}

// drawRecord is one recorded draw. The debug vocabulary is sugar over scene's
// own unit meshes, so a box needs nothing beyond where it stands and what
// colour it is; the mesh it draws is scene's, and the material is the bundled
// one.
type drawRecord struct {
	layers    LayerMask
	transform Transform
	color     m.Color
	// material is the scene material the draw named, or nil for the bundled
	// PBR. Every debug shape leaves it nil, which is what makes a draw literal
	// that omits the field untouched by the field existing.
	material Material
	// mesh is the mesh the draw renders. The debug vocabulary leaves it zero,
	// which the flush reads as scene's own unit box: the box is baked lazily on
	// first use, so its ref cannot be known at record time.
	mesh MeshRef
	// bounds is the draw's explicit local-space sphere, and neverCull exempts
	// it from culling outright. Both are zero for a debug shape, which culls
	// by its mesh's baked sphere.
	bounds    m.Sphere
	neverCull bool
}

// pbrRecord builds the bundled PBR record one recorded draw binds.
//
// A debug shape is paint, not metal: it takes glTF's defaults except for
// metallic, because glTF defaults to a fully metallic surface and a metal has
// no diffuse at all - a debug box would render as a dark mirror of an
// environment that does not exist, which is the opposite of visible.
func (r drawRecord) pbrRecord() scenePbrRecord {
	record := defaultPbrRecord()
	record.MetallicFactor = 0
	record.BaseColorFactor = m.Vec4{X: r.color.R, Y: r.color.G, Z: r.color.B, W: r.color.A}
	return record
}

// Box records a unit cube at transform. It is the scene twin of canvas's
// FillRect: one statement, no mesh handle, no material, no shader.
func (q *opQueue) Box(layers LayerMask, transform Transform, color m.Color) {
	q.draw(drawRecord{layers: layers, transform: transform, color: color})
}

// draw records one draw of any kind. Every recording call is sugar over it.
func (q *opQueue) draw(record drawRecord) { q.draws = append(q.draws, record) }

// publishedDraws lists the draws the flush is consuming, in recording order.
func (q *opQueue) flushDraws() []drawRecord { return q.publishedDraws }

// publishBatches copies one pass's batches into the frame's batch arena and
// records the span, which endFlush resolves into the published PassView.
//
// The span is resolved late because appending to the arena may move it, and a
// PassView published early would then alias a backing nobody writes to again.
func (q *opQueue) publishBatches(batches []BatchView) {
	start := len(q.batchArena)
	q.batchArena = append(q.batchArena, batches...)
	q.passBatches = append(q.passBatches, [2]int{start, len(batches)})
}

// resolveBatches points every published pass at its own span of the batch
// arena, now that the arena has stopped moving.
func (q *opQueue) resolveBatches() {
	for i := range q.passViews {
		if i >= len(q.passBatches) {
			return
		}
		span := q.passBatches[i]
		q.passViews[i].Batches = q.batchArena[span[0] : span[0]+span[1] : span[0]+span[1]]
	}
}
