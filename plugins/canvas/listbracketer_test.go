package canvas_test

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/render"
)

// listBracketer is a test plugin beginning two empty command lists in each
// frame, "before" at order -1 and "after" at order 1, so a list at order 0
// lies between them.
type listBracketer struct {
	testPlugin
}

func (listBracketer) Name() kernel.PluginName { return "list bracketer" }

func (listBracketer) Init(configurator *kernel.Configurator, _ any) error {
	bracket := configurator.RegisterTask1(beginBracketingLists)
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(bracket, kernel.NoArg{}, event.Ready).Handle())
	})
	return nil
}

// beginBracketingLists begins the empty lists "before", at order -1, and
// "after", at order 1.
func beginBracketingLists(k *kernel.Kernel, _ kernel.NoArg, frame render.Frame) kernel.NoResult {
	before := frame.BeginCommandList("before", -1)
	before.End()
	after := frame.BeginCommandList("after", 1)
	after.End()
	return kernel.NoResult{}
}
