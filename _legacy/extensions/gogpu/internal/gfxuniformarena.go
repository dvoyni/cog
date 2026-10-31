package internal

import (
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// gfxbUniformFloor is the arena's floor in bytes and the unit it doubles from:
// 16 KB, 64 aligned blocks, comfortably past the ~50-100 uniform-carrying draws
// a feuds-26 battle measures, so the ordinary frame never grows at all.
const gfxbUniformFloor = 64 * gfx.UniformAlignment

// gfxbUniformArenaID is the id every uniform binding key carries. There is one
// uniform buffer now, so what distinguishes one draw's binding from another's
// is its offset, and the id is constant. It does not collide with a baked
// buffer's id because the key's kind is part of it.
const gfxbUniformArenaID = 0

// gfxbUniformArena is the GPU side of the frame's uniform arena: one buffer the
// gfx queue's arena is written into whole, each block at its own aligned offset.
//
// It replaced a pool of one 256-byte buffer object per uniform-carrying draw,
// each written on its own. The pool grew to the high-water mark of any single
// frame and nothing ever released it, so one spiky frame permanently raised the
// resident buffer count; and it cost a WriteBuffer per draw. One buffer costs
// one of each per frame, and turns "a growing count of GPU objects" into one
// buffer whose size is a number.
//
// It keeps no CPU copy. gfx packs every block straight into the queue's arena,
// already at the offsets the buffer wants, so the frame's write is that arena
// as it stands.
//
// The device work is injected rather than reached for, the same seam
// gfxbBindGroupCache takes and for the same reason: the arena's whole claim is
// that there is one buffer object and not N, and that is unobservable against a
// real device. invalidate is among them because the ordering matters and this is
// the only place that knows it - see reserve.
type gfxbUniformArena struct {
	create     func(size int) (*wgpu.Buffer, error)
	release    func(*wgpu.Buffer)
	write      func(buffer *wgpu.Buffer, data []byte)
	invalidate func()

	buffer *wgpu.Buffer
	// size is the buffer's capacity in bytes. generation moves whenever the
	// buffer is replaced, and rides in every binding key so a cached bind group
	// naming the old one cannot be mistaken for one naming the new.
	size       int
	generation uint32
	// written is how many of this frame's bytes reached the buffer. A block
	// reaching past it has nothing of this frame behind it, and binds nothing.
	written int
}

func newGfxbUniformArena(
	create func(size int) (*wgpu.Buffer, error),
	release func(*wgpu.Buffer),
	write func(buffer *wgpu.Buffer, data []byte),
	invalidate func(),
) *gfxbUniformArena {
	return &gfxbUniformArena{create: create, release: release, write: write, invalidate: invalidate}
}

// upload takes the frame's arena: it makes room for it and writes it in one
// call. It runs from BakeUniforms, before the frame's encoder is created, so a
// replaced buffer is gone before any bind group could name it; and a queue
// write is ordered against the submit that follows it, so writing this early
// still covers every draw.
//
// A device that refused to grow the buffer leaves it short, and only the bytes
// that fit are written. The blocks past them are refused by bound.
func (a *gfxbUniformArena) upload(arena []byte) {
	a.reserve(len(arena))
	a.written = 0
	if a.buffer == nil || len(arena) == 0 {
		return
	}
	a.written = min(len(arena), a.size)
	a.write(a.buffer, arena[:a.written])
}

// reserve makes room for size bytes, doubling from gfxbUniformFloor until it
// fits and never shrinking. A frame needing none allocates none, which is the
// scene-only app: every scene parameter binds as var<storage, read>.
//
// It runs before the frame's encoder is created, which is what keeps it simple.
// The size is the length of the queue's arena, so it is known before the first
// bind group exists, and a replaced buffer can be released on the spot: nothing
// has been recorded against it, and the bind groups that name it are dropped
// first. Both orderings matter. Releasing before invalidating would leave the
// cache serving bind groups over a freed buffer, and growing after the encoder
// opened would mean invalidating groups already recorded into it.
//
// A device that refuses the allocation leaves the arena on the buffer it had.
// bound then refuses every block past what was written, and the draw that asked
// for one leaves its group unfilled, which flushBinds refuses and drops.
func (a *gfxbUniformArena) reserve(size int) {
	if size <= a.size {
		return
	}
	want := max(a.size, gfxbUniformFloor)
	for want < size {
		want *= 2
	}
	buffer, err := a.create(want)
	if err != nil {
		return
	}
	if a.buffer != nil {
		a.invalidate()
		a.release(a.buffer)
	}
	a.buffer = buffer
	a.size = want
	a.generation++
}

// bound reports whether a block of size bytes at offset reached the buffer this
// frame. A false is the caller's cue to emit no binding: binding it would render
// the draw with whatever an earlier frame left there.
func (a *gfxbUniformArena) bound(offset, size int) bool {
	return offset >= 0 && size > 0 && offset+size <= a.written
}

// newGfxbUniformArenaOn builds the arena over a device, which is the only
// caller that is not a test.
func newGfxbUniformArenaOn(b *gfxBackend) *gfxbUniformArena {
	return newGfxbUniformArena(
		func(size int) (*wgpu.Buffer, error) {
			return b.device.CreateBuffer(&wgpu.BufferDescriptor{
				Label: "gfx.uniform", Size: uint64(size),
				Usage: gputypes.BufferUsageUniform | gputypes.BufferUsageCopyDst,
			})
		},
		func(buffer *wgpu.Buffer) { buffer.Release() },
		func(buffer *wgpu.Buffer, data []byte) { _ = b.queue.WriteBuffer(buffer, 0, data) },
		func() { b.bindGroups.invalidateResource(gfxbBindUniform, gfxbUniformArenaID) },
	)
}
