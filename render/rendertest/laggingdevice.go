package rendertest

import "github.com/dvoyni/cog/gfx"

// laggingDevice is a recording device whose GPU finishes the work submitted
// to it only when the harness's next Step begins, as a GPU a frame behind
// would: a frame submitted in one beat is seen complete in the next, so
// FrameEvent's GPULatency reads FrameInterval.
type laggingDevice struct {
	gfx.BackendDevice
	harness *Harness
}

// Submit submits buffers to the recording device, and keeps the index it
// answers as the newest submitted.
func (d laggingDevice) Submit(buffers ...gfx.CommandBuffer) (uint64, error) {
	index, err := d.BackendDevice.Submit(buffers...)
	if err == nil {
		d.harness.submittedIndex.Store(index)
	}
	return index, err
}

// Poll answers the newest submission the recording device finished, but none
// after the newest the harness's last Step found submitted.
func (d laggingDevice) Poll() uint64 {
	return min(d.BackendDevice.Poll(), d.harness.finishedIndex.Load())
}
