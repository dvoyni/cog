package canvas_test

import (
	"sync"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
)

// materialRecorder is a test plugin whose recording task, hung off
// canvas.FrameEvent, makes a 2D material of each of descriptors at its
// first run, then calls record in each frame with the task's draw list and
// Assets, the materials made, in descriptors' order, and the frame's count,
// the first frame's 0.
type materialRecorder struct {
	testPlugin
	descriptors []canvas.MaterialDescriptor
	record      func(list canvas.DrawList, assets canvas.Assets, materials []canvas.Material, frame int)
	// mutex guards materials, none until made, and frameCount, the frames
	// recorded so far.
	mutex      sync.Mutex
	materials  []canvas.Material
	frameCount int
}

func (*materialRecorder) Name() kernel.PluginName { return "material recorder" }

func (r *materialRecorder) Init(configurator *kernel.Configurator, _ any) error {
	record := configurator.RegisterTask2(func(k *kernel.Kernel, _ kernel.NoArg, list canvas.DrawList, assets canvas.Assets) kernel.NoResult {
		r.mutex.Lock()
		if r.materials == nil {
			for _, descriptor := range r.descriptors {
				r.materials = append(r.materials, assets.MakeMaterial(descriptor))
			}
		}
		materials := r.materials
		frame := r.frameCount
		r.frameCount++
		r.mutex.Unlock()
		r.record(list, assets, materials, frame)
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.FrameEvent) (canvas.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(record, kernel.NoArg{}, event.Ready).Handle())
	})
	return nil
}

// madeMaterials answers the materials the plugin made, none until it has.
func (r *materialRecorder) madeMaterials() []canvas.Material {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.materials
}
