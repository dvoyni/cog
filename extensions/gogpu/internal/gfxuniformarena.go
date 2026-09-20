package internal

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// gfxbUniformSlots is the arena's floor and the unit it doubles from: 64 blocks,
// 16 KB, comfortably past the ~50-100 uniform-carrying draws a feuds-26 battle
// measures, so the ordinary frame never grows at all.
const gfxbUniformSlots = 64

// gfxbUniformArenaID is the id every uniform binding key carries. There is one
// uniform buffer now, so what distinguishes one draw's binding from another's
// is its offset, and the id is constant. It does not collide with a baked
// buffer's id because the key's kind is part of it.
const gfxbUniformArenaID = 0

// gfxbUniformArena is the frame's shader-parameter blocks: one GPU buffer, one
// CPU staging copy of it, and a cursor handing out 256-strided slots.
//
// It replaced a pool of one 256-byte buffer object per uniform-carrying draw,
// each written on its own. The pool grew to the high-water mark of any single
// frame and nothing ever released it, so one spiky frame permanently raised the
// resident buffer count; and it cost a WriteBuffer per draw. One buffer at
// 256-strided offsets costs one of each per frame, and turns "a growing count
// of GPU objects" into one buffer whose size is a number - DrawCount x 256.
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

	buffer  *wgpu.Buffer
	staging []byte
	// slots is the capacity in blocks, which is len(staging)/gfxbUniformSize and
	// the buffer's size in the same unit. generation moves whenever the buffer
	// is replaced, and rides in every binding key so a cached bind group naming
	// the old one cannot be mistaken for one naming the new.
	slots      int
	generation uint32
	// cursor is the next slot this frame hands out. It is the frame's, not a
	// pass's: every draw in the frame stages into the same buffer, so all of it
	// can be written in one call before the single submit.
	cursor int
}

func newGfxbUniformArena(
	create func(size int) (*wgpu.Buffer, error),
	release func(*wgpu.Buffer),
	write func(buffer *wgpu.Buffer, data []byte),
	invalidate func(),
) *gfxbUniformArena {
	return &gfxbUniformArena{create: create, release: release, write: write, invalidate: invalidate}
}

// reset starts a frame. Capacity survives it; only the cursor does not.
func (a *gfxbUniformArena) reset() { a.cursor = 0 }

// reserve makes room for the frame's blocks, doubling from gfxbUniformSlots
// until it fits and never shrinking. A frame needing none allocates none, which
// is the scene-only app: every scene parameter binds as var<storage, read>.
//
// It runs before the frame's encoder is created, which is what keeps it simple.
// The count comes from the queue that Execute is handed, so the size is known
// before the first bind group exists, and a replaced buffer can be released on
// the spot: nothing has been recorded against it, and the bind groups that name
// it are dropped first. Both orderings matter. Releasing before invalidating
// would leave the cache serving bind groups over a freed buffer, and growing
// after the encoder opened would mean invalidating groups already recorded into
// it.
//
// A device that refuses the allocation leaves the arena on the buffer it had.
// claim then hands out no slot past that capacity, and the draw that asked for
// one leaves its group unfilled, which flushBinds refuses and drops.
func (a *gfxbUniformArena) reserve(slots int) {
	if slots <= a.slots {
		return
	}
	want := a.slots
	if want < gfxbUniformSlots {
		want = gfxbUniformSlots
	}
	for want < slots {
		want *= 2
	}
	buffer, err := a.create(want * gfxbUniformSize)
	if err != nil {
		return
	}
	if a.buffer != nil {
		a.invalidate()
		a.release(a.buffer)
	}
	a.buffer = buffer
	a.staging = make([]byte, want*gfxbUniformSize)
	a.slots = want
	a.generation++
}

// claim stages one draw's block and reports the offset it was staged at. A
// false is the caller's cue to emit no binding: there is no buffer, or the
// frame asked for more slots than were reserved for it, which means the queue's
// count was an underestimate and cannot be.
//
// The block's tail is cleared rather than left as whatever the last frame put
// at this slot, so what reaches the GPU is a function of this frame alone.
func (a *gfxbUniformArena) claim(params []byte) (int, bool) {
	if a.buffer == nil || a.cursor >= a.slots {
		return 0, false
	}
	offset := a.cursor * gfxbUniformSize
	block := a.staging[offset : offset+gfxbUniformSize]
	n := copy(block, params)
	clear(block[n:])
	a.cursor++
	return offset, true
}

// flush writes every slot the frame claimed, in one call. It runs after the
// passes are encoded and before the submit, which is the order the write needs:
// a queue write is ordered against the submit that follows it.
func (a *gfxbUniformArena) flush() {
	if a.buffer == nil || a.cursor == 0 {
		return
	}
	a.write(a.buffer, a.staging[:a.cursor*gfxbUniformSize])
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
