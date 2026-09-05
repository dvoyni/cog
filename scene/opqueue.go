package scene

import (
	"cmp"
	"slices"
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
	passArena  []Pass
	duplicates []CameraID

	// published is the recording the last flush consumed, kept readable.
	published      []cameraRecord
	publishedArena []Pass
	opViews        []Op
	passViews      []PassView
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
func (q *opQueue) OpCount() int { return len(q.cameras) }

// Reset abandons everything recorded into the frame in progress. It does not
// disturb the published frame.
func (q *opQueue) Reset() {
	clear(q.cameras)
	q.cameras = q.cameras[:0]
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
	q.passArena, q.publishedArena = q.publishedArena, q.passArena
	clear(q.cameras)
	q.cameras = q.cameras[:0]
	q.passArena = q.passArena[:0]

	slices.SortFunc(q.published, func(a, b cameraRecord) int { return cmp.Compare(a.id, b.id) })
	q.passViews = q.passViews[:0]
	q.opViews = q.opViews[:0]
	for i := range q.published {
		q.opViews = append(q.opViews, Op{
			Kind: OpCamera, Camera: q.published[i].id, Descr: q.published[i].descr,
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
}
