package render

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/metronome"
)

// passTimer times every pass and compute pass of each frame recorded while
// Frame.SetPassTimings(true) holds, and delivers each frame's timings once
// the GPU has run it, through a timed frame of its own: timing from the
// frame's beginning, its timestamps resolved in render's closing command
// list once its passes are recorded and read back as a readback of the
// staging pool's, and delivered until PassTimingsEvent's jobs have run. Timed
// frames gone unused for as many frames as render is configured to wait are
// freed, as the other pools' entries are.
type passTimer struct {
	// enabled is set while pass timings are on, and unavailable once they
	// were found unavailable, which they stay.
	enabled     atomic.Bool
	unavailable atomic.Bool
	// recordingFrame is the timed frame of the frame recording, nil when
	// none is timed, read without the mutex by every pass's beginning, the
	// one cost a frame not timed pays.
	recordingFrame atomic.Pointer[timedFrame]
	// mutex guards timedFrames, every timed frame made and not freed, and
	// what each says of where it stands.
	mutex       sync.Mutex
	timedFrames []*timedFrame
}

// setEnabled turns pass timings on or off from the next frame to begin.
// Turning them on where device has no timestamp queries is reported, the
// first time, through reporter, and leaves them off, as it does once they
// were found unavailable.
func (t *passTimer) setEnabled(on bool, device gfx.Device, reporter reporter) {
	if on && t.unavailable.Load() {
		return
	}
	if on && device.Features()&gfx.FeatureFlagTimestampQuery == 0 {
		t.reportUnavailable(errors.New("gfx's device has no FeatureFlagTimestampQuery"), reporter)
		return
	}
	t.enabled.Store(on)
}

// begin begins timing the frame of beat while pass timings are on, in a
// timed frame free for it, or one made on device; one device cannot make is
// reported through reporter, as unavailable for query sets, and logged
// otherwise, but for a device loss.
func (t *passTimer) begin(beat metronome.Beat, device gfx.Device, reporter reporter) {
	if !t.enabled.Load() {
		return
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	var frame *timedFrame
	for _, pooled := range t.timedFrames {
		if !pooled.leased {
			frame = pooled
			break
		}
	}
	if frame == nil {
		made, err := newTimedFrame(device)
		switch {
		case errors.Is(err, gfx.ErrQuerySetsUnavailable):
			t.reportUnavailable(err, reporter)
			return
		case err != nil:
			reporter.logUnlessLost(fmt.Errorf("render: making the query set of pass timings: %w", err))
			return
		}
		frame = made
		t.timedFrames = append(t.timedFrames, frame)
	}
	frame.begin(beat)
	t.recordingFrame.Store(frame)
}

// addPass adds the pass the list at order begins, labelled label, to the
// frame timed, and answers where it writes its timestamps: none when no
// frame is timed.
func (t *passTimer) addPass(order Order, label string) gfx.PassTimestampWrites {
	frame := t.recordingFrame.Load()
	if frame == nil {
		return gfx.PassTimestampWrites{}
	}
	return frame.addPass(order, label)
}

// readBack ends the timing of the frame recording, if any, once its passes
// are recorded, reporting through reporter its passes left untimed, and
// records on device, in render's closing command list of gfxFrame, the
// commands reading their timestamps back through staging. It answers
// whether they are read back, for deliver to deliver; false for no frame
// timed, and for a frame with no pass or whose commands gfx refuses, logged
// but for a device loss, each released.
func (t *passTimer) readBack(device gfx.Device, gfxFrame gfx.Frame, staging *stagingPool, reporter reporter) bool {
	frame := t.recordingFrame.Swap(nil)
	if frame == nil {
		return false
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if frame.untimedPassCount > 0 {
		reporter.report(ReportKindPassUntimed, fmt.Errorf("render: %d passes of beat %d began after the %dth and have no timings", frame.untimedPassCount, frame.beat.Tick, maxTimedPassCount))
	}
	if len(frame.timedPasses) == 0 {
		frame.release()
		return false
	}
	if err := frame.recordReadback(device, gfxFrame, staging); err != nil {
		reporter.logUnlessLost(fmt.Errorf("render: reading the pass timings of beat %d back: %w", frame.beat.Tick, err))
		frame.release()
		return false
	}
	return true
}

// deliver answers the beat of the timed frame whose timestamps readback
// reads back, which gfx has mapped or failed to map, as status says, and
// its timings, read from staging on device and delivered until drop; nil
// timings for a map that failed or could not be read, logged through
// reporter but for a device loss, the frame released. The readback goes back to
// staging either way. False when readback is no timed frame's.
func (t *passTimer) deliver(readback Readback, status ReadbackStatus, device gfx.Device, staging *stagingPool, reporter reporter) (metronome.Beat, []PassTiming, bool) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	var frame *timedFrame
	for _, pooled := range t.timedFrames {
		if pooled.leased && pooled.readback == readback {
			frame = pooled
			break
		}
	}
	if frame == nil {
		return metronome.Beat{}, nil, false
	}
	if status == ReadbackStatusReady {
		if mapped, err := staging.read(readback, device); err != nil {
			reporter.logUnlessLost(fmt.Errorf("render: reading the pass timings of beat %d: %w", frame.beat.Tick, err))
		} else {
			frame.fillTimings(mapped, device.TimestampPeriod())
			frame.delivered = true
		}
	}
	if err := staging.release(readback, device); err != nil {
		reporter.logUnlessLost(fmt.Errorf("render: unmapping the pass timings of beat %d: %w", frame.beat.Tick, err))
	}
	frame.readback = Readback{}
	if !frame.delivered {
		frame.release()
		return frame.beat, nil, true
	}
	return frame.beat, frame.passTimings, true
}

// read answers the timings of the frame of beat while they are delivered.
func (t *passTimer) read(beat metronome.Beat) ([]PassTiming, bool) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if frame := t.findDelivered(beat); frame != nil {
		return frame.passTimings, true
	}
	return nil, false
}

// drop releases the timed frame of beat, once PassTimingsEvent's jobs have
// run.
func (t *passTimer) drop(beat metronome.Beat) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if frame := t.findDelivered(beat); frame != nil {
		frame.release()
	}
}

// endFrame has device release the query set and resolve buffer of each
// timed frame that has now gone unused for idleFrames frames, and forgets
// those frames.
func (t *passTimer) endFrame(idleFrames int, device gfx.Device) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	keptFrames := t.timedFrames[:0]
	for _, frame := range t.timedFrames {
		if !frame.endFrame(idleFrames) {
			keptFrames = append(keptFrames, frame)
			continue
		}
		device.ReleaseQuerySet(frame.querySet)
		device.ReleaseBuffer(frame.resolveBuffer)
	}
	clear(t.timedFrames[len(keptFrames):])
	t.timedFrames = keptFrames
}

// reportUnavailable turns pass timings off for good, and reports, the
// first time, through reporter, that they are unavailable, as err says.
func (t *passTimer) reportUnavailable(err error, reporter reporter) {
	t.enabled.Store(false)
	if t.unavailable.CompareAndSwap(false, true) {
		reporter.report(ReportKindPassTimingsUnavailable, fmt.Errorf("render: pass timings are unavailable: %w", err))
	}
}

// findDelivered answers the timed frame of beat whose timings are
// delivered; nil for none. t's mutex is held.
func (t *passTimer) findDelivered(beat metronome.Beat) *timedFrame {
	for _, frame := range t.timedFrames {
		if frame.delivered && frame.beat == beat {
			return frame
		}
	}
	return nil
}
