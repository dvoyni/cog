package canvas_test

import (
	"image"
	"sync"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// windowCapturer is a test plugin that asks for a capture of the window it
// was armed with in each frame, render refusing a request while one is
// pending, until a capture holds a picture, which it keeps: a frame that
// acquired no framebuffer captures none.
type windowCapturer struct {
	testPlugin
	// mutex guards window, the window to capture, and picture, the picture
	// once read.
	mutex   sync.Mutex
	window  platform.Window
	picture *image.NRGBA
}

func (*windowCapturer) Name() kernel.PluginName { return "window capturer" }

func (c *windowCapturer) Init(configurator *kernel.Configurator, _ any) error {
	request := configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, frame render.Frame) kernel.NoResult {
		c.mutex.Lock()
		defer c.mutex.Unlock()
		if c.window != (platform.Window{}) && c.picture == nil {
			_ = frame.RequestCapture(c.window) // ErrCaptureBusy while one is pending
		}
		return kernel.NoResult{}
	})
	read := configurator.RegisterTask1(func(k *kernel.Kernel, event render.CapturedEvent, frame render.Frame) kernel.NoResult {
		if picture, err := frame.ReadCapture(event); err == nil {
			c.mutex.Lock()
			c.picture = picture
			c.mutex.Unlock()
		}
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(request, kernel.NoArg{}, event.Ready).Handle())
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.CapturedEvent) (render.CapturedEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(read, event, in).Handle())
	})
	return nil
}

// capture arms the capturer with window, which the frames after ask a
// capture of until one holds a picture.
func (c *windowCapturer) capture(window platform.Window) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.window = window
}

// capturedPicture answers the picture captured, nil until it arrives.
func (c *windowCapturer) capturedPicture() *image.NRGBA {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.picture
}
