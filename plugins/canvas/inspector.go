package canvas

import (
	"slices"
	"sync"
	"sync/atomic"

	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/render"
)

// inspector keeps the inspection Assets.RequestInspection asks for, from its
// request to its delivery. While a request is pending, each frame canvas
// draws is written down; render's InspectedEvent then names the frame render
// inspected, and the one canvas wrote down of it is delivered, render's
// beside it, until canvas's InspectedEvent's jobs have run. Writing down
// every frame of a pending request, rather than one chosen at the request,
// keeps the two accounts of one frame even when render joins canvas's
// request to an inspection already pending, or when the request lands while
// a frame begins.
//
// While a request is pending, it also notes the reports canvas logs and the
// first uses it makes, in whichever job, and each frame written down takes
// those noted since the frame before it was, so a frame drawn while the
// request is pending, and not written down, leaves its own to the next.
type inspector struct {
	// requestPending is set from a request until its delivery, a request while
	// one is pending joining it, and read without the mutex, the one check a
	// frame not inspected pays, and the one a report or a first use pays
	// while none is pending.
	requestPending atomic.Bool
	// mutex guards the rest: recordedFrames, the frames written down while
	// the request is pending, oldest first; deliveredInspections, the
	// inspections whose InspectedEvent's jobs run; and noted, the reports
	// and first uses noted since the last frame was written down.
	mutex                sync.Mutex
	recordedFrames       []Inspection
	deliveredInspections []Inspection
	noted                Inspection
}

// record writes inspection down, the frame drawn while a request was
// pending, with the reports and first uses noted since the frame before it
// was written down, unless that request was delivered since.
func (i *inspector) record(inspection Inspection) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	if !i.requestPending.Load() {
		return
	}
	inspection.PackedImages = i.noted.PackedImages
	inspection.LoadedMeshFiles = i.noted.LoadedMeshFiles
	inspection.MadeLayouts = i.noted.MadeLayouts
	inspection.MadeGlyphs = i.noted.MadeGlyphs
	inspection.Reports = i.noted.Reports
	i.noted = Inspection{}
	i.recordedFrames = append(i.recordedFrames, inspection)
}

// addReport notes report, logged while a request is pending.
func (i *inspector) addReport(report Report) {
	i.note(func(noted *Inspection) { noted.Reports = append(noted.Reports, report) })
}

// addPackedImage notes frame of image packed, while a request is pending.
func (i *inspector) addPackedImage(image Image, frame uint32) {
	i.note(func(noted *Inspection) {
		noted.PackedImages = append(noted.PackedImages, InspectedImage{Image: image, Frame: frame})
	})
}

// addLoadedMeshFile notes file loaded, while a request is pending.
func (i *inspector) addLoadedMeshFile(file MeshFile) {
	i.note(func(noted *Inspection) { noted.LoadedMeshFiles = append(noted.LoadedMeshFiles, file) })
}

// addMadeLayout notes the layout of text made on a cache miss, while a
// request is pending, text's spans copied, since its maker's scratch may
// hold them.
func (i *inspector) addMadeLayout(text Text) {
	i.note(func(noted *Inspection) {
		text.Spans = slices.Clone(text.Spans)
		noted.MadeLayouts = append(noted.MadeLayouts, text)
	})
}

// addMadeGlyphs notes count glyphs of text made, while a request is
// pending, text's spans copied, since its maker's scratch may hold them.
func (i *inspector) addMadeGlyphs(text Text, count int) {
	if count == 0 {
		return
	}
	i.note(func(noted *Inspection) {
		text.Spans = slices.Clone(text.Spans)
		noted.MadeGlyphs = append(noted.MadeGlyphs, InspectedGlyphCount{Text: text, Count: count})
	})
}

// note calls add with what is noted while a request is pending, under the
// mutex, and does nothing otherwise, checking again under the mutex, so a
// note racing the delivery is not left for the next request.
func (i *inspector) note(add func(noted *Inspection)) {
	if !i.requestPending.Load() {
		return
	}
	i.mutex.Lock()
	defer i.mutex.Unlock()
	if i.requestPending.Load() {
		add(&i.noted)
	}
}

// deliver delivers the frame of beat, which render inspected, with
// renderInspection beside it, and answers true, when it was written down:
// the request is then answered, and the other frames written down for it
// forgotten. Otherwise it forgets the frames up to beat, which render will
// not inspect now, and answers false.
func (i *inspector) deliver(beat metronome.Beat, renderInspection render.Inspection) bool {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	index := slices.IndexFunc(i.recordedFrames, func(recorded Inspection) bool { return recorded.Beat == beat })
	if index < 0 {
		i.recordedFrames = slices.DeleteFunc(i.recordedFrames, func(recorded Inspection) bool { return recorded.Beat.Tick <= beat.Tick })
		return false
	}
	inspection := i.recordedFrames[index]
	inspection.Render = renderInspection
	i.deliveredInspections = append(i.deliveredInspections, inspection)
	clear(i.recordedFrames)
	i.recordedFrames = i.recordedFrames[:0]
	i.noted = Inspection{}
	i.requestPending.Store(false)
	return true
}

// read answers the inspection of the frame of beat while it is delivered.
func (i *inspector) read(beat metronome.Beat) (Inspection, bool) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	index := slices.IndexFunc(i.deliveredInspections, func(delivered Inspection) bool { return delivered.Beat == beat })
	if index < 0 {
		return Inspection{}, false
	}
	return i.deliveredInspections[index], true
}

// drop drops the inspection of the frame of beat, once its InspectedEvent's
// jobs have run.
func (i *inspector) drop(beat metronome.Beat) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	i.deliveredInspections = slices.DeleteFunc(i.deliveredInspections, func(delivered Inspection) bool { return delivered.Beat == beat })
}
