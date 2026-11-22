package canvas_test

import (
	"image"
	"image/color"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
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

// pictureWidth and pictureHeight are the size of the window captureDrawing
// draws into, in points.
const (
	pictureWidth  = 320
	pictureHeight = 240
)

// The colours pixel tests draw and expect: green and orange, opaque, which
// show as greenShown and orangeShown, and translucentOrange, which shows as
// translucentOverBlack, 0.6 of the encoded (255, 85, 0) over opaque black;
// and opaqueBlack, opaqueWhite, opaqueRed, opaqueGreen, opaqueBlue,
// opaqueYellow and opaqueCyan as they show.
var (
	green                = m.NewColorSrgb(50.0/255, 1, 150.0/255, 1)
	greenShown           = color.NRGBA{R: 50, G: 255, B: 150, A: 255}
	orange               = m.NewColorSrgb(1, 1.0/3, 0, 1)
	orangeShown          = color.NRGBA{R: 255, G: 85, A: 255}
	translucentOrange    = m.NewColorSrgb(1, 1.0/3, 0, 0.6)
	translucentOverBlack = color.NRGBA{R: 153, G: 51, B: 0, A: 255}
	opaqueBlack          = color.NRGBA{A: 255}
	opaqueWhite          = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	opaqueRed            = color.NRGBA{R: 255, A: 255}
	opaqueGreen          = color.NRGBA{G: 255, A: 255}
	opaqueBlue           = color.NRGBA{B: 255, A: 255}
	opaqueYellow         = color.NRGBA{R: 255, G: 255, A: 255}
	opaqueCyan           = color.NRGBA{G: 255, B: 255, A: 255}
)

// windowPicture is a capture of a window that the default camera drew into,
// read at points of the world.
type windowPicture struct {
	picture *image.NRGBA
	// pixelsPerPoint is the framebuffer's pixels to a point, a world unit.
	pixelsPerPoint float32
}

// captureDrawing draws the entries record records into a window of
// pictureWidth×pictureHeight points through the default camera, on the software adapter, each frame
// until a capture of the window holds a picture, and answers it. It fails t
// when no capture arrives within 5s.
func captureDrawing(t *testing.T, record func(list canvas.DrawList)) windowPicture {
	t.Helper()
	return captureHarnessDrawing(t, canvastest.NewHarness(t), nil, record)
}

// captureHarnessDrawing is captureDrawing on h, a harness not yet spawned,
// canvas configured by config, with plugins listed after the capture's own.
func captureHarnessDrawing(t *testing.T, h *canvastest.Harness, config *canvas.Config, record func(list canvas.DrawList), plugins ...kernel.Plugin) windowPicture {
	t.Helper()
	h.UseSoftwareAdapter()
	capturer := &windowCapturer{}
	h.Spawn(nil, config, append([]kernel.Plugin{capturer, eventPump{}}, plugins...)...)
	window := h.OpenWindow(platform.Size{Width: pictureWidth, Height: pictureHeight})
	// A Wayland window gets a framebuffer only once the compositor's frame
	// callback for its last present is read, a while after it, so the test
	// steps for a time, not a count of beats.
	capturer.capture(window)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		h.Record(record)
		h.Step()
		if picture := capturer.capturedPicture(); picture != nil {
			return windowPicture{picture: picture, pixelsPerPoint: float32(picture.Bounds().Dx()) / pictureWidth}
		}
	}
	t.Fatal("no capture of the window arrived within 5s")
	return windowPicture{}
}

// pixel answers the pixel holding point, a point of the world, whose origin
// the default camera centres in the window.
func (p windowPicture) pixel(point m.Vec2) image.Point {
	return image.Point{X: int((point.X + pictureWidth/2) * p.pixelsPerPoint), Y: int((point.Y + pictureHeight/2) * p.pixelsPerPoint)}
}

// probe is a point of the world a pixel test reads, named for the test's
// message, and the colour it wants there.
type probe struct {
	name  string
	point m.Vec2
	want  color.NRGBA
}

// expect fails t for each of probes whose pixel is not the colour it wants.
func (p windowPicture) expect(t *testing.T, probes []probe) {
	t.Helper()
	for _, probe := range probes {
		if got := p.at(probe.point); got != probe.want {
			t.Errorf("%s, at %v, is %v, want %v", probe.name, probe.point, got, probe.want)
		}
	}
}

// at answers the colour of the pixel holding point, a point of the world.
func (p windowPicture) at(point m.Vec2) color.NRGBA {
	pixel := p.pixel(point)
	return p.picture.NRGBAAt(pixel.X, pixel.Y)
}

// countLit answers how many pixels are not opaque black from the pixel
// holding from to the one holding to, both included, along a row or a
// column.
func (p windowPicture) countLit(from, to m.Vec2) int {
	start := p.pixel(from)
	end := p.pixel(to)
	step := image.Point{X: sign(end.X - start.X), Y: sign(end.Y - start.Y)}
	lit := 0
	for pixel := start; ; pixel = pixel.Add(step) {
		if p.picture.NRGBAAt(pixel.X, pixel.Y) != opaqueBlack {
			lit++
		}
		if pixel == end {
			return lit
		}
	}
}

// findUnitColour answers the colour of the unit whose top left is unit, of
// a pixel-perfect camera of four points a unit centred on the window, and
// whether each of its pixels is that colour.
func (p windowPicture) findUnitColour(unit m.Vec2) (color.NRGBA, bool) {
	from := p.pixel(unit.MulS(4))
	to := p.pixel(unit.AddS(1).MulS(4))
	unitColour := p.picture.NRGBAAt(from.X, from.Y)
	for y := from.Y; y < to.Y; y++ {
		for x := from.X; x < to.X; x++ {
			if p.picture.NRGBAAt(x, y) != unitColour {
				return unitColour, false
			}
		}
	}
	return unitColour, true
}

// sign answers -1, 0 or 1 as x is negative, zero or positive.
func sign(x int) int {
	return min(max(x, -1), 1)
}
