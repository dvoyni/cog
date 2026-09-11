package wgpu

import (
	"errors"
	"testing"

	"github.com/gogpu/gputypes"

	cgfx "github.com/dvoyni/cog/gfx"
)

// The readback's bookkeeping, without a device. No test in this repository has
// a GPU, so what is pinned here is the arithmetic that produces a plausible
// wrong image and the state machine that decides who is owed an answer.

// fakeStaging stands in for a staging buffer and the map waiting on it.
type fakeStaging struct {
	ready    bool
	err      error
	pixels   []byte
	released int
}

func (s *fakeStaging) status() (bool, error) { return s.ready, s.err }
func (s *fakeStaging) read(int) []byte       { return s.pixels }
func (s *fakeStaging) release()              { s.released++ }

func TestARowIsPaddedToTheCopyAlignment(t *testing.T) {
	cases := []struct{ width, want int }{
		{width: 64, want: 256},  // exactly one aligned row
		{width: 65, want: 512},  // one byte over, a whole block more
		{width: 5, want: 256},   // twenty bytes, padded to a full block
		{width: 100, want: 512}, // four hundred bytes
	}
	for _, testCase := range cases {
		if got := captureRowBytes(testCase.width, 4); got != testCase.want {
			t.Errorf("row bytes for %d texels = %d, want %d", testCase.width, got, testCase.want)
		}
	}
}

func TestOnly8BitRGBACanBeCaptured(t *testing.T) {
	for _, format := range []cgfx.TextureFormat{cgfx.FormatRGBA8, cgfx.FormatRGBA8Srgb, cgfx.FormatScreen} {
		if !captureSupported(format) {
			t.Errorf("%v was refused, and it is 8-bit RGBA", format)
		}
	}
	if captureSupported(cgfx.FormatDepth32F) {
		t.Error("depth was accepted; a depth capture is a float field, not an image")
	}
}

func TestTheRingHandsBackTheOldestResolvedReadback(t *testing.T) {
	first := &fakeStaging{ready: true, pixels: []byte{1, 2, 3, 4}}
	second := &fakeStaging{ready: true, pixels: []byte{5, 6, 7, 8}}
	var ring captureRing
	ring.push(captureReadback{staging: first, width: 1, height: 1, rowBytes: 256, format: cgfx.FormatRGBA8})
	ring.push(captureReadback{staging: second, width: 1, height: 1, rowBytes: 256, format: cgfx.FormatRGBA8})

	got, ok := ring.take()
	if !ok || got.Pixels[0] != 1 {
		t.Fatalf("take = (%v, %v), want the oldest readback", got.Pixels, ok)
	}
	if first.released != 1 {
		t.Fatalf("the taken staging buffer was released %d times, want once", first.released)
	}
	got, ok = ring.take()
	if !ok || got.Pixels[0] != 5 {
		t.Fatalf("take = (%v, %v), want the next readback", got.Pixels, ok)
	}
	if _, ok := ring.take(); ok {
		t.Fatal("an empty ring handed something back")
	}
}

func TestAnUnresolvedReadbackIsNotHandedBack(t *testing.T) {
	var ring captureRing
	ring.push(captureReadback{staging: &fakeStaging{}, width: 1, height: 1, rowBytes: 256})
	if _, ok := ring.take(); ok {
		t.Fatal("the ring handed back a map that has not resolved, which means it waited for one")
	}
}

func TestAThirdReadbackIsRefusedRatherThanQueued(t *testing.T) {
	var ring captureRing
	ring.push(captureReadback{staging: &fakeStaging{}})
	if ring.full() {
		t.Fatal("one outstanding readback is not full; the next frame's copy overlaps the previous map")
	}
	ring.push(captureReadback{staging: &fakeStaging{}})
	if !ring.full() {
		t.Fatal("two outstanding readbacks should be as many as the backend keeps live")
	}
	ring.refuse(cgfx.ErrCaptureBusy{})
	if ring.full() != true {
		t.Fatal("a refusal changed how many readbacks are live")
	}
}

func TestARefusalTravelsTheSameSeamAsAResult(t *testing.T) {
	var ring captureRing
	ring.refuse(cgfx.ErrCaptureUnsupported{Format: cgfx.FormatDepth32F})

	got, ok := ring.take()
	if !ok {
		t.Fatal("the refusal never arrived")
	}
	var unsupported cgfx.ErrCaptureUnsupported
	if !errors.As(got.Err, &unsupported) {
		t.Fatalf("refusal = %v, want the unsupported format named", got.Err)
	}
}

func TestShutdownReleasesEveryReadbackAndLeavesTheReason(t *testing.T) {
	staging := &fakeStaging{}
	var ring captureRing
	ring.push(captureReadback{staging: staging, width: 1, height: 1, rowBytes: 256})

	ring.abandon()
	if staging.released != 1 {
		t.Fatalf("the abandoned staging buffer was released %d times, want once", staging.released)
	}
	got, ok := ring.take()
	if !ok || !errors.Is(got.Err, cgfx.ErrCaptureAbandoned{}) {
		t.Fatalf("after shutdown take = (%v, %v), want the abandonment", got.Err, ok)
	}
}

func TestRenderableTexturesCanBeCopiedFrom(t *testing.T) {
	renderable := textureUsage(cgfx.TextureDesc{Width: 8, Height: 8, Format: cgfx.FormatRGBA8, Renderable: true})
	if renderable&gputypes.TextureUsageCopySrc == 0 {
		t.Error("a renderable texture cannot be copied from, so nothing can be captured")
	}
	sampled := textureUsage(cgfx.TextureDesc{Width: 8, Height: 8, Format: cgfx.FormatRGBA8})
	if sampled&gputypes.TextureUsageCopySrc != 0 {
		t.Error("an ordinary texture pays for CopySrc, which can cost it framebuffer compression")
	}
}
