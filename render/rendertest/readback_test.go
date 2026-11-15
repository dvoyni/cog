package rendertest_test

import (
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/internal/handle"
	"github.com/dvoyni/cog/render/rendertest"
)

// readbackPlugin is the test's plugin handling render.ReadbackEvent, listed
// after render beside the loaderPlugin that records: on each event it runs
// onRead, unless nil, in a task joined into its out dependency, keeps the
// event, and signals delivered.
type readbackPlugin struct {
	onRead func(event render.ReadbackEvent, frame render.Frame)
	// delivered is sent to once onRead has run for each event.
	delivered chan struct{}
	// mutex guards readbacks, each event delivered.
	mutex     sync.Mutex
	readbacks []render.ReadbackEvent
}

func newReadbackPlugin() *readbackPlugin {
	return &readbackPlugin{delivered: make(chan struct{}, 16)}
}

func (*readbackPlugin) Name() kernel.PluginName { return "readback plugin" }

func (*readbackPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

func (p *readbackPlugin) Init(configurator *kernel.Configurator, _ any) error {
	readTask := configurator.RegisterTask1(func(k *kernel.Kernel, event render.ReadbackEvent, frame render.Frame) kernel.NoResult {
		if p.onRead != nil {
			p.onRead(event, frame)
		}
		p.mutex.Lock()
		p.readbacks = append(p.readbacks, event)
		p.mutex.Unlock()
		p.delivered <- struct{}{}
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.ReadbackEvent) (render.ReadbackEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(readTask, event, in).Handle())
	})
	return nil
}

func (*readbackPlugin) Start(*kernel.Kernel) error { return nil }

func (*readbackPlugin) Stop(*kernel.Kernel) error { return nil }

func (*readbackPlugin) Destroy() error { return nil }

// readReadbacks answers each event p's handler kept.
func (p *readbackPlugin) readReadbacks() []render.ReadbackEvent {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return slices.Clone(p.readbacks)
}

// waitForDeliveries waits until p's handler of render.ReadbackEvent has run
// count times: render publishes a readback from the job that found it
// mapped, a publication of its own that a Step does not wait for.
func (p *readbackPlugin) waitForDeliveries(h *rendertest.Harness, count int) {
	for range count {
		h.WaitForReceive("the ReadbackEvent", p.delivered)
	}
}

// spawnReadbackHarness spawns h with config, plugin, and a loaderPlugin
// running duringFrame in each frame's recording, and opens a window.
func spawnReadbackHarness(t *testing.T, h *rendertest.Harness, config *render.Config, plugin *readbackPlugin, duringFrame func(frameIndex int, inventory render.Inventory, frame render.Frame)) {
	h.Spawn(config, &loaderPlugin{files: makeDrawFiles(t), duringFrame: duringFrame}, plugin)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
}

// stepUntilDelivered steps h through the frame recording readbacks and the
// next, whose submit has gfx resolve their maps, and waits until plugin has
// handled count of them.
func stepUntilDelivered(h *rendertest.Harness, plugin *readbackPlugin, count int) {
	h.Step()
	h.Step()
	plugin.waitForDeliveries(h, count)
}

// makeFirstFrameReadback answers a duringFrame that, in the first frame,
// makes a buffer of 16 bytes and reads all of it back, keeping the Readback
// in readback.
func makeFirstFrameReadback(readback *render.Readback) func(frameIndex int, inventory render.Inventory, frame render.Frame) {
	return func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex != 0 {
			return
		}
		source := inventory.MakeBuffer("source", render.BufferUsageFlagCopySrc, make([]uint32, 4))
		list := frame.BeginCommandList("readback", drawOrder)
		*readback = list.ReadBuffer(render.BufferRange{Buffer: source})
		list.End()
	}
}

// findStagingBuffers answers the staging buffers render made among calls,
// in the order made.
func findStagingBuffers(calls []recording.Call) []recording.CreateBufferArguments {
	var created []recording.CreateBufferArguments
	for _, call := range calls {
		if arguments, ok := call.Arguments.(recording.CreateBufferArguments); ok && arguments.Descriptor.Label == handle.StagingBufferLabel {
			created = append(created, arguments)
		}
	}
	return created
}

// findBufferCalls answers the calls among calls of kind whose Arguments are
// buffer alone, or a map of it.
func findBufferCalls(calls []recording.Call, kind recording.CallKind, buffer gfx.Buffer) []recording.Call {
	var found []recording.Call
	for _, call := range calls {
		if call.Kind != kind {
			continue
		}
		switch arguments := call.Arguments.(type) {
		case gfx.Buffer:
			if arguments == buffer {
				found = append(found, call)
			}
		case recording.MapBufferArguments:
			if arguments.Buffer == buffer {
				found = append(found, call)
			}
		}
	}
	return found
}

// waitForUnmap waits until render has unmapped buffer count times, which it
// does after a readback's publication, outside any beat.
func waitForUnmap(h *rendertest.Harness, buffer gfx.Buffer, count int) {
	h.WaitUntil("the staging buffer's unmap", func() bool {
		return len(findBufferCalls(h.Calls(), recording.CallKindUnmapBuffer, buffer)) >= count
	})
}

func TestAReadbackRecordsItsCopyAtItsPointAmongPassesAndIsDeliveredAfterTheFrame(t *testing.T) {
	h := rendertest.NewHarness(t)
	plugin := newReadbackPlugin()
	var scene copyScene
	var bufferReadback, textureReadback render.Readback
	var mutex sync.Mutex
	read := map[render.Readback][]byte{}
	plugin.onRead = func(event render.ReadbackEvent, frame render.Frame) {
		data, err := frame.ReadbackData(event)
		if err != nil {
			t.Errorf("ReadbackData answered %v", err)
		}
		mutex.Lock()
		defer mutex.Unlock()
		read[event.Readback] = slices.Clone(data)
	}
	spawnReadbackHarness(t, h, nil, plugin, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex != 0 {
			return
		}
		scene = makeCopyScene(inventory)
		list := frame.BeginCommandList("readbacks", drawOrder)
		before := list.BeginPass(scene.colorPass("before"))
		before.End()
		bufferReadback = list.ReadBuffer(render.BufferRange{Buffer: scene.source, Offset: 16, Size: 32})
		textureReadback = list.ReadTexture(scene.image, render.TextureRegion{X: 2, Y: 2, Width: 4, Height: 4})
		after := list.BeginPass(scene.makeKeptPass("after"))
		after.End()
		list.End()
	})

	h.Step()
	if delivered := plugin.readReadbacks(); len(delivered) != 0 {
		t.Errorf("readbacks %+v were delivered with the frame recording them, before its submit ran", delivered)
	}
	h.Step()
	plugin.waitForDeliveries(h, 2)

	if bufferReadback == (render.Readback{}) || textureReadback == (render.Readback{}) || bufferReadback == textureReadback {
		t.Fatalf("the readbacks are %v and %v, want two of their own", bufferReadback, textureReadback)
	}
	delivered := plugin.readReadbacks()
	want := []render.ReadbackEvent{{Readback: bufferReadback, Status: render.ReadbackStatusReady}, {Readback: textureReadback, Status: render.ReadbackStatusReady}}
	if !reflect.DeepEqual(delivered, want) && !reflect.DeepEqual(delivered, []render.ReadbackEvent{want[1], want[0]}) {
		t.Errorf("the readbacks delivered are %+v, want %+v", delivered, want)
	}
	// The recording's copies copy nothing, so the bytes are the zeros of the
	// staging buffer: 32 of the buffer's range, and the texture region's 4
	// rows of 16 bytes, 256 apart.
	mutex.Lock()
	if wantRead := map[render.Readback][]byte{bufferReadback: make([]byte, 32), textureReadback: make([]byte, 3*256+16)}; !reflect.DeepEqual(read, wantRead) {
		t.Errorf("ReadbackData answered %d and %d bytes, want 32 and %d", len(read[bufferReadback]), len(read[textureReadback]), 3*256+16)
	}
	mutex.Unlock()

	calls := h.Calls()
	staging := findStagingBuffers(calls)
	if len(staging) != 2 {
		t.Fatalf("render made %d staging buffers, want one for each readback", len(staging))
	}
	// 784 bytes round up to a quarter of 512, past 256 bytes, the least.
	for index, wantSize := range []uint64{256, 896} {
		if descriptor := staging[index].Descriptor; descriptor.Size != wantSize || descriptor.Usage != gfx.BufferUsageFlagCopyDst|gfx.BufferUsageFlagMapRead {
			t.Errorf("staging buffer %d is %+v, want %d bytes to copy into and map for reading", index, descriptor, wantSize)
		}
	}
	for index, wantMap := range []recording.MapBufferArguments{
		{Buffer: staging[0].Buffer, Mode: gfx.MapModeRead, Size: 32},
		{Buffer: staging[1].Buffer, Mode: gfx.MapModeRead, Size: 3*256 + 16},
	} {
		if maps := findBufferCalls(calls, recording.CallKindMapBuffer, wantMap.Buffer); len(maps) != 1 || maps[0].Arguments != wantMap {
			t.Errorf("staging buffer %d was mapped by %+v, want once, as %+v", index, maps, wantMap)
		}
	}

	source, _ := readBufferCalls(calls).findCreated[recording.CreateBufferArguments](scene.source)
	image, _ := readTextureCalls(calls).findCreated[recording.CreateTextureArguments](scene.image)
	var kinds []recording.CommandKind
	var copies []any
	for _, list := range findListCommands(h) {
		if len(list) == 0 || list[0].Kind != recording.CommandKindBeginRenderPass {
			continue
		}
		for _, command := range list {
			kinds = append(kinds, command.Kind)
			if command.Kind == recording.CommandKindCopyBufferToBuffer || command.Kind == recording.CommandKindCopyTextureToBuffer {
				copies = append(copies, command.Arguments)
			}
		}
	}
	wantKinds := []recording.CommandKind{
		recording.CommandKindBeginRenderPass,
		recording.CommandKindCopyBufferToBuffer, recording.CommandKindCopyTextureToBuffer,
		recording.CommandKindBeginRenderPass,
	}
	if !slices.Equal(kinds, wantKinds) {
		t.Errorf("the list's commands are %v, want the readbacks' copies between the two passes, %v", kinds, wantKinds)
	}
	wantCopies := []any{
		recording.CopyBufferToBufferArguments{Source: source.Buffer, SourceOffset: 16, Destination: staging[0].Buffer, Size: 32},
		recording.CopyTextureToBufferArguments{Source: image.Texture, Destination: staging[1].Buffer, Regions: []gfx.BufferTextureCopy{{
			BufferLayout: gfx.ImageDataLayout{BytesPerRow: 256, RowsPerImage: 4},
			TextureBase:  gfx.ImageCopyTexture{Texture: image.Texture, Origin: gfx.Origin3D{X: 2, Y: 2}, Aspect: gfx.TextureAspectAll},
			Size:         gfx.Extent3D{Width: 4, Height: 4, DepthOrArrayLayers: 1},
		}}},
	}
	if !reflect.DeepEqual(copies, wantCopies) {
		t.Errorf("the readbacks' copies are %+v, want %+v", copies, wantCopies)
	}
}

func TestReadbackDataAnswersTheBytesUntilThePublicationsOutDependencyAndTheBufferGoesBackToThePool(t *testing.T) {
	h := rendertest.NewHarness(t)
	plugin := newReadbackPlugin()
	var source render.Buffer
	var readbacks []render.Readback
	// mutex guards delivered, data and readErr.
	var mutex sync.Mutex
	var delivered render.ReadbackEvent
	var data []byte
	var readErr error
	var laterData []byte
	var laterErr error
	plugin.onRead = func(event render.ReadbackEvent, frame render.Frame) {
		// Long enough that a release of the staging buffer not waiting for
		// this task would unmap it first.
		time.Sleep(50 * time.Millisecond)
		mutex.Lock()
		defer mutex.Unlock()
		if delivered == (render.ReadbackEvent{}) {
			delivered = event
			data, readErr = frame.ReadbackData(event)
		}
	}
	spawnReadbackHarness(t, h, nil, plugin, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		switch frameIndex {
		case 0:
			source = inventory.MakeBuffer("source", render.BufferUsageFlagCopySrc, make([]uint32, 64))
		case 3:
			mutex.Lock()
			laterData, laterErr = frame.ReadbackData(delivered)
			mutex.Unlock()
		default:
			return
		}
		list := frame.BeginCommandList("readback", drawOrder)
		readbacks = append(readbacks, list.ReadBuffer(render.BufferRange{Buffer: source, Size: 64}))
		list.End()
	})

	stepUntilDelivered(h, plugin, 1)
	staging := findStagingBuffers(h.Calls())
	if len(staging) != 1 {
		t.Fatalf("render made %d staging buffers, want one", len(staging))
	}
	waitForUnmap(h, staging[0].Buffer, 1)
	h.Step()
	h.Step()

	mutex.Lock()
	if readErr != nil || len(data) != 64 {
		t.Errorf("ReadbackData answered %d bytes, %v, while the publication's jobs ran, want 64", len(data), readErr)
	}
	mutex.Unlock()
	if laterData != nil || laterErr == nil {
		t.Errorf("ReadbackData after the publication answered %v, %v, want an error", laterData, laterErr)
	}
	calls := h.Calls()
	if staging := findStagingBuffers(calls); len(staging) != 1 {
		t.Errorf("render made %d staging buffers, want the second readback to take the first's back", len(staging))
	}
	if maps := findBufferCalls(calls, recording.CallKindMapBuffer, staging[0].Buffer); len(maps) != 2 {
		t.Errorf("the staging buffer was mapped %d times, want once for each readback", len(maps))
	}
	if len(readbacks) != 2 || readbacks[0] == readbacks[1] {
		t.Errorf("the readbacks are %v, want two of their own", readbacks)
	}
}

func TestTheBytesOfATextureReadbackEndWithItsLastRow(t *testing.T) {
	h := rendertest.NewHarness(t)
	plugin := newReadbackPlugin()
	var data []byte
	var readErr error
	plugin.onRead = func(event render.ReadbackEvent, frame render.Frame) {
		data, readErr = frame.ReadbackData(event)
	}
	spawnReadbackHarness(t, h, nil, plugin, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex != 0 {
			return
		}
		mask := inventory.MakeTexture[byte](render.TextureDescriptor{Label: "mask", Width: 8, Height: 8, Format: render.TextureFormatR8Unorm}, nil)
		list := frame.BeginCommandList("readback", drawOrder)
		list.ReadTexture(mask, render.TextureRegion{X: 1, Y: 1, Width: 3, Height: 2})
		list.End()
	})

	stepUntilDelivered(h, plugin, 1)

	// Two rows of 3 bytes, 256 apart, though the map takes whole words.
	if readErr != nil || len(data) != 256+3 {
		t.Errorf("ReadbackData answered %d bytes, %v, want %d", len(data), readErr, 256+3)
	}
}

func TestReadbacksInFlightTogetherHoldStagingBuffersOfTheirOwn(t *testing.T) {
	h := rendertest.NewHarness(t)
	plugin := newReadbackPlugin()
	spawnReadbackHarness(t, h, nil, plugin, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex != 0 {
			return
		}
		source := inventory.MakeBuffer("source", render.BufferUsageFlagCopySrc, make([]uint32, 8))
		list := frame.BeginCommandList("readbacks", drawOrder)
		list.ReadBuffer(render.BufferRange{Buffer: source, Size: 16})
		list.ReadBuffer(render.BufferRange{Buffer: source, Offset: 16})
		list.End()
	})

	stepUntilDelivered(h, plugin, 2)

	if staging := findStagingBuffers(h.Calls()); len(staging) != 2 || staging[0].Buffer == staging[1].Buffer {
		t.Errorf("render made staging buffers %+v, want one for each readback", staging)
	}
}

func TestAFailedMapDeliversAFailedStatus(t *testing.T) {
	errMapRefused := errors.New("the map is refused")
	for _, test := range []struct {
		name string
		// failed is the call whose answer fails the map: its start, or the
		// status gfx polls.
		failed recording.CallKind
	}{
		{name: "when it starts", failed: recording.CallKindMapBuffer},
		{name: "when it resolves", failed: recording.CallKindMapStatus},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := rendertest.NewHarness(t)
			h.ExpectLoggedError(errMapRefused)
			plugin := newReadbackPlugin()
			var readback render.Readback
			var readErr error
			plugin.onRead = func(event render.ReadbackEvent, frame render.Frame) {
				_, readErr = frame.ReadbackData(event)
			}
			spawnReadbackHarness(t, h, nil, plugin, makeFirstFrameReadback(&readback))
			h.AnswerNext(test.failed, recording.Answer{Error: errMapRefused})

			stepUntilDelivered(h, plugin, 1)
			h.Step()

			if delivered, want := plugin.readReadbacks(), []render.ReadbackEvent{{Readback: readback, Status: render.ReadbackStatusFailed}}; !reflect.DeepEqual(delivered, want) {
				t.Errorf("the readbacks delivered are %+v, want %+v", delivered, want)
			}
			if readErr == nil {
				t.Error("ReadbackData of a failed readback answered no error")
			}
			if staging := findStagingBuffers(h.Calls()); len(staging) != 1 || len(findBufferCalls(h.Calls(), recording.CallKindUnmapBuffer, staging[0].Buffer)) != 0 {
				t.Errorf("render made staging buffers %+v and unmapped the first, want one, never mapped and so not unmapped", staging)
			}
		})
	}
}

func TestReadbackDataAnswersErrDeviceLostWhileTheDeviceIsLost(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectLoggedError(gfx.ErrDeviceLost)
	plugin := newReadbackPlugin()
	var readErr error
	// The texture's make loses the device while the readback is delivered.
	plugin.onRead = func(event render.ReadbackEvent, frame render.Frame) {
		h.AnswerNext(recording.CallKindCreateTexture, recording.Answer{Error: gfx.ErrDeviceLost})
		frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "loser", Width: 8, Height: 8, Format: render.TextureFormatRGBA8Unorm})
		_, readErr = frame.ReadbackData(event)
	}
	var readback render.Readback
	spawnReadbackHarness(t, h, nil, plugin, makeFirstFrameReadback(&readback))

	stepUntilDelivered(h, plugin, 1)

	if !errors.Is(readErr, render.ErrDeviceLost) {
		t.Errorf("ReadbackData answered %v while the device was lost, want ErrDeviceLost", readErr)
	}
}

func TestStagingBuffersUnusedForTemporaryIdleFramesAreFreedButNeverInFlight(t *testing.T) {
	h := rendertest.NewHarness(t)
	plugin := newReadbackPlugin()
	var readErr error
	plugin.onRead = func(event render.ReadbackEvent, frame render.Frame) {
		// Long enough for the frame's end to run while the readback is in
		// flight, and free its staging buffer were it counted as unused.
		time.Sleep(50 * time.Millisecond)
		_, readErr = frame.ReadbackData(event)
	}
	var readback render.Readback
	spawnReadbackHarness(t, h, &render.Config{TemporaryIdleFrames: 1}, plugin, makeFirstFrameReadback(&readback))

	stepUntilDelivered(h, plugin, 1)
	staging := findStagingBuffers(h.Calls())
	if len(staging) != 1 {
		t.Fatalf("render made %d staging buffers, want one", len(staging))
	}
	waitForUnmap(h, staging[0].Buffer, 1)
	h.Step()
	releasedEarly := len(findBufferCalls(h.Calls(), recording.CallKindReleaseBuffer, staging[0].Buffer)) != 0
	h.Step()

	if readErr != nil {
		t.Errorf("ReadbackData answered %v while the readback was in flight", readErr)
	}
	if releasedEarly {
		t.Error("the staging buffer was freed before going a frame unused")
	}
	if released := findBufferCalls(h.Calls(), recording.CallKindReleaseBuffer, staging[0].Buffer); len(released) != 1 {
		t.Errorf("the staging buffer was freed %d times after going a frame unused, want once", len(released))
	}
}

func TestAReadbackThatDoesNotFitIsRefusedReportedAndRecordsNothing(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindCopyRefused)
	plugin := newReadbackPlugin()
	var readbacks []render.Readback
	spawnReadbackHarness(t, h, nil, plugin, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex != 0 {
			return
		}
		scene := makeCopyScene(inventory)
		list := frame.BeginCommandList("readbacks", drawOrder)
		readbacks = append(readbacks,
			// A buffer made without the copy source usage.
			list.ReadBuffer(render.BufferRange{Buffer: scene.vertices}),
			// An offset no multiple of 4.
			list.ReadBuffer(render.BufferRange{Buffer: scene.source, Offset: 2, Size: 8}),
			// A size no multiple of 4.
			list.ReadBuffer(render.BufferRange{Buffer: scene.source, Size: 6}),
			// No bytes, at the buffer's end.
			list.ReadBuffer(render.BufferRange{Buffer: scene.source, Offset: 512}),
			// A buffer render does not hold.
			list.ReadBuffer(render.BufferRange{}),
			// Past the level's edge.
			list.ReadTexture(scene.image, render.TextureRegion{X: 6, Width: 4}),
			// No texels, at the level's edge.
			list.ReadTexture(scene.image, render.TextureRegion{X: 8}),
			// A format gfx's copies cannot fill.
			list.ReadTexture(scene.depth, render.TextureRegion{}),
		)
		list.End()
	})

	h.Step()
	h.Step()

	if count := countReports(h, render.ReportKindCopyRefused); count != 8 {
		t.Errorf("render reported %d refused readbacks, want eight", count)
	}
	if want := make([]render.Readback, 8); !slices.Equal(readbacks, want) {
		t.Errorf("the refused readbacks answered %v, want the zero Readback", readbacks)
	}
	if staging := findStagingBuffers(h.Calls()); len(staging) != 0 {
		t.Errorf("render made staging buffers %+v for refused readbacks", staging)
	}
	if delivered := plugin.readReadbacks(); len(delivered) != 0 {
		t.Errorf("refused readbacks were delivered as %+v", delivered)
	}
}
