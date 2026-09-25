//go:build js

package internal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/dvoyni/cog/libs/assets"
	"github.com/jfreymuth/oggvorbis"
)

// jssound reads the Loop Region out of the same header pass that already gives
// it the duration, the channels and the rate, and reaches the same answer
// otosound and nosound reach over the same bytes. That parity is what lets a
// game's test suite say anything about the game in a browser: a Clip must report
// one duration and one Loop Region whichever Adapter was composed.
//
// It is parsed in Go here for a reason the other two do not have.
// decodeAudioData hands back an AudioBuffer and never the Vorbis comments, so
// the browser cannot be asked where a Clip loops; the encoded bytes the Adapter
// already holds are the only place the answer is.
//
// The tagged Clips are made here rather than checked in: retagged rewrites the
// fixture's comment header and copies every audio page of it verbatim.

// retagged rewrites the fixture's Vorbis comment header with extra comments
// appended, and re-lays the one page that header sits in.
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

// replacePage swaps one packet inside one page for a longer one and re-lays that
// page: a new segment table for the replaced packet, the tables and bodies
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

// oggCRC is the checksum an Ogg page carries: the standard 32-bit polynomial,
// unreflected, with no initial or final inversion.
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
		t.Fatalf("the re-tagged fixture no longer reads as Ogg Vorbis: %v", err)
	}
	if length != clipFrames || format.SampleRate != clipRate || format.Channels != clipChannels {
		t.Fatalf("the re-tagged fixture is %d frames at %d Hz in %d channels, want %d, %d, %d",
			length, format.SampleRate, format.Channels, clipFrames, clipRate, clipChannels)
	}
	header, err := oggvorbis.GetCommentHeader(bytes.NewReader(tagged.Data()))
	if err != nil {
		t.Fatalf("the re-tagged comment header no longer parses: %v", err)
	}
	if value, ok, err := loopTag(header.Comments, loopStartTag); !ok || err != nil || value != 11025 {
		t.Fatalf("the tag reads back as %d, %v, %v; want 11025 present", value, ok, err)
	}
}

// The region comes off the comments in the same pass as the rest, in seconds,
// and reads the three tags case-insensitively. LOOPSTART alone runs to the
// Clip's own end, and LOOPLENGTH beats LOOPEND where a file carries both.
func TestJssoundReportsTheLoopRegionFromTheHeaderPass(t *testing.T) {
	cases := []struct {
		name       string
		tags       []string
		start, end int64
	}{
		{"start and length", []string{"LOOPSTART=11025", "LOOPLENGTH=22050"}, 11025, 33075},
		{"start and end", []string{"LOOPSTART=11025", "LOOPEND=33075"}, 11025, 33075},
		{"start alone runs to the end", []string{"LOOPSTART=11025"}, 11025, clipFrames},
		{"length beats end", []string{"LOOPSTART=11025", "LOOPLENGTH=22050", "LOOPEND=44100"}, 11025, 33075},
		{"in lower case", []string{"loopstart=11025", "looplength=22050"}, 11025, 33075},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := fakeAudio(t)
			b := started(t, f, true)
			_, prepared := resident(t, f, b, retagged(t, fixture(t), c.tags...))

			region, ok := prepared.LoopRegion().Get()
			if !ok {
				t.Fatalf("a Clip tagged %v reports no Loop Region", c.tags)
			}
			if region.Start != seconds(c.start) || region.End != seconds(c.end) {
				t.Fatalf("the region is [%v, %v), want [%v, %v)",
					region.Start, region.End, seconds(c.start), seconds(c.end))
			}
			if prepared.Duration() != clipDuration {
				t.Fatalf("the Clip reports %v, want the file's %v", prepared.Duration(), clipDuration)
			}
		})
	}
}

// A malformed region is dropped whole and never clamped, and the Adapter says so
// once per Clip - through the one subscription jssound has, because a Backend
// holds no Kernel of its own. A clamped loop sounds like a working loop with the
// wrong loop point, which is the single hardest audio bug to attribute.
func TestJssoundDropsAMalformedRegionWholeAndReportsItOncePerClip(t *testing.T) {
	for _, c := range []struct {
		name string
		tags []string
	}{
		{"past the end", []string{"LOOPSTART=11025", "LOOPEND=48705"}},
		{"end before start", []string{"LOOPSTART=33075", "LOOPEND=11025"}},
		{"not a number", []string{"LOOPSTART=the chorus"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := fakeAudio(t)
			b := started(t, f, true)
			_, prepared := resident(t, f, b, retagged(t, fixture(t), c.tags...))

			if prepared.LoopRegion().Present() {
				t.Fatalf("a region tagged %v was kept, and a malformed one is dropped whole", c.tags)
			}
			if prepared.Duration() != clipDuration {
				t.Fatalf("the Clip reports %v, want the file's %v", prepared.Duration(), clipDuration)
			}

			dropped := b.takeDroppedRegions()
			if len(dropped) != 1 {
				t.Fatalf("the Adapter queued %d notices for one bad Clip, want 1", len(dropped))
			}
			var ignored ErrLoopRegionIgnored
			if !errors.As(dropped[0], &ignored) {
				t.Fatalf("the notice is %v, want an ErrLoopRegionIgnored", dropped[0])
			}
			if ignored.Frames != clipFrames {
				t.Fatalf("the notice names %d frames, want the Clip's %d", ignored.Frames, clipFrames)
			}
			if again := b.takeDroppedRegions(); len(again) != 0 {
				t.Fatalf("the same Clip was reported %d times more, and it is reported once", len(again))
			}
		})
	}
}

// A Clip with no tags reports no region and nothing is said about it, which is
// what every Clip in this tree said before the tags existed.
func TestJssoundSaysNothingAboutAnUntaggedClip(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	_, prepared := resident(t, f, b, fixture(t))

	if prepared.LoopRegion().Present() {
		t.Fatal("a Clip with no LOOPSTART tag reported a Loop Region")
	}
	if dropped := b.takeDroppedRegions(); len(dropped) != 0 {
		t.Fatalf("an untagged Clip was reported as %v", dropped)
	}
}

// A Clip whose decode then fails says nothing about its loop tags: the notice
// belongs to a Clip that prepared, and a game never got this one.
func TestALoopNoticeIsNotSaidAboutAClipThatNeverInstalled(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)

	if _, _, err := b.Prepare("token", retagged(t, fixture(t), "LOOPSTART=11025", "LOOPEND=48705")); err != nil {
		t.Fatalf("Prepare refused the tagged fixture: %v", err)
	}
	f.settle(false)

	if taken := b.TakePrepared(); len(taken) != 1 || taken[0].Err == nil {
		t.Fatalf("TakePrepared drained %v, want the one refused decode", taken)
	}
	if dropped := b.takeDroppedRegions(); len(dropped) != 0 {
		t.Fatalf("a Clip that never installed was reported as %v", dropped)
	}
}

// The micro-clip jssound probes the browser with has to be a real Ogg Vorbis
// stream, or the probe answers no on every browser and every page falls back to
// the wasm decoder while nothing says why.
func TestTheEmbeddedProbeClipIsRealOggVorbis(t *testing.T) {
	length, format, err := oggvorbis.GetLength(bytes.NewReader(probeClip))
	if err != nil {
		t.Fatalf("the embedded probe clip does not read as Ogg Vorbis: %v", err)
	}
	if length <= 0 || format.SampleRate <= 0 || format.Channels <= 0 {
		t.Fatalf("the probe clip reports %d frames at %d Hz in %d channels",
			length, format.SampleRate, format.Channels)
	}
	samples, _, err := oggvorbis.ReadAll(bytes.NewReader(probeClip))
	if err != nil {
		t.Fatalf("the embedded probe clip does not decode: %v", err)
	}
	if len(samples) == 0 {
		t.Fatal("the probe clip decodes to no samples, so a browser could refuse it for being empty")
	}
	// Small enough that shipping it in every web build costs nothing worth
	// arguing about, and long enough that a decoder has real audio to refuse.
	if len(probeClip) > 16<<10 {
		t.Fatalf("the probe clip is %d bytes, and it ships in every web build", len(probeClip))
	}
}
