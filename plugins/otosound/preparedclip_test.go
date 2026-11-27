//go:build !js

package otosound

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/jfreymuth/oggvorbis"
)

// The fixture is 48704 frames of 44.1 kHz stereo, 389,632 bytes decoded:
// under the 512 KiB default, and over a limit set below it.
const fixtureDecodedSize = clipFrames * clipChannels * decodedSampleSize

// A clip under the limit is decoded once and holds its samples; one over it
// streams, holding its bytes and no samples, and the two answer the same
// facts, the source's. The device runs at the clip's own rate, so nothing is
// converted, and the streamed clip carries no filter.
func TestAClipUnderTheLimitIsResidentAndOneOverItStreams(t *testing.T) {
	resident, err := prepareClip(readFixture(t), defaultDecodedClipLimit, clipRate)
	if err != nil {
		t.Fatalf("preparing the fixture under the default limit: %v", err)
	}
	if resident.streams() || resident.encoded != nil {
		t.Fatal("a clip under the limit streams, or still holds the bytes it was decoded from")
	}
	if got := len(resident.samples); got != clipFrames*clipChannels {
		t.Fatalf("the resident clip holds %d samples, want %d frames of %d channels", got, clipFrames, clipChannels)
	}

	encoded := readFixture(t)
	streamed, err := prepareClip(encoded, 64<<10, clipRate)
	if err != nil {
		t.Fatalf("preparing the %d byte fixture under a 64 KiB limit: %v", fixtureDecodedSize, err)
	}
	if !streamed.streams() || len(streamed.samples) != 0 || streamed.filter != nil {
		t.Fatalf("a clip over the limit is resident, holds %d samples or a filter it has no use for", len(streamed.samples))
	}
	if &streamed.encoded[0] != &encoded[0] {
		t.Fatal("the streamed clip holds a copy of its bytes rather than the ones Prepare was handed")
	}
	for name, clip := range map[string]*preparedClip{"resident": resident, "streamed": streamed} {
		if clip.Duration() != clipDuration || clip.Channels() != clipChannels || clip.SampleRate() != clipRate {
			t.Fatalf("the %s clip reports %v s, %d channels and %d Hz, want the file's %v, %d and %d",
				name, clip.Duration(), clip.Channels(), clip.SampleRate(), clipDuration, clipChannels, clipRate)
		}
	}
}

// The sentinels choose the tier outright, -1 decoding whatever the size and
// -2 decoding nothing, and a positive limit is the largest decoded size still
// decoded: a clip of exactly the limit is resident, and one a byte over it is
// not.
func TestTheResidencyLimitsSentinelsChooseTheTierOutright(t *testing.T) {
	for name, test := range map[string]struct {
		limit   int
		decoded bool
	}{
		"no limit":            {decodedClipLimitNone, true},
		"always stream":       {decodedClipLimitAlwaysStream, false},
		"exactly the limit":   {fixtureDecodedSize, true},
		"a byte over a limit": {fixtureDecodedSize - 1, false},
	} {
		t.Run(name, func(t *testing.T) {
			clip, err := prepareClip(readFixture(t), test.limit, clipRate)
			if err != nil {
				t.Fatalf("preparing the fixture under a limit of %d: %v", test.limit, err)
			}
			if clip.streams() == test.decoded {
				t.Fatalf("the fixture under a limit of %d streams: %v, want %v", test.limit, clip.streams(), !test.decoded)
			}
		})
	}
}

// The decoded samples are the stream's own, frame for frame what the decoder
// reads out of the file, when the device runs at the clip's own rate and
// nothing is converted.
func TestAResidentClipHoldsTheDecodedFramesOfItsStream(t *testing.T) {
	clip, err := prepareClip(readFixture(t), decodedClipLimitNone, clipRate)
	if err != nil {
		t.Fatalf("preparing the fixture: %v", err)
	}
	want, _, err := oggvorbis.ReadAll(bytes.NewReader(readFixture(t)))
	if err != nil {
		t.Fatalf("decoding the fixture: %v", err)
	}
	if !slices.Equal(clip.samples, want[:clipFrames*clipChannels]) {
		t.Fatal("the resident clip's samples are not the stream's decoded frames")
	}
	if slices.Max(clip.samples) == 0 {
		t.Fatal("the resident clip is silent")
	}
}

// The re-tagging is the fixture the loop region's tests stand on, so it is
// asserted before anything is asserted through it: the bytes it produces are
// still the clip the rest of the suite plays, and the tags it wrote are on
// them.
func TestARetaggedFixtureIsStillTheSameClip(t *testing.T) {
	tagged := retagClip(t, readFixture(t), "LOOPSTART=11025")

	length, format, err := oggvorbis.GetLength(bytes.NewReader(tagged))
	if err != nil {
		t.Fatalf("the re-tagged fixture no longer parses: %v", err)
	}
	if length != clipFrames || format.SampleRate != clipRate || format.Channels != clipChannels {
		t.Fatalf("the re-tagged fixture is %d frames of %d Hz and %d channels, want the fixture's %d, %d and %d",
			length, format.SampleRate, format.Channels, clipFrames, clipRate, clipChannels)
	}
	header, err := oggvorbis.GetCommentHeader(bytes.NewReader(tagged))
	if err != nil {
		t.Fatalf("the re-tagged comment header does not parse: %v", err)
	}
	if !slices.Contains(header.Comments, "LOOPSTART=11025") {
		t.Fatalf("the re-tagged header holds %v, want LOOPSTART=11025 among them", header.Comments)
	}
	if len(header.Comments) < 8 {
		t.Fatalf("re-tagging kept %d comments, and it must keep the file's own 7 beside its own", len(header.Comments))
	}
}

// The region rides in the file, is read in the pass that reads the duration,
// channels and rate, and comes up in seconds, the decoded clip answering it.
func TestAClipsLoopRegionComesOffItsVorbisComments(t *testing.T) {
	clip, err := prepareClip(retagClip(t, readFixture(t), "LOOPSTART=11025", "LOOPLENGTH=22050"), decodedClipLimitNone, clipRate)
	if err != nil {
		t.Fatalf("preparing the tagged fixture: %v", err)
	}
	region, ok := clip.LoopRegion().Get()
	if !ok {
		t.Fatal("a clip tagged LOOPSTART reports no loop region")
	}
	if want := (sfx.LoopRegion{Start: float32(11025.0 / clipRate), End: float32(33075.0 / clipRate)}); region != want {
		t.Fatalf("the region is %+v, want %+v", region, want)
	}
	if err := clip.LoopRegionErr(); err != nil {
		t.Fatalf("a well-formed region answered LoopRegionErr %v", err)
	}
}

// A malformed region is dropped whole and never clamped, and the clip still
// prepares, at its full length: the drop costs it only its loop point.
func TestAMalformedLoopRegionIsDroppedWholeAndNeverClamped(t *testing.T) {
	for name, tags := range map[string][]string{
		"an end past the clip":     {"LOOPSTART=11025", "LOOPEND=48705"},
		"an end before the start":  {"LOOPSTART=22050", "LOOPEND=11025"},
		"a start that is no count": {"LOOPSTART=bar", "LOOPLENGTH=22050"},
	} {
		t.Run(name, func(t *testing.T) {
			clip, err := prepareClip(retagClip(t, readFixture(t), tags...), decodedClipLimitNone, clipRate)
			if err != nil {
				t.Fatalf("a malformed loop tag failed the clip: %v", err)
			}
			if region, kept := clip.LoopRegion().Get(); kept {
				t.Fatalf("a malformed region was kept as %+v, and it must be dropped whole", region)
			}
			if clip.Duration() != clipDuration {
				t.Fatalf("the clip reports %v, want the file's %v", clip.Duration(), clipDuration)
			}
		})
	}
}

// A dropped region is answered by the clip's LoopRegionErr, sfx's
// ErrLoopRegionIgnored with no path, naming the span asked for against the
// clip's true length: otosound has no name for a clip, and sfx fills the path
// in when it reports it.
func TestADroppedLoopRegionIsAnsweredByLoopRegionErrWithoutItsPath(t *testing.T) {
	clip, err := prepareClip(retagClip(t, readFixture(t), "LOOPSTART=99999999"), decodedClipLimitNone, clipRate)
	if err != nil {
		t.Fatalf("a malformed loop tag failed the clip: %v", err)
	}
	var ignored sfx.ErrLoopRegionIgnored
	if !errors.As(clip.LoopRegionErr(), &ignored) {
		t.Fatalf("LoopRegionErr is %v, want an sfx.ErrLoopRegionIgnored", clip.LoopRegionErr())
	}
	want := sfx.ErrLoopRegionIgnored{Start: 99999999, End: clipFrames, Frames: clipFrames}
	if ignored != want {
		t.Fatalf("LoopRegionErr is %+v, want %+v", ignored, want)
	}
}

// A clip with no tags reports no region, which means the whole clip, and
// nothing is dropped.
func TestAnUntaggedClipReportsNoRegionAndNothingIsDropped(t *testing.T) {
	clip, err := prepareClip(readFixture(t), decodedClipLimitNone, clipRate)
	if err != nil {
		t.Fatalf("preparing the fixture: %v", err)
	}
	if clip.LoopRegion().Present() {
		t.Fatal("a clip with no LOOPSTART tag reported a loop region")
	}
	if err := clip.LoopRegionErr(); err != nil {
		t.Fatalf("an untagged clip answered LoopRegionErr %v", err)
	}
}

// A resident clip is trimmed to its granule end, never to whatever the
// decoder handed back, which may carry a final packet's padding; a LOOPEND is
// in range only against that corrected count.
func TestAResidentClipIsTrimmedToItsGranuleEnd(t *testing.T) {
	clip, err := decodeClip(readFixture(t), clipFrames)
	if err != nil {
		t.Fatalf("decoding the fixture: %v", err)
	}
	if clip.frames != clipFrames || len(clip.samples) != clipFrames*clipChannels {
		t.Fatalf("the decoded clip is %d frames and %d samples against a granule end of %d", clip.frames, len(clip.samples), clipFrames)
	}

	// Two frames past the granule end are dropped rather than played, and a
	// LOOPEND at the decoded end is past the clip.
	const granuleEnd = clipFrames - 2
	short, err := decodeClip(retagClip(t, readFixture(t), "LOOPSTART=0", "LOOPEND=48704"), granuleEnd)
	if err != nil {
		t.Fatalf("decoding the fixture against a shorter granule end: %v", err)
	}
	if short.frames != granuleEnd || len(short.samples) != granuleEnd*clipChannels {
		t.Fatalf("a granule end two frames short left %d frames and %d samples", short.frames, len(short.samples))
	}
	if short.Duration() != float32(granuleEnd)/float32(clipRate) {
		t.Fatalf("the trimmed clip reports %v s, want its granule end's %v", short.Duration(), float32(granuleEnd)/float32(clipRate))
	}
	var ignored sfx.ErrLoopRegionIgnored
	if !errors.As(short.LoopRegionErr(), &ignored) || ignored.Frames != granuleEnd {
		t.Fatalf("a LOOPEND past the granule end answered %v, want an ErrLoopRegionIgnored against %d frames", short.LoopRegionErr(), granuleEnd)
	}
}

// A streamed clip whose source's rate is not the device's carries the filter
// its voices share, built once for the pair of rates, and still answers its
// source's rate.
func TestAStreamedClipKeepsTheFilterItsVoicesShare(t *testing.T) {
	clip, err := prepareClip(readFixture(t), decodedClipLimitAlwaysStream, defaultSampleRate)
	if err != nil {
		t.Fatalf("preparing the fixture to stream: %v", err)
	}
	if clip.filter == nil || clip.filter.sourceRate != clipRate || clip.filter.outputRate != defaultSampleRate {
		t.Fatalf("the streamed clip's filter is %+v, want one from %d Hz to %d Hz", clip.filter, clipRate, defaultSampleRate)
	}
	if clip.SampleRate() != clipRate {
		t.Fatalf("the streamed clip reports %d Hz, want its file's %d", clip.SampleRate(), clipRate)
	}
}
