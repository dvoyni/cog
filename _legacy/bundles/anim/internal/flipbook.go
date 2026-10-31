package internal

// Flipbook is a Sequence that steps through a fixed list of frames, holding
// each for an equal slice of the track. The anim root aliases it as
// anim.Flipbook, whose documentation shows how a track embeds it.
type Flipbook[T any] struct {
	// Frames are the frames in play order. An empty list produces the zero
	// value of T.
	Frames []T
	// FPS is the rate the frames are meant to play at. It shapes no value on
	// its own, since the track's duration is what advances the flipbook, but Params
	// derives that duration from it, so the art declares its own rate.
	FPS float32
}

// At returns the frame progress falls in: the frames divide [0, 1] into equal
// slices, so a linear track of the flipbook's own Params holds each for exactly
// 1/FPS seconds. Progress of 1, which a finished one-shot track reports, is the
// last frame; a looping track wraps to the first.
func (f Flipbook[T]) At(progress float32) T {
	var frame T
	if len(f.Frames) == 0 {
		return frame
	}
	index := int(progress * float32(len(f.Frames)))
	return f.Frames[min(max(index, 0), len(f.Frames)-1)]
}

// Params returns the one-shot linear track that plays every frame once at FPS.
// Compose WithLoop for a flipbook that runs forever, or WithImmediate to start
// it now instead of at the chain point. A flipbook with no frames or a
// non-positive FPS asks for a zero-duration track.
func (f Flipbook[T]) Params() Params {
	if len(f.Frames) == 0 || f.FPS <= 0 {
		return Params{}
	}
	return Over(float32(len(f.Frames)) / f.FPS)
}
