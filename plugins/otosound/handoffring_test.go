//go:build !js

package otosound

import (
	"errors"
	"testing"
	"time"

	"github.com/dvoyni/cog/plugins/sfx"
)

// spawnHandoff answers the backend of a kernel composing sfx and otosound
// over a fake device whose open is refused, so no player ever pulls the
// ring, and its plugin, so a test emits and drains by hand. Nothing steps
// the kernel, so no flush emits beside the test.
func spawnHandoff(t *testing.T) (backend, *Plugin) {
	t.Helper()
	_, plugin := spawnOverFake(t, &fakeDevice{openErr: errors.New("no device in this test")})
	return backend{plugin: plugin}, plugin
}

// spawnPulledHandoff is spawnHandoff with the ring attached, as once a
// player plays, so a destroyed clip is held against the applied counter and
// a full ring merges into the staging changes rather than discards them.
func spawnPulledHandoff(t *testing.T) (backend, *Plugin) {
	t.Helper()
	b, plugin := spawnHandoff(t)
	plugin.mixer.ring.pulled.Store(true)
	return b, plugin
}

// drained drains ring as the mixer does, and answers the operations it
// applied, in order.
func drained(ring *handoffRing) []operation {
	var applied []operation
	ring.drain(func(operation *operation) { applied = append(applied, *operation) })
	return applied
}

// emitStart emits one flush's changes starting a voice on clipID, in a slot
// of eight it picks from clipID.
func emitStart(b backend, clipID sfx.ClipID) {
	b.Emit(&sfx.FlushChanges{Starts: []sfx.VoiceStart{{Slot: sfx.VoiceSlot(clipID % 8), Clip: clipID}}})
}

// wantStartsInOrder fails the test unless applied is exactly total starts,
// on clips 1 to total in that order.
func wantStartsInOrder(t *testing.T, applied []operation, total int) {
	t.Helper()
	if len(applied) != total {
		t.Fatalf("%d operations were applied, want %d: the handoff lost some", len(applied), total)
	}
	for index, operation := range applied {
		if want := sfx.ClipID(index + 1); operation.kind != operationKindStart || operation.clipID != want {
			t.Fatalf("operation %d is %+v, want the start on clip %d: the handoff reordered them", index, operation, want)
		}
	}
}

// wantStopThenStart fails the test unless applied is slot 0's stop and then
// the start on clipID that reuses slot 0.
func wantStopThenStart(t *testing.T, applied []operation, clipID sfx.ClipID) {
	t.Helper()
	if len(applied) != 2 {
		t.Fatalf("%d operations were applied, want the stop and the start", len(applied))
	}
	if applied[0].kind != operationKindStop || applied[0].slot != 0 {
		t.Fatalf("the first operation is %+v, want slot 0's stop", applied[0])
	}
	if applied[1].kind != operationKindStart || applied[1].slot != 0 || applied[1].clipID != clipID {
		t.Fatalf("the second operation is %+v, want the start on clip %d that reuses slot 0", applied[1], clipID)
	}
}

// pendingCount answers how many published change lists the mixer has not
// drained.
func pendingCount(ring *handoffRing) uint64 {
	return ring.published.Load() - ring.applied.Load()
}

// Several flushes' changes can land in one block, and they are applied whole
// and in order rather than coalesced: two flushes' operations taking effect
// at one instant is correct, since they were ordered and stay ordered.
func TestEveryFlushsPublishedChangesAreAppliedWholeAndInOrder(t *testing.T) {
	b, plugin := spawnHandoff(t)
	gains := [2][2]float32{{0.5, 0.5}}
	for clipID := sfx.ClipID(1); clipID <= 3; clipID++ {
		b.Emit(&sfx.FlushChanges{
			Starts:  []sfx.VoiceStart{{Slot: sfx.VoiceSlot(clipID), Clip: clipID}},
			Updates: []sfx.VoiceUpdate{{Slot: sfx.VoiceSlot(clipID), Params: sfx.VoiceParams{Gains: gains}}},
		})
	}
	if pending := pendingCount(plugin.mixer.ring); pending != 3 {
		t.Fatalf("%d flushes' changes are pending, want 3", pending)
	}

	applied := drained(plugin.mixer.ring)
	if len(applied) != 6 {
		t.Fatalf("%d operations were applied, want each flush's start and update", len(applied))
	}
	for index, operation := range applied {
		slot := sfx.VoiceSlot(index/2 + 1)
		kind := []operationKind{operationKindStart, operationKindUpdate}[index%2]
		if operation.kind != kind || operation.slot != slot {
			t.Fatalf("operation %d is %+v, want kind %d on slot %d", index, operation, kind, slot)
		}
	}
	if pending := pendingCount(plugin.mixer.ring); pending != 0 {
		t.Fatalf("%d flushes' changes are still pending after a drain", pending)
	}
}

// A full ring means the device thread has not run for RingDepth flushes. It
// neither blocks the game nor drops the starts sfx's contract keeps: the
// flush merges into staging changes the mixer cannot be reading, and
// publishes them once there is room, so draining delivers every play in
// order.
func TestAFullRingMergesIntoStagingRatherThanBlockingOrDropping(t *testing.T) {
	b, plugin := spawnPulledHandoff(t)
	depth := plugin.config.RingDepth
	total := depth * 3

	for clipID := 1; clipID <= total; clipID++ {
		emitStart(b, sfx.ClipID(clipID))
	}
	if pending := pendingCount(plugin.mixer.ring); pending != uint64(depth) {
		t.Fatalf("%d flushes' changes are pending, want the ring's %d", pending, depth)
	}
	if len(plugin.mixer.ring.staging.operations) == 0 {
		t.Fatal("the staging changes are empty, so the operations a full ring could not take were dropped")
	}

	applied := drained(plugin.mixer.ring)
	b.Emit(&sfx.FlushChanges{})
	applied = append(applied, drained(plugin.mixer.ring)...)

	wantStartsInOrder(t, applied, total)
	if len(plugin.mixer.ring.staging.operations) != 0 {
		t.Fatal("the staging changes still hold operations after there was room to publish them")
	}
}

// A stop in one flush and a start of the same slot in the next are two
// operations whose order decides whether the second voice sounds at all:
// the failing sequence four kind-by-kind slices cannot express once two
// flushes have been merged, and why a change list is an ordered one.
func TestAStartAfterAStopOnOneSlotSurvivesTheMerge(t *testing.T) {
	b, plugin := spawnPulledHandoff(t)
	for clipID := 1; clipID <= plugin.config.RingDepth; clipID++ {
		emitStart(b, sfx.ClipID(clipID))
	}
	b.Emit(&sfx.FlushChanges{Stops: []sfx.VoiceSlot{0}})
	b.Emit(&sfx.FlushChanges{Starts: []sfx.VoiceStart{{Slot: 0, Clip: 99}}})

	drained(plugin.mixer.ring)
	b.Emit(&sfx.FlushChanges{})
	wantStopThenStart(t, drained(plugin.mixer.ring), 99)
}

// An update coalesces into the last operation standing for its slot, the
// same coalescing sfx performs within a flush, widened over the flushes a
// merge covers. A start and a stop never coalesce: they are what sfx's
// contract promises not to lose.
func TestAMergeCoalescesUpdatesAndKeepsStartsAndStops(t *testing.T) {
	b, plugin := spawnPulledHandoff(t)
	for clipID := 1; clipID <= plugin.config.RingDepth; clipID++ {
		emitStart(b, sfx.ClipID(clipID))
	}
	gains := func(gain float32) [2][2]float32 { return [2][2]float32{{gain, gain}} }
	b.Emit(&sfx.FlushChanges{Starts: []sfx.VoiceStart{{Slot: 1, Params: sfx.VoiceParams{Gains: gains(0.1), Rate: 1}}}})
	b.Emit(&sfx.FlushChanges{Updates: []sfx.VoiceUpdate{{Slot: 1, Params: sfx.VoiceParams{Gains: gains(0.5), Rate: 1}}}})
	b.Emit(&sfx.FlushChanges{Updates: []sfx.VoiceUpdate{{Slot: 1, Params: sfx.VoiceParams{Gains: gains(0.9), Rate: 1}}}})
	b.Emit(&sfx.FlushChanges{Stops: []sfx.VoiceSlot{2, 3}})

	staged := plugin.mixer.ring.staging.operations
	if len(staged) != 3 {
		t.Fatalf("the merge kept %d operations, want a start and two stops, the updates folded in", len(staged))
	}
	if staged[0].kind != operationKindStart || staged[0].params.Gains != gains(0.9) {
		t.Fatalf("the updates did not fold into the start: it is %+v", staged[0])
	}
	if staged[1].kind != operationKindStop || staged[2].kind != operationKindStop {
		t.Fatal("a stop was coalesced away, and a stop never coalesces")
	}
}

// The applied counter is the flush side's permission to let a destroyed clip
// go, so it does not move before the changes it counts have been applied
// whole.
func TestTheAppliedCounterOnlyMovesOnceARingSlotsChangesHaveBeenApplied(t *testing.T) {
	b, plugin := spawnHandoff(t)
	emitStart(b, 1)
	if applied := plugin.mixer.ring.applied.Load(); applied != 0 {
		t.Fatalf("the applied counter is %d before any block", applied)
	}

	plugin.mixer.ring.drain(func(*operation) {
		if applied := plugin.mixer.ring.applied.Load(); applied != 0 {
			t.Errorf("the applied counter is %d while the changes it counts are being applied", applied)
		}
	})
	if applied := plugin.mixer.ring.applied.Load(); applied != 1 {
		t.Fatalf("the applied counter is %d after one flush's changes were applied, want 1", applied)
	}
}

// A steal states two things about one slot in one flush: the victim's stop,
// and the start that takes the slot over. Emit records the stops first, so
// the stop lands on the voice it was meant for rather than on the one that
// replaced it, which it would otherwise fade out before it was heard.
func TestAStolenSlotIsStoppedBeforeTheStartThatReusesIt(t *testing.T) {
	b, plugin := spawnHandoff(t)
	b.Emit(&sfx.FlushChanges{
		Starts: []sfx.VoiceStart{{Slot: 0, Clip: 1}},
		Stops:  []sfx.VoiceSlot{0},
	})

	wantStopThenStart(t, drained(plugin.mixer.ring), 1)
}

// A play on an installed clip is an entry in the staging changes and a slot
// of the ring, both made at Voices, so Emit and a block together allocate
// nothing.
func TestAPlayAllocatesNothingInTheBackend(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	b, plugin := spawnHandoff(t)
	plugin.clips[1] = &fakeClip{}
	changes := &sfx.FlushChanges{Starts: []sfx.VoiceStart{{Slot: 0, Clip: 1}}}
	block := make([]byte, 480*bytesPerFrame)
	b.Emit(changes)
	plugin.mixer.Read(block)

	allocations := testing.AllocsPerRun(200, func() {
		b.Emit(changes)
		plugin.mixer.Read(block)
	})
	if allocations != 0 {
		t.Fatalf("a play allocated %v times in the backend", allocations)
	}
}

// The flush side publishing as fast as it can while the mixer drains as fast
// as it can: every operation arrives exactly once and in the order it was
// recorded, and under -race no access to a change list crosses the two
// sides unordered.
func TestNoOperationIsLostWhenTheFlushAndTheMixerRunTogether(t *testing.T) {
	const total = 4000
	b, plugin := spawnPulledHandoff(t)
	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		for clipID := 1; clipID <= total; clipID++ {
			emitStart(b, sfx.ClipID(clipID))
		}
		for len(plugin.mixer.ring.staging.operations) > 0 {
			b.Emit(&sfx.FlushChanges{})
			time.Sleep(time.Microsecond)
		}
	}()

	var applied []operation
	deadline := time.Now().Add(10 * time.Second)
	for len(applied) < total {
		applied = append(applied, drained(plugin.mixer.ring)...)
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d operations arrived before the deadline", len(applied), total)
		}
	}
	<-flushed
	applied = append(applied, drained(plugin.mixer.ring)...)

	wantStartsInOrder(t, applied, total)
}

// While no player pulls the ring, before the device's first block or after
// a lost device, a full ring discards its staging changes rather than merge
// into them: sfx restates every live voice once the device turns ready, so
// nothing is lost, and the staging changes neither grow without bound nor
// keep released clips alive.
func TestAFullRingThatNoPlayerPullsDiscardsItsStagingChanges(t *testing.T) {
	b, plugin := spawnHandoff(t)
	last := sfx.ClipID(plugin.config.RingDepth + 2)
	for clipID := sfx.ClipID(1); clipID <= last; clipID++ {
		emitStart(b, clipID)
	}

	staged := plugin.mixer.ring.staging.operations
	if len(staged) != 1 || staged[0].clipID != last {
		t.Fatalf("the staging changes hold %+v, want only the last flush's start: an earlier one was merged rather than discarded", staged)
	}
}

// The ring is pulled before the Device turns ready, so a flush that sees it
// ready, and restates every live voice, never finds its changes discarded
// for a mixer that has not drained its first block yet.
func TestChangesStagedOnceTheDeviceIsReadyAreNeverDiscarded(t *testing.T) {
	_, plugin := spawnOverFake(t, &fakeDevice{})
	waitForReady(t, plugin)
	b := backend{plugin: plugin}
	total := plugin.config.RingDepth * 3

	for clipID := 1; clipID <= total; clipID++ {
		emitStart(b, sfx.ClipID(clipID))
	}
	applied := drained(plugin.mixer.ring)
	b.Emit(&sfx.FlushChanges{})
	applied = append(applied, drained(plugin.mixer.ring)...)

	wantStartsInOrder(t, applied, total)
}
