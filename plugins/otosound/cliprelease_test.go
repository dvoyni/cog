//go:build !js

package otosound

import (
	"testing"

	"github.com/dvoyni/cog/plugins/sfx"
)

// fakeClip is a clip a test puts in the clip table, as Install keeps one. It
// answers none of a prepared clip's facts: nothing the handoff does reads
// them.
type fakeClip struct {
	sfx.PreparedClip
}

// heldCount answers how many destroyed clips plugin still holds.
func heldCount(plugin *Plugin) int {
	return len(plugin.releases)
}

// The mixer never frees, as a test. A destroy travels inside the flush's
// changes after the stops that precede it, and the flush side lets the clip
// go only once the applied counter has passed the changes that carried it, so
// the mixer can be mid-copy out of the clip for as long as it takes and never
// reads it after it is freed, while the flush never waits.
func TestAReleasedClipIsHeldUntilTheMixerHasPassedTheChangesThatDestroyedIt(t *testing.T) {
	b, plugin := spawnPulledHandoff(t)
	clip := &fakeClip{}
	plugin.clips[1] = clip

	b.Emit(&sfx.FlushChanges{Starts: []sfx.VoiceStart{{Slot: 0, Clip: 1}}})
	applied := drained(plugin.mixer.ring)
	if len(applied) != 1 || applied[0].clip != clip {
		t.Fatalf("the mixer applied %+v, want the start on the installed clip", applied)
	}

	b.Emit(&sfx.FlushChanges{Stops: []sfx.VoiceSlot{0}, Destroys: []sfx.ClipID{1}})
	if held := heldCount(plugin); held != 1 {
		t.Fatalf("%d released clips are held, want the one the mixer has not passed", held)
	}
	if _, installed := plugin.clips[1]; installed {
		t.Fatal("the destroyed clip is still in the clip table")
	}
	b.Emit(&sfx.FlushChanges{})
	if held := heldCount(plugin); held != 1 || plugin.releases[0].clip != clip {
		t.Fatal("the released clip was let go before the mixer applied the changes that destroyed it")
	}

	drained(plugin.mixer.ring)
	b.Emit(&sfx.FlushChanges{})
	if held := heldCount(plugin); held != 0 {
		t.Fatalf("%d released clips are still held after the mixer passed them", held)
	}
}

// A destroy merged into the staging changes of a full ring has no sequence
// until they are published, and is held until they are, and then applied.
func TestAReleaseMergedIntoAFullRingIsHeldUntilItsChangesAreApplied(t *testing.T) {
	b, plugin := spawnPulledHandoff(t)
	plugin.clips[1] = &fakeClip{}
	for clipID := 2; clipID < 2+plugin.config.RingDepth; clipID++ {
		emitStart(b, sfx.ClipID(clipID))
	}

	b.Emit(&sfx.FlushChanges{Destroys: []sfx.ClipID{1}})
	drained(plugin.mixer.ring)
	b.Emit(&sfx.FlushChanges{})
	if held := heldCount(plugin); held != 1 {
		t.Fatalf("%d released clips are held, want the one whose changes were merged and not yet applied", held)
	}

	drained(plugin.mixer.ring)
	b.Emit(&sfx.FlushChanges{})
	if held := heldCount(plugin); held != 0 {
		t.Fatalf("%d released clips are still held after the mixer passed them", held)
	}
}

// Until a mixer has pulled a block there is no device thread to be mid-copy
// out of anything, so a release is let go at once. Otherwise otosound under a
// device that never opened would hold every clip a game ever released, which
// is not behaving as the headless backend does.
func TestAReleasedClipIsFreedAtOnceWhileNoMixerHasEverPulled(t *testing.T) {
	b, plugin := spawnHandoff(t)
	plugin.clips[1] = &fakeClip{}

	b.Emit(&sfx.FlushChanges{Destroys: []sfx.ClipID{1}})

	if held := heldCount(plugin); held != 0 {
		t.Fatalf("%d released clips are held against a counter nothing will ever move", held)
	}
	if _, installed := plugin.clips[1]; installed {
		t.Fatal("the destroyed clip is still in the clip table")
	}
}
