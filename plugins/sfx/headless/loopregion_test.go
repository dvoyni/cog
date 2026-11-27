package headless_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/headless"
	"github.com/jfreymuth/oggvorbis"
)

// The headless backend reads the loop region out of the same header pass that
// gives it the duration, the channels and the rate, and reaches the answer
// otosound reaches over the same bytes. The tagged clips are made here rather
// than checked in: retagClip rewrites the fixture's comment header and copies
// every audio page of it verbatim.

// The region comes off the comments in the same pass as the rest, in seconds,
// and reads the three tags case-insensitively.
func TestHeadlessReportsTheLoopRegionFromTheHeaderPass(t *testing.T) {
	cases := []struct {
		name       string
		tags       []string
		start, end int64
	}{
		{"start and length", []string{"LOOPSTART=11025", "LOOPLENGTH=22050"}, 11025, 33075},
		{"start and end", []string{"LOOPSTART=11025", "LOOPEND=33075"}, 11025, 33075},
		{"start alone runs to the end", []string{"LOOPSTART=11025"}, 11025, clipFrames},
		{"in lower case", []string{"loopstart=11025", "looplength=22050"}, 11025, 33075},
		{"length wins over end", []string{"LOOPSTART=11025", "LOOPEND=44100", "LOOPLENGTH=22050"}, 11025, 33075},
		{"a repeated tag is read by its first", []string{"LOOPSTART=11025", "LOOPSTART=0", "LOOPEND=33075"}, 11025, 33075},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prepared, _, err := headless.NewBackend(48000).Prepare(nil, retagClip(t, readFixture(t), c.tags...))
			if err != nil {
				t.Fatalf("preparing the tagged fixture: %v", err)
			}
			region, ok := prepared.LoopRegion().Get()
			if !ok {
				t.Fatalf("a clip tagged %v reports no loop region", c.tags)
			}
			if region.Start != convertToSeconds(c.start) || region.End != convertToSeconds(c.end) {
				t.Fatalf("the region is [%v, %v), want [%v, %v)",
					region.Start, region.End, convertToSeconds(c.start), convertToSeconds(c.end))
			}
			if prepared.Duration() != clipDuration {
				t.Fatalf("the clip reports %v, want the file's %v", prepared.Duration(), clipDuration)
			}
			if err := prepared.LoopRegionErr(); err != nil {
				t.Fatalf("a well-formed region answered LoopRegionErr %v", err)
			}
		})
	}
}

// A malformed region is dropped whole and never clamped, and the clip answers
// why from LoopRegionErr, with no path: the backend has no name for a clip, so
// sfx fills the path in and reports it once for the clip.
func TestHeadlessDropsAMalformedRegionWholeAndReportsItOncePerClip(t *testing.T) {
	cases := []struct {
		name string
		tags []string
		want sfx.ErrLoopRegionIgnored
		// unparsed says the notice carries the parse failure of a tag.
		unparsed bool
	}{
		{"an end past the clip", []string{"LOOPSTART=11025", "LOOPEND=48705"},
			sfx.ErrLoopRegionIgnored{Start: 11025, End: 48705, Frames: clipFrames}, false},
		{"an end before the start", []string{"LOOPSTART=11025", "LOOPEND=11025"},
			sfx.ErrLoopRegionIgnored{Start: 11025, End: 11025, Frames: clipFrames}, false},
		{"a negative start", []string{"LOOPSTART=-1", "LOOPLENGTH=100"},
			sfx.ErrLoopRegionIgnored{Start: -1, End: 99, Frames: clipFrames}, false},
		{"a value that is no count", []string{"LOOPSTART=one", "LOOPEND=33075"},
			sfx.ErrLoopRegionIgnored{Frames: clipFrames}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prepared, _, err := headless.NewBackend(48000).Prepare(nil, retagClip(t, readFixture(t), c.tags...))
			if err != nil {
				t.Fatalf("a malformed loop tag failed the clip: %v", err)
			}
			if prepared.LoopRegion().Present() {
				t.Fatal("a malformed region was kept, and it must be dropped whole")
			}
			if prepared.Duration() != clipDuration {
				t.Fatalf("the clip reports %v, want the file's %v", prepared.Duration(), clipDuration)
			}
			var ignored sfx.ErrLoopRegionIgnored
			if !errors.As(prepared.LoopRegionErr(), &ignored) {
				t.Fatalf("LoopRegionErr is %v, want an sfx.ErrLoopRegionIgnored", prepared.LoopRegionErr())
			}
			if ignored.Path != "" {
				t.Fatalf("the backend named the clip %q, and only sfx knows its path", ignored.Path)
			}
			if ignored.Start != c.want.Start || ignored.End != c.want.End || ignored.Frames != c.want.Frames {
				t.Fatalf("the notice names [%d, %d) against %d frames, want [%d, %d) against %d",
					ignored.Start, ignored.End, ignored.Frames, c.want.Start, c.want.End, c.want.Frames)
			}
			var parse *strconv.NumError
			if unparsed := errors.As(ignored.Err, &parse); unparsed != c.unparsed {
				t.Fatalf("the notice carries %v as its parse failure, want one: %v", ignored.Err, c.unparsed)
			}
		})
	}
}

// A clip with no tags reports no region and says nothing about it.
func TestHeadlessSaysNothingAboutAnUntaggedClip(t *testing.T) {
	prepared, _, err := headless.NewBackend(48000).Prepare(nil, readFixture(t))
	if err != nil {
		t.Fatalf("preparing the fixture: %v", err)
	}
	if prepared.LoopRegion().Present() {
		t.Fatal("a clip with no LOOPSTART tag reported a loop region")
	}
	if err := prepared.LoopRegionErr(); err != nil {
		t.Fatalf("an untagged clip answered LoopRegionErr %v", err)
	}
}

// convertToSeconds answers a count of source frames in seconds, as the seam
// carries it, converted the way the backend converts it.
func convertToSeconds(frames int64) float32 { return float32(float64(frames) / float64(clipRate)) }

// retagClip rewrites the fixture's Vorbis comment header with extra comments
// appended, and re-lays the one page that header sits in.
func retagClip(t *testing.T, encoded []byte, extra ...string) []byte {
	t.Helper()
	header, err := oggvorbis.GetCommentHeader(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("reading the fixture's comment header: %v", err)
	}
	tagged := makeCommentPacket(header.Vendor, slices.Concat(header.Comments, extra))

	for at := 0; at < len(encoded); {
		page := encoded[at:]
		head, body := measurePage(t, page)
		begin := 0
		from := head
		to := head
		for i, segment := range page[27:head] {
			to += int(segment)
			if segment == 255 {
				continue
			}
			// The comment header is packet type 3 and the codec's signature.
			if packet := page[from:to]; len(packet) >= 7 && packet[0] == 3 && string(packet[1:7]) == "vorbis" {
				return replacePage(t, encoded, at, begin, i, tagged)
			}
			begin = i + 1
			from = to
		}
		at += head + body
	}
	t.Fatal("the fixture carries no Vorbis comment header to re-tag")
	return nil
}

// makeCommentPacket builds a Vorbis comment header packet: the type byte and
// the signature, the vendor string, the comments, and the framing bit the format
// ends it with.
func makeCommentPacket(vendor string, comments []string) []byte {
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

// replacePage swaps the packet laced in segments [beginSegment, endSegment]
// of the page at offset at for packet, and re-lays that page: a new segment
// table for the packet, the segments and bodies either side of it kept as
// they were, and the page's checksum recomputed over the result.
func replacePage(t *testing.T, data []byte, at, beginSegment, endSegment int, packet []byte) []byte {
	t.Helper()
	page := data[at:]
	head, body := measurePage(t, page)
	table := page[27:head]
	from := head
	for _, size := range table[:beginSegment] {
		from += int(size)
	}
	to := from
	for _, size := range table[beginSegment : endSegment+1] {
		to += int(size)
	}
	segments := slices.Concat(table[:beginSegment], makeLacing(len(packet)), table[endSegment+1:])
	if len(segments) > 255 {
		t.Fatalf("the re-tagged comment header needs %d segments, and a page holds 255", len(segments))
	}

	relaid := slices.Concat(page[:27], segments, page[head:from], packet, page[to:head+body])
	relaid[26] = byte(len(segments))
	sealPage(t, relaid)
	return slices.Concat(data[:at], relaid, data[at+head+body:])
}

// makeLacing writes a packet length as Ogg segment sizes: as many 255s as it
// takes and then the remainder, which marks the packet's end.
func makeLacing(length int) []byte {
	var segments []byte
	for length >= 255 {
		segments = append(segments, 255)
		length -= 255
	}
	return append(segments, byte(length))
}
