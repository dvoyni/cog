//go:build !js

package internal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/jfreymuth/oggvorbis"
)

// The Loop Region's tests. A loop point is a fact about a Clip, so they are
// about bytes and about frames: what a file's Vorbis comments say, and what the
// Mixer and the read-ahead then produce at the wrap.
//
// The tagged Clips are made here rather than checked in. retagged rewrites the
// fixture's comment header and leaves every audio page of it untouched, so a
// test names the loop point it wants in frames and the bytes underneath are
// still the public-domain clip the rest of this suite plays. Nothing in the
// #304 prototype kit could be used for it: four of its five Clips are CC BY-SA.

// retagged rewrites the fixture's Vorbis comment header with extra comments
// appended, and re-lays the one page that header sits in. Every other page is
// copied verbatim - the audio, its granule positions and its lengths are the
// file's own, so a re-tagged Clip is the same sound with a loop point written
// on it.
func retagged(t *testing.T, encoded assets.Blob, extra ...string) assets.Blob {
	t.Helper()
	header, err := oggvorbis.GetCommentHeader(bytes.NewReader(encoded.Data()))
	if err != nil {
		t.Fatalf("reading the fixture's comment header: %v", err)
	}
	packet := commentPacket(header.Vendor, append(append([]string(nil), header.Comments...), extra...))

	data := encoded.Data()
	for at := 0; at+27 <= len(data); {
		if string(data[at:at+4]) != "OggS" {
			t.Fatalf("the fixture is not a run of Ogg pages: no capture pattern at %d", at)
		}
		head := 27 + int(data[at+26])
		table := data[at+27 : at+head]
		body := 0
		for _, segment := range table {
			body += int(segment)
		}
		begin, from, offset := 0, 0, 0
		for i, segment := range table {
			offset += int(segment)
			if segment == 255 {
				continue
			}
			if isCommentHeader(data[at+head+from : at+head+offset]) {
				return assets.NewBlob(replacePage(t,
					data, at, head, body, table, begin, i, from, offset, packet))
			}
			begin, from = i+1, offset
		}
		at += head + body
	}
	t.Fatal("the fixture carries no Vorbis comment header to re-tag")
	return assets.Blob{}
}

// isCommentHeader reports whether a packet is the Vorbis comment header: packet
// type 3 and the codec's own signature.
func isCommentHeader(packet []byte) bool {
	return len(packet) >= 7 && packet[0] == 3 && string(packet[1:7]) == "vorbis"
}

// commentPacket builds a Vorbis comment header packet: the type byte and the
// signature, the vendor string, the comments, and the framing bit the format
// ends it with.
func commentPacket(vendor string, comments []string) []byte {
	packet := append([]byte{3}, "vorbis"...)
	packet = binary.LittleEndian.AppendUint32(packet, uint32(len(vendor)))
	packet = append(packet, vendor...)
	packet = binary.LittleEndian.AppendUint32(packet, uint32(len(comments)))
	for _, comment := range comments {
		packet = binary.LittleEndian.AppendUint32(packet, uint32(len(comment)))
		packet = append(packet, comment...)
	}
	return append(packet, 1)
}

// replacePage swaps one packet inside one page for a longer one and re-lays
// that page: a new segment table for the replaced packet, the tables and bodies
// either side of it kept exactly as they were, and the page's CRC recomputed
// over the result.
func replacePage(
	t *testing.T,
	data []byte, at, head, body int, table []byte,
	beginSegment, endSegment, from, to int,
	packet []byte,
) []byte {
	t.Helper()
	segments := append([]byte(nil), table[:beginSegment]...)
	segments = append(segments, lacing(len(packet))...)
	segments = append(segments, table[endSegment+1:]...)
	if len(segments) > 255 {
		t.Fatalf("the re-tagged comment header needs %d segments, and a page holds 255", len(segments))
	}

	page := append([]byte(nil), data[at:at+27]...)
	page[26] = byte(len(segments))
	page = append(page, segments...)
	page = append(page, data[at+head:at+head+from]...)
	page = append(page, packet...)
	page = append(page, data[at+head+to:at+head+body]...)
	clear(page[22:26])
	binary.LittleEndian.PutUint32(page[22:26], oggCRC(page))

	out := append([]byte(nil), data[:at]...)
	out = append(out, page...)
	return append(out, data[at+head+body:]...)
}

// lacing is a packet length written as Ogg segment sizes: as many 255s as it
// takes and then the remainder, which is what marks the packet's end.
func lacing(length int) []byte {
	var segments []byte
	for length >= 255 {
		segments = append(segments, 255)
		length -= 255
	}
	return append(segments, byte(length))
}

// oggCRC is Ogg's page checksum: CRC-32 over the whole page with the checksum
// field zeroed, polynomial 0x04c11db7, no reflection and no final inversion.
func oggCRC(page []byte) uint32 {
	var crc uint32
	for _, b := range page {
		crc ^= uint32(b) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// seconds is a count of source frames as the seam carries it: the Loop Region
// crosses in seconds, so a test that means a frame says which one and converts
// the same way the Adapter does.
func seconds(frames int64) float32 { return float32(float64(frames) / float64(clipRate)) }

// The re-tagging is the fixture these tests stand on, so it is asserted before
// anything is asserted through it: the bytes it produces are still the Clip the
// rest of the suite plays, and the tags it wrote are on them.
func TestARetaggedFixtureIsStillTheSameClip(t *testing.T) {
	tagged := retagged(t, fixture(t), "LOOPSTART=11025")

	length, format, err := oggvorbis.GetLength(bytes.NewReader(tagged.Data()))
	if err != nil {
		t.Fatalf("the re-tagged fixture no longer parses: %v", err)
	}
	if length != clipFrames || format.SampleRate != clipRate || format.Channels != clipChannels {
		t.Fatalf("the re-tagged fixture is %d frames of %d Hz and %d channels, want the fixture's %d/%d/%d",
			length, format.SampleRate, format.Channels, clipFrames, clipRate, clipChannels)
	}
	header, err := oggvorbis.GetCommentHeader(bytes.NewReader(tagged.Data()))
	if err != nil {
		t.Fatalf("the re-tagged comment header does not parse: %v", err)
	}
	if got, _, _ := loopTag(header.Comments, loopStartTag); got != 11025 {
		t.Fatalf("the re-tagged header reports LOOPSTART %d, want 11025", got)
	}
	if len(header.Comments) < 8 {
		t.Fatalf("re-tagging kept %d comments, and it must keep the file's own 7 beside its own",
			len(header.Comments))
	}
}

// The region rides in the file, is read in the pass that already reads duration,
// channels and rate, and comes up in seconds. Both tiers read the same bytes,
// so a Clip cannot mean one thing resident and another streamed.
func TestAClipsLoopRegionComesOffItsVorbisComments(t *testing.T) {
	tagged := retagged(t, fixture(t), "LOOPSTART=11025", "LOOPLENGTH=22050")

	resident, err := prepare(tagged, defaultSampleRate, neverStream)
	if err != nil {
		t.Fatalf("preparing the tagged fixture resident: %v", err)
	}
	streamed, err := prepare(tagged, defaultSampleRate, alwaysStream)
	if err != nil {
		t.Fatalf("preparing the tagged fixture streamed: %v", err)
	}
	if resident.streams() || !streamed.streams() {
		t.Fatal("the sentinels did not put one Clip in each tier")
	}

	for _, clip := range []*clipData{resident, streamed} {
		region, ok := clip.LoopRegion().Get()
		if !ok {
			t.Fatal("a Clip tagged LOOPSTART reports no Loop Region")
		}
		if region.Start != seconds(11025) || region.End != seconds(11025+22050) {
			t.Fatalf("the region is [%v, %v), want [%v, %v)",
				region.Start, region.End, seconds(11025), seconds(33075))
		}
		if clip.ignored != nil {
			t.Fatalf("a well-formed region was reported as dropped: %v", clip.ignored)
		}
	}
}

// A Vorbis comment's field name is case-insensitive by the format's own
// definition, and LOOPSTART alone is the case this feature is named for: an
// intro that runs into a loop which carries on to the end of the file.
func TestLoopTagsAreCaseInsensitiveAndLoopstartAloneRunsToTheClipsEnd(t *testing.T) {
	clip, err := prepare(retagged(t, fixture(t), "loopStart=11025"), defaultSampleRate, neverStream)
	if err != nil {
		t.Fatalf("preparing the tagged fixture: %v", err)
	}

	region, ok := clip.LoopRegion().Get()
	if !ok {
		t.Fatal("a Clip tagged loopStart in lower case reports no Loop Region")
	}
	if region.Start != seconds(11025) || region.End != clip.Duration() {
		t.Fatalf("the region is [%v, %v), want [%v, %v) - LOOPSTART alone loops to the end",
			region.Start, region.End, seconds(11025), clip.Duration())
	}
}

// LOOPEND is what a file without LOOPLENGTH says its loop ends at, and
// LOOPLENGTH wins where a file carries both.
func TestLoopendIsUsedWhenLooplengthIsAbsentAndLooplengthWinsWhenItIsNot(t *testing.T) {
	ends, err := prepare(retagged(t, fixture(t),
		"LOOPSTART=11025", "LOOPEND=33075"), defaultSampleRate, neverStream)
	if err != nil {
		t.Fatalf("preparing the LOOPEND fixture: %v", err)
	}
	if region, _ := ends.LoopRegion().Get(); region.End != seconds(33075) {
		t.Fatalf("LOOPEND=33075 gave an end of %v, want %v", region.End, seconds(33075))
	}

	both, err := prepare(retagged(t, fixture(t),
		"LOOPSTART=11025", "LOOPLENGTH=22050", "LOOPEND=44100"), defaultSampleRate, neverStream)
	if err != nil {
		t.Fatalf("preparing the fixture carrying both: %v", err)
	}
	if region, _ := both.LoopRegion().Get(); region.End != seconds(33075) {
		t.Fatalf("LOOPLENGTH beside LOOPEND gave an end of %v, want LOOPLENGTH's %v",
			region.End, seconds(33075))
	}
}

// A malformed region is dropped whole and never clamped. A clamped loop sounds
// like a working loop with the wrong loop point, which is the single hardest
// audio bug there is to attribute, and clamping would make a tag's meaning
// depend on the Clip it sits in.
func TestAMalformedLoopRegionIsDroppedWholeAndNeverClamped(t *testing.T) {
	cases := []struct {
		name string
		tags []string
	}{
		{"a start before the beginning", []string{"LOOPSTART=-1", "LOOPLENGTH=22050"}},
		{"an end at the start", []string{"LOOPSTART=11025", "LOOPEND=11025"}},
		{"an end before the start", []string{"LOOPSTART=22050", "LOOPEND=11025"}},
		{"an end past the Clip", []string{"LOOPSTART=11025", "LOOPEND=48705"}},
		{"a length past the Clip", []string{"LOOPSTART=11025", "LOOPLENGTH=48704"}},
		{"a start that is not a number", []string{"LOOPSTART=bar", "LOOPLENGTH=22050"}},
		{"a length that is not a number", []string{"LOOPSTART=11025", "LOOPLENGTH=3.5"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clip, err := prepare(retagged(t, fixture(t), c.tags...), defaultSampleRate, neverStream)
			if err != nil {
				t.Fatalf("a malformed loop tag failed the Clip: %v", err)
			}
			if clip.LoopRegion().Present() {
				region, _ := clip.LoopRegion().Get()
				t.Fatalf("a malformed region was kept as [%v, %v), and it must be dropped whole",
					region.Start, region.End)
			}
			if clip.Duration() != clipDuration {
				t.Fatalf("the Clip still reports %v, want the file's %v", clip.Duration(), clipDuration)
			}
			var ignored ErrLoopRegionIgnored
			if !errors.As(clip.ignored, &ignored) {
				t.Fatalf("the dropped region was reported as %v, want an ErrLoopRegionIgnored", clip.ignored)
			}
		})
	}
}

// The Adapter says it out loud, once per Clip, through the one thing in this
// Extension that holds a Kernel. A prepare runs once per entry in sound's
// table, so draining the queue is what makes the report once.
func TestADroppedLoopRegionIsReportedOncePerClip(t *testing.T) {
	b := newTestBackend(t, &fakeAudio{})

	b.Prepare(nil, retagged(t, fixture(t), "LOOPSTART=99999999"))
	if completed := waitPrepared(t, b); completed.Err != nil {
		t.Fatalf("a malformed loop tag failed the Clip: %v", completed.Err)
	}

	dropped := b.takeDroppedRegions()
	if len(dropped) != 1 {
		t.Fatalf("the Adapter queued %d notices for one bad Clip, want 1", len(dropped))
	}
	var ignored ErrLoopRegionIgnored
	if !errors.As(dropped[0], &ignored) || ignored.Frames != clipFrames {
		t.Fatalf("the notice is %v, want an ErrLoopRegionIgnored naming the Clip's %d frames",
			dropped[0], clipFrames)
	}
	if again := b.takeDroppedRegions(); len(again) != 0 {
		t.Fatalf("the same Clip was reported %d times more, and it is reported once", len(again))
	}
}

// A Clip with no tags reports no region, which means the whole Clip - so
// nothing that worked before the tags existed moves.
func TestAnUntaggedClipReportsNoRegionAndNothingIsDropped(t *testing.T) {
	clip, err := prepare(fixture(t), defaultSampleRate, neverStream)
	if err != nil {
		t.Fatalf("preparing the fixture: %v", err)
	}
	if clip.LoopRegion().Present() {
		t.Fatal("a Clip with no LOOPSTART tag reported a Loop Region")
	}
	if clip.ignored != nil {
		t.Fatalf("a Clip with no loop tags reported a dropped region: %v", clip.ignored)
	}
	start, end := clip.loopBounds()
	if start != 0 || end != float64(clip.frames) {
		t.Fatalf("an absent region bounds a loop at [%v, %v), want the whole Clip [0, %d)",
			start, end, clip.frames)
	}
}

// The resident Clip is trimmed to its granule end and never to whatever the
// decoder handed back, which may carry a final packet's padding. An absent
// region means loop-to-the-granule-end, and that is where it comes from.
func TestAResidentClipIsTrimmedToItsGranuleEnd(t *testing.T) {
	clip, err := decode(fixture(t), clipRate, clipFrames)
	if err != nil {
		t.Fatalf("decoding the fixture: %v", err)
	}
	if clip.frames != clipFrames || clip.sourceFrames != clipFrames {
		t.Fatalf("the decoded Clip is %d frames against a granule end of %d", clip.frames, clipFrames)
	}
	if got := len(clip.samples); got != clipFrames*clipChannels {
		t.Fatalf("the Clip holds %d samples for %d frames of %d channels",
			got, clipFrames, clipChannels)
	}

	// Two frames of padding past the granule end are dropped rather than
	// played, which is what a Clip whose last packet overruns would otherwise
	// loop through every time round.
	short, err := decode(fixture(t), clipRate, clipFrames-2)
	if err != nil {
		t.Fatalf("decoding the fixture against a shorter granule end: %v", err)
	}
	if short.frames != clipFrames-2 || len(short.samples) != (clipFrames-2)*clipChannels {
		t.Fatalf("a granule end two frames short left %d frames and %d samples",
			short.frames, len(short.samples))
	}
}

// The wrap is sample-exact on the resident tier: the frame after the last one
// of the loop is the first one of the loop, with nothing repeated and nothing
// dropped. It is asserted here, at the Adapter, by rendering to a buffer - the
// game-facing surface by design cannot see a frame.
func TestAResidentLoopIsGaplessAtTheRegionsWrap(t *testing.T) {
	const frames, from, to = 300, 100, 250
	mx, handoff := newTestMixer(4)
	clip := rampClip(frames)
	clip.region = m.Some(sound.LoopRegion{
		Start: float32(float64(from) / testRate),
		End:   float32(float64(to) / testRate),
	})
	start(handoff, 0, clip, mono(1), true)

	out := left(render(t, mx, 1))
	for i, got := range out {
		at := i
		if at >= to {
			at = from + (at-to)%(to-from)
		}
		if want := clip.samples[at]; got != want {
			t.Fatalf("frame %d of the loop is %v, want frame %d's %v - the wrap repeated or dropped a frame",
				i, got, at, want)
		}
	}
	if !mx.voices[0].active {
		t.Fatal("the looping voice ended at the Loop Region's end")
	}
}

// A Loop Region bounds a looping Voice and nothing else. A one-shot on a Clip
// with an intro, a loop and a tail plays all three and ends at the Clip's end:
// the region says where to repeat between, never where the Clip stops.
func TestAOneShotOnAClipWithALoopRegionPlaysToTheClipsEnd(t *testing.T) {
	const frames, from, to = testBlock / 2, 10, 20
	mx, handoff := newTestMixer(4)
	clip := constantClip(frames, 1, 1)
	clip.region = m.Some(sound.LoopRegion{
		Start: float32(float64(from) / testRate),
		End:   float32(float64(to) / testRate),
	})
	start(handoff, 0, clip, mono(1), false)

	out := left(render(t, mx, 1))
	for i := range frames {
		if out[i] != 1 {
			t.Fatalf("the one-shot went silent at frame %d, inside a Clip it should play whole", i)
		}
	}
	for i := frames; i < len(out); i++ {
		if out[i] != 0 {
			t.Fatalf("the one-shot was still sounding %d frames past its end", i-frames)
		}
	}
	if mx.voices[0].active {
		t.Fatal("the slot was still active after the Clip ran out")
	}
}

// The streamed tier wraps in the read-ahead, on its own goroutine, ahead of
// where the Mixer is reading: the decoder seeks back to the loop start while it
// is filling the ring, so the page decode never lands on the wrap's own block
// and the frames come out of the ring in one unbroken run.
//
// There is no race detector in this environment, so this one runs with
// -count=10 instead:
//
//	go test ./extensions/otosound/internal/ -run StreamedLoop -count=10
func TestAStreamedLoopWrapsInTheReadAheadAtTheRegionsBounds(t *testing.T) {
	const from, to = 400, 1200
	gen := &generator{frames: 4 * testBlock, channels: 1, guard: true}
	clip := gen.clip()
	clip.region = m.Some(sound.LoopRegion{
		Start: float32(float64(from) / testRate),
		End:   float32(float64(to) / testRate),
	})
	if start, end := clip.sourceLoopBounds(); start != from || end != to {
		t.Fatalf("the read-ahead would loop [%d, %d), want [%d, %d)", start, end, from, to)
	}

	read := newStream(clip, 0, true)
	t.Cleanup(read.halt)
	const rendered = to + 3*(to-from)
	waitFor(t, "the read-ahead to fill past three wraps", func() bool {
		return read.ring.held() >= rendered
	})

	for i := range uint64(rendered) {
		at := int64(i)
		if at >= to {
			at = from + (at-to)%(to-from)
		}
		if got, want := read.ring.at(i, 0, 1), generatedSample(at, 0); got != want {
			t.Fatalf("frame %d of the streamed loop is %v, want source frame %d's %v", i, got, at, want)
		}
	}
	// The decoder's own seeks are the wraps. The source's wrap counter counts
	// only the seeks that followed the stream running out, and a Loop Region's
	// wrap comes a long way before that - which is the point of it - so what
	// says the read-ahead went back is that it sought at all: it opened at
	// frame 0 and was never asked for a position by anything else.
	if seeks := gen.source(t, 0).seeks.Load(); seeks < 3 {
		t.Fatalf("the read-ahead sought back %d times over three spans of the loop", seeks)
	}
	if wraps := gen.source(t, 0).wraps.Load(); wraps != 0 {
		t.Fatalf("the read-ahead ran the stream out %d times, and a Loop Region ends before it", wraps)
	}
	if read.ring.length() != unknownLength {
		t.Fatal("a looping stream published its length, and a looping Voice never ends by itself")
	}
}

// The strongest thing that can be said about the two tiers: a real Ogg file
// with a real loop tag, prepared both ways, wraps at the same frame and comes
// out frame for frame identical across three passes of its loop. A game must
// not be able to tell which tier it got, and a loop point is exactly the kind
// of fact that would give it away.
func TestBothTiersWrapARealClipAtTheSameFrame(t *testing.T) {
	const from, length = 1000, 2000
	tagged := retagged(t, fixture(t), "LOOPSTART=1000", "LOOPLENGTH=2000")
	resident, err := prepare(tagged, clipRate, neverStream)
	if err != nil {
		t.Fatalf("preparing the tagged fixture resident: %v", err)
	}
	streamed, err := prepare(tagged, clipRate, alwaysStream)
	if err != nil {
		t.Fatalf("preparing the tagged fixture streamed: %v", err)
	}
	if start, end := resident.loopBounds(); start != from || end != from+length {
		t.Fatalf("the resident tier loops [%v, %v), want [%d, %d)", start, end, from, from+length)
	}

	read := newStream(streamed, 0, true)
	t.Cleanup(read.halt)
	const rendered = from + 3*length
	waitFor(t, "the read-ahead to fill past three wraps", func() bool {
		return read.ring.held() >= rendered
	})

	for i := range uint64(rendered) {
		at := int64(i)
		if at >= from+length {
			at = from + (at-from)%length
		}
		for c := range clipChannels {
			want := resident.samples[at*clipChannels+int64(c)]
			if got := read.ring.at(i, c, clipChannels); got != want {
				t.Fatalf("frame %d channel %d is %v streamed and %v resident, at source frame %d",
					i, c, got, want, at)
			}
		}
	}
}

// The Mixer reads the streamed loop the way it reads any other stream: the ring
// did the wrapping, so the playhead only ever moves forwards and the Voice
// never ends.
func TestAStreamedLoopIsGaplessThroughTheMixer(t *testing.T) {
	const from, to = 400, 1200
	gen := &generator{frames: 4 * testBlock, channels: 1, guard: true}
	clip := gen.clip()
	clip.region = m.Some(sound.LoopRegion{
		Start: float32(float64(from) / testRate),
		End:   float32(float64(to) / testRate),
	})

	read := newStream(clip, 0, true)
	t.Cleanup(read.halt)
	waitFor(t, "the read-ahead to fill a block", func() bool { return read.ring.held() >= testBlock })

	mx, handoff := newTestMixer(4)
	handoff.record(op{
		kind: opStart, slot: 0, clip: clip, ring: read.ring, loop: true,
		params: sound.VoiceParams{Gains: mono(1), Rate: 1},
	}, false)
	handoff.publish()

	out := left(pullBlocks(t, mx, 1))
	for i, got := range out {
		at := int64(i)
		if at >= to {
			at = from + (at-to)%(to-from)
		}
		if want := generatedSample(at, 0); got != want {
			t.Fatalf("frame %d out of the Mixer is %v, want source frame %d's %v", i, got, at, want)
		}
	}
	if !mx.voices[0].active {
		t.Fatal("the streamed looping voice ended at the Loop Region's end")
	}
}
