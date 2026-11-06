package gfx

import (
	"testing"

	"github.com/dvoyni/cog/platform"
)

func TestFramePoolHandsOutASlotPerFrameInFlight(t *testing.T) {
	var pool framePool
	first, _ := pool.acquire()
	second, _ := pool.acquire()

	if first == (Frame{}) || second == (Frame{}) || first.slot == second.slot {
		t.Fatalf("frames %+v and %+v in flight, want two slots, neither zero", first, second)
	}
	if count := len(pool.slots); count != 2 {
		t.Errorf("%d slots for two frames in flight, want 2", count)
	}
}

func TestFramePoolReusesAReleasedSlotUnderTheNextGeneration(t *testing.T) {
	var pool framePool
	first, _ := pool.acquire()
	pool.release(first)

	next, _ := pool.acquire()

	if next.slot != first.slot || next.generation == first.generation {
		t.Fatalf("frame %+v after releasing %+v, want its slot under a new generation", next, first)
	}
	if count := len(pool.slots); count != 1 {
		t.Errorf("%d slots for one frame at a time, want 1", count)
	}
}

func TestFramePoolAnswersAFramebufferUntilItsFrameIsReleased(t *testing.T) {
	var pool framePool
	window := platform.Window{}
	view := TextureView{handle{index: 3, generation: 1}}
	frame, _ := pool.acquire()
	pool.addFramebuffer(frame, framebuffer{window: window, view: view})

	if got := pool.framebuffer(frame, window); got != view {
		t.Fatalf("framebuffer = %v, want %v", got, view)
	}
	pool.release(frame)
	if got := pool.framebuffer(frame, window); got != (TextureView{}) {
		t.Errorf("framebuffer of a released frame = %v, want the zero view", got)
	}
	next, _ := pool.acquire()
	if got := pool.framebuffer(next, window); got != (TextureView{}) {
		t.Errorf("framebuffer of the slot's next frame = %v, want the zero view", got)
	}
}
