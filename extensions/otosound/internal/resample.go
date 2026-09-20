//go:build !js

package internal

import "math"

// This is the expensive resampler, and it is expensive on purpose: it runs
// offline, in Prepare, and on a streamed Voice's read-ahead goroutine, and
// never on the thread that fills the device buffer. The device thread's own
// interpolation - linear, two samples, for a Voice's Rate alone - is in
// voice.go, and the whole reason this file exists is so that the device thread
// never has to convert a base rate.
//
// The failing sequence for the cheap filter doing this job: a 96 kHz Clip
// against a 48 kHz device, decimated by picking every other sample. Everything
// the source held above 24 kHz folds back under it - a 30 kHz partial arrives
// at 18 kHz, at full amplitude, in the middle of the band a listener hears
// best. Linear interpolation is barely better than picking, because at an exact
// 2:1 ratio every output sample lands on an input sample and the interpolation
// never runs at all.

const (
	// sincZeros is how many zero crossings of the sinc the window keeps on each
	// side. At unity ratio that is the 48 taps the specification names; when
	// decimating, the kernel is stretched to the lower cutoff and spans more
	// input frames, because a lower cutoff is a wider impulse response and a
	// window that did not grow with it would be a window over the wrong filter.
	sincZeros = 24
	// sincPhases is how finely the fractional delay between two input frames is
	// tabulated. An output frame lands at an arbitrary fraction, so its kernel
	// is blended from the two tabulated phases either side of it; 256 phases
	// blended puts the interpolation error far below the stopband the window
	// buys, and keeps the table small enough that a streamed Clip can hold one.
	sincPhases = 256
	// decimationMargin places the cutoff below the output's Nyquist rather than
	// on it. A windowed sinc has a transition band of real width - about 5.5/N
	// of the input rate for a Blackman window over N taps - and a cutoff placed
	// exactly on Nyquist puts half of that transition above it, where whatever
	// survives folds. The cost is the top tenth of the new band, 21.6 kHz
	// upwards against a 48 kHz device, which is above what anyone hears.
	decimationMargin = 0.9
)

// sincFilter is a Blackman-windowed sinc tabulated at sincPhases fractional
// delays, built once for a (source rate, device rate) pair and then read-only.
// A streamed Clip keeps its filter and every Voice on that Clip shares it,
// which is what keeps a filter out of the per-Voice cost; a resident Clip uses
// one once in Prepare and drops it with the encoded bytes.
type sincFilter struct {
	// ratio is output frames per input frame, and step is its reciprocal: the
	// input position one output frame advances by.
	ratio, step float64
	// span is the kernel's half width in input frames and taps is 2*span, the
	// number of input frames one output frame is made from.
	span, taps int
	// table is (sincPhases+1) rows of taps, normalised so that each row sums to
	// one. The normalisation is what makes the filter unity gain at DC whatever
	// the phase, and is why decimation cannot come out quieter at one fraction
	// than at another; the extra row is the fraction 1.0, so blending the last
	// tabulated phase never indexes off the end.
	table []float32
}

// newSincFilter builds the filter that converts from to to.
func newSincFilter(from, to int) *sincFilter {
	ratio := float64(to) / float64(from)
	// The cutoff is in cycles per input frame. Upsampling needs no lowpass
	// beyond the source's own Nyquist - there is nothing above it to fold -
	// so the cutoff is 0.5 and the filter is a pure interpolator. Decimating
	// needs the output's Nyquist, with the margin the transition band asks for.
	cutoff := 0.5
	if ratio < 1 {
		cutoff = 0.5 * ratio * decimationMargin
	}
	span := int(math.Ceil(sincZeros / (2 * cutoff)))
	taps := 2 * span
	f := &sincFilter{
		ratio: ratio,
		step:  1 / ratio,
		span:  span,
		taps:  taps,
		table: make([]float32, (sincPhases+1)*taps),
	}
	for phase := 0; phase <= sincPhases; phase++ {
		frac := float64(phase) / sincPhases
		row := f.table[phase*taps : (phase+1)*taps]
		var sum float64
		for k := range taps {
			// Tap k reads the input frame span-1-k before the output position,
			// so u is the distance from the output position to that frame.
			weight := windowedSinc(frac+float64(span-1-k), cutoff, float64(span))
			row[k] = float32(weight)
			sum += weight
		}
		if sum != 0 {
			scale := float32(1 / sum)
			for k := range row {
				row[k] *= scale
			}
		}
	}
	return f
}

// windowedSinc is one tap: the ideal lowpass at cutoff, multiplied by the
// Blackman window over the kernel's span.
//
// Blackman rather than a rectangular truncation or a Hann: truncating the sinc
// leaves -21 dB sidelobes, which is an alias folded 21 dB down and plainly
// audible, and Blackman's -74 dB stopband is what makes the assertion this file
// is tested against - a decimated 30 kHz tone leaving nothing measurable at
// 18 kHz - true rather than nearly true.
func windowedSinc(u, cutoff, span float64) float64 {
	if u <= -span || u >= span {
		return 0
	}
	x := (u + span) / (2 * span)
	window := 0.42 - 0.5*math.Cos(2*math.Pi*x) + 0.08*math.Cos(4*math.Pi*x)
	return 2 * cutoff * sinc(2*cutoff*u) * window
}

// sinc is sin(pi*x)/(pi*x), and 1 at zero.
func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	return math.Sin(math.Pi*x) / (math.Pi * x)
}

// resampler is one conversion in progress: input frames are fed in, output
// frames are drained out, and the filter window's history is kept across the
// two so that a conversion done in chunks is sample-for-sample what the same
// conversion done whole would have produced.
//
// That property is the whole reason it is incremental rather than two
// functions. Prepare converts a resident Clip in one call; a streamed Voice's
// read-ahead goroutine converts the same Clip a few thousand frames at a time,
// including across a loop wrap, and the two must not be two different filters.
type resampler struct {
	filter   *sincFilter
	channels int
	// history is the input kept, interleaved, starting at frame base. It holds
	// everything the next output frame's window still needs and is trimmed from
	// the front as the window moves on, so a five-minute stream is never more
	// than a few thousand frames of it.
	history []float32
	base    int64
	// in is how many input frames have been fed in total, and out is how many
	// output frames have been produced. Both are absolute, which is what lets
	// the window reach across a chunk boundary.
	in, out int64
	// blend is the kernel for the fraction this output frame landed on, mixed
	// from the two tabulated phases either side of it. It is made once so that
	// draining allocates nothing.
	blend []float32
}

func newResampler(filter *sincFilter, channels int) *resampler {
	return &resampler{
		filter:   filter,
		channels: channels,
		blend:    make([]float32, filter.taps),
	}
}

// feed appends input frames, interleaved.
func (r *resampler) feed(in []float32) {
	r.history = append(r.history, in...)
	r.in += int64(len(in) / r.channels)
}

// outFrames is how many output frames the input fed so far finally becomes. It
// is the floor of the input count scaled by the ratio, which is the same
// arithmetic the resident path sizes its buffer with.
func (r *resampler) outFrames() int64 { return int64(float64(r.in) * r.filter.ratio) }

// drain converts as much as it can into dst and reports how many output frames
// it wrote. Without eof it stops at the last output frame whose whole window is
// inside the input fed so far, so no frame is ever made from input that has not
// arrived; with eof it runs to the end, treating the input past the last frame
// as silence, which is the same edge the offline conversion has always had.
func (r *resampler) drain(dst []float32, eof bool) int {
	f := r.filter
	capacity := len(dst) / r.channels
	wrote := 0
	for wrote < capacity {
		at := float64(r.out) * f.step
		first := int64(math.Floor(at))
		if eof {
			if r.out >= r.outFrames() {
				break
			}
		} else if first+int64(f.span) >= r.in {
			break
		}

		// The window covers input frames [first-span+1, first+span], and the
		// taps outside the input are silence: before the head at the start of a
		// Clip, and past the end at eof.
		start := first - int64(f.span) + 1
		from, to := 0, f.taps
		if start < 0 {
			from = int(-start)
		}
		if start+int64(f.taps) > r.in {
			to = int(r.in - start)
		}
		r.mix(at - float64(first))
		for c := range r.channels {
			var acc float32
			for k := from; k < to; k++ {
				acc += r.blend[k] * r.history[int(start+int64(k)-r.base)*r.channels+c]
			}
			dst[wrote*r.channels+c] = acc
		}
		r.out++
		wrote++
	}
	r.trim()
	return wrote
}

// mix blends the two tabulated phases either side of frac into one kernel.
func (r *resampler) mix(frac float64) {
	f := r.filter
	exact := frac * sincPhases
	phase := int(exact)
	if phase >= sincPhases {
		phase = sincPhases - 1
	}
	weight := float32(exact - float64(phase))
	low := f.table[phase*f.taps : (phase+1)*f.taps]
	high := f.table[(phase+1)*f.taps : (phase+2)*f.taps]
	for k := range r.blend {
		r.blend[k] = low[k] + (high[k]-low[k])*weight
	}
}

// trim drops the input the window has moved past, shifting what is left to the
// front so that the next feed reuses the same backing array. A streamed Voice
// therefore allocates for its history once and then never again, which matters
// because it is doing this every few milliseconds for as long as it plays.
func (r *resampler) trim() {
	keep := int64(math.Floor(float64(r.out)*r.filter.step)) - int64(r.filter.span) + 1
	if keep <= r.base {
		return
	}
	drop := int(keep-r.base) * r.channels
	if drop >= len(r.history) {
		r.history, r.base = r.history[:0], r.base+int64(len(r.history)/r.channels)
		return
	}
	r.history = append(r.history[:0], r.history[drop:]...)
	r.base = keep
}

// convertRate resamples a whole decoded Clip to the device rate, once, off both
// the tick and the device thread. Doing it here is what lets the Mixer's Rate
// be pure pitch: a 44.1 kHz Clip played against a 48 kHz device by
// interpolating on the device thread would be resampling a base rate, which
// obligation 3 refuses outright.
//
// It is the same filter, through the same resampler, that a streamed Voice's
// read-ahead goroutine uses - one conversion in one call rather than one every
// few milliseconds. Two resamplers would be two answers to "what does this Clip
// sound like", and which one a game got would depend on how long the Clip was.
func convertRate(in []float32, channels, frames, from, to int) ([]float32, int) {
	filter := newSincFilter(from, to)
	outFrames := int(float64(frames) * filter.ratio)
	if outFrames <= 0 {
		return nil, 0
	}
	out := make([]float32, outFrames*channels)
	r := newResampler(filter, channels)
	r.feed(in[:frames*channels])
	r.drain(out, true)
	return out, outFrames
}
