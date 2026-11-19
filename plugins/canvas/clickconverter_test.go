package canvas_test

import (
	"slices"
	"sync"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
)

// clickConverter is a test plugin whose recording task, hung off
// canvas.FrameEvent, sets windowCamera in each frame and converts click into
// the world through it and through textures' camera into the texture it
// made, as Targets saw the frames before it, keeping what each frame
// converted.
type clickConverter struct {
	testPlugin
	windowCamera canvas.Camera
	textures     *textureCamera
	click        m.Vec2
	// mutex guards conversions, each frame's.
	mutex       sync.Mutex
	conversions []frameConversion
}

// frameConversion is what a frame converted a click to through a window's
// camera and a texture's, and whether each answered it lies in its
// camera's viewport of a target drawn.
type frameConversion struct {
	window, texture     m.Vec2
	inWindow, inTexture bool
}

func (*clickConverter) Name() kernel.PluginName { return "click converter" }

func (c *clickConverter) Init(configurator *kernel.Configurator, _ any) error {
	convert := configurator.RegisterTask2(func(k *kernel.Kernel, _ kernel.NoArg, targets kernel.Read[canvas.Targets], list canvas.DrawList) kernel.NoResult {
		list.SetCamera(c.windowCamera)
		textureCamera := c.textures.camera
		textureCamera.Texture = c.textures.madeTexture()
		var conversion frameConversion
		conversion.window, conversion.inWindow = c.windowCamera.ScreenToWorld(c.click, targets.Get())
		conversion.texture, conversion.inTexture = textureCamera.ScreenToWorld(c.click, targets.Get())
		c.mutex.Lock()
		c.conversions = append(c.conversions, conversion)
		c.mutex.Unlock()
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.FrameEvent) (canvas.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(convert, kernel.NoArg{}, event.Ready).Handle())
	})
	return nil
}

// convertedClicks answers what each frame converted, in order.
func (c *clickConverter) convertedClicks() []frameConversion {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return slices.Clone(c.conversions)
}
