//go:build !js

package otosound

import (
	"encoding/binary"
	"math"
)

// The format the mixer writes and the device plays: stereo, since panning is,
// of little-endian float32 samples.
const (
	outputChannels = 2
	bytesPerFrame  = outputChannels * 4
)

// silentGains is the gain matrix a stopping or paused voice ramps to.
var silentGains [2][2]float32

// mixer is the io.Reader the device's player pulls, one read a block, on the
// device's thread. At each block start it drains the handoff ring, applying
// every flush's published changes in order, so a flush's operations become
// audible together; then it mixes every sounding voice through its gain
// matrix, ramping from the matrix the last block ended on to this block's
// target, and sums them. It never blocks, since reads are sequential across a
// context's players, and a blocking read would hold up every other kernel's.
//
// It may touch its own voice table, the handoff ring through its atomics, the
// prepared clip data it was given, and its read-ahead rings through their
// atomics. It may not allocate, take any lock, free anything, call into sfx
// or the kernel, or decode: everything it needs is made once, at Voices, in
// Prepare, or at a voice's start on the flush side. It never decides anything either: it
// holds no notion of a bus, a priority, a position, the cap or a handle, and a
// stolen voice reaches it as an ordinary stop.
type mixer struct {
	// ring is the handoff ring, made at Voices: the flush side publishes to
	// it and the mixer drains it, the mixer through its atomics alone.
	ring *handoffRing
	// voices is the voice table, one voice a slot, indexed by the seam's
	// VoiceSlot. sfx stops a slot before it reuses one, so a voice carries no
	// generation.
	voices []voice
	// soundingSlots are the slots that sound this block, gathered at its start
	// so the loop over frames carries no decisions. Its capacity is the slot
	// count, and it never grows.
	soundingSlots []int
	// outputSampleRate is the rate the mixer produces and a resident clip's
	// samples are held at, which a start's offset and loop region, in
	// seconds, become frames at: the rate in force once Voices has taken the
	// device, and Config's until then, or when the take failed. Voices sets
	// it before the player can pull a block.
	outputSampleRate int
}

// makeMixer returns a mixer draining ring, with a voice table of voiceCount
// silent slots, producing outputSampleRate.
func makeMixer(ring *handoffRing, voiceCount, outputSampleRate int) mixer {
	return mixer{
		ring:             ring,
		voices:           make([]voice, voiceCount),
		soundingSlots:    make([]int, 0, voiceCount),
		outputSampleRate: outputSampleRate,
	}
}

// Read fills block with as many whole frames as it holds, one block of the
// mix. It first applies every flush's changes published since the last block,
// each whole, so a flush's changes published mid-block are heard from the next
// block start, at most one block late. Each output frame is clamped to [-1, 1],
// since a mix of many voices can exceed unity and a wrapped sample is far
// louder than a clipped one.
func (m *mixer) Read(block []byte) (int, error) {
	frameCount := len(block) / bytesPerFrame
	if frameCount == 0 {
		// A ramp over no frames would be a step: nothing is applied until a
		// block can carry it.
		return 0, nil
	}
	m.ring.drain(m.apply)
	m.gatherSoundingSlots()
	// The ramp runs to (frame + 1) / frameCount, so the block ends exactly on
	// its target, not nearly on it, and a stop reaches real silence.
	rampStep := 1 / float32(frameCount)
	for frame := range frameCount {
		rampFraction := float32(frame+1) * rampStep
		var left, right float32
		for _, slot := range m.soundingSlots {
			slotVoice := &m.voices[slot]
			if slotVoice.clip == nil {
				// A one-shot that ran out earlier in this block.
				continue
			}
			gains := slotVoice.rampGains(rampFraction)
			leftSample, rightSample := slotVoice.sampleFrame()
			if slotVoice.clip.channels == 1 {
				left += leftSample * gains[0][0]
				right += leftSample * gains[0][1]
			} else {
				left += leftSample*gains[0][0] + rightSample*gains[1][0]
				right += leftSample*gains[0][1] + rightSample*gains[1][1]
			}
			if !slotVoice.paused {
				slotVoice.advance()
			}
		}
		at := frame * bytesPerFrame
		binary.LittleEndian.PutUint32(block[at:], math.Float32bits(max(-1, min(1, left))))
		binary.LittleEndian.PutUint32(block[at+4:], math.Float32bits(max(-1, min(1, right))))
	}
	// Every ramp lands exactly on its target, and each stopping voice is
	// freed, now that it has reached silence.
	for slot := range m.voices {
		m.voices[slot].endBlock()
	}
	return frameCount * bytesPerFrame, nil
}

// apply applies one drained operation to the voice table. A destroy does
// nothing: the stops ordered before it already dropped every reference the
// table held, and the flush side lets the clip go once the applied counter
// has passed it. A slot outside the table, which sfx never sends, is ignored
// rather than indexed.
func (m *mixer) apply(applied *operation) {
	if applied.slot < 0 || int(applied.slot) >= len(m.voices) {
		return
	}
	slotVoice := &m.voices[applied.slot]
	switch applied.kind {
	case operationKindStart:
		slotVoice.start(applied, m.outputSampleRate)
	case operationKindUpdate:
		slotVoice.update(applied.params)
	case operationKindStop:
		slotVoice.stop()
	}
}

// gatherSoundingSlots collects the slots that sound this block and fixes each
// one's target: silence for a stopping or paused voice, and sfx's last target
// otherwise. A paused voice already ramped to silence is left out entirely,
// frozen and inaudible.
func (m *mixer) gatherSoundingSlots() {
	m.soundingSlots = m.soundingSlots[:0]
	for slot := range m.voices {
		slotVoice := &m.voices[slot]
		if slotVoice.clip == nil {
			continue
		}
		slotVoice.beginBlock()
		slotVoice.endGains = slotVoice.targetGains
		if slotVoice.stopping || slotVoice.paused {
			slotVoice.endGains = silentGains
		}
		if slotVoice.paused && slotVoice.startGains == slotVoice.endGains {
			continue
		}
		m.soundingSlots = append(m.soundingSlots, slot)
	}
}
