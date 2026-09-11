package wgpu

import (
	cgfx "github.com/dvoyni/cog/gfx"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// The readback, and where the wait went.
//
// Device.Poll(PollWait) calls WaitIdle, a device-wide CPU-on-GPU stall and the
// most expensive call in the API. Blocking the render thread on a map is not
// merely slow, it is self-defeating: a capture that stalls the render thread
// changes the thing it is measuring, and the agent asking why a frame is slow
// makes it slow by asking. So nothing here waits. The sequence, all of it on
// the render thread:
//
//  1. Inside Execute, after the present: CopyTextureToBuffer into a fresh
//     staging buffer, then Finish, then the frame's single Submit.
//  2. Immediately after that submit: MapAsync, keeping the *MapPending.
//  3. The next frame's own Submit triages it. No polling code is needed
//     anywhere - Queue.Submit auto-polls at its tail, and gogpu's own buffer
//     documentation says callers should rely on exactly that.
//  4. TakeCapture checks Status, a field read, and on ready copies the bytes
//     out before Unmap.
//
// The render thread's whole added cost per frame with nothing outstanding is
// one length check. The consequence to carry upward is that one frame of
// latency is a fixed property of every capture, which is why pause cannot mean
// "no submits".

// captureAlignment is the row pitch a texture-to-buffer copy's destination has
// to respect. Nothing public exposes it: hal.Alignments is HAL-only and Limits
// has no row-pitch field, so it is a constant here as it is everywhere else
// that does this. The copy always uses offset zero, which dodges DX12's
// separate 512-byte offset alignment entirely.
const captureAlignment = 256

// maxOutstandingCaptures is how many readbacks the backend keeps live at once.
// Two rather than one, and not because two captures may be in flight: the
// frame that encodes the next still is the frame whose submit resolves the
// previous one, so one slot is transiently held by a readback that has
// resolved and not yet been taken. A third would mean gfx armed two captures
// in one frame, which it never does.
const maxOutstandingCaptures = 2

// captureRowBytes is the padded stride of one row of a readback.
func captureRowBytes(width, bytesPerTexel int) int {
	row := width * bytesPerTexel
	return (row + captureAlignment - 1) / captureAlignment * captureAlignment
}

// captureSupported reports whether a format is one a capture can be an image
// of. Depth is refused outright, and so is anything else that is not 8-bit
// RGBA: cog's format table is closed, so this is a whitelist rather than a
// guess.
func captureSupported(format cgfx.TextureFormat) bool {
	switch format.Resolve() {
	case cgfx.FormatRGBA8, cgfx.FormatRGBA8Srgb:
		return true
	default:
		return false
	}
}

// captureStaging is the CPU-visible half of one outstanding readback. It is an
// interface so the ring's bookkeeping - which readback is next, which refusals
// are pending, what shutdown does to both - is exercised without a GPU device,
// which no test in this repository has.
type captureStaging interface {
	// status reports whether the map has resolved, and the failure it resolved
	// with if it failed.
	status() (ready bool, err error)
	// read copies size bytes out of the mapped range. The copy is not
	// optional: the range is a pointer into HAL memory rather than a copy of
	// it, and it stops being valid at Unmap.
	read(size int) []byte
	// release unmaps and drops the staging buffer and the pending handle. A
	// MapPending that is not released is a leak, whatever the pool does with
	// the entry afterwards.
	release()
}

// captureReadback is one outstanding readback: either a staging buffer being
// mapped, or a refusal that never reached the GPU at all.
type captureReadback struct {
	staging       captureStaging
	width, height int
	rowBytes      int
	format        cgfx.TextureFormat
	err           error
}

// captureRing holds the readbacks the backend owes an answer for, in the order
// they were encoded. It is a ring rather than a single slot only because of
// the one-frame overlap maxOutstandingCaptures describes.
type captureRing struct{ outstanding []captureReadback }

// full reports whether another readback would exceed what the backend keeps
// live. Refusals cost nothing and do not count.
func (r *captureRing) full() bool {
	live := 0
	for i := range r.outstanding {
		if r.outstanding[i].staging != nil {
			live++
		}
	}
	return live >= maxOutstandingCaptures
}

// refuse records a capture that will never happen, so the reason travels the
// same seam a result would have.
func (r *captureRing) refuse(err error) {
	r.outstanding = append(r.outstanding, captureReadback{err: err})
}

// push records an encoded readback waiting on its map.
func (r *captureRing) push(entry captureReadback) {
	r.outstanding = append(r.outstanding, entry)
}

// take reports the oldest readback if it has resolved, and clears it. It is a
// Status check - a field read - when the head has not resolved, and reports
// nothing at all when nothing is outstanding.
func (r *captureRing) take() (cgfx.GpuCapture, bool) {
	if len(r.outstanding) == 0 {
		return cgfx.GpuCapture{}, false
	}
	head := r.outstanding[0]
	if head.err == nil {
		ready, err := head.staging.status()
		if !ready {
			return cgfx.GpuCapture{}, false
		}
		head.err = err
	}
	r.pop()
	if head.staging == nil {
		return cgfx.GpuCapture{Err: head.err}, true
	}
	defer head.staging.release()
	if head.err != nil {
		return cgfx.GpuCapture{Err: head.err}, true
	}
	return cgfx.GpuCapture{
		Pixels: head.staging.read(head.rowBytes * head.height),
		Width:  head.width, Height: head.height,
		Format: head.format, BytesPerRow: head.rowBytes,
	}, true
}

// abandon releases every outstanding readback and leaves the reason in its
// place. The engine cancels its context before any Stop, so a capture armed in
// the last frame has no further submit to resolve on; left alone its waiter
// learns nothing until the client's own idle abort.
func (r *captureRing) abandon() {
	for i := range r.outstanding {
		if staging := r.outstanding[i].staging; staging != nil {
			staging.release()
		}
		r.outstanding[i] = captureReadback{err: cgfx.ErrCaptureAbandoned{}}
	}
}

// pop drops the head, keeping the backing array.
func (r *captureRing) pop() {
	r.outstanding = append(r.outstanding[:0], r.outstanding[1:]...)
}

// gfxbStaging is a real staging buffer and the map waiting on it.
type gfxbStaging struct {
	buffer  *wgpu.Buffer
	pending *wgpu.MapPending
}

func (s *gfxbStaging) status() (bool, error) {
	if s.pending == nil {
		return false, nil
	}
	return s.pending.Status()
}

func (s *gfxbStaging) read(size int) []byte {
	if s.buffer == nil || size <= 0 {
		return nil
	}
	mapped, err := s.buffer.MappedRange(0, uint64(size))
	if err != nil {
		return nil
	}
	pixels := append([]byte(nil), mapped.Bytes()...)
	mapped.Release()
	return pixels
}

func (s *gfxbStaging) release() {
	if s.buffer != nil {
		_ = s.buffer.Unmap()
	}
	if s.pending != nil {
		s.pending.Release()
		s.pending = nil
	}
	if s.buffer != nil {
		s.buffer.Release()
		s.buffer = nil
	}
}

// Capture encodes the frame's readback into the frame's own encoder, after the
// present. It never waits: the map is started after the submit, in armCapture.
func (b *gfxBackend) Capture(desc cgfx.GpuCaptureDesc) {
	if b.encoder == nil {
		return
	}
	texture, width, height, format := b.captureSource(desc)
	switch {
	case texture == nil:
		b.captures.refuse(cgfx.ErrCaptureNoTarget{})
		return
	case !captureSupported(format):
		b.captures.refuse(cgfx.ErrCaptureUnsupported{Format: format})
		return
	case b.captures.full():
		b.captures.refuse(cgfx.ErrCaptureBusy{})
		return
	}

	rowBytes := captureRowBytes(width, bytesPerTexel(format))
	staging, err := b.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "gfx.capture", Size: uint64(rowBytes * height),
		Usage: gputypes.BufferUsageMapRead | gputypes.BufferUsageCopyDst,
	})
	if err != nil {
		b.captures.refuse(err)
		return
	}

	// The frame buffer is the one attachment gfx never names, so its barriers
	// are the backend's. It arrives here in TextureBinding, because the present
	// pass put it there and then sampled it, and it is put back afterwards so
	// that the next frame's present barrier still names the layout the buffer
	// is actually in. A texture capture needs neither: gfx tracked that
	// texture's role all frame and declared the transition itself.
	if desc.Screen {
		b.captureBarrier(texture, gputypes.TextureUsageTextureBinding, gputypes.TextureUsageCopySrc)
	}
	b.encoder.CopyTextureToBuffer(texture.tex, staging, []wgpu.BufferTextureCopy{{
		BufferLayout: wgpu.ImageDataLayout{
			Offset: 0, BytesPerRow: uint32(rowBytes), RowsPerImage: uint32(height),
		},
		TextureBase: wgpu.ImageCopyTexture{
			Texture: texture.tex, MipLevel: 0,
			Origin: wgpu.Origin3D{}, Aspect: gputypes.TextureAspectAll,
		},
		Size: wgpu.Extent3D{Width: uint32(width), Height: uint32(height), DepthOrArrayLayers: 1},
	}})
	if desc.Screen {
		b.captureBarrier(texture, gputypes.TextureUsageCopySrc, gputypes.TextureUsageTextureBinding)
	}
	b.captures.push(captureReadback{
		staging: &gfxbStaging{buffer: staging},
		width:   width, height: height, rowBytes: rowBytes, format: format,
	})
}

// captureBarrier moves the frame buffer between the role the present pass left
// it in and the one a copy needs.
func (b *gfxBackend) captureBarrier(texture *gfxbTexture, from, to gputypes.TextureUsage) {
	b.encoder.TransitionTextures([]wgpu.TextureBarrier{{
		Texture: texture.tex,
		Range: wgpu.TextureRange{
			Aspect: gputypes.TextureAspectAll, MipLevelCount: 1, ArrayLayerCount: 1,
		},
		Usage: wgpu.TextureUsageTransition{OldUsage: from, NewUsage: to},
	}})
}

// captureSource resolves what a capture reads. Screen is the frame buffer gfx
// owns, which only the backend can resolve because it is sized from the
// surface; a texture capture always reads mip 0, layer 0.
func (b *gfxBackend) captureSource(desc cgfx.GpuCaptureDesc) (*gfxbTexture, int, int, cgfx.TextureFormat) {
	if desc.Screen {
		if b.frame == nil {
			return nil, 0, 0, cgfx.FrameBufferFormat
		}
		return b.frame, b.frameW, b.frameH, cgfx.FrameBufferFormat
	}
	texture, ok := b.bakedTextures[desc.Texture]
	if !ok || texture == nil {
		return nil, 0, 0, cgfx.FormatRGBA8
	}
	held := b.bakedTextureDescs[desc.Texture]
	return texture, held.Width, held.Height, held.Format
}

// armCapture starts the map for the readback this frame encoded. It runs
// immediately after the frame's single submit, so the map has a submission to
// resolve against and the next frame's submit is what triages it.
func (b *gfxBackend) armCapture() {
	for i := range b.captures.outstanding {
		entry := &b.captures.outstanding[i]
		staging, ok := entry.staging.(*gfxbStaging)
		if !ok || staging.pending != nil || staging.buffer == nil {
			continue
		}
		pending, err := staging.buffer.MapAsync(
			wgpu.MapModeRead, 0, uint64(entry.rowBytes*entry.height))
		if err != nil {
			// MapAsync validates synchronously, so a bad range fails here
			// rather than a frame later.
			staging.release()
			*entry = captureReadback{err: err}
			continue
		}
		staging.pending = pending
	}
}

// TakeCapture returns a completed readback, if one is ready, and clears it.
func (b *gfxBackend) TakeCapture() (cgfx.GpuCapture, bool) {
	return b.captures.take()
}
