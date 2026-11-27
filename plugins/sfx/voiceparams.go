package sfx

import "math"

// VoiceParams is everything about how a voice sounds, as a backend sees it:
// sixteen bytes, and no equation below the seam.
type VoiceParams struct {
	// Gains maps source channels to output channels: Gains[source][output].
	// Row 0 is a mono clip's only row. sfx computes it from the voice's volume,
	// its bus, its falloff and cone and the W3C panning equations; the backend
	// multiplies and nothing else.
	Gains [2][2]float32
	// Rate is the playback rate, 1 being the clip's own.
	Rate float32
	// Paused stops the voice advancing without ending it. It is a field rather
	// than a verb because zeroing a gain does not suspend: a resident buffer
	// started from its head keeps advancing whatever its gain is.
	Paused bool
}

// makeCentredVoiceParams answers the params of a voice heard centred at
// audibility, at its clip's own rate: W3C's equal-power panning at azimuth 0,
// which leaves a stereo clip on its own diagonal and puts a mono clip at
// cos(π/4) in each ear rather than unity in both, the matrix then scaled by
// audibility.
func makeCentredVoiceParams(channels int, audibility float32) VoiceParams {
	if channels >= 2 {
		return VoiceParams{Gains: [2][2]float32{{audibility, 0}, {0, audibility}}, Rate: 1}
	}
	side := math.Sqrt2 / 2 * audibility
	return VoiceParams{Gains: [2][2]float32{{side, side}}, Rate: 1}
}
