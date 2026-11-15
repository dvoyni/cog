package rendertest_test

import (
	"errors"
	"image"
	"image/color"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

// capturePlugin is the test's plugin, listed after render: on each
// render.FrameEvent it keeps the frame's beat and runs duringFrame, given
// the frame's index counted from zero, in a recording task; on each
// render.CapturedEvent it runs onCaptured in a task joined into its out
// dependency, keeps what a user sees of the event, and signals delivered.
// Each is skipped when nil.
type capturePlugin struct {
	duringFrame func(frameIndex int, frame render.Frame)
	onCaptured  func(event render.CapturedEvent, frame render.Frame)
	// delivered is sent to once onCaptured has run for each event.
	delivered chan struct{}
	// mutex guards frameBeats, each frame's beat, and captured, each event
	// delivered.
	mutex      sync.Mutex
	frameBeats []metronome.Beat
	captured   []render.CapturedEvent
}

func newCapturePlugin() *capturePlugin {
	return &capturePlugin{delivered: make(chan struct{}, 16)}
}

func (*capturePlugin) Name() kernel.PluginName { return "capture plugin" }

func (*capturePlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

func (p *capturePlugin) Init(configurator *kernel.Configurator, _ any) error {
	recordTask := configurator.RegisterTask1(func(k *kernel.Kernel, event render.FrameEvent, frame render.Frame) kernel.NoResult {
		p.mutex.Lock()
		frameIndex := len(p.frameBeats)
		p.frameBeats = append(p.frameBeats, event.Beat)
		p.mutex.Unlock()
		if p.duringFrame != nil {
			p.duringFrame(frameIndex, frame)
		}
		return kernel.NoResult{}
	})
	readTask := configurator.RegisterTask1(func(k *kernel.Kernel, event render.CapturedEvent, frame render.Frame) kernel.NoResult {
		if p.onCaptured != nil {
			p.onCaptured(event, frame)
		}
		p.mutex.Lock()
		p.captured = append(p.captured, render.CapturedEvent{Window: event.Window, Beat: event.Beat, Status: event.Status})
		p.mutex.Unlock()
		p.delivered <- struct{}{}
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(recordTask, event, event.Ready).Handle())
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.CapturedEvent) (render.CapturedEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(readTask, event, in).Handle())
	})
	return nil
}

func (*capturePlugin) Start(*kernel.Kernel) error { return nil }

func (*capturePlugin) Stop(*kernel.Kernel) error { return nil }

func (*capturePlugin) Destroy() error { return nil }

// makeExpectedCapture answers the capture of window taken from the frame of
// frameIndex, as p kept its beat.
func (p *capturePlugin) makeExpectedCapture(window platform.Window, frameIndex int, status render.CaptureStatus) render.CapturedEvent {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return render.CapturedEvent{Window: window, Beat: p.frameBeats[frameIndex], Status: status}
}

// readCaptured answers what a user sees of each event p's handler kept.
func (p *capturePlugin) readCaptured() []render.CapturedEvent {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return slices.Clone(p.captured)
}

// waitForDelivery waits until p's handler of render.CapturedEvent has run:
// gfx publishes the capture from the job that found it readable, a
// publication of its own that a Step does not wait for.
func (p *capturePlugin) waitForDelivery(h *rendertest.Harness) {
	h.WaitForReceive("the CapturedEvent", p.delivered)
}

func TestACaptureIsDeliveredFromTheFirstFrameAcquiredAfterItsRequest(t *testing.T) {
	h := rendertest.NewHarness(t)
	plugin := newCapturePlugin()
	var window platform.Window
	request := func(frame render.Frame) {
		if err := frame.RequestCapture(window); err != nil {
			t.Errorf("RequestCapture answered %v", err)
		}
	}
	// Frame 0's request, while it records, is taken from frame 1; the
	// request made while the first capture is delivered, between frames, is
	// taken from frame 2, the next acquired.
	plugin.duringFrame = func(frameIndex int, frame render.Frame) {
		if frameIndex == 0 {
			request(frame)
		}
	}
	firstDelivery := true
	plugin.onCaptured = func(event render.CapturedEvent, frame render.Frame) {
		if firstDelivery {
			firstDelivery = false
			request(frame)
		}
	}
	h.Spawn(nil, plugin)
	window = h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()
	plugin.waitForDelivery(h)
	h.Step()
	plugin.waitForDelivery(h)

	want := []render.CapturedEvent{
		plugin.makeExpectedCapture(window, 1, render.CaptureStatusCaptured),
		plugin.makeExpectedCapture(window, 2, render.CaptureStatusCaptured),
	}
	if captured := plugin.readCaptured(); !reflect.DeepEqual(captured, want) {
		t.Errorf("delivered %+v, want %+v", captured, want)
	}
}

func TestACaptureOfAWindowAcquiringNothingIsDeliveredNotAcquired(t *testing.T) {
	h := rendertest.NewHarness(t)
	plugin := newCapturePlugin()
	var window platform.Window
	var readErr error
	plugin.duringFrame = func(frameIndex int, frame render.Frame) {
		if frameIndex == 0 {
			if err := frame.RequestCapture(window); err != nil {
				t.Errorf("RequestCapture answered %v", err)
			}
		}
	}
	plugin.onCaptured = func(event render.CapturedEvent, frame render.Frame) {
		_, readErr = frame.ReadCapture(event)
	}
	h.Spawn(nil, plugin)
	window = h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Step()
	h.AnswerNext(recording.CallKindAcquireFramebuffer, recording.Answer{Status: uint8(gfx.AcquireStatusTimeout)})

	h.Step()
	plugin.waitForDelivery(h)

	want := []render.CapturedEvent{plugin.makeExpectedCapture(window, 1, render.CaptureStatusNotAcquired)}
	if captured := plugin.readCaptured(); !reflect.DeepEqual(captured, want) {
		t.Errorf("delivered %+v, want %+v", captured, want)
	}
	if readErr == nil {
		t.Errorf("ReadCapture of a capture of nothing answered no error")
	}
}

func TestReadCaptureAnswersThePictureWhileThePublicationsJobsRunAndGfxFreesItAfter(t *testing.T) {
	h := rendertest.NewHarness(t)
	plugin := newCapturePlugin()
	var window platform.Window
	var mutex sync.Mutex
	var kept render.CapturedEvent
	var read, readLater *image.NRGBA
	var readErr, laterErr error
	plugin.duringFrame = func(frameIndex int, frame render.Frame) {
		switch frameIndex {
		case 0:
			if err := frame.RequestCapture(window); err != nil {
				t.Errorf("RequestCapture answered %v", err)
			}
		case 2:
			mutex.Lock()
			defer mutex.Unlock()
			readLater, laterErr = frame.ReadCapture(kept)
		}
	}
	plugin.onCaptured = func(event render.CapturedEvent, frame render.Frame) {
		// Long enough that a release of the picture not waiting for this
		// task would come first.
		time.Sleep(50 * time.Millisecond)
		mutex.Lock()
		defer mutex.Unlock()
		kept = event
		read, readErr = frame.ReadCapture(event)
	}
	h.Spawn(nil, plugin)
	window = h.OpenWindow(platform.Size{Width: 3, Height: 2})
	h.Step()
	h.Step()
	plugin.waitForDelivery(h)
	waitForCaptureRelease(h)

	h.Step()

	// The recording's copy holds no texels, so the picture is the zeros of
	// its buffer, alpha opaque.
	want := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := range 2 {
		for x := range 3 {
			want.SetNRGBA(x, y, color.NRGBA{A: 0xFF})
		}
	}
	mutex.Lock()
	defer mutex.Unlock()
	if readErr != nil || !reflect.DeepEqual(read, want) {
		t.Errorf("ReadCapture answered %+v, %v, want %+v", read, readErr, want)
	}
	if readLater != nil || laterErr == nil {
		t.Errorf("ReadCapture after the publication answered %+v, %v, want an error", readLater, laterErr)
	}
}

// waitForCaptureRelease waits until gfx has released the buffer of a
// capture, which it does after the publication's out dependency, outside
// any beat.
func waitForCaptureRelease(h *rendertest.Harness) {
	var captureBuffer gfx.Buffer
	scannedCount := 0
	h.WaitUntil("the capture's release", func() bool {
		calls := h.Calls()
		for _, call := range calls[scannedCount:] {
			switch arguments := call.Arguments.(type) {
			case recording.CreateBufferArguments:
				if arguments.Descriptor.Label == "gfx: capture" {
					captureBuffer = arguments.Buffer
				}
			case gfx.Buffer:
				if call.Kind == recording.CallKindReleaseBuffer && captureBuffer != (gfx.Buffer{}) && arguments == captureBuffer {
					return true
				}
			}
		}
		scannedCount = len(calls)
		return false
	})
}

func TestAWindowThatCannotBeCapturedOrIsCapturingAnswersAsGfxDoes(t *testing.T) {
	h := rendertest.NewHarness(t)
	plugin := newCapturePlugin()
	var window platform.Window
	var answers []error
	plugin.duringFrame = func(frameIndex int, frame render.Frame) {
		if frameIndex == 0 {
			answers = append(answers,
				frame.RequestCapture(platform.Window{}),
				frame.RequestCapture(window),
				frame.RequestCapture(window),
			)
		}
	}
	h.Spawn(nil, plugin)
	window = h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()

	want := []error{render.ErrCaptureUnsupported, nil, render.ErrCaptureBusy}
	if len(answers) != len(want) {
		t.Fatalf("RequestCapture answered %v, want %v", answers, want)
	}
	for index, answer := range answers {
		if !errors.Is(answer, want[index]) {
			t.Errorf("RequestCapture %d answered %v, want %v", index, answer, want[index])
		}
	}
}

func TestCaptureCallsAnswerErrDeviceLostWhileTheDeviceIsLost(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectLoggedError(gfx.ErrDeviceLost)
	plugin := newCapturePlugin()
	var window platform.Window
	var requestErr, readErr error
	plugin.duringFrame = func(frameIndex int, frame render.Frame) {
		if frameIndex == 0 {
			if err := frame.RequestCapture(window); err != nil {
				t.Errorf("RequestCapture answered %v", err)
			}
		}
	}
	// The texture's make loses the device while the capture is delivered.
	plugin.onCaptured = func(event render.CapturedEvent, frame render.Frame) {
		h.AnswerNext(recording.CallKindCreateTexture, recording.Answer{Error: gfx.ErrDeviceLost})
		frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "loser", Width: 8, Height: 8, Format: render.TextureFormatRGBA8Unorm})
		requestErr = frame.RequestCapture(window)
		_, readErr = frame.ReadCapture(event)
	}
	h.Spawn(nil, plugin)
	window = h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()
	plugin.waitForDelivery(h)

	if !errors.Is(requestErr, render.ErrDeviceLost) || !errors.Is(readErr, render.ErrDeviceLost) {
		t.Errorf("RequestCapture answered %v and ReadCapture %v while the device was lost, want ErrDeviceLost", requestErr, readErr)
	}
}
