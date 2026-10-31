//go:build js

package internal

import (
	"encoding/binary"
	"errors"
)

// This is the Ogg container, walked in Go, and it exists for one reason: a
// WebCodecs AudioDecoder takes Vorbis packets and knows nothing about Ogg. The
// browser will decode the audio; the demux is ours either way, which is what
// the ticket means by "demuxed in Go and decoded in chunks".
//
// jfreymuth/oggvorbis demuxes too, and this does not replace it: that decoder
// keeps its pages to itself behind a Reader, and what an EncodedAudioChunk
// needs is the packet bytes. So the container is walked once more, here, over
// the same run of bytes the Clip already retained - sub-sliced rather than
// copied, so a five-minute track costs a slice header per packet and no audio
// at all.
//
// Nothing here validates Vorbis beyond what the route needs. A file this
// mis-reads falls back to the Go decoder, which parses it properly and fails it
// properly, so this being strict would only move an error report to a worse
// place.

const (
	// oggHeaderBytes is the fixed part of a page header, up to but not
	// including the segment table.
	oggHeaderBytes = 27
	// oggSegmentCount is where the segment count sits in that header, and
	// oggSerial where the bitstream's serial number does.
	oggSegmentCount = 26
	oggSerial       = 14
	// vorbisHeaders is how many setup packets a Vorbis stream begins with:
	// identification, comment and setup. They are the three the decoder
	// description carries, and every packet after them is audio.
	vorbisHeaders = 3
	// idHeaderBytes is the shortest identification header that carries the two
	// facts the decoder config needs, channels and sample rate. The real one is
	// 30 bytes; anything shorter than this is not one.
	idHeaderBytes = 16
)

// errNotVorbisPackets is a stream this walker could not take apart into the
// three Vorbis setup headers and a run of audio packets. It is never reported
// to a game: the streamed route answers it by opening the Go decoder instead,
// which is the same bytes read by something that will say what is wrong with
// them properly.
var errNotVorbisPackets = errors.New("jssound: these bytes are not an Ogg Vorbis bitstream this walker can take apart")

// vorbisStream is one Ogg Vorbis bitstream as WebCodecs wants it: the format
// the identification header declares, the three setup headers folded into one
// decoder description, and every audio packet in order.
//
// The packets are sub-slices of the bytes handed in, except where one spans a
// page boundary and has to be rejoined. That is the difference between holding
// a 4.8 MB track and holding a second copy of it.
type vorbisStream struct {
	channels   int
	sampleRate int
	// description is the xiph extradata layout an AudioDecoder config carries:
	// a count, the lacing of the first two header lengths, and the three
	// headers back to back. Chrome accepts it and the layout is asserted byte
	// for byte in this package's tests against a real Clip, which is what was
	// missing when this route was first declined.
	description []byte
	packets     [][]byte
}

// demuxVorbis walks an Ogg bitstream and takes out everything the WebCodecs
// route needs from it.
func demuxVorbis(data []byte) (*vorbisStream, error) {
	packets, err := oggPackets(data)
	if err != nil {
		return nil, err
	}
	if len(packets) <= vorbisHeaders {
		return nil, errNotVorbisPackets
	}
	id, comment, setup := packets[0], packets[1], packets[2]
	if !vorbisHeaderIs(id, 1) || !vorbisHeaderIs(comment, 3) || !vorbisHeaderIs(setup, 5) {
		return nil, errNotVorbisPackets
	}
	if len(id) < idHeaderBytes {
		return nil, errNotVorbisPackets
	}
	// The identification header's own layout: the packet type and the "vorbis"
	// signature are seven bytes, then a four-byte version, then the channel
	// count at byte 11 and the sample rate at bytes 12 to 15.
	channels := int(id[11])
	rate := int(binary.LittleEndian.Uint32(id[12:16]))
	if channels <= 0 || rate <= 0 {
		return nil, errNotVorbisPackets
	}
	return &vorbisStream{
		channels:    channels,
		sampleRate:  rate,
		description: xiphExtradata(id, comment, setup),
		packets:     packets[vorbisHeaders:],
	}, nil
}

// vorbisHeaderIs reports whether a packet is the Vorbis header of a given type:
// the type byte, then the six bytes "vorbis".
func vorbisHeaderIs(packet []byte, kind byte) bool {
	return len(packet) >= 7 && packet[0] == kind && string(packet[1:7]) == "vorbis"
}

// xiphExtradata folds the three Vorbis setup headers into the one byte string a
// decoder configuration carries as its description.
//
// The layout is the count of headers less one, then the length of each header
// but the last in Ogg's own lacing - 255 as many times as it goes, then the
// remainder - then the headers back to back. The last length is not written
// because it is whatever is left.
//
// It is the layout the ticket called untestable, and it is testable after all:
// the bytes it produces for the fixture Clip are asserted here, and the same
// bytes were accepted by a real AudioDecoder.isConfigSupported before this was
// built.
func xiphExtradata(headers ...[]byte) []byte {
	out := []byte{byte(len(headers) - 1)}
	for _, header := range headers[:len(headers)-1] {
		length := len(header)
		for length >= 255 {
			out = append(out, 255)
			length -= 255
		}
		out = append(out, byte(length))
	}
	for _, header := range headers {
		out = append(out, header...)
	}
	return out
}

// oggPackets walks the pages of one Ogg bitstream and returns its packets in
// order.
//
// A page is a magic word, a fixed header, a table of segment lengths and the
// segments themselves; a packet is the segments up to and including the first
// one shorter than 255, and it may run off the end of a page and continue on the
// next. Only the first bitstream's serial number is taken, because a Clip is one
// stream and a page carrying another serial is a multiplexed file this Adapter
// does not claim to play.
func oggPackets(data []byte) ([][]byte, error) {
	var packets [][]byte
	var partial []byte
	var serial uint32
	var haveSerial bool

	for at := 0; at+oggHeaderBytes <= len(data); {
		if string(data[at:at+4]) != "OggS" {
			return nil, errNotVorbisPackets
		}
		segments := int(data[at+oggSegmentCount])
		table := at + oggHeaderBytes
		body := table + segments
		if body > len(data) {
			return nil, errNotVorbisPackets
		}
		page := uint32(binary.LittleEndian.Uint32(data[at+oggSerial : at+oggSerial+4]))
		if !haveSerial {
			serial, haveSerial = page, true
		}
		lengths := data[table:body]
		total := 0
		for _, length := range lengths {
			total += int(length)
		}
		if body+total > len(data) {
			return nil, errNotVorbisPackets
		}
		if page != serial {
			at = body + total
			continue
		}
		// Segments of one packet are contiguous inside a page, so a packet that
		// does not span one is a sub-slice and costs nothing. Only a packet
		// carried over a page boundary is rejoined, and a Vorbis audio packet is
		// smaller than a page often enough that this is rare.
		start, run := body, 0
		for _, length := range lengths {
			run += int(length)
			if length == 255 {
				continue
			}
			segment := data[start : start+run]
			if partial != nil {
				partial = append(partial, segment...)
				packets = append(packets, partial)
				partial = nil
			} else {
				packets = append(packets, segment)
			}
			start, run = start+run, 0
		}
		if run > 0 {
			partial = append(partial, data[start:start+run]...)
		}
		at = body + total
	}
	if len(packets) == 0 {
		return nil, errNotVorbisPackets
	}
	return packets, nil
}
