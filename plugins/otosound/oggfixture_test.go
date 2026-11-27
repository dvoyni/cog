//go:build !js

package otosound

import (
	"bytes"
	"encoding/binary"
	"os"
	"slices"
	"testing"

	"github.com/jfreymuth/oggvorbis"
)

// The fixture clip's own facts, read off the file it was cut to and written
// down rather than computed, so that a change to the file fails here. They are
// the numbers the headless backend's suite writes down: both backends report
// one clip identically.
const (
	clipFrames   = 48704
	clipRate     = 44100
	clipChannels = 2
)

// clipDuration is the fixture's length in seconds as the seam carries it.
const clipDuration = float32(clipFrames) / float32(clipRate)

// readFixture answers the fixture clip's bytes.
func readFixture(t testing.TB) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/pianoroll.ogg")
	if err != nil {
		t.Fatalf("reading the fixture clip: %v", err)
	}
	return data
}

// retagClip rewrites the fixture's Vorbis comment header with extra comments
// appended, and re-lays the one page that header sits in. Every other page is
// copied verbatim, so a re-tagged clip is the same sound with a loop point
// written on it.
func retagClip(t testing.TB, encoded []byte, extra ...string) []byte {
	t.Helper()
	header, err := oggvorbis.GetCommentHeader(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("reading the fixture's comment header: %v", err)
	}
	commentPacket := makeCommentPacket(header.Vendor, slices.Concat(header.Comments, extra))

	for pageOffset := 0; pageOffset < len(encoded); {
		page := encoded[pageOffset:]
		headLength, bodyLength := measurePage(t, page)
		packetFirstSegment := 0
		packetStart := headLength
		packetEnd := headLength
		for segmentIndex, segment := range page[27:headLength] {
			packetEnd += int(segment)
			if segment == 255 {
				continue
			}
			// The comment header is packet type 3 and the codec's signature.
			if packet := page[packetStart:packetEnd]; len(packet) >= 7 && packet[0] == 3 && string(packet[1:7]) == "vorbis" {
				return replacePage(t, encoded, pageOffset, packetFirstSegment, segmentIndex, commentPacket)
			}
			packetFirstSegment = segmentIndex + 1
			packetStart = packetEnd
		}
		pageOffset += headLength + bodyLength
	}
	t.Fatal("the fixture carries no Vorbis comment header to re-tag")
	return nil
}

// makeCommentPacket builds a Vorbis comment header packet: the type byte and
// the signature, the vendor string, the comments, and the framing bit the
// format ends it with.
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

// replacePage swaps the packet laced in segments [firstSegment, lastSegment]
// of the page at pageOffset for packet, and re-lays that page: a new segment
// table for the packet, the segments and bodies either side of it kept as
// they were, and the page's checksum recomputed over the result.
func replacePage(t testing.TB, data []byte, pageOffset, firstSegment, lastSegment int, packet []byte) []byte {
	t.Helper()
	page := data[pageOffset:]
	headLength, bodyLength := measurePage(t, page)
	segmentTable := page[27:headLength]
	packetStart := headLength
	for _, size := range segmentTable[:firstSegment] {
		packetStart += int(size)
	}
	packetEnd := packetStart
	for _, size := range segmentTable[firstSegment : lastSegment+1] {
		packetEnd += int(size)
	}
	segments := slices.Concat(segmentTable[:firstSegment], makeLacing(len(packet)), segmentTable[lastSegment+1:])
	if len(segments) > 255 {
		t.Fatalf("the re-tagged comment header needs %d segments, and a page holds 255", len(segments))
	}

	relaidPage := slices.Concat(page[:27], segments, page[headLength:packetStart], packet, page[packetEnd:headLength+bodyLength])
	relaidPage[26] = byte(len(segments))
	sealPage(t, relaidPage)
	return slices.Concat(data[:pageOffset], relaidPage, data[pageOffset+headLength+bodyLength:])
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

// clearLastGranule zeroes the granule position of a stream's last page, the
// page the stream's length is read from, and fixes that page's checksum, so
// the stream is still well formed and its length reads zero.
func clearLastGranule(t testing.TB, data []byte) []byte {
	t.Helper()
	cleared := bytes.Clone(data)
	lastPageOffset := 0
	for pageOffset := 0; pageOffset < len(cleared); {
		headLength, bodyLength := measurePage(t, cleared[pageOffset:])
		lastPageOffset = pageOffset
		pageOffset += headLength + bodyLength
	}
	clear(cleared[lastPageOffset+6 : lastPageOffset+14])
	sealPage(t, cleared[lastPageOffset:])
	return cleared
}

// rewriteFormat rewrites the channel count and the sample rate the
// identification header names, the first packet of the first page, and fixes
// that page's checksum.
func rewriteFormat(t testing.TB, data []byte, channels byte, rate uint32) []byte {
	t.Helper()
	rewritten := bytes.Clone(data)
	headLength, _ := measurePage(t, rewritten)
	if rewritten[headLength] != 1 || string(rewritten[headLength+1:headLength+7]) != "vorbis" {
		t.Fatal("the fixture's first packet is not a Vorbis identification header")
	}
	// The type byte, the signature and a 32-bit version come before the
	// channel count, and the rate follows it.
	rewritten[headLength+11] = channels
	binary.LittleEndian.PutUint32(rewritten[headLength+12:headLength+16], rate)
	sealPage(t, rewritten)
	return rewritten
}

// measurePage answers the length of the header, its segment table included,
// and of the body of the Ogg page that page begins with.
func measurePage(t testing.TB, page []byte) (headLength, bodyLength int) {
	t.Helper()
	if len(page) < 27 || string(page[:4]) != "OggS" {
		t.Fatal("no Ogg capture pattern where a page should begin")
	}
	headLength = 27 + int(page[26])
	for _, size := range page[27:headLength] {
		bodyLength += int(size)
	}
	return headLength, bodyLength
}

// sealPage recomputes the checksum of the Ogg page that page begins with.
func sealPage(t testing.TB, page []byte) {
	t.Helper()
	headLength, bodyLength := measurePage(t, page)
	clear(page[22:26])
	binary.LittleEndian.PutUint32(page[22:26], computeOggCRC(page[:headLength+bodyLength]))
}

// computeOggCRC answers Ogg's page checksum: a 32-bit CRC over the whole page
// with the checksum field zeroed, polynomial 0x04c11db7, no reflection and no
// final xor.
func computeOggCRC(page []byte) uint32 {
	var checksum uint32
	for _, b := range page {
		checksum ^= uint32(b) << 24
		for range 8 {
			if checksum&0x80000000 != 0 {
				checksum = checksum<<1 ^ 0x04c11db7
			} else {
				checksum <<= 1
			}
		}
	}
	return checksum
}
