package canvas_test

import (
	"sync"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/render"
)

// textureCamera is a test plugin whose recording task, hung off
// canvas.FrameEvent, makes a texture of descriptor at its first run, and
// releases it at once when releases is set, and sets camera into it in each
// frame.
type textureCamera struct {
	testPlugin
	descriptor render.TextureDescriptor
	releases   bool
	camera     canvas.Camera
	// mutex guards texture, the texture made, zero until it is.
	mutex   sync.Mutex
	texture render.Texture
}

func (*textureCamera) Name() kernel.PluginName { return "texture camera" }

func (c *textureCamera) Init(configurator *kernel.Configurator, _ any) error {
	setCamera := configurator.RegisterTask2(func(k *kernel.Kernel, _ kernel.NoArg, inventory render.Inventory, list canvas.CommandList) kernel.NoResult {
		c.mutex.Lock()
		if c.texture == (render.Texture{}) {
			c.texture = inventory.MakeTexture(c.descriptor, []byte(nil))
			if c.releases {
				inventory.ReleaseTexture(c.texture)
			}
		}
		camera := c.camera
		camera.Texture = c.texture
		c.mutex.Unlock()
		list.SetCamera(camera)
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.FrameEvent) (canvas.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(setCamera, kernel.NoArg{}, event.Ready).Handle())
	})
	return nil
}

// madeTexture answers the texture the plugin made, zero until it is.
func (c *textureCamera) madeTexture() render.Texture {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.texture
}
