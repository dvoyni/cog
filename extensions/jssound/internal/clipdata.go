//go:build js

package internal

import (
	"bytes"
	"syscall/js"

	"github.com/dvoyni/cog/extensions/jssound"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/jfreymuth/oggvorbis"
)

const (
	// outChannels is this Adapter's output width, and the width of the channel
	// splitter and merger a Voice is mixed through. It is stereo because
	// panning is, and it is a constant rather than a knob.
	outChannels = 2
	// renderQuantum is Web Audio's own block, in frames, and is fixed by the
	// specification rather than chosen. It is the block rate this Adapter
	// declicks over, which is why declicking belongs to the Adapter: sound runs
	// at 16.7 ms and this runs at 2.7 ms, so anything sound emitted would be a
	// step by the time it was heard.
	renderQuantum = 128
)

// clipData is a prepared Clip: an AudioBuffer, and the four facts the seam asks
// a PreparedClip for.
//
// It is garbage-collectable, which is the Port's rule, and it satisfies it the
// way sound.md says jssound would: the value is a js.Value, and syscall/js drops
// the JS-side reference when the Go value is collected. So a Clip released while
// its prepare was in flight - its completion dropped with nothing called,
// because there is no Destroy for something never installed - leaves nothing
// unfreed. The only handles in this Extension that are not collectable are the
// two js.Funcs a decode is asked with, and those are released by whichever of
// them fires.
//
// The four facts are the source's rather than the browser's. A game asking a
// Clip its rate is asking about the file; the AudioBuffer behind this is at
// whatever rate the decode produced, and nothing above the seam can tell.
type clipData struct {
	// buffer is the AudioBuffer a Voice is started from. It is undefined on a
	// Clip prepared with no Web Audio at all, which is jssound behaving exactly
	// as nosound does: the Clip still installs, so a Voice waiting on it starts
	// and ends on schedule, and it is simply never audible.
	buffer js.Value
	// duration is the source's length in seconds, computed from the granule end
	// the way otosound and nosound compute it, so that the three Adapters report
	// one Clip identically.
	duration float32
	// channels is the source's, one or two. The 2x2 gain matrix addresses no
	// more than two, so a third is refused in Prepare rather than truncated.
	channels int
	// sourceRate is what the file said, and is what SampleRate reports.
	sourceRate int
	// frames is the source's length in its own frames, corrected to the granule
	// end. It is what the Loop Region was checked against.
	frames int64
	// region is the Clip's Loop Region, read off its Vorbis comments. Absent is
	// what a Clip with no tag says, and means the whole Clip.
	region m.Maybe[sound.LoopRegion]
	// ignored is the Loop Region this Clip declared and could not have. It rides
	// on the Clip rather than being reported where it is found, because a report
	// needs a Kernel and only the tick holds one.
	ignored error
}

func (c *clipData) Duration() float32 { return c.duration }

func (c *clipData) Channels() int { return c.channels }

func (c *clipData) SampleRate() int { return c.sourceRate }

func (c *clipData) LoopRegion() m.Maybe[sound.LoopRegion] { return c.region }

// audible reports whether this Clip has samples a Voice could be started from.
// A Clip prepared with no Web Audio has none, and a start on it is recorded and
// makes no sound - which is what the Device being absent means everywhere else
// in this contract too.
func (c *clipData) audible() bool { return c.buffer.Truthy() }

// loopSpan is where a looping Voice on this Clip repeats between, in seconds,
// and is what loopStart and loopEnd on the source node are set to.
//
// An absent region is the whole Clip, and the whole Clip ends at the granule end
// rather than at the AudioBuffer's end. That distinction is the reason the
// headers are parsed in Go at all: a decoded buffer carries the final packet's
// padding, and a loop that wrapped at the buffer's end would repeat a fraction
// of a block of silence every time round.
func (c *clipData) loopSpan() (start, end float64) {
	region, ok := c.region.Get()
	if !ok {
		return 0, float64(c.duration)
	}
	return float64(region.Start), float64(region.End)
}

// readHeaders is the one pass over a Clip's own bytes for everything the seam
// asks about it: the source rate, the channels, the granule-corrected length,
// the duration that follows from those two, and the Loop Region.
//
// It runs whichever decoder will produce the samples, because decodeAudioData
// surfaces none of it - an AudioBuffer has a length and a rate and no Vorbis
// comments at all, and its rate is the context's rather than the file's.
//
// It reads over a bytes.Reader on the bytes it was handed rather than whatever
// storage opened, because the length is read from the stream's granule positions
// and so needs an io.Seeker, which storage.FileSystem.Open does not promise.
func readHeaders(encoded assets.Blob) (*clipData, error) {
	length, format, err := oggvorbis.GetLength(bytes.NewReader(encoded.Data()))
	if err != nil {
		return nil, jssound.ErrNotOggVorbis{Err: err}
	}
	if format.SampleRate <= 0 || format.Channels <= 0 || format.Channels > outChannels {
		return nil, jssound.ErrNoStreamFormat{SampleRate: format.SampleRate, Channels: format.Channels}
	}
	// A length of zero is a stream whose granule positions say nothing - a
	// truncated file. otosound streams such a Clip, because a streamed Clip
	// needs no decoded size; this tier has no such escape, and a Clip of no
	// duration would make the playhead fiction and silently remove
	// ReasonFinished from every Voice that named it.
	if length <= 0 {
		return nil, jssound.ErrNoStreamLength{}
	}
	frames, region, ignored := clipBounds(encoded, format.SampleRate, length, length)
	if frames <= 0 {
		return nil, jssound.ErrNoStreamLength{}
	}
	return &clipData{
		buffer:     js.Undefined(),
		duration:   float32(frames) / float32(format.SampleRate),
		channels:   format.Channels,
		sourceRate: format.SampleRate,
		frames:     frames,
		region:     region,
		ignored:    ignored,
	}, nil
}

// decodeInWasm is the fallback: jfreymuth/oggvorbis decodes the Clip in Go, and
// the samples reach the browser through an AudioBuffer made at the source's own
// rate and filled with copyToChannel.
//
// It is reached only when the probe at init found a browser that cannot decode
// Ogg Vorbis, which today means Safari on macOS before 15.4 or iOS before 18.4.
// WebCodecs would not help: Safari's AudioDecoder reaches the same OS codec.
//
// The cost is stated rather than hidden. Go's wasm target is single-threaded and
// has no asynchronous preemption, so this decode is a stall in the page however
// it is scheduled - running it on a goroutine moves it off sound's flush and off
// nothing else. That is the price of a browser with no Vorbis, and it is paid by
// the Clip's first load rather than by every frame that plays it.
func decodeInWasm(audio *webAudio, encoded assets.Blob, clip *clipData) (js.Value, error) {
	samples, format, err := oggvorbis.ReadAll(bytes.NewReader(encoded.Data()))
	if err != nil {
		return js.Undefined(), jssound.ErrNotOggVorbis{Err: err}
	}
	if format.SampleRate != clip.sourceRate || format.Channels != clip.channels {
		return js.Undefined(), jssound.ErrNoStreamFormat{SampleRate: format.SampleRate, Channels: format.Channels}
	}
	// Trimmed to the granule end rather than believed: the last packet may carry
	// padding past the stream's true end, and the buffer this fills is what
	// loopEnd is measured against.
	frames := int(clip.frames)
	if decoded := len(samples) / format.Channels; frames > decoded {
		frames = decoded
	}
	if frames <= 0 {
		return js.Undefined(), jssound.ErrNoStreamLength{}
	}
	samples = samples[:frames*format.Channels]

	buffer, err := audio.newBuffer(format.Channels, frames, format.SampleRate)
	if err != nil {
		return js.Undefined(), jssound.ErrDecodeRefused{Message: err.Error()}
	}
	for channel := range format.Channels {
		copyToChannel(buffer, channel, format.Channels, samples)
	}
	return buffer, nil
}
