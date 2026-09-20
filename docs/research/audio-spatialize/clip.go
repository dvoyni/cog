package main

// THROWAWAY PROTOTYPE - not production code. See doc.go.
//
// This file is otosound's Prepare: #298's PreparedClip, resident tier. A clip
// is decoded once and resampled once, to the device's rate, so the mixer's
// interpolator only ever has `rate` (pitch) to do - which is what makes item 4
// a question about pitch rather than a question about 96 kHz source material.
//
// Only the resident tier is built here. Streaming is #298's other tier and it
// changes nothing about how a voice sounds, which is all this prototype judges.

import (
	"bytes"
	"fmt"
	"math"
	"os"

	"github.com/jfreymuth/oggvorbis"
)

// PreparedClip is samples at the device rate, interleaved, plus the channel
// count the arithmetic needs to know (mono and stereo pan differently).
type PreparedClip struct {
	Name       string
	Samples    []float32 // interleaved, Channels per frame
	Channels   int
	Frames     int
	SourceRate int // what the file said, kept only for the readout
	Duration   float64
}

// PrepareOgg decodes one Ogg Vorbis file and resamples it to deviceRate.
func PrepareOgg(path, name string, deviceRate int) (*PreparedClip, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	samples, format, err := oggvorbis.ReadAll(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	channels := format.Channels
	frames := len(samples) / channels

	clip := &PreparedClip{
		Name:       name,
		Channels:   channels,
		SourceRate: format.SampleRate,
	}
	if format.SampleRate == deviceRate {
		clip.Samples, clip.Frames = samples, frames
	} else {
		clip.Samples, clip.Frames = resample(samples, channels, frames, format.SampleRate, deviceRate)
	}
	clip.Duration = float64(clip.Frames) / float64(deviceRate)
	return clip, nil
}

// resample converts to the device rate with a windowed-sinc kernel, and
// lowpasses when it is decimating.
//
// This is deliberately the good one, and it is offline. The Stirling engine is
// 44.1 kHz and the 5-cylinder engine is 96 kHz; if the base conversion were
// done with the same linear interpolator the mixer uses for pitch, the 96 kHz
// clip would fold everything above 24 kHz back into the audible band and item 4
// would be answered by an artefact that otosound would never actually ship.
// Prepare is not on the device thread, so it can afford this.
func resample(in []float32, channels, frames, from, to int) ([]float32, int) {
	ratio := float64(to) / float64(from)
	outFrames := int(float64(frames) * ratio)
	out := make([]float32, outFrames*channels)

	// Cutoff is the lower of the two Nyquists, expressed as a fraction of the
	// source rate; taps scale with how much band we are throwing away.
	cutoff := math.Min(0.5, 0.5*ratio) * 0.92
	const halfTaps = 24

	for i := 0; i < outFrames; i++ {
		center := float64(i) / ratio
		first := int(math.Floor(center)) - halfTaps + 1
		for c := 0; c < channels; c++ {
			var acc, wsum float64
			for t := 0; t < 2*halfTaps; t++ {
				j := first + t
				if j < 0 || j >= frames {
					continue
				}
				x := center - float64(j)
				w := sincKernel(x, cutoff, halfTaps)
				acc += w * float64(in[j*channels+c])
				wsum += w
			}
			if wsum != 0 {
				acc /= wsum
			}
			out[i*channels+c] = float32(acc)
		}
	}
	return out, outFrames
}

// sincKernel is a Blackman-windowed sinc at the given cutoff.
func sincKernel(x, cutoff float64, half int) float64 {
	if math.Abs(x) > float64(half) {
		return 0
	}
	s := 2 * cutoff
	if x != 0 {
		s = math.Sin(2*math.Pi*cutoff*x) / (math.Pi * x)
	}
	// Blackman window over [-half, half].
	n := (x + float64(half)) / (2 * float64(half))
	w := 0.42 - 0.5*math.Cos(2*math.Pi*n) + 0.08*math.Cos(4*math.Pi*n)
	return s * w
}
