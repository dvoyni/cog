//go:build !js

package internal

import (
	"bytes"
	"math"

	"github.com/dvoyni/cog/extensions/otosound"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/jfreymuth/oggvorbis"
)

// clipData is a resident Clip: the whole thing decoded once, interleaved, and
// already converted to the rate the Mixer produces, so that the device thread
// only ever interpolates for a Voice's Rate and never for a base rate.
//
// It is also the sound.PreparedClip handed back through the Port, which is why
// the four facts it reports are the source's rather than the device's: a game
// asking a Clip its rate is asking about the file, and an Adapter that answered
// with the device's would make one Clip two cache entries the day the device
// changed. Nothing above the seam can tell that the samples were converted.
//
// It is garbage-collectable, which is the Port's rule: a Clip released while
// its prepare was in flight has its completion dropped with nothing called, so
// anything needing explicit release would never be freed.
type clipData struct {
	// samples are interleaved, channels per frame, at rate.
	samples []float32
	// frames is len(samples)/channels, after conversion.
	frames int
	// channels is the source's, one or two. The 2x2 gain matrix addresses no
	// more than two, so a third is refused in Prepare rather than truncated.
	channels int
	// rate is the device rate the samples were converted to.
	rate int
	// sourceRate is what the file said, and is what SampleRate reports.
	sourceRate int
	// duration is the source's length in seconds, computed the way nosound
	// computes it so that the two Adapters report one Clip identically.
	duration float32
	// region is the Clip's Loop Region. Reading the LOOPSTART tag out of the
	// Vorbis comment header is issue 483; until then every Clip reports none,
	// which is what a Clip with no tag says anyway, so a looping Voice loops
	// the whole Clip and nothing moves when the tag lands.
	region m.Maybe[sound.LoopRegion]
}

func (c *clipData) Duration() float32 { return c.duration }

func (c *clipData) Channels() int { return c.channels }

func (c *clipData) SampleRate() int { return c.sourceRate }

func (c *clipData) LoopRegion() m.Maybe[sound.LoopRegion] { return c.region }

// loopBounds is the span in converted frames a looping Voice repeats between:
// the Clip's Loop Region when it declares one, and the whole Clip otherwise.
// It is computed once, when a Voice starts, so the device thread never touches
// a Maybe or a seconds-to-frames conversion inside a block.
func (c *clipData) loopBounds() (start, end float64) {
	end = float64(c.frames)
	region, ok := c.region.Get()
	if !ok {
		return 0, end
	}
	start = math.Round(float64(region.Start) * float64(c.rate))
	stop := math.Round(float64(region.End) * float64(c.rate))
	if stop > start && stop <= end {
		end = stop
	}
	if start < 0 || start >= end {
		return 0, float64(c.frames)
	}
	return start, end
}

// decode turns encoded Ogg Vorbis into a resident Clip at deviceRate. It runs
// on the goroutine Prepare spawns and never on the tick or the device thread,
// which is the first obligation an Adapter owes: nothing decodes on the thread
// that fills the device buffer, and nothing decodes on the thread the game runs
// on either.
//
// It decodes over a bytes.Reader on the bytes it was handed rather than
// whatever storage opened, because the length is read from the stream's granule
// positions and so needs an io.Seeker, which storage.FileSystem.Open does not
// promise.
func decode(encoded assets.Blob, deviceRate int) (*clipData, error) {
	samples, format, err := oggvorbis.ReadAll(bytes.NewReader(encoded.Data()))
	if err != nil {
		return nil, otosound.ErrNotOggVorbis{Err: err}
	}
	if format.SampleRate <= 0 || format.Channels <= 0 || format.Channels > 2 {
		return nil, otosound.ErrNoStreamFormat{SampleRate: format.SampleRate, Channels: format.Channels}
	}
	frames := len(samples) / format.Channels
	if frames <= 0 {
		return nil, otosound.ErrNoStreamLength{}
	}

	clip := &clipData{
		channels:   format.Channels,
		rate:       deviceRate,
		sourceRate: format.SampleRate,
		duration:   float32(frames) / float32(format.SampleRate),
	}
	if format.SampleRate == deviceRate {
		clip.samples, clip.frames = samples[:frames*format.Channels], frames
		return clip, nil
	}
	clip.samples, clip.frames = convertRate(samples, format.Channels, frames, format.SampleRate, deviceRate)
	if clip.frames <= 0 {
		return nil, otosound.ErrNoStreamLength{}
	}
	return clip, nil
}

// convertRate resamples a decoded Clip to the device rate, once, off both the
// tick and the device thread. Doing it here is what lets the Mixer's Rate be
// pure pitch: a 44.1 kHz Clip played against a 48 kHz device by interpolating
// on the device thread would be resampling a base rate, which obligation 3
// refuses outright.
//
// This is linear, and it is deliberately the cheap one for now. The
// Blackman-windowed sinc the specification asks for - 48 taps, cutoff at the
// lower of the two Nyquists, normalised by the window sum so decimation cannot
// fold everything above the new Nyquist back into the audible band - lands with
// the streamed tier in issue 482, whose read-ahead goroutine resamples with the
// same filter and whose acceptance criterion is the aliasing test this one
// would fail. Upsampling, which is every case the resident tier meets in
// practice, is where linear costs least.
func convertRate(in []float32, channels, frames, from, to int) ([]float32, int) {
	ratio := float64(to) / float64(from)
	outFrames := int(float64(frames) * ratio)
	if outFrames <= 0 {
		return nil, 0
	}
	out := make([]float32, outFrames*channels)
	for i := range outFrames {
		at := float64(i) / ratio
		left := int(at)
		frac := float32(at - float64(left))
		right := left + 1
		if right >= frames {
			right = frames - 1
		}
		for c := range channels {
			a, b := in[left*channels+c], in[right*channels+c]
			out[i*channels+c] = a + (b-a)*frac
		}
	}
	return out, outFrames
}
