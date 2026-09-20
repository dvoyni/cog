// Command audio-spatialize is a THROWAWAY PROTOTYPE. It is not part of cog and
// nothing here is production code.
//
// It exists to answer one question, from
// https://github.com/dvoyni/cog/issues/304: does hand-rolled panning and
// attenuation actually sound right? The whole audio route rests on sound
// computing the spatialization itself, and the 2026-09-12 oto spike proved only
// that the mixer is not starved - it proved nothing about whether the result
// sounds like anything. That is a taste question and it needs ears.
//
// Run it:
//
//	go run ./docs/research/audio-spatialize
//
// Wear headphones. Panning is the thing under judgement and speakers in a room
// will not show you a fold-back.
//
// # What is real here and what is not
//
// The three layers the map settled are all present and separated as the map
// separates them, because a prototype that blurs the seam cannot tell you
// whether the seam is in the right place:
//
//   - spatial.go is sound. It computes W3C's azimuth, distance and cone and
//     produces #376's 2x2 source->output gain matrix. It has no idea a device
//     exists.
//   - mixer.go is otosound. It is handed slots, a matrix, a rate and a paused
//     flag across #302's SPSC ring of preallocated batches, and it declicks over
//     its own blocks. It never sees a position.
//   - main.go is the game. It records into the queue once per tick from a real
//     cog tick loop, so tick-rate parameter updates are genuine rather than
//     simulated.
//
// Not built, because none of it changes how a voice sounds: streaming (#298's
// other tier - every clip here is resident), buses, the opaque Voice handle and
// its generation, stealing, VoiceEndedEvent, and the ECS face.
//
// # The clips
//
// docs/research/audio-spatialize/clips, all Ogg Vorbis from Wikimedia Commons
// under CC / public domain. See clips/ATTRIBUTION.md.
//
// # Controls
//
// They are all drawn on screen. The ones that matter most are B (bypass
// spatialization, the A/B the whole judgement rests on), K (declick on/off),
// I (interpolator) and M (downmix a stereo source before panning).
package main
