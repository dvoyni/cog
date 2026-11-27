package sfxtest

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/headless"
)

// readEvent has the harness's plugin run Read's job, between two steps.
type readEvent struct{}

// harnessPlugin is the harness's own plugin, listed right after sfx: it binds
// the headless backend, or the one UseBackend gave, and the harness's memory
// mount, schedules the
// harness's records and then sfx's flush on each beat, runs Read's jobs, and
// keeps each ending sfx publishes, for Endings.
type harnessPlugin struct {
	harness *Harness
}

// Name answers the harness plugin's name.
func (harnessPlugin) Name() kernel.PluginName { return "sfxtest" }

// Dependencies names sfx, whose Tasks Init reads.
func (harnessPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{sfx.PluginName}
}

// Init binds sfx's BackendSocket to the backend UseBackend gave, or else to a
// headless backend, and the memory mount as a read mount at the root, registers the recording task and the reading
// task, and subscribes to metronome.UpdateEvent, to schedule the recording
// task and then sfx's flush with the beat's Delta plus its Lost, joined into
// the out dependency; to readEvent; and to sfx.VoiceEndedEvent. The tasks'
// bodies stay closures, since they hand the test's records the queue and the
// test's read the views.
func (p harnessPlugin) Init(configurator *kernel.Configurator, _ any) error {
	backend := p.harness.backend
	if backend == nil {
		backend = headless.NewBackend(0)
	}
	configurator.BindSocket(backend)
	configurator.BindSocket(platform.ReadMountSocket{FS: p.harness.memory})
	tasks, _ := configurator.Resource[sfx.Tasks]()
	flush := tasks.Flush
	record := configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, queue kernel.Write[sfx.Queue]) kernel.NoResult {
		for _, queued := range p.harness.takeRecords() {
			queued(queue.Get())
		}
		return kernel.NoResult{}
	})
	read := configurator.RegisterTask4(func(k *kernel.Kernel, _ kernel.NoArg, voices kernel.Read[sfx.LiveVoices], clips kernel.Read[sfx.ClipTable], buses kernel.Read[sfx.BusVolumes], listener kernel.Read[sfx.Listener]) kernel.NoResult {
		p.harness.reading(voices.Get(), clips.Get(), buses.Get(), listener.Get())
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, beat metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
		recorded := ex.Schedule(record, kernel.NoArg{})
		flushed := ex.Schedule(flush, beat.Delta+beat.Lost, recorded.Handle())
		return beat, ex.Join(in, flushed.Handle())
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event readEvent) (readEvent, kernel.JobHandle) {
		return event, ex.Schedule(read, kernel.NoArg{}, in).Handle()
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, ending sfx.VoiceEndedEvent) (sfx.VoiceEndedEvent, kernel.JobHandle) {
		p.harness.noteEnding(ending)
		return ending, in
	})
	return nil
}

// Start does nothing.
func (harnessPlugin) Start(*kernel.Kernel) error { return nil }

// Stop does nothing.
func (harnessPlugin) Stop(*kernel.Kernel) error { return nil }

// Destroy does nothing.
func (harnessPlugin) Destroy() error { return nil }
