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

// submissionOf answers the submission of one command buffer per buffer, each
// clearing its buffer's first 4 bytes.
func submissionOf(buffers ...gfx.Buffer) recording.Submission {
	var submission recording.Submission
	for _, buffer := range buffers {
		submission.Buffers = append(submission.Buffers, recording.CommandBuffer{Commands: []recording.Command{{
			Name:      "ClearBuffer",
			Arguments: recording.ClearBufferArguments{Buffer: buffer, Offset: 0, Size: 4},
		}}})
	}
	return submission
}
