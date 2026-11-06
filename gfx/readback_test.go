package gfx_test

import (
	"cmp"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/gfx"
)

func TestAMapResolvesAtPollPublishedFromItsJobAndReadsTheBytesWritten(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		source, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 8, Usage: gfx.BufferUsageFlagCopySrc | gfx.BufferUsageFlagCopyDst})
		mustSucceed(t, err)
		buffer := createReadbackBuffer(t, device, 8)
		mustSucceed(t, device.WriteBuffer(source, 0, []byte{1, 2, 3, 4, 5, 6, 7, 8}))
		encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
		mustSucceed(t, err)
		encoder.CopyBufferToBuffer(source, 0, buffer, 0, 8)
		commandBuffer, err := encoder.Finish()
		mustSucceed(t, err)
		mustSucceed(t, device.Submit(commandBuffer))
		mustSucceed(t, device.MapBuffer(buffer, gfx.MapModeRead, 0, 8))
		if mapped := h.mappedEvents(); len(mapped) != 0 {
			t.Errorf("MapBuffer published %+v before a Poll or a submit, want nothing", mapped)
			return
		}

		device.Poll()

		want := []gfx.BufferMappedEvent{{Buffer: buffer, Status: gfx.MapStatusMapped}}
		if mapped := h.mappedEvents(); !reflect.DeepEqual(mapped, want) {
			t.Errorf("published %+v by the end of Poll, want %+v", mapped, want)
			return
		}
		data, err := device.MappedRange(buffer, 0, 8)
		mustSucceed(t, err)
		if want := []byte{1, 2, 3, 4, 5, 6, 7, 8}; !slices.Equal(data, want) {
			t.Errorf("MappedRange answered %v, want %v", data, want)
		}
		mustSucceed(t, device.UnmapBuffer(buffer))
		if _, err := device.MappedRange(buffer, 0, 8); err == nil {
			t.Errorf("MappedRange answered no error after UnmapBuffer")
		}
	})
}

func TestAMapResolvesAtTheNextStandaloneSubmit(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	buffers := h.createBuffers(1)

	h.readDevice(func(device gfx.Device) {
		buffer := createReadbackBuffer(t, device, 4)
		mustSucceed(t, device.MapBuffer(buffer, gfx.MapModeRead, 0, 4))
		encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
		mustSucceed(t, err)
		encoder.ClearBuffer(buffers[0], 0, 4)
		commandBuffer, err := encoder.Finish()
		mustSucceed(t, err)

		mustSucceed(t, device.Submit(commandBuffer))

		want := []gfx.BufferMappedEvent{{Buffer: buffer, Status: gfx.MapStatusMapped}}
		if mapped := h.mappedEvents(); !reflect.DeepEqual(mapped, want) {
			t.Errorf("published %+v by the end of Submit, want %+v", mapped, want)
		}
	})
}

func TestAMapResolvesAtTheNextFramesFlush(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	var buffer gfx.Buffer
	h.readDevice(func(device gfx.Device) {
		buffer = createReadbackBuffer(t, device, 4)
		mustSucceed(t, device.MapBuffer(buffer, gfx.MapModeRead, 0, 4))
	})

	h.advance(time.Millisecond)

	want := []gfx.BufferMappedEvent{{Buffer: buffer, Status: gfx.MapStatusMapped}}
	if mapped := h.mappedEvents(); !reflect.DeepEqual(mapped, want) {
		t.Errorf("published %+v after a frame, want %+v", mapped, want)
	}
}

func TestAResolvedMapIsPublishedOnce(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		buffer := createReadbackBuffer(t, device, 4)
		mustSucceed(t, device.MapBuffer(buffer, gfx.MapModeRead, 0, 4))

		device.Poll()
		device.Poll()

		if mapped := h.mappedEvents(); len(mapped) != 1 {
			t.Errorf("published %+v after two Polls, want one event", mapped)
		}
	})
}

func TestAMapEndedBeforeItResolvesPublishesNothing(t *testing.T) {
	for _, c := range []struct {
		name string
		end  func(device gfx.Device, buffer gfx.Buffer)
	}{
		{"by UnmapBuffer", func(device gfx.Device, buffer gfx.Buffer) {
			mustSucceed(t, device.UnmapBuffer(buffer))
		}},
		{"by ReleaseBuffer", func(device gfx.Device, buffer gfx.Buffer) {
			device.ReleaseBuffer(buffer)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(nil)

			h.readDevice(func(device gfx.Device) {
				ended := createReadbackBuffer(t, device, 4)
				mustSucceed(t, device.MapBuffer(ended, gfx.MapModeRead, 0, 4))
				c.end(device, ended)
				mapped := createReadbackBuffer(t, device, 4)
				mustSucceed(t, device.MapBuffer(mapped, gfx.MapModeRead, 0, 4))

				device.Poll()

				want := []gfx.BufferMappedEvent{{Buffer: mapped, Status: gfx.MapStatusMapped}}
				if got := h.mappedEvents(); !reflect.DeepEqual(got, want) {
					t.Errorf("published %+v, want %+v", got, want)
				}
			})
		})
	}
}

func TestABufferMappedAgainAfterItsUnmapIsPublishedOnce(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		buffer := createReadbackBuffer(t, device, 4)
		mustSucceed(t, device.MapBuffer(buffer, gfx.MapModeRead, 0, 4))
		mustSucceed(t, device.UnmapBuffer(buffer))
		mustSucceed(t, device.MapBuffer(buffer, gfx.MapModeRead, 0, 4))

		device.Poll()

		want := []gfx.BufferMappedEvent{{Buffer: buffer, Status: gfx.MapStatusMapped}}
		if got := h.mappedEvents(); !reflect.DeepEqual(got, want) {
			t.Errorf("published %+v, want %+v", got, want)
		}
	})
}

func TestPollsInParallelPublishEachMapOnce(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		buffers := make([]gfx.Buffer, 16)
		for index := range buffers {
			buffers[index] = createReadbackBuffer(t, device, 4)
			mustSucceed(t, device.MapBuffer(buffers[index], gfx.MapModeRead, 0, 4))
		}

		var group sync.WaitGroup
		for range 4 {
			group.Go(device.Poll)
		}
		group.Wait()

		var published []gfx.Buffer
		for _, event := range h.mappedEvents() {
			published = append(published, event.Buffer)
		}
		slices.SortFunc(published, func(a, b gfx.Buffer) int { return cmp.Compare(a.Index(), b.Index()) })
		if !slices.Equal(published, buffers) {
			t.Errorf("published maps of %v, want each of %v once", published, buffers)
		}
	})
}

func TestAMapTheBackendRefusesIsAnsweredAndPublishesNothing(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		buffer, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 4, Usage: gfx.BufferUsageFlagCopyDst})
		mustSucceed(t, err)

		if err := device.MapBuffer(buffer, gfx.MapModeRead, 0, 4); err == nil {
			t.Errorf("MapBuffer of a buffer without MapRead answered no error")
		}
		device.Poll()

		if mapped := h.mappedEvents(); len(mapped) != 0 {
			t.Errorf("published %+v for a refused map, want nothing", mapped)
		}
	})
}

func TestMapsOfAStaleBufferAnswerErrReleasedBeforeTheBackend(t *testing.T) {
	for _, c := range []struct {
		name string
		call func(device gfx.Device, stale gfx.Buffer) error
	}{
		{"MapBuffer", func(device gfx.Device, stale gfx.Buffer) error {
			return device.MapBuffer(stale, gfx.MapModeRead, 0, 4)
		}},
		{"MappedRange", func(device gfx.Device, stale gfx.Buffer) error {
			_, err := device.MappedRange(stale, 0, 4)
			return err
		}},
		{"UnmapBuffer", func(device gfx.Device, stale gfx.Buffer) error {
			return device.UnmapBuffer(stale)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(nil)

			h.readDevice(func(device gfx.Device) {
				stale := createReadbackBuffer(t, device, 4)
				device.ReleaseBuffer(stale)
				before := len(h.instance.Recording().Calls)

				if err := c.call(device, stale); !errors.Is(err, gfx.ErrReleased) {
					t.Errorf("%s answered %v, want ErrReleased", c.name, err)
				}
				if after := len(h.instance.Recording().Calls); after != before {
					t.Errorf("%s reached the backend: %v", c.name, h.instance.Recording().Calls[before:])
				}
			})
		})
	}
}

// createReadbackBuffer answers a buffer of size bytes to map for reading and
// write into.
func createReadbackBuffer(t *testing.T, device gfx.Device, size uint64) gfx.Buffer {
	t.Helper()
	buffer, err := device.CreateBuffer(gfx.BufferDescriptor{
		Size:  size,
		Usage: gfx.BufferUsageFlagMapRead | gfx.BufferUsageFlagCopyDst,
	})
	mustSucceed(t, err)
	return buffer
}
