package render

import (
	"errors"
	"fmt"
	"math/bits"
	"sync"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/render/internal/handle"
)

// stagingPool is the pool of staging buffers readbacks are copied into, with
// the readbacks in flight. A buffer is leased by one readback from its
// recording until the out dependency of the publication delivering it,
// mapped from the end of the frame the readback was recorded in, and freed
// once it has gone unused for as many frames as render is configured to
// wait. Its size is the readback's rounded up to a quarter of the power of
// two below it, so readbacks of sizes near one another share buffers and a
// buffer wastes at most a quarter of itself, and at least 256 bytes, a
// copied row's.
type stagingPool struct {
	// mutex guards the rest: buffers, every staging buffer made and not
	// freed; readbacks, the readbacks in flight; recordedReadbacks, those
	// recorded since the last frame's end mapped them; and
	// mappingReadbacks, those gfx is mapping, by their staging buffers.
	mutex             sync.Mutex
	buffers           []*stagingBuffer
	readbacks         handle.SlotMap[Readback, readbackSlot]
	recordedReadbacks []Readback
	mappingReadbacks  map[gfx.Buffer]Readback
}

// acquire hands out a readback of size bytes, more than none, and the gfx
// buffer to copy them into from its start: a staging buffer no readback
// leases, made by device when the pool has none of size's size class. The
// error is gfx's, when a buffer cannot be made.
func (p *stagingPool) acquire(size uint64, device gfx.Device) (Readback, gfx.Buffer, error) {
	if size == 0 {
		panic("render: acquiring a readback of no bytes")
	}
	sizeStep := uint64(1) << max(bits.Len64(size)-3, 0)
	bufferSize := max(copyRowAlignment, (size+sizeStep-1)&^(sizeStep-1))
	p.mutex.Lock()
	defer p.mutex.Unlock()
	var staging *stagingBuffer
	for _, pooled := range p.buffers {
		if !pooled.leased && pooled.size == bufferSize {
			staging = pooled
			break
		}
	}
	if staging == nil {
		buffer, err := device.CreateBuffer(gfx.BufferDescriptor{Label: handle.StagingBufferLabel, Size: bufferSize, Usage: gfx.BufferUsageFlagCopyDst | gfx.BufferUsageFlagMapRead})
		if err != nil {
			return Readback{}, gfx.Buffer{}, err
		}
		staging = &stagingBuffer{buffer: buffer, size: bufferSize}
		p.buffers = append(p.buffers, staging)
	}
	staging.leased = true
	readback, slot := p.readbacks.Acquire()
	*slot = readbackSlot{staging: staging, size: size}
	p.recordedReadbacks = append(p.recordedReadbacks, readback)
	return readback, staging.buffer, nil
}

// mapRecorded starts the map of the staging buffer of each readback
// recorded since it last ran, on device, whose submit carrying their copies
// has run, and answers the readbacks whose map failed to start, to deliver
// failed; their errors go to reporter, but for a device loss, which gfx has
// logged.
func (p *stagingPool) mapRecorded(device gfx.Device, reporter reporter) []Readback {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.mappingReadbacks == nil {
		p.mappingReadbacks = map[gfx.Buffer]Readback{}
	}
	var failedReadbacks []Readback
	for _, readback := range p.recordedReadbacks {
		slot, _ := p.readbacks.Find(readback)
		if err := device.MapBuffer(slot.staging.buffer, gfx.MapModeRead, 0, (slot.size+3)&^3); err != nil {
			reporter.logUnlessLost(fmt.Errorf("render: mapping the staging buffer of readback %v: %w", readback, err))
			failedReadbacks = append(failedReadbacks, readback)
			continue
		}
		p.mappingReadbacks[slot.staging.buffer] = readback
	}
	p.recordedReadbacks = p.recordedReadbacks[:0]
	return failedReadbacks
}

// resolveMap answers the readback whose staging buffer gfx has mapped, or
// failed to map, as status says, and marks it ready when mapped; false when
// buffer is no staging buffer of a readback being mapped.
func (p *stagingPool) resolveMap(buffer gfx.Buffer, status ReadbackStatus) (Readback, bool) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	readback, ok := p.mappingReadbacks[buffer]
	if !ok {
		return Readback{}, false
	}
	delete(p.mappingReadbacks, buffer)
	slot, _ := p.readbacks.Find(readback)
	slot.ready = status == ReadbackStatusReady
	return readback, true
}

// read answers readback's bytes, mapped by device, while it is delivered
// with them; an error once it is released, or when it holds none.
func (p *stagingPool) read(readback Readback, device gfx.Device) ([]byte, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	slot, ok := p.readbacks.Find(readback)
	switch {
	case !ok:
		return nil, errors.New("it was never recorded, or was delivered and its publication's jobs have run")
	case !slot.ready:
		return nil, errors.New("it holds no bytes")
	}
	mapped, err := device.MappedRange(slot.staging.buffer, 0, (slot.size+3)&^3)
	if err != nil {
		return nil, err
	}
	return mapped[:slot.size:slot.size], nil
}

// release ends readback, its staging buffer going back to the pool, used in
// this frame, unmapped by device when its bytes were mapped; the error is
// the unmap's.
func (p *stagingPool) release(readback Readback, device gfx.Device) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	slot, ok := p.readbacks.Find(readback)
	if !ok {
		return nil
	}
	var err error
	if slot.ready {
		err = device.UnmapBuffer(slot.staging.buffer)
	}
	slot.staging.leased = false
	slot.staging.held = true
	p.readbacks.Release(readback)
	return err
}

// endFrame takes back every buffer used this frame, and has device release
// those no readback leases that have now gone unused for idleFrames frames.
func (p *stagingPool) endFrame(idleFrames int, device gfx.Device) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	keptBuffers := p.buffers[:0]
	for _, staging := range p.buffers {
		if staging.endFrame(idleFrames) {
			device.ReleaseBuffer(staging.buffer)
			continue
		}
		keptBuffers = append(keptBuffers, staging)
	}
	clear(p.buffers[len(keptBuffers):])
	p.buffers = keptBuffers
}

// stagingBuffer is one buffer of the staging pool: whether a readback leases
// it or it was used this frame, and gfx's buffer, of size bytes.
type stagingBuffer struct {
	poolHold
	buffer gfx.Buffer
	size   uint64
}

// readbackSlot is a readback in flight: the staging buffer it leases, how
// many bytes of it the readback fills, and whether it was delivered with
// them mapped.
type readbackSlot struct {
	staging *stagingBuffer
	size    uint64
	ready   bool
}
