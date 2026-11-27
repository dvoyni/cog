//go:build !js

package otosound

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// The windowed sinc's shape, as the design's Resampling sets it, and the
// chunk a whole clip is converted in.
const (
	// sincZeroCrossings is how many zero crossings of the sinc the window
	// keeps on each side: 48 taps at unity. When decimating, the filter is
	// stretched to the lower cutoff and spans more input frames, since a
	// lower cutoff is a wider impulse response, and a window that did not
	// grow with it would be a window over the wrong filter.
	sincZeroCrossings = 24
	// sincPhaseCount is how finely the fractional delay between two input
	// frames is tabulated. An output frame lands at any fraction, so its taps
	// are blended from the two tabulated phases either side of it; 256
	// phases blended put the interpolation error far below the stopband the
	// window buys.
	sincPhaseCount = 256
	// decimationMargin places a decimating cutoff below the new Nyquist
	// rather than on it. A windowed sinc's transition band has real width,
	// about 5.5/N of the input rate for a Blackman window over N taps, and a
	// cutoff on Nyquist puts half of it above, where whatever survives
	// folds. The margin costs the top tenth of the new band, 21.6 kHz
	// upwards against a 48 kHz device, above what anyone hears.
	decimationMargin = 0.9
	// conversionChunkFrames is how many input frames convertRate feeds the
	// resampler at a time, as many as a read-ahead decodes at a time, so a
	// whole clip is never copied into the resampler's history at once. It
	// changes no output frame, since chunked and whole are the same frames.
	conversionChunkFrames = 4096
)

// convertRate converts a whole clip's samples, interleaved, channels to a
// frame, from sourceRate to outputRate, once, and answers the converted
// samples: the input's frames scaled by the ratio and rounded up, so any
// frame in gives at least one out. Samples already at outputRate are
// answered as they are. It is the expensive resampler, and expensive on
// purpose: it runs in Prepare, off the flush and the device thread, so that
// the device thread only ever interpolates for a voice's rate and never
// converts a base rate.
//
// A cheap filter fails this outright: a 96 kHz clip against a 48 kHz device,
// decimated by picking every other sample, folds everything above 24 kHz
// back under it, and a 30 kHz partial arrives at 18 kHz at full amplitude.
// Linear interpolation is no better, since at an exact 2:1 ratio every
// output frame lands on an input frame and the interpolation never runs.
//
// It drives the same incremental resampler a read-ahead drives, a chunk at a
// time, so a clip sounds the same whichever tier holds it.
func convertRate(samples []float32, channels, sourceRate, outputRate int) []float32 {
	if sourceRate == outputRate {
		return samples
	}
	filter := newSincFilter(sourceRate, outputRate)
	converter := newResampler(filter, channels)
	converted := make([]float32, filter.outputFrameCount(int64(len(samples)/channels))*int64(channels))
	chunkSamples := conversionChunkFrames * channels
	writtenFrames := 0
	for chunkStart := 0; chunkStart < len(samples); chunkStart += chunkSamples {
		converter.feed(samples[chunkStart:min(chunkStart+chunkSamples, len(samples))])
		writtenFrames += converter.drain(converted[writtenFrames*channels:], false)
	}
	converter.drain(converted[writtenFrames*channels:], true)
	return converted
}

// resampler is one conversion in progress: input frames are fed in, output
// frames drained out, and the window's history kept between the two, so a
// conversion done in chunks is sample for sample the same conversion done
// whole. Prepare drives it a chunk at a time, as a streamed voice's
// read-ahead will, across a loop's wrap too, and the two must not be two
// different filters.
type resampler struct {
	filter   *sincFilter
	channels int
	// history is the input kept, interleaved, from frame historyStart to the
	// last frame fed: everything the next output frame's window still needs.
	// It is trimmed from the front as the window moves on, so a long stream
	// is never more than a few thousand frames of it.
	history      []float32
	historyStart int64
	// drainedFrames counts the output frames drained out from the start of
	// the conversion, which, against the input frames fed, is what lets the
	// window reach across a chunk.
	drainedFrames int64
	// blendedTaps are the taps for the fraction the current output frame
	// lands on, blended from the two tabulated phases either side of it.
	// They are made once, so draining allocates nothing.
	blendedTaps []float32
}

// newResampler starts a conversion through filter of channels-channel
// frames.
func newResampler(filter *sincFilter, channels int) *resampler {
	return &resampler{
		filter:      filter,
		channels:    channels,
		blendedTaps: make([]float32, filter.tapCount),
	}
}

// feed appends input frames, interleaved.
func (r *resampler) feed(input []float32) { r.history = append(r.history, input...) }

// drain converts as much as it can into output, interleaved, and answers how
// many frames it wrote. Until the input has ended it stops at the last
// output frame whose whole window is inside the input fed so far, so no
// frame is made from input that has not arrived; once inputEnded, it runs to
// the filter's outputFrameCount, reading the input past the last frame as
// silence.
func (r *resampler) drain(output []float32, inputEnded bool) int {
	filter := r.filter
	fedFrames := r.historyStart + int64(len(r.history)/r.channels)
	endFrame := filter.outputFrameCount(fedFrames)
	outputCapacity := len(output) / r.channels
	writtenFrames := 0
	for writtenFrames < outputCapacity {
		// The window covers tapCount input frames from windowStart, and the
		// taps outside the input read silence: before the clip's head, and
		// past its end once the input has ended.
		windowStart, fraction := filter.windowStart(r.drainedFrames)
		windowEnd := windowStart + int64(filter.tapCount)
		if inputEnded && r.drainedFrames >= endFrame || !inputEnded && windowEnd > fedFrames {
			break
		}
		firstTap := 0
		if windowStart < 0 {
			firstTap = int(-windowStart)
		}
		endTap := filter.tapCount
		if windowEnd > fedFrames {
			endTap = int(fedFrames - windowStart)
		}
		r.blendTaps(fraction)
		for channel := range r.channels {
			var filtered float32
			for tap := firstTap; tap < endTap; tap++ {
				filtered += r.blendedTaps[tap] * r.history[int(windowStart+int64(tap)-r.historyStart)*r.channels+channel]
			}
			output[writtenFrames*r.channels+channel] = filtered
		}
		r.drainedFrames++
		writtenFrames++
	}
	r.trimHistory()
	return writtenFrames
}

// blendTaps blends the two tabulated phases either side of fraction into
// blendedTaps.
func (r *resampler) blendTaps(fraction float64) {
	filter := r.filter
	phasePosition := fraction * sincPhaseCount
	phase := min(int(phasePosition), sincPhaseCount-1)
	weight := float32(phasePosition - float64(phase))
	lowerRow := filter.tapTable[phase*filter.tapCount : (phase+1)*filter.tapCount]
	upperRow := filter.tapTable[(phase+1)*filter.tapCount : (phase+2)*filter.tapCount]
	for tap := range r.blendedTaps {
		r.blendedTaps[tap] = m.Lerp(lowerRow[tap], upperRow[tap], weight)
	}
}

// trimHistory drops the input the next output frame's window has moved
// past, shifting what is left to the front, so the next feed reuses the same
// backing array: a streamed voice allocates for its history once. The
// window always still reaches the last frame fed, since it spans far more
// input frames than one output frame moves.
func (r *resampler) trimHistory() {
	keptStart, _ := r.filter.windowStart(r.drainedFrames)
	if keptStart <= r.historyStart {
		return
	}
	r.history = append(r.history[:0], r.history[int(keptStart-r.historyStart)*r.channels:]...)
	r.historyStart = keptStart
}

// sincFilter is a Blackman-windowed sinc, lowpassed when decimating,
// tabulated at sincPhaseCount fractional delays: built once for a pair of
// rates, and read-only after, so the voices of one streamed clip can share
// it.
type sincFilter struct {
	// sourceRate and outputRate are the rates it converts between.
	sourceRate int
	outputRate int
	// inputStep is how far, in input frames, one output frame moves.
	inputStep float64
	// halfWidth is the filter's half width in input frames.
	halfWidth int
	// tapCount is twice halfWidth: how many input frames one output frame is
	// made from.
	tapCount int
	// tapTable is sincPhaseCount+1 rows of tapCount taps, each row
	// normalized to sum to one, so the filter is unity gain at DC whatever
	// the phase. The extra row is the fraction 1, so blending the last
	// tabulated phase never reads past the end.
	tapTable []float32
}

// newSincFilter builds the filter that converts sourceRate to outputRate.
// Upsampling keeps the cutoff at the source's own Nyquist, half a cycle per
// input frame, since there is nothing above it to fold; decimating lowers it
// to the new Nyquist, less decimationMargin.
func newSincFilter(sourceRate, outputRate int) *sincFilter {
	ratio := float64(outputRate) / float64(sourceRate)
	cutoff := 0.5
	if ratio < 1 {
		cutoff = 0.5 * ratio * decimationMargin
	}
	halfWidth := int(math.Ceil(sincZeroCrossings / (2 * cutoff)))
	filter := &sincFilter{
		sourceRate: sourceRate,
		outputRate: outputRate,
		inputStep:  1 / ratio,
		halfWidth:  halfWidth,
		tapCount:   2 * halfWidth,
		tapTable:   make([]float32, (sincPhaseCount+1)*2*halfWidth),
	}
	for phase := 0; phase <= sincPhaseCount; phase++ {
		fraction := float64(phase) / sincPhaseCount
		phaseRow := filter.tapTable[phase*filter.tapCount : (phase+1)*filter.tapCount]
		var weightSum float64
		for tap := range phaseRow {
			// Tap k reads the input frame halfWidth-1-k before the output
			// frame's position, which is how far the weight is taken from.
			weight := windowedSinc(fraction+float64(halfWidth-1-tap), cutoff, float64(halfWidth))
			phaseRow[tap] = float32(weight)
			weightSum += weight
		}
		scale := float32(1 / weightSum)
		for tap := range phaseRow {
			phaseRow[tap] *= scale
		}
	}
	return filter
}

// outputFrameCount is how many output frames inputFrames input frames
// become: scaled by the ratio and rounded up, so the last output frame is
// the last whose position lies inside the input.
func (f *sincFilter) outputFrameCount(inputFrames int64) int64 {
	return (inputFrames*int64(f.outputRate) + int64(f.sourceRate) - 1) / int64(f.sourceRate)
}

// windowStart answers the first of the tapCount input frames outputFrame is
// made from, and the fraction of an input frame its position lies past the
// one halfWidth-1 frames after that.
func (f *sincFilter) windowStart(outputFrame int64) (int64, float64) {
	position := float64(outputFrame) * f.inputStep
	frameBefore := math.Floor(position)
	return int64(frameBefore) - int64(f.halfWidth) + 1, position - frameBefore
}

// windowedSinc is one tap's weight, distance input frames from the output
// frame's position: the ideal lowpass at cutoff, in cycles per input frame,
// under a Blackman window over halfWidth frames either side. Blackman rather
// than a truncated sinc or a Hann: truncating leaves sidelobes at -21 dB, an
// alias folded 21 dB down and plainly audible, and Blackman's -74 dB
// stopband is what leaves the band above a decimated Nyquist empty.
func windowedSinc(distance, cutoff, halfWidth float64) float64 {
	if distance <= -halfWidth || distance >= halfWidth {
		return 0
	}
	windowPosition := (distance + halfWidth) / (2 * halfWidth)
	window := 0.42 - 0.5*math.Cos(2*math.Pi*windowPosition) + 0.08*math.Cos(4*math.Pi*windowPosition)
	ideal := 2 * cutoff
	if sincArgument := 2 * cutoff * distance; sincArgument != 0 {
		ideal *= math.Sin(math.Pi*sincArgument) / (math.Pi * sincArgument)
	}
	return ideal * window
}
