package internal

import (
	"bytes"
	"errors"
	"testing"

	"github.com/dvoyni/cog/slots/gfx"
	"github.com/gogpu/wgpu"
)

// align is where a block may start in the arena, and the unit these tests lay
// blocks out in.
const align = gfx.UniformAlignment

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
		arena.upload(make([]byte, 8*align))
		for block := 0; block < 8; block++ {
			if !arena.bound(block*align, 64) {
				t.Fatalf("frame %d block %d is not bound", frame, block)
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

// The write is the queue's arena byte for byte, since the arena keeps no copy
// of its own: every block reaches the buffer at the offset gfx packed it at.
func TestTheUniformArenaWritesTheQueuesArenaAsItStands(t *testing.T) {
	log := newArenaLog()
	arena := log.arena()

	data := make([]byte, 3*align)
	for i, fill := range []byte{0xA1, 0xB2, 0xC3} {
		copy(data[i*align:i*align+16], bytes.Repeat([]byte{fill}, 16))
	}
	arena.upload(data)

	if len(log.writes) != 1 {
		t.Fatalf("writes = %d, want 1", len(log.writes))
	}
	if !bytes.Equal(log.writes[0], data) {
		t.Error("the write is not the queue's arena as it stands")
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
	arena.reserve(gfxbUniformFloor + 1)
	if arena.generation == first {
		t.Error("the buffer was replaced without moving the generation")
	}
	if got := log.count("create"); got != 2 {
		t.Fatalf("creates = %d, want a second for the growth", got)
	}
	if got, want := log.sizes[1], 2*gfxbUniformFloor; got != want {
		t.Errorf("grown size = %d, want %d - a doubling, not the exact need", got, want)
	}

	// A frame back under the peak reallocates nothing: the arena keeps its
	// high-water mark, which is the whole of its way back down.
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
	arena.reserve(gfxbUniformFloor + 1)

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
// binds no block it cannot back. The draw that asked for one then leaves its
// group unfilled, which flushBinds refuses and drops - the alternative being a
// draw that renders with another draw's parameters.
func TestAUniformArenaThatCannotAllocateBindsNothing(t *testing.T) {
	log := newArenaLog()
	log.failFrom = 0
	arena := log.arena()

	arena.upload(make([]byte, 4*align))
	if arena.bound(0, 64) {
		t.Error("a block was bound with no buffer to back it")
	}
	if got := log.count("write"); got != 0 {
		t.Errorf("writes = %d, want none - there is nothing to write to", got)
	}
}

// A device that refuses to grow the buffer leaves it on the capacity it had:
// the bytes that fit are written, and a block reaching past them - wholly or
// only its tail - is refused rather than served from bytes an earlier frame
// left behind.
func TestAUniformArenaThatCannotGrowWritesWhatFits(t *testing.T) {
	log := newArenaLog()
	arena := log.arena()
	arena.upload(make([]byte, align))
	log.failFrom = 1

	arena.upload(make([]byte, gfxbUniformFloor+align))
	if got := len(log.writes[len(log.writes)-1]); got != gfxbUniformFloor {
		t.Errorf("written bytes = %d, want the %d that fit", got, gfxbUniformFloor)
	}
	if !arena.bound(gfxbUniformFloor-align, align) {
		t.Error("the last block inside capacity was refused")
	}
	if arena.bound(gfxbUniformFloor, 64) {
		t.Error("a block past capacity was bound")
	}
	if arena.bound(gfxbUniformFloor-16, 32) {
		t.Error("a block whose tail reaches past capacity was bound")
	}
}

// A block is good for the frame that wrote it and no other. A smaller frame
// after a larger one leaves the larger one's bytes in the buffer, and binding
// them would render a draw with parameters from a frame ago.
func TestTheUniformArenaRefusesABlockThisFrameDidNotWrite(t *testing.T) {
	log := newArenaLog()
	arena := log.arena()

	arena.upload(make([]byte, 4*align))
	arena.upload(make([]byte, align))
	if !arena.bound(0, 64) {
		t.Error("the frame's own block was refused")
	}
	if arena.bound(2*align, 64) {
		t.Error("a block only the previous frame wrote was bound")
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
	data := make([]byte, 256*align)
	b.ReportAllocs()
	for b.Loop() {
		arena.upload(data)
		for block := 0; block < 256; block++ {
			arena.bound(block*align, 128)
		}
	}
}

// What a draw's uniform binding says once the pool became one buffer: the id is
// constant, the offset is the draw's own, and the size is its block's own
// rather than a fixed stride.
func TestAUniformBindingIsKeyedByOffsetAndSize(t *testing.T) {
	b := newGfxBackend()
	log := newArenaLog()
	b.uniforms = log.arena()
	b.BakeUniforms(make([]byte, 4*align))
	shader := &gfxbShader{
		label:      "canvas.wgsl",
		bgLayouts:  []*wgpu.BindGroupLayout{{}},
		groupSizes: []int{1},
		uniform:    &gfx.ShaderResource{Name: "params", Kind: gfx.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 64},
	}
	pass := &gfxRenderPass{backend: b, shader: shader}

	generation := b.uniforms.generation
	for block := 0; block < 3; block++ {
		offset := block * align
		pass.SetUniformBlock(offset, 64)
		if len(b.acc[0]) != 1 {
			t.Fatalf("block %d emitted %d entries, want 1", block, len(b.acc[0]))
		}
		key := b.acc[0][0].key
		want := gfxbBindingKey{
			kind: gfxbBindUniform, binding: 0, id: gfxbUniformArenaID,
			generation: generation, offset: uint32(offset), size: 64,
		}
		if key != want {
			t.Errorf("block %d key = %+v, want %+v", block, key, want)
		}
		if native := b.acc[0][0].native; native.Buffer != b.uniforms.buffer ||
			native.Offset != uint64(offset) || native.Size != 64 {
			t.Errorf("block %d native = %+v, want the arena's buffer at its own offset and size", block, native)
		}
		b.resetAcc()
	}
}

// The cache still hits across frames, which is what keeps this a change of
// shape and not of cost: a draw at a stable offset with a stable texture builds
// one bind group per offset. A resize is the one thing that rebuilds them, and
// it rebuilds only the group the uniform is in.
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
		uniform:    &gfx.ShaderResource{Name: "params", Kind: gfx.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 64},
	}
	pass := &gfxRenderPass{backend: b, shader: shader}

	// Two frames of two draws each, every draw the same shader, offset and
	// texture. The cache is asked for the groups the way flushBinds asks.
	frame := func(size int) {
		b.BakeUniforms(make([]byte, size))
		for draw := 0; draw < 2; draw++ {
			pass.SetUniformBlock(draw*align, 64)
			b.addEntry(1, gfxbBindEntry{key: gfxbBindingKey{kind: gfxbBindTexture, binding: 0, id: 7}})
			b.bindGroups.get(shader, 0, b.acc[0])
			b.bindGroups.get(shader, 1, b.acc[1])
			b.resetAcc()
		}
	}

	frame(2 * align)
	first := created
	// One per offset for the uniform, because its offset is per draw, and one
	// in total for the texture, because both draws name the same one.
	if first != 3 {
		t.Fatalf("groups created in the first frame = %d, want 2 uniform and 1 texture", first)
	}
	frame(2 * align)
	if created != first {
		t.Errorf("groups created in the second frame = %d, want every one reused", created-first)
	}

	// The resize replaces the buffer, so the uniform's groups are gone and the
	// texture's are not: only the uniform's keys name the generation that moved.
	frame(gfxbUniformFloor + align)
	if rebuilt := created - first; rebuilt != 2 {
		t.Errorf("groups rebuilt after the resize = %d, want the 2 the uniform is in", rebuilt)
	}
}
