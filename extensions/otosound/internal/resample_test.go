//go:build !js

package internal

import (
	"math"
	"testing"
)

// The resampler's tests synthesise their fixtures rather than reading one, for
// two reasons. A 96 kHz clip is not a thing the repo has and four of the five
// clips in the #304 prototype kit are CC BY-SA, so shipping one would put a
// share-alike obligation in the engine's tree; and a generated tone is a far
// better aliasing fixture than music, because what is measured is the absence
// of energy at one frequency and a synthetic source has nothing else there.

// tone builds one interleaved mono buffer of summed sine waves at rate.
func tone(rate, frames int, amplitude float64, hertz ...float64) []float32 {
	out := make([]float32, frames)
	for i := range out {
		var sum float64
		for _, f := range hertz {
			sum += amplitude * math.Sin(2*math.Pi*f*float64(i)/float64(rate))
		}
		out[i] = float32(sum)
	}
	return out
}

// amplitudeAt measures the amplitude of one frequency in a mono buffer, with a
// Hann window so that a frequency the buffer holds nothing at reads as what is
// there rather than as what leaked in from the frequencies it does hold.
func amplitudeAt(samples []float32, rate int, hertz float64) float64 {
	var real, imaginary, window float64
	n := float64(len(samples))
	for i, sample := range samples {
		w := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/n)
		angle := 2 * math.Pi * hertz * float64(i) / float64(rate)
		real += w * float64(sample) * math.Cos(angle)
		imaginary -= w * float64(sample) * math.Sin(angle)
		window += w
	}
	return 2 * math.Hypot(real, imaginary) / window
}

// decibels expresses one amplitude against another, which is how a stopband is
// read: -60 dB is a thousandth of the tone that folded into it.
func decibels(got, against float64) float64 {
	if got <= 0 {
		return math.Inf(-1)
	}
	return 20 * math.Log10(got/against)
}

// This is the obligation the placeholder could not keep. A 96 kHz source
// against a 48 kHz device holds a whole octave above the device's Nyquist, and
// a conversion that does not lowpass folds every bit of it back into the band a
// listener hears best: a 30 kHz partial arrives at 18 kHz at full amplitude.
//
// Linear interpolation fails this outright rather than narrowly - at an exact
// 2:1 ratio every output frame lands on an input frame, so the interpolation
// never runs and the conversion is plain decimation.
func TestTheOfflineResamplerDecimatesWithoutFoldingTheBandAboveNyquist(t *testing.T) {
	const from, to, frames = 96000, 48000, 24000
	const passband, stopband, alias = 1000.0, 30000.0, 18000.0
	in := tone(from, frames, 0.5, passband, stopband)

	out, outFrames := convertRate(in, 1, frames, from, to)
	if outFrames != frames*to/from {
		t.Fatalf("decimating %d frames produced %d, want %d", frames, outFrames, frames*to/from)
	}

	// The filter's own edges are the head and the tail, where the window reads
	// silence past the ends of the Clip. What is measured is the middle.
	measured := out[1000 : outFrames-1000]
	kept := amplitudeAt(measured, to, passband)
	folded := amplitudeAt(measured, to, alias)
	t.Logf("passband %.0f Hz kept at %.4f (%.2f dB), alias of %.0f Hz at %.0f Hz is %.2f dB",
		passband, kept, decibels(kept, 0.5), stopband, alias, decibels(folded, 0.5))

	if decibels(kept, 0.5) < -0.5 {
		t.Fatalf("the %.0f Hz tone came through at %.2f dB, and the passband is not what the filter is for",
			passband, decibels(kept, 0.5))
	}
	if level := decibels(folded, 0.5); level > -60 {
		t.Fatalf("the %.0f Hz tone folded to %.0f Hz at %.2f dB, and the band above the new Nyquist must be empty",
			stopband, alias, level)
	}
}

// The case the resident tier actually meets: the fixture Clip is 44.1 kHz and
// the default device is 48 kHz, so every test run converts upwards. Nothing
// folds when upsampling - there is nothing above the source's Nyquist to fold -
// so what is asserted is that the passband survives at its own amplitude and
// that the filter has not quietly halved everything.
func TestTheOfflineResamplerUpsamplesWithoutChangingTheBandItCarries(t *testing.T) {
	const from, to, frames = 44100, 48000, 22050
	const passband = 1000.0
	in := tone(from, frames, 0.5, passband)

	out, outFrames := convertRate(in, 1, frames, from, to)

	kept := amplitudeAt(out[1000:outFrames-1000], to, passband)
	if level := decibels(kept, 0.5); level < -0.5 || level > 0.5 {
		t.Fatalf("a %.0f Hz tone upsampled to %.4f, which is %.2f dB of what it was", passband, kept, level)
	}
}

// A streamed Voice converts the same Clip a few thousand frames at a time, and
// a Clip must not sound different because it was long enough to stream. The
// resampler is therefore one incremental filter that the offline path drives in
// a single call, and this is that claim: chunked and whole are the same frames,
// not merely similar ones.
func TestAChunkedConversionIsSampleForSampleTheWholeOne(t *testing.T) {
	const from, to, frames = 44100, 48000, 8000
	in := tone(from, frames, 0.4, 440, 3000)

	whole, wholeFrames := convertRate(in, 1, frames, from, to)

	r := newResampler(newSincFilter(from, to), 1)
	chunked := make([]float32, 0, wholeFrames)
	buf := make([]float32, 512)
	for at := 0; at < frames; at += 333 {
		end := min(at+333, frames)
		r.feed(in[at:end])
		for {
			n := r.drain(buf, false)
			chunked = append(chunked, buf[:n]...)
			if n < len(buf) {
				break
			}
		}
	}
	for {
		n := r.drain(buf, true)
		chunked = append(chunked, buf[:n]...)
		if n == 0 {
			break
		}
	}

	if len(chunked) != wholeFrames {
		t.Fatalf("the chunked conversion produced %d frames and the whole one %d", len(chunked), wholeFrames)
	}
	for i := range whole {
		if whole[i] != chunked[i] {
			t.Fatalf("frame %d is %v chunked and %v whole, and a Clip must not sound different for being streamed",
				i, chunked[i], whole[i])
		}
	}
}
