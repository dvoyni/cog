package render

import (
	"cmp"
	"encoding/binary"
	"slices"
	"sync"
	"time"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/render/internal/handle"
)

// The layout of a timed frame's timestamps: maxTimestampQueryCount is how
// many its query set holds, WebGPU's most for a query set; maxTimedPassCount
// how many passes of a frame it times, each writing two, its beginning's and
// then its end's; and timestampSize the size in bytes of one read back, a
// uint64.
const (
	maxTimestampQueryCount = 4096
	maxTimedPassCount      = maxTimestampQueryCount / 2
	timestampSize          = 8
)

// timedFrame is one frame's pass timings, leased from the frame's beginning
// until PassTimingsEvent's jobs have run, and then free for another frame
// to reuse what it made: its query set, which every pass of the frame writes
// its beginning's and end's timestamps into, two queries a pass in the order
// the passes began, and the buffer the GPU resolves them into.
type timedFrame struct {
	// poolHold says whether the frame is leased, and how long it has gone
	// unused since; delivered, whether its timings are read; and beat is
	// the frame's: all under the pass timer's mutex.
	poolHold
	delivered bool
	beat      metronome.Beat
	// querySet and resolveBuffer are the frame's query set and the buffer
	// its timestamps are resolved into, then copied into readback's staging
	// buffer, while it is in flight.
	querySet      gfx.QuerySet
	resolveBuffer gfx.Buffer
	readback      Readback
	// mutex guards timedPasses and untimedPassCount while the frame
	// records: the passes timed, in the order they began, and how many
	// began once maxTimedPassCount had.
	mutex            sync.Mutex
	timedPasses      []timedPass
	untimedPassCount int
	// passTimings are the passes' timings once read, by order, until
	// PassTimingsEvent's jobs have run.
	passTimings []PassTiming
}

// newTimedFrame makes, on device, a timed frame's query set and the buffer
// it resolves into, releasing the query set when the buffer fails; the
// error is gfx's.
func newTimedFrame(device gfx.Device) (*timedFrame, error) {
	querySet, err := device.CreateQuerySet(gfx.QuerySetDescriptor{Label: handle.PassTimingsLabel, Type: gfx.QueryTypeTimestamp, Count: maxTimestampQueryCount})
	if err != nil {
		return nil, err
	}
	resolveBuffer, err := device.CreateBuffer(gfx.BufferDescriptor{
		Label: handle.PassTimingsLabel,
		Size:  timestampSize * maxTimestampQueryCount,
		Usage: gfx.BufferUsageFlagQueryResolve | gfx.BufferUsageFlagCopySrc,
	})
	if err != nil {
		device.ReleaseQuerySet(querySet)
		return nil, err
	}
	return &timedFrame{querySet: querySet, resolveBuffer: resolveBuffer}, nil
}

// begin leases f to time the frame of beat.
func (f *timedFrame) begin(beat metronome.Beat) {
	f.leased = true
	f.held = true
	f.delivered = false
	f.beat = beat
	f.timedPasses = f.timedPasses[:0]
	f.untimedPassCount = 0
}

// addPass answers where the pass the list at order begins, labelled label,
// writes its timestamps: the next two queries of f's query set, or none once
// maxTimedPassCount passes have begun.
func (f *timedFrame) addPass(order Order, label string) gfx.PassTimestampWrites {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if len(f.timedPasses) == maxTimedPassCount {
		f.untimedPassCount++
		return gfx.PassTimestampWrites{}
	}
	firstQuery := 2 * uint32(len(f.timedPasses))
	f.timedPasses = append(f.timedPasses, timedPass{order: order, label: label})
	return gfx.PassTimestampWrites{QuerySet: f.querySet, BeginningOfPassWriteIndex: firstQuery, EndOfPassWriteIndex: firstQuery + 1}
}

// recordReadback records, in render's closing command list of gfxFrame, on
// device, after every pass of the frame, the commands reading f's
// timestamps back: their resolve into f's resolve buffer, and its copy into
// the staging buffer of a readback acquired from staging, which the staging
// pool maps at the frame's end. The error is gfx's, and acquires none.
func (f *timedFrame) recordReadback(device gfx.Device, gfxFrame gfx.Frame, staging *stagingPool) error {
	queryCount := 2 * uint32(len(f.timedPasses))
	size := timestampSize * uint64(queryCount)
	encoder, err := device.Encoder(gfxFrame, gfx.SubmitOrder(closingOrder), handle.PassTimingsLabel)
	if err != nil {
		return err
	}
	readback, stagingBuffer, err := staging.acquire(size, device)
	if err != nil {
		return err
	}
	f.readback = readback
	encoder.ResolveQuerySet(f.querySet, 0, queryCount, f.resolveBuffer, 0)
	encoder.CopyBufferToBuffer(f.resolveBuffer, 0, stagingBuffer, 0, size)
	return nil
}

// fillTimings fills f's timings from mapped, the timestamps of its passes,
// each a tick of period nanoseconds: each pass's from its beginning's to
// its end's, none when its end is not later, sorted by order, those of one
// order in the order they began.
func (f *timedFrame) fillTimings(mapped []byte, period float32) {
	f.passTimings = f.passTimings[:0]
	for index, pass := range f.timedPasses {
		beginning := binary.LittleEndian.Uint64(mapped[2*timestampSize*index:])
		end := binary.LittleEndian.Uint64(mapped[2*timestampSize*index+timestampSize:])
		var duration time.Duration
		if end > beginning {
			duration = time.Duration(float64(end-beginning) * float64(period))
		}
		f.passTimings = append(f.passTimings, PassTiming{Order: pass.order, Label: pass.label, Duration: duration})
	}
	slices.SortStableFunc(f.passTimings, func(a, b PassTiming) int { return cmp.Compare(a.Order, b.Order) })
}

// release gives f back to the pass timer, for another frame to lease.
func (f *timedFrame) release() {
	f.leased = false
	f.delivered = false
	f.readback = Readback{}
}

// timedPass is a pass a timed frame times: its list's order and its label.
type timedPass struct {
	order Order
	label string
}
