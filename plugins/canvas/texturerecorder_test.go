package canvas_test

import (
	"sync"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/render"
)

// textureRecorder is a test plugin whose recording task, hung off
// canvas.FrameEvent, makes each of made at its first run, then calls record
// in each frame with the task's command list and Assets and the textures
// made, in made's order.
type textureRecorder struct {
	testPlugin
	made   []madeTexture
	record func(list canvas.CommandList, assets canvas.Assets, textures []render.Texture)
	// mutex guards textures, none until made.
	mutex    sync.Mutex
	textures []render.Texture
}

// madeTexture is a texture a textureRecorder makes: its descriptor, and its
// texels, none leaving it transparent.
type madeTexture struct {
	descriptor render.TextureDescriptor
	texels     []byte
}

func (*textureRecorder) Name() kernel.PluginName { return "texture recorder" }

func (r *textureRecorder) Init(configurator *kernel.Configurator, _ any) error {
	record := configurator.RegisterTask3(func(k *kernel.Kernel, _ kernel.NoArg, inventory render.Inventory, list canvas.CommandList, assets canvas.Assets) kernel.NoResult {
		r.mutex.Lock()
		if r.textures == nil {
			for _, made := range r.made {
				r.textures = append(r.textures, inventory.MakeTexture(made.descriptor, made.texels))
			}
		}
		textures := r.textures
		r.mutex.Unlock()
		r.record(list, assets, textures)
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.FrameEvent) (canvas.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(record, kernel.NoArg{}, event.Ready).Handle())
	})
	return nil
}

// madeTextures answers the textures the plugin made, none until it has.
func (r *textureRecorder) madeTextures() []render.Texture {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.textures
}
