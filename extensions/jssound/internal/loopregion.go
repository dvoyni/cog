//go:build js

package internal

import (
	"bytes"
	"cmp"
	"strconv"
	"strings"

	"github.com/dvoyni/cog/extensions/jssound"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/jfreymuth/oggvorbis"
)

// This is the Loop Region parse, and it is deliberately the same arithmetic
// otosound runs in clipBounds and nosound runs in its own copy, over the same
// bytes, reaching the same answer.
//
// It is duplicated rather than shared because an Extension's internal/ may not
// reach another Extension's, and because the thing that must not diverge is the
// answer rather than the code: a Clip must report one duration and one Loop
// Region whichever Adapter a game composed, or a test that passes under nosound
// says nothing about the game under jssound. The duration is already written
// three times for exactly that reason.
//
// It is parsed in Go here for a reason the other two do not have:
// decodeAudioData hands back an AudioBuffer and never the Vorbis comments, so
// the browser cannot be asked where a Clip loops. The bytes this Adapter already
// holds are the only place the answer is.
//
// The three Vorbis comments a Loop Region is declared with, in sample frames.
// They are matched case-insensitively, because a Vorbis comment's field name is
// case-insensitive by the format's own definition and the tools in the wild
// disagree about it.
const (
	loopStartTag  = "LOOPSTART"
	loopLengthTag = "LOOPLENGTH"
	loopEndTag    = "LOOPEND"
)

// clipBounds is the Adapter's one pass over the headers for the two facts that
// say where a Clip ends and where it repeats: the true final frame its granule
// positions name, and the Loop Region its Vorbis comments declare, in seconds.
//
// The two are one function because they are one fact looked at twice. An absent
// region means loop to the granule end and never to a padded buffer's end - and
// a browser's AudioBuffer is exactly such a padded buffer, resampled into the
// context's rate with the final packet's padding still on it - and a LOOPEND is
// only in range against that same count. Resolved apart, the region would be
// checked against a length the Clip does not have.
//
// sound sees neither input. What crosses the seam is a duration and a span, both
// in seconds, and no signature above here names a sample.
//
// A malformed region is dropped whole and returned as an error to be reported,
// never clamped: a clamped loop sounds like a working loop with the wrong loop
// point, which is the single hardest audio bug there is to attribute.
func clipBounds(encoded assets.Blob, sourceRate int, granule, decoded int64) (int64, m.Maybe[sound.LoopRegion], error) {
	var none m.Maybe[sound.LoopRegion]
	frames := decoded
	if granule > 0 && granule < frames {
		frames = granule
	}

	header, err := oggvorbis.GetCommentHeader(bytes.NewReader(encoded.Data()))
	if err != nil {
		// The identification header parsed and this one did not, so the file is
		// damaged in a way that costs it only its tags. The Clip still plays.
		return frames, none, jssound.ErrLoopRegionIgnored{Frames: frames, Err: err}
	}
	start, hasStart, startErr := loopTag(header.Comments, loopStartTag)
	length, hasLength, lengthErr := loopTag(header.Comments, loopLengthTag)
	stop, hasEnd, endErr := loopTag(header.Comments, loopEndTag)
	if err := cmp.Or(startErr, lengthErr, endErr); err != nil {
		return frames, none, jssound.ErrLoopRegionIgnored{Frames: frames, Err: err}
	}
	if !hasStart && !hasLength && !hasEnd {
		return frames, none, nil
	}

	// LOOPSTART alone is the case this whole feature is named for - an intro
	// that runs into a loop that carries on to the end of the file - so the end
	// a Clip does not name is the Clip's own end rather than nothing at all.
	// LOOPLENGTH wins over LOOPEND where a file carries both, which is the
	// order the table in the spec states them in.
	switch {
	case hasLength:
		stop = start + length
	case !hasEnd:
		stop = frames
	}
	if start < 0 || stop <= start || stop > frames {
		return frames, none, jssound.ErrLoopRegionIgnored{Start: start, End: stop, Frames: frames}
	}
	return frames, m.Some(sound.LoopRegion{
		Start: float32(float64(start) / float64(sourceRate)),
		End:   float32(float64(stop) / float64(sourceRate)),
	}), nil
}

// loopTag reads one loop comment in sample frames, and reports whether the file
// carried it at all. A Vorbis comment is NAME=value and a file may repeat a
// name; the first one wins, so a re-tagged file plays the way the tool that
// re-tagged it last meant rather than the way the one before it did.
func loopTag(comments []string, name string) (int64, bool, error) {
	for _, comment := range comments {
		at := strings.IndexByte(comment, '=')
		if at < 0 || !strings.EqualFold(comment[:at], name) {
			continue
		}
		value, err := strconv.ParseInt(strings.TrimSpace(comment[at+1:]), 10, 64)
		if err != nil {
			return 0, true, err
		}
		return value, true, nil
	}
	return 0, false, nil
}
