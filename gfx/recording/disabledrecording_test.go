package recording_test

import (
	"bytes"
	"cmp"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/platform"
)

func TestWithRecordingDisabledNothingIsKeptAndCallsAnswerAsBefore(t *testing.T) {
	instance, device := requestDeviceRecordingNothing(t)
	live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
	frame := newFrameRecorder(t, instance, device, live)
	instance.AnswerNext(recording.CallKindAcquireFramebuffer, recording.Answer{Status: uint8(gfx.AcquireStatusSuboptimal)})

	status, err := device.AcquireFramebuffer(frame.window, frame.framebufferTexture, frame.framebuffer, false)
	mustSucceed(t, err)
	mustSucceed(t, device.WriteBuffer(live.buffer, 0, sequence(1, 16)))
	encoder := createEncoder(t, device)
	inRenderPass(encoder, live, func(pass gfx.RenderPassEncoder) { pass.Draw(gfx.DrawArgs{VertexCount: 3, InstanceCount: 1}) })
	commandBuffer, err := encoder.Finish()
	mustSucceed(t, err)
	index, err := device.Submit(commandBuffer)
	mustSucceed(t, err)
	mustSucceed(t, device.Present(frame.window))

	if status != gfx.AcquireStatusSuboptimal {
		t.Errorf("the acquire answered %v, want the status queued", status)
	}
	if index != 1 {
		t.Errorf("the first submit answered index %d, want 1", index)
	}
	if got, want := readBuffer(t, device, live.buffer), sequence(1, 16); !bytes.Equal(got, want) {
		t.Errorf("the written buffer holds %v, want %v", got, want)
	}
	if kept := instance.Recording(); len(kept.Calls) > 0 || len(kept.Submissions) > 0 || len(kept.Frames()) > 0 {
		t.Errorf("the recording kept %d calls, %d submissions and %d frames, want none", len(kept.Calls), len(kept.Submissions), len(kept.Frames()))
	}
}

func TestWithRecordingDisabledAWarmFrameAllocatesNothing(t *testing.T) {
	instance, device := requestDeviceRecordingNothing(t)
	live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
	frame := newFrameRecorder(t, instance, device, live)
	mustSucceed(t, frame.record())

	var err error
	allocations := testing.AllocsPerRun(100, func() { err = cmp.Or(err, frame.record()) })

	mustSucceed(t, err)
	if allocations != 0 {
		t.Errorf("a warm frame allocated %v times, want none", allocations)
	}
}

// frameRecorder records the frame the allocation gate measures, on live's
// objects and a window's surface, every slice its calls take made once.
type frameRecorder struct {
	device             gfx.BackendDevice
	live               objects
	window             platform.Window
	framebufferTexture gfx.Texture
	framebuffer        gfx.TextureView
	group              gfx.BindGroup
	groupDescriptor    gfx.BindGroupDescriptor
	pass               gfx.RenderPassDescriptor
	data               []byte
	dynamicOffsets     []uint32
	commandBuffers     []gfx.CommandBuffer
}

// newFrameRecorder answers a frameRecorder on live, creating and configuring
// its window's surface.
func newFrameRecorder(t *testing.T, instance *recording.Instance, device gfx.BackendDevice, live objects) *frameRecorder {
	t.Helper()
	frame := &frameRecorder{
		device:             device,
		live:               live,
		framebufferTexture: handleAt[gfx.Texture](5),
		framebuffer:        handleAt[gfx.TextureView](5),
		group:              handleAt[gfx.BindGroup](5),
		groupDescriptor: gfx.BindGroupDescriptor{Layout: live.groupLayout, Entries: []gfx.BindGroupEntry{
			{Binding: 0, Buffer: live.buffer, Size: 16},
			{Binding: 1, Sampler: live.sampler},
			{Binding: 2, TextureView: live.view},
		}},
		data:           sequence(1, 16),
		dynamicOffsets: []uint32{0},
		commandBuffers: make([]gfx.CommandBuffer, 1),
	}
	frame.pass = gfx.RenderPassDescriptor{ColorAttachments: []gfx.RenderPassColorAttachment{{View: frame.framebuffer, LoadOp: gfx.LoadOpClear, StoreOp: gfx.StoreOpStore}}}
	mustSucceed(t, instance.CreateSurface(frame.window, platform.NativeHandle{}))
	mustSucceed(t, device.ConfigureSurface(frame.window, gfx.SurfaceConfiguration{}))
	return frame
}

// record records one frame: a write, a bind group made and released, the
// window's framebuffer acquired, a pass of two draws, each with a vertex
// buffer and a bind group at a dynamic offset, the submit and the present;
// the first error, if any.
func (frame *frameRecorder) record() error {
	device := frame.device
	_, acquireErr := device.AcquireFramebuffer(frame.window, frame.framebufferTexture, frame.framebuffer, false)
	writeErr := device.WriteBuffer(frame.live.buffer, 0, frame.data)
	groupErr := device.CreateBindGroup(frame.group, frame.groupDescriptor)
	encoder, encoderErr := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
	if err := cmp.Or(acquireErr, writeErr, groupErr, encoderErr); err != nil {
		return err
	}
	pass := encoder.BeginRenderPass(frame.pass)
	pass.SetPipeline(frame.live.renderPipeline)
	for range 2 {
		pass.SetVertexBuffer(0, frame.live.buffer, 0)
		pass.SetBindGroup(0, frame.group, frame.dynamicOffsets)
		pass.Draw(gfx.DrawArgs{VertexCount: 3, InstanceCount: 1})
	}
	pass.End()
	commandBuffer, err := encoder.Finish()
	if err != nil {
		return err
	}
	frame.commandBuffers[0] = commandBuffer
	_, submitErr := device.Submit(frame.commandBuffers...)
	device.ReleaseBindGroup(frame.group)
	return cmp.Or(submitErr, device.Present(frame.window))
}

// requestDeviceRecordingNothing answers a recording instance with its
// recording disabled, and its device.
func requestDeviceRecordingNothing(t *testing.T) (*recording.Instance, gfx.BackendDevice) {
	t.Helper()
	instance := recording.NewInstance()
	instance.DisableRecording()
	return instance, requestDeviceOf(t, instance)
}
