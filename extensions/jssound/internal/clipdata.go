//go:build js

package internal

import (
	"bytes"
	"io"
	"math"
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
	// bytesPerSample is one decoded sample as an AudioBuffer holds it, which is
	// what the decoded size a Clip is measured against is counted in.
	bytesPerSample = 4
	// defaultDecodedClipLimit is the limit a Config that names none gets: the
	// largest decoded size a Clip may have and still be decoded whole. At
	// 512 KiB that is 2.97 s mono 44.1 kHz, 1.49 s stereo 44.1 kHz, 1.37 s
	// stereo 48 kHz. It is otosound's figure, unchanged, because the two
	// Adapters are answering one question about one Clip.
	defaultDecodedClipLimit = 512 << 10
	// neverStream and alwaysStream are the two sentinels DecodedClipLimit takes
	// beside a size in bytes. One field with sentinels rather than a second
	// bool beside it, because two fields can disagree and one cannot.
	neverStream  = -1
	alwaysStream = -2
)

// clipData is a prepared Clip in either tier, and the four facts the seam asks
// a PreparedClip for.
//
// It is one type for both tiers because both tiers report the same four facts
// and sound must not be able to tell which it got. A second type would be a
// second thing for the Port to carry, and the day something switched on it
// would be the day the tier stopped being the Adapter's business.
//
// A resident Clip is an AudioBuffer the browser or the wasm decoder made once;
// a streamed Clip is the encoded bytes and a way to open decoders against them,
// and its Voices each get a decoder, a resampler and a queue of chunk buffers
// scheduled on the context clock.
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

	// encoded is the Clip's own bytes. A streamed Clip reads them for as long as
	// it lives; a resident one is handed them for the header pass and never
	// looks again. Retaining them costs nothing either way: a Blob is a pointer
	// and a length, so it references the run the asset Library already holds.
	encoded assets.Blob
	// open is how a Voice on this Clip gets a decoder, and is nil on a resident
	// Clip - which is what "does this Clip stream" asks. It is a field rather
	// than a package function so that a test can stream a Clip it generated with
	// no Ogg anywhere in it, which is how a repeated or dropped frame at a loop
	// point becomes an assertion about frame numbers rather than about floats.
	open opener
	// filter converts this Clip's frames to the context rate, shared by every
	// Voice on it, and is nil when the file is already at that rate. Only a
	// streamed Clip has one: a resident Clip is an AudioBuffer at the file's own
	// rate and the browser converts it, seamlessly, because it is one buffer.
	filter *sincFilter
	// unmeasured is a Clip whose granule positions named no length. It must
	// stream - there is no decoded size to compare against a limit - and its
	// frames are counted before it completes, because a Clip that reported no
	// duration would be worse than one that streams.
	unmeasured bool
}

func (c *clipData) Duration() float32 { return c.duration }

func (c *clipData) Channels() int { return c.channels }

func (c *clipData) SampleRate() int { return c.sourceRate }

func (c *clipData) LoopRegion() m.Maybe[sound.LoopRegion] { return c.region }

// streams reports whether this Clip's Voices read through a decoder rather than
// out of one shared AudioBuffer.
func (c *clipData) streams() bool { return c.open != nil }

// audible reports whether this Clip has samples a Voice could be started from.
// A Clip prepared with no Web Audio has none, and a start on it is recorded and
// makes no sound - which is what the Device being absent means everywhere else
// in this contract too.
//
// A streamed Clip is audible with no buffer at all: its samples arrive one chunk
// at a time once a Voice asks for them.
func (c *clipData) audible() bool { return c.streams() || c.buffer.Truthy() }

// decodedBytes is what this Clip would cost held resident, and is what
// Config.DecodedClipLimit is compared against. It is computed from the headers
// before anything is decoded, which is the whole reason the limit is on decoded
// size at all.
//
// The frames counted are the file's rather than the context's, which is the
// figure otosound compares too. A browser resamples into the output device's
// rate, so the AudioBuffer really costs this scaled by the context rate over the
// file's - but that rate moves when the player changes headphones, and a limit
// whose meaning moved with it would put one Clip on either side of the line on
// one machine.
func (c *clipData) decodedBytes() int64 {
	return c.frames * int64(c.channels) * bytesPerSample
}

// overLimit reports whether a decoded size is more than a Config will decode
// whole. Zero is the default limit, -1 refuses to stream anything and -2 streams
// everything. It is otosound's function, spelled the same way, because the two
// Adapters must put one Clip in the same tier under the same number.
func overLimit(limit int, decoded int64) bool {
	switch limit {
	case alwaysStream:
		return true
	case neverStream:
		return false
	case 0:
		return decoded > defaultDecodedClipLimit
	default:
		return decoded > int64(limit)
	}
}

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

// sourceLoopBounds is the span a looping streamed Voice repeats between, in the
// file's own frames. That is the domain the wrap has to happen in: a streamed
// Voice loops by seeking its decoder, so the wrap is before the conversion
// rather than after it, and the resampler's window reaches across the join the
// same way it reaches across a chunk boundary. A loop applied after the
// conversion would put the filter's edge on the loop point, which is the
// repeated-or-dropped frame the promise forbids.
func (c *clipData) sourceLoopBounds() (start, end int64) {
	end = c.frames
	region, ok := c.region.Get()
	if !ok {
		return 0, end
	}
	start = int64(math.Round(float64(region.Start) * float64(c.sourceRate)))
	stop := int64(math.Round(float64(region.End) * float64(c.sourceRate)))
	if stop > start && stop <= end {
		end = stop
	}
	if start < 0 || start >= end {
		return 0, c.frames
	}
	return start, end
}

// sourceFrame is where a position in Clip seconds lands in the file's own
// frames, clamped into the Clip so that an offset past the end reads the end
// rather than seeking off the stream.
func (c *clipData) sourceFrame(seconds float64) int64 {
	at := int64(seconds * float64(c.sourceRate))
	if at < 0 {
		return 0
	}
	if at > c.frames {
		return c.frames
	}
	return at
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
	clip := &clipData{
		buffer:     js.Undefined(),
		channels:   format.Channels,
		sourceRate: format.SampleRate,
		encoded:    encoded,
	}
	// A length of zero is a stream whose granule positions say nothing - a
	// truncated file. It used to be terminal here, and is not any more: the spec
	// says a Clip with no computable decoded size streams, which is what
	// otosound has always done with one, so this Clip streams too and its frames
	// are counted by measure on the goroutine that was going to decode them
	// anyway. Counting means decoding, which is why it does not happen here: the
	// header pass runs inside sound's flush, and the tick is the one thread this
	// Adapter owes a whole frame to.
	//
	// What is not relaxed is a stream that truly holds no frames. A Clip of no
	// duration would make the playhead and the duration fiction and silently
	// remove ReasonFinished from every Voice that named it, so measure still
	// fails such a Clip with the same error this used to return.
	if length <= 0 {
		clip.unmeasured = true
		return clip, nil
	}
	if err := clip.resolve(length); err != nil {
		return nil, err
	}
	return clip, nil
}

// resolve fills in the facts that follow from a length in source frames: the
// granule-corrected frame count, the duration, and the Loop Region the same one
// pass over the comment header reads.
//
// It is its own step because a Clip whose granule positions named no length
// reaches it later and from another goroutine, once the frames have been
// counted, and the two must reach the same answer from the same arithmetic.
func (c *clipData) resolve(length int64) error {
	frames, region, ignored := clipBounds(c.encoded, c.sourceRate, length, length)
	if frames <= 0 {
		return jssound.ErrNoStreamLength{}
	}
	c.frames, c.region, c.ignored = frames, region, ignored
	c.duration = float32(frames) / float32(c.sourceRate)
	return nil
}

// measure counts an unmeasured Clip's frames by decoding it and throwing the
// samples away, and then resolves it.
//
// It is the only path in this Adapter that decodes a whole Clip it will not
// keep, and it runs only for a file whose granule positions carry no length,
// which is a broken one. It runs on the goroutine the streamed route spawns,
// never on the flush.
func (c *clipData) measure() error {
	decoder, err := c.open(c.encoded)
	if err != nil {
		return jssound.ErrNotOggVorbis{Err: err}
	}
	scratch := make([]float32, decodeChunk*c.channels)
	var frames int64
	for {
		read, err := decoder.read(scratch)
		frames += int64(read / c.channels)
		if err != nil || read == 0 {
			if err != nil && err != io.EOF {
				return jssound.ErrNotOggVorbis{Err: err}
			}
			break
		}
	}
	if frames <= 0 {
		return jssound.ErrNoStreamLength{}
	}
	c.unmeasured = false
	return c.resolve(frames)
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
