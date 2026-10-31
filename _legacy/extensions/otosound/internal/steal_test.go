//go:build !js

package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/sound"
)

// A steal states two things about one slot in one tick: the victim's stop, and
// the start that takes the slot over. The Adapter applies the stops first, so
// the stop lands on the Voice it was meant for rather than on the one that
// replaced it.
//
// Recorded the other way round this is silent rather than wrong-sounding: the
// start would install the new Voice and the stop would immediately begin
// fading it out, and the sound the game asked for would never be heard.
func TestAStolenSlotIsStoppedBeforeTheStartThatReusesIt(t *testing.T) {
	b := newTestBackend(t, &fakeAudio{})

	b.Emit(&sound.Batch{
		Starts: []sound.VoiceStart{{Slot: 0, Params: sound.VoiceParams{Gains: mono(1), Rate: 1}}},
		Stops:  []sound.VoiceSlot{0},
	})

	got := drained(b.ring)
	if len(got) != 2 {
		t.Fatalf("the batch recorded %d operations, want the stop and the start", len(got))
	}
	if got[0].kind != opStop || got[0].slot != 0 {
		t.Fatalf("the first operation is %+v, want the stop slot 0 is owed", got[0])
	}
	if got[1].kind != opStart || got[1].slot != 0 {
		t.Fatalf("the second operation is %+v, want the start that reuses slot 0", got[1])
	}
}
