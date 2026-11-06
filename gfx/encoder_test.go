package gfx_test

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/platform"
)

func TestFrameEncodersAreSubmittedOnceSortedByOrder(t *testing.T) {
	h := newHarness(t)
	var buffers []gfx.Buffer
	h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
		for _, taken := range []struct {
			order  gfx.SubmitOrder
			buffer gfx.Buffer
		}{{30, buffers[2]}, {10, buffers[0]}, {20, buffers[1]}} {
			encoder, err := device.Encoder(event.Frame, taken.order)
			if err != nil {
				t.Errorf("Encoder at order %d answered %v", taken.order, err)
			}
			encoder.ClearBuffer(taken.buffer, 0, 4)
		}
	}
	h.mustSpawn(nil)
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)
	buffers = h.createBuffers(3)

	h.advance(time.Millisecond)

	submissions := h.instance.Recording().Submissions
	if want := []recording.Submission{submissionOf(buffers...)}; !reflect.DeepEqual(submissions, want) {
		t.Fatalf("submissions = %+v, want %+v", submissions, want)
	}
	frames := h.instance.Recording().Frames()
	want := []recording.Frame{{
		Acquired:    map[platform.Window]uint8{window: uint8(gfx.AcquireStatusAcquired)},
		Submissions: submissions,
		Presented:   []platform.Window{window},
	}}
	if !reflect.DeepEqual(frames, want) {
		t.Errorf("Frames() = %+v, want %+v", frames, want)
	}
}

func TestAnOrderTakenTwiceAnswersErrOrderTakenWithAnEncoderRecordingNothing(t *testing.T) {
	h := newHarness(t)
	var buffers []gfx.Buffer
	var mutex sync.Mutex
	var taken error
	h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
		first, _ := device.Encoder(event.Frame, 1)
		first.ClearBuffer(buffers[0], 0, 4)
		second, err := device.Encoder(event.Frame, 1)
		second.ClearBuffer(buffers[1], 0, 4)
		pass := second.BeginRenderPass(gfx.RenderPassDescriptor{})
		pass.Draw(gfx.DrawArgs{VertexCount: 3, InstanceCount: 1})
		pass.End()
		mutex.Lock()
		defer mutex.Unlock()
		taken = err
	}
	h.mustSpawn(nil)
	buffers = h.createBuffers(2)

	h.advance(time.Millisecond)

	if !errors.Is(taken, gfx.ErrOrderTaken) {
		t.Errorf("the second Encoder at order 1 answered %v, want ErrOrderTaken", taken)
	}
	submissions := h.instance.Recording().Submissions
	if want := []recording.Submission{submissionOf(buffers[0])}; !reflect.DeepEqual(submissions, want) {
		t.Errorf("submissions = %+v, want %+v", submissions, want)
	}
}

func TestAFlushedFrameAnswersErrFrameFlushedWithAnEncoderRecordingNothing(t *testing.T) {
	h := newHarness(t)
	var buffers []gfx.Buffer
	release := make(chan struct{})
	h.holdFrame = func(gfx.DrawEvent) { <-release }
	h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
		encoder, _ := device.Encoder(event.Frame, 1)
		encoder.ClearBuffer(buffers[0], 0, 4)
	}
	h.mustSpawn(nil)
	h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)
	buffers = h.createBuffers(3)
	h.clock.Jump(time.Millisecond)
	h.waitForCalls(recording.CallKindAcquireFramebuffer, 1)
	close(release)
	h.waitForSubmissions(1)
	frame := h.drawEvents()[0].Frame

	var flushed, presented error
	h.readDeviceWithoutTicking(func(device gfx.Device) {
		var encoder gfx.CommandEncoder
		encoder, flushed = device.Encoder(frame, 2)
		encoder.ClearBuffer(buffers[1], 0, 4)
	})
	h.settle()
	h.readDevice(func(device gfx.Device) {
		var encoder gfx.CommandEncoder
		encoder, presented = device.Encoder(frame, 3)
		encoder.ClearBuffer(buffers[2], 0, 4)
	})
	h.advance(time.Millisecond)

	if !errors.Is(flushed, gfx.ErrFrameFlushed) || !errors.Is(presented, gfx.ErrFrameFlushed) {
		t.Errorf("Encoder answered %v once flushed and %v once presented, want ErrFrameFlushed for both", flushed, presented)
	}
	submissions := h.instance.Recording().Submissions
	if want := []recording.Submission{submissionOf(buffers[0]), submissionOf(buffers[0])}; !reflect.DeepEqual(submissions, want) {
		t.Errorf("submissions = %+v, want %+v", submissions, want)
	}
}

func TestAFailingFinishIsLoggedAndTheRestAreSubmitted(t *testing.T) {
	h := newHarness(t)
	var buffers []gfx.Buffer
	h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
		for order, buffer := range buffers {
			encoder, _ := device.Encoder(event.Frame, gfx.SubmitOrder(order))
			encoder.ClearBuffer(buffer, 0, 4)
		}
	}
	failure := errors.New("finish failed")
	h.instance.AnswerNext(recording.CallKindFinish, recording.Answer{Error: failure})
	h.mustSpawn(nil)
	buffers = h.createBuffers(3)

	h.advance(time.Millisecond)

	logged := h.takeLogged()
	if len(logged) != 1 || !errors.Is(logged[0], failure) {
		t.Errorf("logged %v, want the failing Finish", logged)
	}
	submissions := h.instance.Recording().Submissions
	if want := []recording.Submission{submissionOf(buffers[1], buffers[2])}; !reflect.DeepEqual(submissions, want) {
		t.Errorf("submissions = %+v, want %+v", submissions, want)
	}
}

func TestAFrameWithNoEncoderSubmitsNothing(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)

	h.advance(time.Millisecond)

	if submissions := h.instance.Recording().Submissions; len(submissions) != 0 {
		t.Errorf("submissions = %+v with no encoder taken, want none", submissions)
	}
}

func TestFramesGroupEachFramesSubmissionWithItsAcquiresAndPresents(t *testing.T) {
	h := newHarness(t)
	var buffers []gfx.Buffer
	h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
		encoder, _ := device.Encoder(event.Frame, 1)
		encoder.ClearBuffer(buffers[event.Tick-1], 0, 4)
	}
	h.instance.AnswerNext(recording.CallKindAcquireFramebuffer, recording.Answer{Status: uint8(gfx.AcquireStatusOutdated)})
	h.mustSpawn(nil)
	first := h.openWindow()
	second := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 2)
	buffers = h.createBuffers(2)

	h.advance(time.Millisecond)
	h.advance(time.Millisecond)

	want := []recording.Frame{
		{
			Acquired:    map[platform.Window]uint8{first: uint8(gfx.AcquireStatusOutdated), second: uint8(gfx.AcquireStatusAcquired)},
			Submissions: []recording.Submission{submissionOf(buffers[0])},
			Presented:   []platform.Window{second},
		},
		{
			Acquired:    map[platform.Window]uint8{first: uint8(gfx.AcquireStatusAcquired), second: uint8(gfx.AcquireStatusAcquired)},
			Submissions: []recording.Submission{submissionOf(buffers[1])},
			Presented:   []platform.Window{first, second},
		},
	}
	if frames := h.instance.Recording().Frames(); !reflect.DeepEqual(frames, want) {
		t.Errorf("Frames() = %+v, want %+v", frames, want)
	}
}

func TestFramesWithNoWindowEachHoldTheirSubmission(t *testing.T) {
	h := newHarness(t)
	var buffers []gfx.Buffer
	h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
		encoder, _ := device.Encoder(event.Frame, 1)
		encoder.ClearBuffer(buffers[event.Tick-1], 0, 4)
	}
	h.mustSpawn(nil)
	buffers = h.createBuffers(2)

	h.advance(time.Millisecond)
	h.advance(time.Millisecond)

	want := []recording.Frame{
		{Submissions: []recording.Submission{submissionOf(buffers[0])}},
		{Submissions: []recording.Submission{submissionOf(buffers[1])}},
	}
	if frames := h.instance.Recording().Frames(); !reflect.DeepEqual(frames, want) {
		t.Errorf("Frames() = %+v, want %+v", frames, want)
	}
}

func TestStandaloneEncodersAreFinishedAndSubmittedInTheOrderGiven(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	buffers := h.createBuffers(2)

	h.readDevice(func(device gfx.Device) {
		var commandBuffers []gfx.CommandBuffer
		for _, buffer := range []gfx.Buffer{buffers[1], buffers[0]} {
			encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
			mustSucceed(t, err)
			encoder.ClearBuffer(buffer, 0, 4)
			commandBuffer, err := encoder.Finish()
			mustSucceed(t, err)
			commandBuffers = append(commandBuffers, commandBuffer)
		}
		if err := device.Submit(commandBuffers...); err != nil {
			t.Errorf("Submit answered %v", err)
		}
	})

	want := []recording.Submission{submissionOf(buffers[1], buffers[0])}
	if submissions := h.instance.Recording().Submissions; !reflect.DeepEqual(submissions, want) {
		t.Errorf("submissions = %+v, want %+v", submissions, want)
	}
}

func TestAFailingFinishOfAStandaloneEncoderIsAnsweredNotLogged(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	buffers := h.createBuffers(1)
	failure := errors.New("finish failed")
	h.instance.AnswerNext(recording.CallKindFinish, recording.Answer{Error: failure})

	h.readDevice(func(device gfx.Device) {
		encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
		mustSucceed(t, err)
		encoder.ClearBuffer(buffers[0], 0, 4)
		if commandBuffer, err := encoder.Finish(); !errors.Is(err, failure) || commandBuffer != nil {
			t.Errorf("Finish answered %v, %v, want no buffer and %v", commandBuffer, err, failure)
		}
	})

	if logged := h.takeLogged(); len(logged) != 0 {
		t.Errorf("logged %v, want nothing", logged)
	}
}

func TestADiscardedStandaloneEncoderCannotBeFinished(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	buffers := h.createBuffers(1)

	h.readDevice(func(device gfx.Device) {
		encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
		mustSucceed(t, err)
		encoder.ClearBuffer(buffers[0], 0, 4)
		encoder.DiscardEncoding()
		if commandBuffer, err := encoder.Finish(); err == nil || commandBuffer != nil {
			t.Errorf("Finish after DiscardEncoding answered %v, %v, want no buffer and an error", commandBuffer, err)
		}
	})
}

func TestAStandaloneEncoderFinishedTwiceAnswersAnError(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
		mustSucceed(t, err)
		_, err = encoder.Finish()
		mustSucceed(t, err)
		if commandBuffer, err := encoder.Finish(); err == nil || commandBuffer != nil {
			t.Errorf("the second Finish answered %v, %v, want no buffer and an error", commandBuffer, err)
		}
	})
}

func TestAFailingSubmitIsAnsweredAndSubmitsNothing(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	buffers := h.createBuffers(1)
	failure := errors.New("submit failed")
	h.instance.AnswerNext(recording.CallKindSubmit, recording.Answer{Error: failure})

	h.readDevice(func(device gfx.Device) {
		encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
		mustSucceed(t, err)
		encoder.ClearBuffer(buffers[0], 0, 4)
		commandBuffer, err := encoder.Finish()
		mustSucceed(t, err)
		if err := device.Submit(commandBuffer); !errors.Is(err, failure) {
			t.Errorf("Submit answered %v, want %v", err, failure)
		}
	})

	if submissions := h.instance.Recording().Submissions; len(submissions) != 0 {
		t.Errorf("submissions = %+v after a failing Submit, want none", submissions)
	}
	if logged := h.takeLogged(); len(logged) != 0 {
		t.Errorf("logged %v, want nothing", logged)
	}
}

// submissionOf answers the submission of one command buffer per buffer, each
// clearing its buffer's first 4 bytes.
func submissionOf(buffers ...gfx.Buffer) recording.Submission {
	var submission recording.Submission
	for _, buffer := range buffers {
		submission.Buffers = append(submission.Buffers, recording.CommandBuffer{Commands: []recording.Command{{
			Kind:      recording.CommandKindClearBuffer,
			Arguments: recording.ClearBufferArguments{Buffer: buffer, Offset: 0, Size: 4},
		}}})
	}
	return submission
}

func TestPassesAndCommandsAreRecordedWithTheirArguments(t *testing.T) {
	h := newHarness(t)
	var objects commandObjects
	h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
		encoder, _ := device.Encoder(event.Frame, 1)
		recordEveryCommand(encoder, objects, nil)
	}
	h.mustSpawn(nil)
	h.readDevice(func(device gfx.Device) { objects = createCommandObjects(t, device) })

	h.advance(time.Millisecond)

	want := []recording.Submission{{Buffers: []recording.CommandBuffer{{Commands: everyCommand(objects)}}}}
	if submissions := h.instance.Recording().Submissions; !reflect.DeepEqual(submissions, want) {
		t.Errorf("submissions = %+v, want %+v", submissions, want)
	}
}

func TestRecordedCommandsKeepTheirSlicesWhenTheCallerReusesThem(t *testing.T) {
	h := newHarness(t)
	var objects commandObjects
	h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
		encoder, _ := device.Encoder(event.Frame, 1)
		recordEveryCommand(encoder, objects, func(attachments []gfx.RenderPassColorAttachment, offsets []uint32, bufferRegions []gfx.BufferTextureCopy, textureRegions []gfx.TextureCopy) {
			attachments[0].LoadOp = gfx.LoadOpLoad
			offsets[0] = 512
			bufferRegions[0].Size.Width = 1
			textureRegions[0].Size.Width = 1
		})
	}
	h.mustSpawn(nil)
	h.readDevice(func(device gfx.Device) { objects = createCommandObjects(t, device) })

	h.advance(time.Millisecond)

	want := []recording.Submission{{Buffers: []recording.CommandBuffer{{Commands: everyCommand(objects)}}}}
	if submissions := h.instance.Recording().Submissions; !reflect.DeepEqual(submissions, want) {
		t.Errorf("submissions = %+v, want %+v", submissions, want)
	}
}

// commandObjects are the objects recordEveryCommand records with.
type commandObjects struct {
	fixture
	group           gfx.BindGroup
	renderPipeline  gfx.RenderPipeline
	computePipeline gfx.ComputePipeline
}

// createCommandObjects creates the objects recordEveryCommand records with.
func createCommandObjects(t *testing.T, device gfx.Device) commandObjects {
	t.Helper()
	objects := commandObjects{fixture: createFixture(t, device)}
	var err error
	objects.group, err = device.CreateBindGroup(gfx.BindGroupDescriptor{Layout: objects.groupLayout, Entries: []gfx.BindGroupEntry{{Binding: 0, Buffer: objects.buffer}}})
	mustSucceed(t, err)
	objects.renderPipeline, err = device.CreateRenderPipeline(renderPipelineDescriptor(objects.fixture))
	mustSucceed(t, err)
	objects.computePipeline, err = device.CreateComputePipeline(computePipelineDescriptor(objects.fixture))
	mustSucceed(t, err)
	return objects
}

// recordEveryCommand records a render pass and a compute pass of every
// command each takes, then every copy and a clear outside them, and hands
// reuse, when given, the slices it passed, once recorded.
func recordEveryCommand(encoder gfx.CommandEncoder, objects commandObjects, reuse func([]gfx.RenderPassColorAttachment, []uint32, []gfx.BufferTextureCopy, []gfx.TextureCopy)) {
	attachments := []gfx.RenderPassColorAttachment{{View: objects.view, LoadOp: gfx.LoadOpClear, StoreOp: gfx.StoreOpStore, ClearValue: gfx.Color{A: 1}}}
	offsets := []uint32{256}
	bufferRegions := []gfx.BufferTextureCopy{{BufferLayout: gfx.ImageDataLayout{BytesPerRow: 256, RowsPerImage: 4}, TextureBase: gfx.ImageCopyTexture{Texture: objects.texture}, Size: gfx.Extent3D{Width: 4, Height: 4, DepthOrArrayLayers: 1}}}
	textureRegions := []gfx.TextureCopy{{Source: gfx.ImageCopyTexture{Texture: objects.texture}, Destination: gfx.ImageCopyTexture{Texture: objects.texture, MipLevel: 1}, Size: gfx.Extent3D{Width: 2, Height: 2, DepthOrArrayLayers: 1}}}

	render := encoder.BeginRenderPass(gfx.RenderPassDescriptor{Label: "opaque", ColorAttachments: attachments})
	render.SetPipeline(objects.renderPipeline)
	render.SetBindGroup(0, objects.group, offsets)
	render.SetVertexBuffer(0, objects.buffer, 16)
	render.SetIndexBuffer(objects.buffer, gfx.IndexFormatUint16, 32)
	render.SetViewport(gfx.Viewport{Width: 4, Height: 4, MaxDepth: 1})
	render.SetScissorRect(gfx.ScissorRect{Width: 2, Height: 2})
	render.SetBlendConstant(gfx.Color{R: 0.5})
	render.SetStencilReference(7)
	render.Draw(gfx.DrawArgs{VertexCount: 3, InstanceCount: 1})
	render.DrawIndexed(gfx.DrawIndexedArgs{IndexCount: 6, InstanceCount: 2, BaseVertex: -1})
	render.DrawIndirect(objects.buffer, 64)
	render.DrawIndexedIndirect(objects.buffer, 80)
	render.MultiDrawIndirect(objects.buffer, 96, 2)
	render.MultiDrawIndexedIndirect(objects.buffer, 128, 3)
	render.End()

	compute := encoder.BeginComputePass(gfx.ComputePassDescriptor{Label: "cull"})
	compute.SetPipeline(objects.computePipeline)
	compute.SetBindGroup(0, objects.group, nil)
	compute.Dispatch(8, 4, 1)
	compute.DispatchIndirect(objects.buffer, 160)
	compute.End()

	encoder.CopyBufferToBuffer(objects.buffer, 0, objects.buffer, 128, 64)
	encoder.CopyBufferToTexture(objects.buffer, objects.texture, bufferRegions)
	encoder.CopyTextureToBuffer(objects.texture, objects.buffer, bufferRegions)
	encoder.CopyTextureToTexture(objects.texture, objects.texture, textureRegions)
	encoder.ClearBuffer(objects.buffer, 192, 64)

	if reuse != nil {
		reuse(attachments, offsets, bufferRegions, textureRegions)
	}
}

// everyCommand answers the commands recordEveryCommand records.
func everyCommand(objects commandObjects) []recording.Command {
	bufferRegions := []gfx.BufferTextureCopy{{BufferLayout: gfx.ImageDataLayout{BytesPerRow: 256, RowsPerImage: 4}, TextureBase: gfx.ImageCopyTexture{Texture: objects.texture}, Size: gfx.Extent3D{Width: 4, Height: 4, DepthOrArrayLayers: 1}}}
	return []recording.Command{
		{
			Kind: recording.CommandKindBeginRenderPass,
			Arguments: gfx.RenderPassDescriptor{Label: "opaque", ColorAttachments: []gfx.RenderPassColorAttachment{
				{View: objects.view, LoadOp: gfx.LoadOpClear, StoreOp: gfx.StoreOpStore, ClearValue: gfx.Color{A: 1}},
			}},
			Commands: []recording.Command{
				{Kind: recording.CommandKindSetPipeline, Arguments: objects.renderPipeline},
				{Kind: recording.CommandKindSetBindGroup, Arguments: recording.SetBindGroupArguments{Index: 0, Group: objects.group, DynamicOffsets: []uint32{256}}},
				{Kind: recording.CommandKindSetVertexBuffer, Arguments: recording.SetVertexBufferArguments{Slot: 0, Buffer: objects.buffer, Offset: 16}},
				{Kind: recording.CommandKindSetIndexBuffer, Arguments: recording.SetIndexBufferArguments{Buffer: objects.buffer, Format: gfx.IndexFormatUint16, Offset: 32}},
				{Kind: recording.CommandKindSetViewport, Arguments: gfx.Viewport{Width: 4, Height: 4, MaxDepth: 1}},
				{Kind: recording.CommandKindSetScissorRect, Arguments: gfx.ScissorRect{Width: 2, Height: 2}},
				{Kind: recording.CommandKindSetBlendConstant, Arguments: gfx.Color{R: 0.5}},
				{Kind: recording.CommandKindSetStencilReference, Arguments: uint32(7)},
				{Kind: recording.CommandKindDraw, Arguments: gfx.DrawArgs{VertexCount: 3, InstanceCount: 1}},
				{Kind: recording.CommandKindDrawIndexed, Arguments: gfx.DrawIndexedArgs{IndexCount: 6, InstanceCount: 2, BaseVertex: -1}},
				{Kind: recording.CommandKindDrawIndirect, Arguments: recording.IndirectArguments{Buffer: objects.buffer, Offset: 64}},
				{Kind: recording.CommandKindDrawIndexedIndirect, Arguments: recording.IndirectArguments{Buffer: objects.buffer, Offset: 80}},
				{Kind: recording.CommandKindMultiDrawIndirect, Arguments: recording.MultiDrawIndirectArguments{Buffer: objects.buffer, Offset: 96, DrawCount: 2}},
				{Kind: recording.CommandKindMultiDrawIndexedIndirect, Arguments: recording.MultiDrawIndirectArguments{Buffer: objects.buffer, Offset: 128, DrawCount: 3}},
			},
		},
		{
			Kind:      recording.CommandKindBeginComputePass,
			Arguments: gfx.ComputePassDescriptor{Label: "cull"},
			Commands: []recording.Command{
				{Kind: recording.CommandKindSetPipeline, Arguments: objects.computePipeline},
				{Kind: recording.CommandKindSetBindGroup, Arguments: recording.SetBindGroupArguments{Index: 0, Group: objects.group}},
				{Kind: recording.CommandKindDispatch, Arguments: recording.DispatchArguments{X: 8, Y: 4, Z: 1}},
				{Kind: recording.CommandKindDispatchIndirect, Arguments: recording.IndirectArguments{Buffer: objects.buffer, Offset: 160}},
			},
		},
		{Kind: recording.CommandKindCopyBufferToBuffer, Arguments: recording.CopyBufferToBufferArguments{Source: objects.buffer, SourceOffset: 0, Destination: objects.buffer, DestinationOffset: 128, Size: 64}},
		{Kind: recording.CommandKindCopyBufferToTexture, Arguments: recording.CopyBufferToTextureArguments{Source: objects.buffer, Destination: objects.texture, Regions: bufferRegions}},
		{Kind: recording.CommandKindCopyTextureToBuffer, Arguments: recording.CopyTextureToBufferArguments{Source: objects.texture, Destination: objects.buffer, Regions: bufferRegions}},
		{Kind: recording.CommandKindCopyTextureToTexture, Arguments: recording.CopyTextureToTextureArguments{Source: objects.texture, Destination: objects.texture, Regions: []gfx.TextureCopy{
			{Source: gfx.ImageCopyTexture{Texture: objects.texture}, Destination: gfx.ImageCopyTexture{Texture: objects.texture, MipLevel: 1}, Size: gfx.Extent3D{Width: 2, Height: 2, DepthOrArrayLayers: 1}},
		}}},
		{Kind: recording.CommandKindClearBuffer, Arguments: recording.ClearBufferArguments{Buffer: objects.buffer, Offset: 192, Size: 64}},
	}
}
