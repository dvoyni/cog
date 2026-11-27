package sfx

import "fmt"

// clipFailedLogKey is the prefix of a clip's key, the key its failure is
// logged once under: the prefix, then the clip's path.
const clipFailedLogKey = "sfx: clip failed: "

// ErrClipUnreadable reports a clip whose bytes could not be read: the failed
// read of its path through platform.FS. It is terminal, as every clip failure
// is: the voices recorded against the clip end with ReasonFailed, and only a
// release clears it.
type ErrClipUnreadable struct {
	// Path is the clip's path.
	Path string
	// Err is what the filesystem answered.
	Err error
}

func (e ErrClipUnreadable) Error() string {
	return fmt.Sprintf("sfx: clip %q could not be read: %v", e.Path, e.Err)
}

func (e ErrClipUnreadable) Unwrap() error { return e.Err }

// ErrClipFailed reports a clip the backend refused: its prepare or install
// failure, such as bytes that are not Ogg Vorbis, wrapped with the path it
// happened to. The backend knows what went wrong and sfx knows which clip it
// was, and a report carrying only one of the two names a condition nobody
// can find. It is terminal, as every clip failure is.
type ErrClipFailed struct {
	// Path is the clip's path.
	Path string
	// Err is what the backend answered.
	Err error
}

func (e ErrClipFailed) Error() string {
	return fmt.Sprintf("sfx: clip %q could not be prepared: %v", e.Path, e.Err)
}

func (e ErrClipFailed) Unwrap() error { return e.Err }

// ErrClipNotPrepared reports a backend that answered a prepare with neither a
// prepared clip nor an error, or installed one under the zero ID: a bug in the
// backend. Either way nothing can start a voice, and the clip fails for good.
type ErrClipNotPrepared struct {
	// Path is the clip's path.
	Path string
}

func (e ErrClipNotPrepared) Error() string {
	return fmt.Sprintf("sfx: clip %q was not prepared by the backend", e.Path)
}

// ErrLoopRegionIgnored reports a clip whose LOOPSTART, LOOPLENGTH or LOOPEND
// tags do not describe a span inside it, or would not parse, so its loop
// region was dropped whole and a looping voice on it repeats the whole clip.
// The region is dropped rather than clamped, since a clamped loop sounds like
// a working loop with the wrong loop point.
//
// It is not terminal: the clip prepares and plays. A backend answers it from
// PreparedClip.LoopRegionErr with no Path, since it has no name for a clip,
// and sfx fills the Path in when it reports it.
type ErrLoopRegionIgnored struct {
	// Path is the clip's path, filled in by sfx; empty as a backend answers it.
	Path string
	// Start and End are the span the tags asked for, in sample frames. Both
	// are zero when Err says no value was read.
	Start, End int64
	// Frames is the clip's true length in sample frames, which the span must
	// lie inside.
	Frames int64
	// Err is the parse failure, when a tag carried something that is not a
	// count of frames, and nil when the values parsed and the span did not fit.
	Err error
}

func (e ErrLoopRegionIgnored) Error() string {
	clip := "a clip"
	if e.Path != "" {
		clip = fmt.Sprintf("clip %q", e.Path)
	}
	if e.Err != nil {
		return fmt.Sprintf("sfx: the loop tags of %s could not be read, so it loops whole: %v", clip, e.Err)
	}
	return fmt.Sprintf("sfx: the loop region [%d, %d) frames of %s is not inside its %d frames, so it loops whole",
		e.Start, e.End, clip, e.Frames)
}

func (e ErrLoopRegionIgnored) Unwrap() error { return e.Err }
