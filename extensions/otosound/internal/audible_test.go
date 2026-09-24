//go:build !js

package internal

import (
	"flag"
	"testing"
	"time"

	"github.com/dvoyni/cog/slots/sound"
)

// audible opts a run in to the one thing in this package a machine cannot
// judge. Every other test renders to a buffer, which is what keeps them
// runnable in CI; this one opens the real device and plays a real Ogg, and what
// it asserts is only that nothing errored. Whether it sounded right is a
// person's answer.
//
//	go test ./extensions/otosound/internal/ -run TestTheFixtureClipIsAudible -audible -v
//
// It is skipped by default for two reasons. CI has no sound card, and a process
// has exactly one oto context to spend - so this test, once it has run, has
// spent it for every test after it in the same binary.
var audible = flag.Bool("audible", false,
	"play the fixture clip through the real audio device, for a person to judge by ear")

func TestTheFixtureClipIsAudible(t *testing.T) {
	if !*audible {
		t.Skip("pass -audible to play the fixture clip through the real audio device")
	}

	b := newBackend(Config{}, &otoAudio{})
	t.Cleanup(b.stop)
	b.Voices(8)

	device := waitReady(t, b)
	t.Logf("device: %s at %d Hz x%d, latency %v",
		device.Name, device.SampleRate, device.Channels, device.Latency)

	b.Prepare(nil, fixture(t))
	completed := waitPrepared(t, b)
	if completed.Err != nil {
		t.Fatalf("preparing the fixture clip: %v", completed.Err)
	}
	id, err := b.Install(completed.Clip)
	if err != nil {
		t.Fatalf("installing the fixture clip: %v", err)
	}

	// One tick's worth of batch: a start at full volume, centred.
	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Gains: mono(0.8), Rate: 1},
	}}})

	// The device pulls on its own clock; all this goroutine does is stay alive
	// for as long as the Clip lasts, and then stop the Voice so the stop is
	// declicked rather than cut by the shutdown.
	play := time.Duration(float64(completed.Clip.Duration()) * float64(time.Second))
	t.Logf("playing %v of %s", play, "testdata/pianoroll.ogg")
	time.Sleep(play)

	b.Emit(&sound.Batch{Stops: []sound.VoiceSlot{0}})
	time.Sleep(100 * time.Millisecond)
}
