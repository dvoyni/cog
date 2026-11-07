package recording_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
)

func TestAWriteLandsAtTheNextSubmitBeforeItsCommandBuffers(t *testing.T) {
	_, device := requestDevice(t)
	source := createBuffer(t, device, 1, gfx.BufferUsageFlagCopyDst|gfx.BufferUsageFlagCopySrc|gfx.BufferUsageFlagMapRead)
	destination := createBuffer(t, device, 2, gfx.BufferUsageFlagCopyDst|gfx.BufferUsageFlagMapRead)
	encoder := createEncoder(t, device)
	encoder.CopyBufferToBuffer(source, 0, destination, 0, 16)
	copied, err := encoder.Finish()
	mustSucceed(t, err)
	data := sequence(1, 16)

	mustSucceed(t, device.WriteBuffer(source, 0, data))
	data[0] = 99

	if got := readBuffer(t, device, source); !bytes.Equal(got, make([]byte, 16)) {
		t.Errorf("before a submit the source holds %v, want zeros", got)
	}
	submit(t, device, copied)
	if got, want := readBuffer(t, device, destination), sequence(1, 16); !bytes.Equal(got, want) {
		t.Errorf("the copy encoded before the write copied %v, want %v", got, want)
	}
}

func TestTheLastOfTwoWritesToARegionLands(t *testing.T) {
	_, device := requestDevice(t)
	buffer := createBuffer(t, device, 1, gfx.BufferUsageFlagCopyDst|gfx.BufferUsageFlagMapRead)

	mustSucceed(t, device.WriteBuffer(buffer, 4, []byte{1, 2, 3, 4}))
	mustSucceed(t, device.WriteBuffer(buffer, 4, []byte{5, 6, 7, 8}))
	submit(t, device)

	want := []byte{0, 0, 0, 0, 5, 6, 7, 8, 0, 0, 0, 0, 0, 0, 0, 0}
	if got := readBuffer(t, device, buffer); !bytes.Equal(got, want) {
		t.Errorf("the buffer holds %v, want %v", got, want)
	}
}

func TestCopiesAndClearsApplyInTheOrderSubmitted(t *testing.T) {
	_, device := requestDevice(t)
	source := createBuffer(t, device, 1, gfx.BufferUsageFlagCopyDst|gfx.BufferUsageFlagCopySrc)
	destination := createBuffer(t, device, 2, gfx.BufferUsageFlagCopyDst|gfx.BufferUsageFlagMapRead)
	mustSucceed(t, device.WriteBuffer(source, 0, sequence(1, 16)))
	mustSucceed(t, device.WriteBuffer(destination, 0, sequence(17, 16)))
	submit(t, device)

	copying := createEncoder(t, device)
	copying.CopyBufferToBuffer(source, 0, destination, 4, 8)
	copied, err := copying.Finish()
	mustSucceed(t, err)
	clearing := createEncoder(t, device)
	clearing.ClearBuffer(destination, 4, 4)
	cleared, err := clearing.Finish()
	mustSucceed(t, err)
	unsubmitted := createEncoder(t, device)
	unsubmitted.ClearBuffer(destination, 0, 16)
	_, err = unsubmitted.Finish()
	mustSucceed(t, err)
	submit(t, device, copied, cleared)

	want := []byte{17, 18, 19, 20, 0, 0, 0, 0, 5, 6, 7, 8, 29, 30, 31, 32}
	if got := readBuffer(t, device, destination); !bytes.Equal(got, want) {
		t.Errorf("the destination holds %v, want %v", got, want)
	}
}

func TestAWriteMappedRangeIsWhatTheBufferHolds(t *testing.T) {
	_, device := requestDevice(t)
	mapped := createBuffer(t, device, 1, gfx.BufferUsageFlagMapWrite|gfx.BufferUsageFlagCopySrc)
	destination := createBuffer(t, device, 2, gfx.BufferUsageFlagCopyDst|gfx.BufferUsageFlagMapRead)

	mustSucceed(t, device.MapBuffer(mapped, gfx.MapModeWrite, 0, 16))
	if ready, err := device.MapStatus(mapped); !ready || err != nil {
		t.Fatalf("MapStatus answered %v, %v, want a resolved map", ready, err)
	}
	mappedRange, err := device.MappedRange(mapped, 8, 8)
	mustSucceed(t, err)
	copy(mappedRange, sequence(1, 8))
	mustSucceed(t, device.UnmapBuffer(mapped))
	encoder := createEncoder(t, device)
	encoder.CopyBufferToBuffer(mapped, 0, destination, 0, 16)
	copied, err := encoder.Finish()
	mustSucceed(t, err)
	submit(t, device, copied)

	want := append(make([]byte, 8), sequence(1, 8)...)
	if got := readBuffer(t, device, destination); !bytes.Equal(got, want) {
		t.Errorf("the destination holds %v, want %v", got, want)
	}
}

func TestAMapStatusAnsweredAnErrorEndsTheMap(t *testing.T) {
	instance, device := requestDevice(t)
	buffer := createBuffer(t, device, 1, gfx.BufferUsageFlagMapRead)
	failure := errors.New("map failed")
	instance.AnswerNext(recording.CallKindMapStatus, recording.Answer{Error: failure})
	mustSucceed(t, device.MapBuffer(buffer, gfx.MapModeRead, 0, 16))

	if ready, err := device.MapStatus(buffer); ready || !errors.Is(err, failure) {
		t.Fatalf("MapStatus answered %v, %v, want the error queued", ready, err)
	}
	if _, err := device.MappedRange(buffer, 0, 16); err == nil {
		t.Errorf("MappedRange answered no error after the map failed")
	}
	mustSucceed(t, device.MapBuffer(buffer, gfx.MapModeRead, 0, 16))
	if ready, err := device.MapStatus(buffer); !ready || err != nil {
		t.Errorf("MapStatus of the next map answered %v, %v, want a resolved map", ready, err)
	}
	for _, call := range instance.Recording().Calls {
		if call.Kind == recording.CallKindMapStatus {
			t.Errorf("MapStatus was recorded: %+v", call)
		}
	}
}

// createBuffer creates a 16-byte buffer at index with usage.
func createBuffer(t *testing.T, device gfx.BackendDevice, index uint32, usage gfx.BufferUsageFlags) gfx.Buffer {
	t.Helper()
	buffer := handleAt[gfx.Buffer](index)
	mustSucceed(t, device.CreateBuffer(buffer, gfx.BufferDescriptor{Size: 16, Usage: usage}))
	return buffer
}

// readBuffer maps the whole of a 16-byte buffer to read it, and answers a copy
// of its bytes.
func readBuffer(t *testing.T, device gfx.BackendDevice, buffer gfx.Buffer) []byte {
	t.Helper()
	mustSucceed(t, device.MapBuffer(buffer, gfx.MapModeRead, 0, 16))
	if ready, err := device.MapStatus(buffer); !ready || err != nil {
		t.Fatalf("MapStatus answered %v, %v, want a resolved map", ready, err)
	}
	mappedRange, err := device.MappedRange(buffer, 0, 16)
	mustSucceed(t, err)
	contents := bytes.Clone(mappedRange)
	mustSucceed(t, device.UnmapBuffer(buffer))
	return contents
}

// sequence answers count bytes counting up from first.
func sequence(first byte, count int) []byte {
	contents := make([]byte, count)
	for index := range contents {
		contents[index] = first + byte(index)
	}
	return contents
}

func TestSubmitsAreIndexedFromOneOnEachDeviceAndFinishAtOnce(t *testing.T) {
	instance, device := requestDevice(t)
	if completed := device.Poll(); completed != 0 {
		t.Errorf("Poll before any submit answered %d, want 0", completed)
	}
	for want := uint64(1); want <= 2; want++ {
		index, err := device.Submit()
		mustSucceed(t, err)
		if index != want {
			t.Errorf("submit %d answered index %d", want, index)
		}
		if completed := device.Poll(); completed != want {
			t.Errorf("Poll after submit %d answered %d, want it finished", want, completed)
		}
	}
	failure := errors.New("vulkan: submit refused")
	instance.AnswerNext(recording.CallKindSubmit, recording.Answer{Error: failure})
	if index, err := device.Submit(); index != 0 || !errors.Is(err, failure) {
		t.Errorf("a refused submit answered %d, %v, want 0, %v", index, err, failure)
	}
	if completed := device.Poll(); completed != 2 {
		t.Errorf("Poll after a refused submit answered %d, want 2", completed)
	}

	adapter, err := instance.RequestAdapter(gfx.RequestAdapterOptions{})
	mustSucceed(t, err)
	second, err := adapter.RequestDevice(gfx.DeviceDescriptor{})
	mustSucceed(t, err)
	if index, err := second.Submit(); index != 1 || err != nil {
		t.Errorf("a second device's first submit answered %d, %v, want 1", index, err)
	}
}
