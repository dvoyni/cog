//go:build !js

package otosound

import (
	"bytes"
	"io"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/loopregion"
	"github.com/jfreymuth/oggvorbis"
)

// decodedSampleSize is one decoded sample as a resident clip holds it, a
// float32, which a clip's decoded size is counted in.
const decodedSampleSize = 4

// preparedClip is otosound's prepared clip, decoded once: its samples, which
// every voice on it shares, and the facts sfx is told, its source's. It is Go
// memory alone, so a clip released while its prepare is in flight leaves
// nothing unfreed. The encoded bytes it was decoded from are not kept, since
// nothing reads them again.
type preparedClip struct {
	// samples are the clip's frames, interleaved, channels to a frame, at the
	// source's rate: converting them to the device's is not built yet.
	samples []float32
	// frames is the clip's true length in its source's frames: the granule
	// end where the decoder handed back more.
	frames int64
	// channels is the source's, one or two.
	channels int
	// sampleRate is the source's, which SampleRate answers.
	sampleRate int
	// loopRegion is the region the clip's loop tags declare, absent for the
	// whole clip.
	loopRegion m.Maybe[sfx.LoopRegion]
	// loopRegionErr is why the region the tags declared was dropped, nil
	// when it is fine or absent.
	loopRegionErr error
}

// prepareClip reads encoded's identification header and its end granule
// position, without the setup header, and decodes the clip when its decoded
// size, frames × channels × 4 bytes, is within decodedClipLimit,
// Config.DecodedClipLimit with its default filled in: -1 is no limit, -2
// decodes none, and a positive limit is the largest size still decoded. A
// clip over it streams, which is not built yet, and fails with
// errStreamingNotBuilt. Headers that will not parse answer ErrNotOggVorbis, a
// rate or a channel count the mixer cannot play ErrNoStreamFormat, and a
// length that reads zero ErrNoStreamLength. It runs on a prepare goroutine,
// never on the flush or the device thread.
func prepareClip(encoded []byte, decodedClipLimit int) (*preparedClip, error) {
	length, format, err := oggvorbis.GetLength(bytes.NewReader(encoded))
	if err != nil {
		return nil, ErrNotOggVorbis{Err: err}
	}
	if format.SampleRate <= 0 || format.Channels <= 0 || format.Channels > outputChannels {
		return nil, ErrNoStreamFormat{SampleRate: format.SampleRate, Channels: format.Channels}
	}
	if length <= 0 {
		return nil, ErrNoStreamLength{}
	}
	decodedSize := length * int64(format.Channels) * decodedSampleSize
	if decodedClipLimit == decodedClipLimitAlwaysStream || decodedClipLimit != decodedClipLimitNone && decodedSize > int64(decodedClipLimit) {
		return nil, errStreamingNotBuilt
	}
	return decodeClip(encoded, length)
}

// decodeClip decodes encoded up to granuleEnd, the last page's granule
// position, and no further: what a decoder would hand back past it is a
// final packet's padding. The loop region is read against the frames
// decoded, so an absent region loops to the granule end and a LOOPEND is in
// range only against it.
func decodeClip(encoded []byte, granuleEnd int64) (*preparedClip, error) {
	reader, err := oggvorbis.NewReader(bytes.NewReader(encoded))
	if err != nil {
		return nil, ErrNotOggVorbis{Err: err}
	}
	channels := reader.Channels()
	samples := make([]float32, granuleEnd*int64(channels))
	decodedCount := 0
	for decodedCount < len(samples) {
		readCount, err := reader.Read(samples[decodedCount:])
		decodedCount += readCount
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrNotOggVorbis{Err: err}
		}
		if readCount == 0 {
			return nil, ErrNotOggVorbis{Err: io.ErrNoProgress}
		}
	}
	frames := int64(decodedCount / channels)
	if frames <= 0 {
		return nil, ErrNoStreamLength{}
	}
	loopRegion, loopRegionErr := loopregion.Read(reader.CommentHeader().Comments, reader.SampleRate(), frames)
	return &preparedClip{
		samples:       samples[:frames*int64(channels)],
		frames:        frames,
		channels:      channels,
		sampleRate:    reader.SampleRate(),
		loopRegion:    loopRegion,
		loopRegionErr: loopRegionErr,
	}, nil
}

// Duration is the clip's true length in frames over its source's rate, as
// the headless backend computes it, so the two report one clip identically.
func (c *preparedClip) Duration() float32 { return float32(c.frames) / float32(c.sampleRate) }

// Channels is the source's, one or two.
func (c *preparedClip) Channels() int { return c.channels }

// SampleRate is the source's, never the device's.
func (c *preparedClip) SampleRate() int { return c.sampleRate }

// LoopRegion is the region the clip's loop tags declare, absent for the
// whole clip.
func (c *preparedClip) LoopRegion() m.Maybe[sfx.LoopRegion] { return c.loopRegion }

// LoopRegionErr is the sfx.ErrLoopRegionIgnored, with no Path, of a region
// the clip's tags declared and the clip could not have, nil when the region
// is fine or absent.
func (c *preparedClip) LoopRegionErr() error { return c.loopRegionErr }
