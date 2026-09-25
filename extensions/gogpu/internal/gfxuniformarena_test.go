package internal

import (
	"bytes"
	"errors"
	"testing"

	"github.com/dvoyni/cog/slots/gfx"
	"github.com/gogpu/wgpu"
)

// arenaLog records what an arena asked of the device, in order. The order is
// half of what these tests assert: an invalidation that followed its release
// would leave the cache handing out bind groups that name a freed buffer.
type arenaLog struct {
	calls    []string
	buffers  []*wgpu.Buffer
	writes   [][]byte
	sizes    []int
	failFrom int
}

func newArenaLog() *arenaLog { return &arenaLog{failFrom: -1} }

func (l *arenaLog) arena() *gfxbUniformArena {
	return newGfxbUniformArena(
		func(size int) (*wgpu.Buffer, error) {
			if l.failFrom >= 0 && len(l.sizes) >= l.failFrom {
				l.calls = append(l.calls, "create-failed")
				return nil, errors.New("out of memory")
			}
			l.sizes = append(l.sizes, size)
			l.calls = append(l.calls, "create")
			buffer := &wgpu.Buffer{}
			l.buffers = append(l.buffers, buffer)
			return buffer, nil
		},
		func(*wgpu.Buffer) { l.calls = append(l.calls, "release") },
		func(_ *wgpu.Buffer, data []byte) {
			l.calls = append(l.calls, "write")
			l.writes = append(l.writes, bytes.Clone(data))
		},
		func() { l.calls = append(l.calls, "invalidate") },
	)
}

func (l *arenaLog) count(call string) int {
	n := 0
	for _, c := range l.calls {
		if c == call {
			n++
		}
	}
	return n
}

// The point of the arena: N uniform-carrying draws are one buffer object and
// one write, not N of each. The pool it replaced created a 256-byte buffer per
// draw and wrote each one separately, so a spiky frame raised the resident
// buffer count for the life of the process.
func TestTheUniformArenaIsOneBufferAndOneWritePerFrame(t *testing.T) {
	log := newArenaLog()
	arena := log.arena()

	for frame := 0; frame < 3; frame++ {
		arena.upload(make([]gfx.UniformBlock, 8))
		for slot := 0; slot < 8; slot++ {
			if _, ok := arena.offset(slot); !ok {
				t.Fatalf("frame %d slot %d has no offset", frame, slot)
			}
		}
	}

	if got := log.count("create"); got != 1 {
		t.Errorf("buffers created = %d, want 1 for 24 uniform draws over 3 frames", got)
	}
	if got := log.count("write"); got != 3 {
		t.Errorf("writes = %d, want one per frame", got)
	}
}

// Every block lands at its own 256-strided offset, which is what makes one
// buffer usable at all: a uniform binding's offset must be a multiple of
// minUniformBufferOffsetAlignment, and 256 is both that and the stride gfx
// packs its blocks at. The write is the queue's arena byte for byte, since the
// arena keeps no copy of its own.
func TestTheUniformArenaWritesEachBlockAtItsOwnStride(t *testing.T) {
	log := newArenaLog()
	arena := log.arena()

	fills := []byte{0xA1, 0xB2, 0xC3}
	blocks := make([]gfx.UniformBlock, len(fills))
	for slot, fill := range fills {
		copy(blocks[slot][:16], bytes.Repeat([]byte{fill}, 16))
	}
	arena.upload(blocks)

	for slot, want := range []int{0, gfxbUniformSize, 2 * gfxbUniformSize} {
		offset, ok := arena.offset(slot)
		if !ok || offset != want {
			t.Errorf("slot %d offset = (%d, %v), want (%d, true)", slot, offset, ok, want)
		}
	}
	if len(log.writes) != 1 {
		t.Fatalf("writes = %d, want 1", len(log.writes))
	}
	written := log.writes[0]
	if len(written) != 3*gfxbUniformSize {
		t.Fatalf("written bytes = %d, want 3 full strides", len(written))
	}
	for slot := range fills {
		block := written[slot*gfxbUniformSize : (slot+1)*gfxbUniformSize]
		if !bytes.Equal(block, blocks[slot][:]) {
			t.Errorf("slot %d = %x..., want the queue's block", slot, block[:4])
		}
	}
}

// Capacity doubles rather than tracking the frame's exact need. Growing to the
// need would reallocate - and flush every cached bind group naming the buffer -
// on every frame whose draw count ticks up by one, which in a scene with a
// rising entity count is every frame.
func TestTheUniformArenaDoublesAndKeepsItsPeak(t *testing.T) {
	log := newArenaLog()
	arena := log.arena()

	arena.reserve(1)
	first := arena.generation
	arena.reserve(gfxbUniformSlots + 1)
	if arena.generation == first {
		t.Error("the buffer was replaced without moving the generation")
	}
	if got := log.count("create"); got != 2 {
		t.Fatalf("creates = %d, want a second for the growth", got)
	}
	if got, want := log.sizes[1], 2*gfxbUniformSlots*gfxbUniformSize; got != want {
		t.Errorf("grown size = %d, want %d - a doubling, not the exact need", got, want)
	}

	// A frame back under the peak reallocates nothing: the arena keeps its
	// high-water mark, which is the whole of its way back down. One buffer at
	// the peak is 25 KB at 100 draws, and FrameView.DrawCount already bounds it.
	grown := arena.generation
	arena.reserve(4)
	if got := log.count("create"); got != 2 {
		t.Errorf("creates after shrinking back = %d, want no reallocation", got)
	}
	if arena.generation != grown {
		t.Error("a frame under the peak moved the generation")
	}
}

// The replaced buffer is released, and the bind groups naming it are dropped
// first. Both happen before the frame's encoder exists, which is why this needs
// no deferred-release list: nothing has been recorded against the old buffer.
func TestTheUniformArenaInvalidatesBeforeItReleases(t *testing.T) {
	log := newArenaLog()
	arena := log.arena()

	arena.reserve(1)
	arena.reserve(gfxbUniformSlots + 1)

	want := []string{"create", "create", "invalidate", "release"}
	if len(log.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", log.calls, want)
	}
	for i := range want {
		if log.calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", log.calls, want)
		}
	}
}

// A device that refuses the allocation leaves the arena with no buffer, and it
// hands out no slot it cannot back. The draw that asked for one then leaves its
// group unfilled, which flushBinds refuses and drops - the alternative being a
// draw that renders with another draw's parameters.
func TestAUniformArenaThatCannotAllocateHandsOutNoSlot(t *testing.T) {
	log := newArenaLog()
	log.failFrom = 0
	arena := log.arena()

	arena.upload(make([]gfx.UniformBlock, 4))
	if _, ok := arena.offset(0); ok {
		t.Error("a slot was handed out with no buffer to back it")
	}
	if got := log.count("write"); got != 0 {
		t.Errorf("writes = %d, want none - there is nothing to write to", got)
	}
}

// A device that refuses to grow the buffer leaves it on the capacity it had:
// the blocks that fit are written, and a slot past them is refused rather than
// served from bytes an earlier frame left behind.
func TestAUniformArenaThatCannotGrowWritesWhatFits(t *testing.T) {
	log := newArenaLog()
	arena := log.arena()
	arena.upload(make([]gfx.UniformBlock, 1))
	log.failFrom = 1

	arena.upload(make([]gfx.UniformBlock, gfxbUniformSlots+1))
	if got := len(log.writes[len(log.writes)-1]); got != gfxbUniformSlots*gfxbUniformSize {
		t.Errorf("written bytes = %d, want the %d slots that fit", got, gfxbUniformSlots)
	}
	if _, ok := arena.offset(gfxbUniformSlots - 1); !ok {
		t.Error("the last slot inside capacity was refused")
	}
	if _, ok := arena.offset(gfxbUniformSlots); ok {
		t.Error("a slot past capacity was served")
	}
}

// A slot is good for the frame that wrote it and no other. A smaller frame
// after a larger one leaves the larger one's blocks in the buffer, and binding
// one of them would render a draw with parameters from a frame ago.
func TestTheUniformArenaRefusesASlotThisFrameDidNotWrite(t *testing.T) {
	log := newArenaLog()
	arena := log.arena()

	arena.upload(make([]gfx.UniformBlock, 4))
	arena.upload(make([]gfx.UniformBlock, 1))
	if _, ok := arena.offset(0); !ok {
		t.Error("the frame's own slot was refused")
	}
	if _, ok := arena.offset(2); ok {
		t.Error("a slot only the previous frame wrote was served")
	}
}

// A frame with no uniform-carrying draw holds no uniform buffer. Scene binds
// every parameter as var<storage, read>, so a scene-only app never allocates
// one at all.
func TestTheUniformArenaAllocatesNothingForAFrameWithNoUniforms(t *testing.T) {
	log := newArenaLog()
	arena := log.arena()

	arena.upload(nil)

	if got := log.count("create"); got != 0 {
		t.Errorf("creates = %d, want none", got)
	}
	if got := log.count("write"); got != 0 {
		t.Errorf("writes = %d, want none", got)
	}
}

func BenchmarkGfxUniformArenaUpload(b *testing.B) {
	arena := newGfxbUniformArena(
		func(int) (*wgpu.Buffer, error) { return &wgpu.Buffer{}, nil },
		func(*wgpu.Buffer) {},
		func(*wgpu.Buffer, []byte) {},
		func() {},
	)
	blocks := make([]gfx.UniformBlock, 256)
	b.ReportAllocs()
	for b.Loop() {
		arena.upload(blocks)
		for slot := range blocks {
			arena.offset(slot)
		}
	}
}

// What a draw's uniform binding says once the pool became one buffer: the id is
// constant and the offset is the draw's own slot. The pool it replaced said the
// opposite - a distinct id per slot, offset zero - so this is the whole change
// as the bind-group cache sees it.
func TestAUniformBindingIsKeyedByOffsetAndNotByBuffer(t *testing.T) {
	b := newGfxBackend()
	log := newArenaLog()
	b.uniforms = log.arena()
	b.BakeUniforms(make([]gfx.UniformBlock, 4))
	shader := &gfxbShader{
		label:      "canvas.wgsl",
		bgLayouts:  []*wgpu.BindGroupLayout{{}},
		groupSizes: []int{1},
		layout:     gfx.ShaderLayout{UniformSize: 64, UniformGroup: 0, UniformBinding: 0},
	}
	pass := &gfxRenderPass{backend: b, shader: shader}

	generation := b.uniforms.generation
	for slot := 0; slot < 3; slot++ {
		pass.SetUniformBlock(slot)
		if len(b.acc[0]) != 1 {
			t.Fatalf("slot %d emitted %d entries, want 1", slot, len(b.acc[0]))
		}
		key := b.acc[0][0].key
		want := gfxbBindingKey{
			kind: gfxbBindUniform, binding: 0, id: gfxbUniformArenaID,
			generation: generation, offset: uint32(slot * gfxbUniformSize),
			size: gfxbUniformSize,
		}
		if key != want {
			t.Errorf("slot %d key = %+v, want %+v", slot, key, want)
		}
		if native := b.acc[0][0].native; native.Buffer != b.uniforms.buffer ||
			native.Offset != uint64(slot*gfxbUniformSize) || native.Size != gfxbUniformSize {
			t.Errorf("slot %d native = %+v, want the arena's buffer at its own offset", slot, native)
		}
		b.resetAcc()
	}
}

// The cache still hits across frames, which is what keeps this a change of
// shape and not of cost: a draw at a stable slot with a stable texture built
// one bind group per slot before and builds one per slot now, the key differing
// by offset where it used to differ by id. A resize is the one thing that
// rebuilds them, and it rebuilds only the group the uniform is in.
func TestUniformBindGroupsSurviveFramesAndNotAResize(t *testing.T) {
	b := newGfxBackend()
	log := newArenaLog()
	b.uniforms = log.arena()
	created := 0
	b.bindGroups = newGfxBindGroupCache(
		func(*wgpu.BindGroupLayout, []wgpu.BindGroupEntry) (*wgpu.BindGroup, error) {
			created++
			return &wgpu.BindGroup{}, nil
		},
		func(*wgpu.BindGroup) {},
	)
	shader := &gfxbShader{
		label:      "canvas.wgsl",
		bgLayouts:  []*wgpu.BindGroupLayout{{}, {}},
		groupSizes: []int{1, 1},
		layout:     gfx.ShaderLayout{UniformSize: 64, UniformGroup: 0, UniformBinding: 0},
	}
	pass := &gfxRenderPass{backend: b, shader: shader}

	// Two frames of two draws each, every draw the same shader, slot and
	// texture. The cache is asked for the groups the way flushBinds asks.
	frame := func(slots int) {
		b.BakeUniforms(make([]gfx.UniformBlock, slots))
		for draw := 0; draw < 2; draw++ {
			pass.SetUniformBlock(draw)
			b.addEntry(1, gfxbBindEntry{key: gfxbBindingKey{kind: gfxbBindTexture, binding: 0, id: 7}})
			b.bindGroups.get(shader, 0, b.acc[0])
			b.bindGroups.get(shader, 1, b.acc[1])
			b.resetAcc()
		}
	}

	frame(2)
	first := created
	// One per slot for the uniform, because its offset is per draw, and one in
	// total for the texture, because both draws name the same one.
	if first != 3 {
		t.Fatalf("groups created in the first frame = %d, want 2 uniform and 1 texture", first)
	}
	frame(2)
	if created != first {
		t.Errorf("groups created in the second frame = %d, want every one reused", created-first)
	}

	// The resize replaces the buffer, so the uniform's groups are gone and the
	// texture's are not: only the uniform's keys name the generation that moved.
	frame(gfxbUniformSlots + 1)
	if rebuilt := created - first; rebuilt != 2 {
		t.Errorf("groups rebuilt after the resize = %d, want the 2 the uniform is in", rebuilt)
	}
}
