package headless

import (
	"bytes"
	"fmt"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/loopregion"
	"github.com/jfreymuth/oggvorbis"
)

// Backend is an sfx.BackendSocket that plays nothing. It accepts every start,
// update, stop and destroy and keeps none of them, prepares a clip by reading
// its headers, and answers a device that is ready. sfx calls it from one flush
// at a time, so it guards nothing.
type Backend struct {
	// device is what Device reports, built once: nothing is opened, so
	// nothing can be lost.
	device sfx.Device
	// lastClipID is the ID Install minted last; the next is one more, so an
	// ID is never zero and never repeats.
	lastClipID sfx.ClipID
}

// NewBackend answers a headless backend whose device reports sampleRate, zero
// meaning 48000. A negative rate panics.
func NewBackend(sampleRate int) *Backend {
	if sampleRate < 0 {
		panic(fmt.Sprintf("headless: NewBackend's sample rate is %d, and it may not be negative", sampleRate))
	}
	if sampleRate == 0 {
		sampleRate = 48000
	}
	return &Backend{device: sfx.Device{
		Ready:      true,
		Name:       "headless",
		SampleRate: sampleRate,
		Channels:   2,
	}}
}

// Voices keeps the number nowhere: there is no table to size when nothing is
// mixed.
func (b *Backend) Voices(n int) {}

// Emit accepts one flush's changes and plays none of them.
func (b *Backend) Emit(changes *sfx.FlushChanges) {}

// Device reports a working device that plays nothing: ready, stereo, at the
// rate the backend was made with, and at zero latency, since nothing is
// buffered.
func (b *Backend) Device() sfx.Device { return b.device }

// Prepare reads the Ogg headers, the stream's length and its loop tags, and
// decodes no samples. It is always done, so nothing ever appears in
// TakePrepared. Bytes that are not Ogg Vorbis answer ErrNotOggVorbis, a header
// naming no rate or no channels ErrNoStreamFormat, and a stream whose length
// reads zero ErrNoStreamLength. A malformed loop region costs the clip only
// its region, answered by the prepared clip's LoopRegionErr.
//
// It parses a bytes.Reader over encoded because the length is the last page's
// granule position, which only a reader that seeks can find. Its cost is flat
// whatever the clip's length. The reader decodes the first audio packet as
// well as the three headers, since Vorbis data need not start at position
// zero, so "decoding no samples" is constant work rather than none.
func (b *Backend) Prepare(token any, encoded []byte) (sfx.PreparedClip, bool, error) {
	reader, err := oggvorbis.NewReader(bytes.NewReader(encoded))
	if err != nil {
		return nil, false, ErrNotOggVorbis{Err: err}
	}
	sampleRate := reader.SampleRate()
	channels := reader.Channels()
	if sampleRate <= 0 || channels <= 0 {
		return nil, false, ErrNoStreamFormat{SampleRate: sampleRate, Channels: channels}
	}
	frames := reader.Length()
	if frames <= 0 {
		return nil, false, ErrNoStreamLength{}
	}
	loopRegion, loopRegionErr := loopregion.Read(reader.CommentHeader().Comments, sampleRate, frames)
	return preparedClip{
		duration:      float32(frames) / float32(sampleRate),
		channels:      channels,
		sampleRate:    sampleRate,
		loopRegion:    loopRegion,
		loopRegionErr: loopRegionErr,
	}, true, nil
}

// TakePrepared answers nothing, since Prepare is always done.
func (b *Backend) TakePrepared() []sfx.Prepared { return nil }

// Install mints the clip's ID and keeps nothing else of it. The prepared
// value is a header's facts and garbage-collectable, so a clip released while
// it was prepared leaves nothing unfreed.
func (b *Backend) Install(clip sfx.PreparedClip) (sfx.ClipID, error) {
	b.lastClipID++
	return b.lastClipID, nil
}

// preparedClip is everything the headless backend keeps of a clip: the facts
// the contract must be able to report, and no samples.
type preparedClip struct {
	duration      float32
	channels      int
	sampleRate    int
	loopRegion    m.Maybe[sfx.LoopRegion]
	loopRegionErr error
}

func (c preparedClip) Duration() float32 { return c.duration }

func (c preparedClip) Channels() int { return c.channels }

func (c preparedClip) SampleRate() int { return c.sampleRate }

func (c preparedClip) LoopRegion() m.Maybe[sfx.LoopRegion] { return c.loopRegion }

func (c preparedClip) LoopRegionErr() error { return c.loopRegionErr }
