//go:build !js

package otosound

import (
	"bytes"
	"io"
	"math"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/loopregion"
	"github.com/jfreymuth/oggvorbis"
)

// decodedSampleSize is one decoded sample as a resident clip holds it, a
// float32, which a clip's decoded size is counted in.
const decodedSampleSize = 4

// preparedClip is otosound's prepared clip, in either tier, and the facts
// sfx is told, its source's, never the device's, which both tiers answer
// alike, so sfx cannot tell which it got. A resident clip is decoded once and
// converted to the rate the mixer produces, its samples shared by every voice
// on it, and the encoded bytes it was decoded from are not kept, since
// nothing reads them again. A streamed clip keeps its encoded bytes, the
// filter its voices share and the way to open decoders over them, and each
// voice on it decodes into a read-ahead ring of its own. It is Go memory
// alone, so a clip released while its prepare is in flight leaves nothing
// unfreed.
type preparedClip struct {
	// samples are a resident clip's frames, interleaved, channels to a frame,
	// at the rate the mixer produces, converted from the source's once, in
	// Prepare: len(samples)/channels converted frames, which frames is not.
	// A streamed clip has none: its frames arrive in each voice's ring.
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
	// encoded is a streamed clip's bytes, which its voices' decoders read
	// for as long as the clip is held; nil on a resident clip.
	encoded []byte
	// filter converts a streamed clip's frames to the rate the mixer
	// produces, built once for its pair of rates and read-only after, so
	// every voice on it shares it; nil when the source is at that rate, and
	// on a resident clip, which used one once and dropped it.
	filter *sincFilter
	// openDecoder opens a decoder over encoded for a voice on a streamed
	// clip, and is nil on a resident clip, which is what streams asks. It is
	// a field so that a test can stream frames it generated, through a
	// decoder that panics if the device thread ever calls it.
	openDecoder func(encoded []byte) (streamDecoder, error)
}

// prepareClip reads encoded's identification header and its end granule
// position, without the setup header, and decodes the clip when its decoded
// size, frames × channels × 4 bytes, is within decodedClipLimit,
// Config.DecodedClipLimit with its default filled in: -1 is no limit, -2
// decodes none, and a positive limit is the largest size still decoded. A
// decoded clip whose source's rate is not outputRate, the rate the mixer
// produces, is converted to it, once, here. A clip over the limit streams.
// Headers that will not parse answer ErrNotOggVorbis, a rate or a channel
// count the mixer cannot play ErrNoStreamFormat, and a length that reads
// zero ErrNoStreamLength, as under the headless backend: such a stream's
// last page carries a granule position of zero, and the decoder panics on
// the last packet of one rather than counting it. It runs on a prepare goroutine, never on the flush or
// the device thread.
func prepareClip(encoded []byte, decodedClipLimit, outputRate int) (*preparedClip, error) {
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
		return retainClip(encoded, length, outputRate)
	}
	clip, err := decodeClip(encoded, length)
	if err != nil {
		return nil, err
	}
	clip.samples = convertRate(clip.samples, clip.channels, clip.sampleRate, outputRate)
	return clip, nil
}

// decodeClip decodes encoded up to granuleEnd, the last page's granule
// position, and no further: what a decoder would hand back past it is a
// final packet's padding. The loop region is read against the frames
// decoded, so an absent region loops to the granule end and a LOOPEND is in
// range only against it. Its samples are at the source's rate, which
// prepareClip converts.
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

// retainClip prepares a clip of length source frames that streams: it keeps
// encoded, reads the loop region off its comment header, and builds the
// filter its voices will share when its source's rate is not outputRate,
// and decodes nothing a voice will play. Opening its reader parses the setup
// header, so a clip whose decoder could never open is refused here rather
// than at its first voice.
func retainClip(encoded []byte, length int64, outputRate int) (*preparedClip, error) {
	reader, err := oggvorbis.NewReader(bytes.NewReader(encoded))
	if err != nil {
		return nil, ErrNotOggVorbis{Err: err}
	}
	loopRegion, loopRegionErr := loopregion.Read(reader.CommentHeader().Comments, reader.SampleRate(), length)
	clip := &preparedClip{
		frames:        length,
		channels:      reader.Channels(),
		sampleRate:    reader.SampleRate(),
		loopRegion:    loopRegion,
		loopRegionErr: loopRegionErr,
		encoded:       encoded,
		openDecoder:   openOggDecoder,
	}
	if clip.sampleRate != outputRate {
		clip.filter = newSincFilter(clip.sampleRate, outputRate)
	}
	return clip, nil
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

// streams is whether the clip's voices each read a decoder of their own
// through a read-ahead ring, rather than the samples it holds.
func (c *preparedClip) streams() bool { return c.openDecoder != nil }

// loopBounds is the span a looping voice repeats between, held as frames
// frames at rate frames a second: the loop region's seconds times rate,
// rounded and capped at frames, and the whole clip when it declares none. A
// region sfx accepted spans at least one of its source's frames, which
// rounds to none only where the frames are held at a far lower rate; such a
// span would never let the playhead wrap, so it loops whole. A resident
// voice asks it at the rate the mixer produces, of the frames its clip's
// samples hold; a streamed voice's read-ahead at its source's rate, of its
// source's frames, since it wraps before the conversion rather than after
// it. Neither scales the clip's frame count.
func (c *preparedClip) loopBounds(rate, frames float64) (start, end float64) {
	region, declared := c.loopRegion.Get()
	if declared {
		start = math.Round(float64(region.Start) * rate)
		end = min(frames, math.Round(float64(region.End)*rate))
		if start >= 0 && start < end {
			return start, end
		}
	}
	return 0, frames
}
