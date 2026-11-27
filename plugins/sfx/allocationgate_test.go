package sfx_test

import (
	"fmt"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// gateWarmBeatCount is how many beats the gate steps before it measures: the
// clip's first play, its read, its prepare and its install land in the first,
// and by the last every voice of the first beats has ended, the table has
// filled and turned over, and the flush's scratch, the queue's lists and the
// kernel's pools have grown to what a beat of the scenario needs.
const gateWarmBeatCount = 96

// gateMeasuredBeatCount is how many beats the gate measures, besides
// AllocsPerRun's own warm-up beat: two whole cycles of the gate's pause, its
// stops, seeks and bus changes. Its count a beat is rounded down, so fewer
// allocations than this over them all, made by the runtime, the test
// framework or the harness keeping each ending meanwhile, count none.
const gateMeasuredBeatCount = 32

// gateKeptVoiceCount is how many of the voices it played the gate's recorder
// keeps, to move, seek and stop.
const gateKeptVoiceCount = 16

// gatePausePeriod is how many beats apart the gate's recorder pauses every
// voice, the pause lasting two beats.
const gatePausePeriod = 16

// gateScenario is one shape of the gate's steady beat: the cap and how many
// one-shots each beat plays.
type gateScenario struct {
	maxVoices    int
	playsPerBeat int
}

func (s gateScenario) String() string {
	return fmt.Sprintf("voices=%d/plays=%d", s.maxVoices, s.playsPerBeat)
}

// gateScenarios are the gate's beats: a table that never fills, its voices
// ending as their clips run out, stopped or seeked past their ends, and one at
// its cap, every play stealing besides.
var gateScenarios = []gateScenario{
	{maxVoices: 64, playsPerBeat: 2},
	{maxVoices: 8, playsPerBeat: 4},
}

// gatePlugin records the gate's steady beat into sfx's queue, in a task the
// beat's handler schedules after the harness's flush, so what it records lands
// in the next beat's flush. Each beat it plays one-shots of the resident clip
// at positions around the listener, from near enough their end that each
// finishes a few beats later, moves every voice it keeps, turns the listener,
// and on beats of their own seeks, stops, changes a voice's volume and pitch,
// moves a bus's volume, seeks a voice past its end and pauses every voice;
// on its first beat it plays a looping track one band up, which plays on.
type gatePlugin struct {
	scenario gateScenario
	// beat counts the beats recorded, and kept is the voices played last, a
	// ring indexed by keptCount, how many were played; only the recording
	// task, run once a beat, reads and writes them.
	beat      int
	kept      [gateKeptVoiceCount]sfx.Voice
	keptCount int
}

func (*gatePlugin) Name() kernel.PluginName { return "gate" }

func (*gatePlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{sfx.PluginName}
}

func (p *gatePlugin) Init(configurator *kernel.Configurator, _ any) error {
	record := configurator.RegisterTask1(p.record)
	configurator.Subscribe(func(_ *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, beat metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
		return beat, ex.Schedule(record, kernel.NoArg{}, in).Handle()
	})
	return nil
}

func (*gatePlugin) Start(*kernel.Kernel) error { return nil }

func (*gatePlugin) Stop(*kernel.Kernel) error { return nil }

func (*gatePlugin) Destroy() error { return nil }

// record records one beat of the gate.
func (p *gatePlugin) record(_ *kernel.Kernel, _ kernel.NoArg, queueHandle kernel.Write[sfx.Queue]) kernel.NoResult {
	queue := queueHandle.Get()
	beat := p.beat
	p.beat++
	if beat == 0 {
		queue.Play(clipPath, 0, sfx.VoiceOptions{Bus: m.Some(busMusic), Priority: m.Some(1), Loop: m.Some(true)})
	}
	for index := range p.scenario.playsPerBeat {
		angle := float64(beat*p.scenario.playsPerBeat+index) * 0.7
		remaining := time.Duration(4+(beat+index)%8) * sfxtest.StepInterval
		p.kept[p.keptCount%gateKeptVoiceCount] = queue.Play(clipPath, clipDuration-remaining, sfx.VoiceOptions{
			Bus:         m.Some(busEffects),
			Position:    m.Some(m.Vec3{X: float32(4 * math.Cos(angle)), Z: float32(4 * math.Sin(angle))}),
			Orientation: m.Some(m.QuatRotationY(float32(angle))),
			Falloff:     m.Some(sfx.Falloff{Model: sfx.DistanceLinear, Ref: 1, Max: 20, Rolloff: 1}),
			Cone:        m.Some(sfx.Cone{Inner: 90, Outer: 270, OuterGain: 0.25}),
		})
		p.keptCount++
	}
	for index, voice := range &p.kept {
		angle := float64(beat+index) * 0.3
		queue.SetVoice(voice, sfx.VoiceOptions{Position: m.Some(m.Vec3{X: float32(3 * math.Cos(angle)), Y: 1, Z: float32(3 * math.Sin(angle))})})
	}
	queue.SetListener(sfx.ListenerOptions{
		Position:    m.Some(m.Vec3{X: float32(beat%5) * 0.1}),
		Orientation: m.Some(m.QuatRotationY(float32(beat) * 0.05)),
	})
	oldest := p.kept[p.keptCount%gateKeptVoiceCount]
	switch beat % 4 {
	case 0:
		queue.Seek(oldest, 2*sfxtest.StepInterval)
	case 1:
		queue.Stop(oldest)
	case 2:
		queue.SetVoice(oldest, sfx.VoiceOptions{Volume: m.Some(0.5 + float32(beat%3)*0.25), Pitch: m.Some(1 + float32(beat%2)*0.5)})
	case 3:
		queue.SetBus(busEffects, 0.5+float32(beat%7)*0.05)
		queue.Seek(p.kept[(p.keptCount+1)%gateKeptVoiceCount], clipDuration)
	}
	switch beat % gatePausePeriod {
	case 0:
		queue.SetPaused(true)
	case 2:
		queue.SetPaused(false)
	}
	return kernel.NoResult{}
}

// arrivingDevicePlugin wraps sfx's BackendSocket, whichever binds it, so its
// device reads not ready until device.arrived is set, as on a machine whose
// device was lost, or never opened, before the game started, and keeps in
// device how many starts each flush hands the backend.
type arrivingDevicePlugin struct{ device *arrivingDevice }

func (arrivingDevicePlugin) Name() kernel.PluginName { return "arrivingdevice" }

func (arrivingDevicePlugin) Dependencies() []kernel.PluginName { return nil }

func (p arrivingDevicePlugin) Init(configurator *kernel.Configurator, _ any) error {
	configurator.WrapSocket[sfx.BackendSocket](func(bound sfx.BackendSocket) sfx.BackendSocket {
		return arrivingDeviceBackend{BackendSocket: bound, device: p.device}
	})
	return nil
}

func (arrivingDevicePlugin) Start(*kernel.Kernel) error { return nil }

func (arrivingDevicePlugin) Stop(*kernel.Kernel) error { return nil }

func (arrivingDevicePlugin) Destroy() error { return nil }

// arrivingDevice is whether the device of an arrivingDeviceBackend has
// arrived, and the starts its flushes handed the backend: the most any flush
// handed before the device arrived, and how many the last one did. The test
// writes arrived and the flush's job reads it, and the other way about for
// the counts, so each is atomic.
type arrivingDevice struct {
	arrived        atomic.Bool
	mostStartCount atomic.Int64
	lastStartCount atomic.Int64
}

// arrivingDeviceBackend is the bound backend, its device not ready until
// device.arrived is set.
type arrivingDeviceBackend struct {
	sfx.BackendSocket
	device *arrivingDevice
}

func (b arrivingDeviceBackend) Device() sfx.Device {
	if !b.device.arrived.Load() {
		return sfx.Device{}
	}
	return b.BackendSocket.Device()
}

func (b arrivingDeviceBackend) Emit(changes *sfx.FlushChanges) {
	startCount := int64(len(changes.Starts))
	b.device.lastStartCount.Store(startCount)
	if !b.device.arrived.Load() && startCount > b.device.mostStartCount.Load() {
		b.device.mostStartCount.Store(startCount)
	}
	b.BackendSocket.Emit(changes)
}

// spawnWarmGate answers a harness spawned with scenario's cap, its
// gatePlugin and plugins, the fixture clip mounted, stepped
// gateWarmBeatCount beats.
func spawnWarmGate(t *testing.T, scenario gateScenario, plugins ...kernel.Plugin) *sfxtest.Harness {
	t.Helper()
	h := newHarness(t, &sfx.Config{MaxVoices: scenario.maxVoices}, append([]kernel.Plugin{&gatePlugin{scenario: scenario}}, plugins...)...)
	for range gateWarmBeatCount {
		h.Step()
	}
	return h
}

// checkGateBeats fails the test unless h's warm beats did what a steady
// flush holds: voices have run out, been stopped and, at scenario's cap
// alone, been stolen, while the looping track one band up plays on among
// positional voices.
func checkGateBeats(t *testing.T, h *sfxtest.Harness, scenario gateScenario) {
	t.Helper()
	reasonCounts := map[sfx.Reason]int{}
	for _, ending := range h.Endings() {
		reasonCounts[ending.Reason]++
	}
	atCap := scenario.maxVoices < gateKeptVoiceCount
	if reasonCounts[sfx.ReasonFinished] == 0 || reasonCounts[sfx.ReasonStopped] == 0 || (reasonCounts[sfx.ReasonStolen] > 0) != atCap {
		t.Errorf("the gate's voices ended %v, want some finished and some stopped, and stolen only at the cap", reasonCounts)
	}
	h.Read(func(voices sfx.LiveVoices, _ sfx.ClipTable, _ sfx.BusVolumes, _ sfx.Listener, _ sfx.DeviceView) {
		trackCount := 0
		positionalCount := 0
		for entry := range voices.All() {
			if entry.Options.Loop.Or(false) && entry.Options.Priority.Or(0) == 1 {
				trackCount++
			}
			if entry.Options.Position.Present() {
				positionalCount++
			}
		}
		if trackCount != 1 || positionalCount == 0 {
			t.Errorf("the view holds %d looping tracks and %d positional voices, want the one track among positional voices", trackCount, positionalCount)
		}
	})
}

// TestASteadyFlushAllocatesNothingOnceWarm steps the gate's beats once warm,
// checks they did what a steady flush holds, and counts what a beat
// allocates: voices played, moved, seeked, stopped, stolen and running out,
// buses, the listener and the pause changing, and every ending published.
// Under the race detector, whose runtime allocates on its own, it steps the
// beats and checks no count.
func TestASteadyFlushAllocatesNothingOnceWarm(t *testing.T) {
	for _, scenario := range gateScenarios {
		t.Run(scenario.String(), func(t *testing.T) {
			h := spawnWarmGate(t, scenario)
			checkGateBeats(t, h, scenario)
			allocations := testing.AllocsPerRun(gateMeasuredBeatCount, h.Step)
			if allocations != 0 && !raceEnabled {
				t.Errorf("a warm flush allocated %v times, want none", allocations)
			}
		})
	}
}

// TestTheFlushWhereTheDeviceArrivesAllocatesNothingOnceWarm steps the gate's
// beats with no device, then has the device arrive and counts what that one
// beat allocates: every live voice restarted at once, more starts than any
// beat before it handed the backend. Under the race detector it steps the
// beats and checks no count.
func TestTheFlushWhereTheDeviceArrivesAllocatesNothingOnceWarm(t *testing.T) {
	device := &arrivingDevice{}
	h := spawnWarmGate(t, gateScenarios[0], arrivingDevicePlugin{device: device})
	// AllocsPerRun calls once before it counts, so the device arrives in the
	// second call, the one it counts.
	callCount := 0
	allocations := testing.AllocsPerRun(1, func() {
		callCount++
		device.arrived.Store(callCount == 2)
		h.Step()
	})
	if allocations != 0 && !raceEnabled {
		t.Errorf("the flush where the device arrived allocated %v times, want none", allocations)
	}
	if most, arrival := device.mostStartCount.Load(), device.lastStartCount.Load(); arrival <= most {
		t.Errorf("the flush where the device arrived handed the backend %d starts, want more than the %d of the busiest flush before it", arrival, most)
	}
}
