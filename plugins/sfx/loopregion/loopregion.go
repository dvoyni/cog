// Package loopregion reads the loop region a clip's Vorbis comments declare,
// for every sfx backend: the headless backend and otosound both read a
// clip's region through it, so a clip reports one loop region whichever
// backend a game composed. It works on plain values, the comments and the
// clip's length in frames, and reads no Ogg itself.
//
// See ../docs/design.md, The loop region.
package loopregion

import (
	"cmp"
	"strconv"
	"strings"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/sfx"
)

// The Vorbis comments a loop region is declared with, in sample frames. They
// are matched case-insensitively, since a Vorbis comment's field name is
// case-insensitive by the format's own definition and tools disagree.
const (
	loopStartTag  = "LOOPSTART"
	loopLengthTag = "LOOPLENGTH"
	loopEndTag    = "LOOPEND"
)

// Read answers the loop region comments declare, in seconds, against a clip
// of frames at sampleRate: absent when they declare none, which means the
// whole clip. frames is the clip's true length, the granule end where a
// decoder handed back more, so a region is checked against the length the
// clip has.
//
// LOOPSTART alone ends the region at the clip's end, and LOOPLENGTH wins over
// LOOPEND. A malformed region, a start below zero, an end at or before the
// start or past frames, or a value that is not an integer, is dropped whole,
// never clamped, and answered as an sfx.ErrLoopRegionIgnored with no Path.
func Read(comments []string, sampleRate int, frames int64) (m.Maybe[sfx.LoopRegion], error) {
	var none m.Maybe[sfx.LoopRegion]
	start, hasStart, startErr := readLoopTag(comments, loopStartTag)
	length, hasLength, lengthErr := readLoopTag(comments, loopLengthTag)
	end, hasEnd, endErr := readLoopTag(comments, loopEndTag)
	if err := cmp.Or(startErr, lengthErr, endErr); err != nil {
		return none, sfx.ErrLoopRegionIgnored{Frames: frames, Err: err}
	}
	if !hasStart && !hasLength && !hasEnd {
		return none, nil
	}
	switch {
	case hasLength:
		end = start + length
	case !hasEnd:
		end = frames
	}
	if start < 0 || end <= start || end > frames {
		return none, sfx.ErrLoopRegionIgnored{Start: start, End: end, Frames: frames}
	}
	return m.Some(sfx.LoopRegion{
		Start: float32(float64(start) / float64(sampleRate)),
		End:   float32(float64(end) / float64(sampleRate)),
	}), nil
}

// readLoopTag reads one loop comment in sample frames, and reports whether the
// clip carried it at all. A comment is NAME=value and a clip may repeat a
// name; the first one is read.
func readLoopTag(comments []string, name string) (int64, bool, error) {
	for _, comment := range comments {
		at := strings.IndexByte(comment, '=')
		if at < 0 || !strings.EqualFold(comment[:at], name) {
			continue
		}
		value, err := strconv.ParseInt(strings.TrimSpace(comment[at+1:]), 10, 64)
		return value, true, err
	}
	return 0, false, nil
}
