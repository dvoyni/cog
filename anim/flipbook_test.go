package anim

import "testing"

type frameSeq struct{ Flipbook[string] }

func TestFlipbookAtSplitsProgressIntoFrames(t *testing.T) {
	book := Flipbook[string]{Frames: []string{"a", "b", "c", "d"}, FPS: 4}

	for _, c := range []struct {
		progress float32
		want     string
	}{{0, "a"}, {0.24, "a"}, {0.26, "b"}, {0.5, "c"}, {0.99, "d"}, {1, "d"}} {
		if got := book.At(c.progress); got != c.want {
			t.Fatalf("At(%v) = %q, want %q", c.progress, got, c.want)
		}
	}
}

func TestFlipbookWithoutFramesIsZero(t *testing.T) {
	var book Flipbook[string]
	if got := book.At(0.5); got != "" {
		t.Fatalf("At on an empty flipbook = %q, want the zero frame", got)
	}
	if params := book.Params(); params.Duration != 0 {
		t.Fatalf("Params on an empty flipbook: duration = %v, want 0", params.Duration)
	}
	if params := (Flipbook[string]{Frames: []string{"a"}}).Params(); params.Duration != 0 {
		t.Fatalf("Params without an FPS: duration = %v, want 0", params.Duration)
	}
}

func TestFlipbookParamsPlaysEveryFrameOnce(t *testing.T) {
	book := Flipbook[string]{Frames: []string{"a", "b", "c"}, FPS: 30}
	if params := book.Params(); !near(params.Duration, 0.1) {
		t.Fatalf("params duration = %v, want 0.1", params.Duration)
	}
}

func TestFlipbookLoopsOnTheTimeline(t *testing.T) {
	book := Flipbook[string]{Frames: []string{"a", "b", "c", "d"}, FPS: 4}
	tl := &Timeline{}
	tl.Add("flag", frameSeq{book}, book.Params().WithLoop().WithImmediate())

	// One second holds each of the four frames for a quarter of a second, and
	// the second pass repeats them: a looping track never finishes.
	for step, want := range []string{"a", "b", "c", "d", "a", "b"} {
		if got := tl.Value[frameSeq]("flag", ""); got != want {
			t.Fatalf("step %d: frame = %q, want %q", step, got, want)
		}
		tl.advance(0.25)
	}
	if tl.Idle() != true {
		t.Fatalf("a looping flipbook must leave the timeline idle")
	}
}
