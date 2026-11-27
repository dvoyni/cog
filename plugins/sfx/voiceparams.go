package sfx

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
