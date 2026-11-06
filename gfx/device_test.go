package gfx_test

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
)

func TestCreateBufferIsRecordedAndAnswersItsDescriptor(t *testing.T) {
	descriptor := gfx.BufferDescriptor{Label: "vertices", Size: 256, Usage: gfx.BufferUsageFlagVertex | gfx.BufferUsageFlagCopyDst}
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		buffer, err := device.CreateBuffer(descriptor)
		if err != nil {
			t.Fatalf("CreateBuffer answered %v", err)
		}
		if buffer == (gfx.Buffer{}) {
			t.Fatal("CreateBuffer answered the zero handle")
		}
		if got := device.BufferDescriptor(buffer); got != descriptor {
			t.Errorf("BufferDescriptor = %+v, want %+v", got, descriptor)
		}
		call := lastCall(h)
		want := recording.Call{Kind: recording.CallKindCreateBuffer, Arguments: recording.CreateBufferArguments{Buffer: buffer, Descriptor: descriptor}}
		if call != want {
			t.Errorf("last call = %+v, want %+v", call, want)
		}
	})
}

func TestReleaseBufferIsRecordedAndStalesTheHandle(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		buffer, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 16})
		if err != nil {
			t.Fatal(err)
		}
		device.ReleaseBuffer(buffer)

		want := recording.Call{Kind: recording.CallKindReleaseBuffer, Arguments: buffer}
		if call := lastCall(h); call != want {
			t.Errorf("last call = %+v, want %+v", call, want)
		}
		if got := device.BufferDescriptor(buffer); got != (gfx.BufferDescriptor{}) {
			t.Errorf("BufferDescriptor of a released buffer = %+v, want the zero descriptor", got)
		}
	})
}

func TestSecondReleaseBufferLogsErrReleasedAndDoesNothing(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		buffer, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 16})
		if err != nil {
			t.Fatal(err)
		}
		device.ReleaseBuffer(buffer)
		before := len(h.instance.Recording().Calls)
		device.ReleaseBuffer(buffer)

		if after := len(h.instance.Recording().Calls); after != before {
			t.Errorf("the second release reached the backend: %v", h.instance.Recording().Calls[before:])
		}
	})
	if logged := h.takeLogged(); len(logged) != 1 || !errors.Is(logged[0], gfx.ErrReleased) {
		t.Fatalf("logged %v, want one ErrReleased", logged)
	}
}

func TestAReleasedIndexIsCreatedAgainAfterItsRelease(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		first, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 16})
		if err != nil {
			t.Fatal(err)
		}
		device.ReleaseBuffer(first)
		second, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 32})
		if err != nil {
			t.Fatalf("creating at the released index answered %v", err)
		}
		if second.Index() != first.Index() || second == first {
			t.Errorf("second = %+v, want index %d under a new generation than %+v", second, first.Index(), first)
		}
		if got := device.BufferDescriptor(first); got != (gfx.BufferDescriptor{}) {
			t.Errorf("the first handle answers %+v, want the zero descriptor", got)
		}
	})
	for _, call := range h.instance.Recording().Calls {
		if call.Error != nil {
			t.Errorf("%v answered %v", call.Kind, call.Error)
		}
	}
}

func TestCreateBufferAnswersTheBackendsError(t *testing.T) {
	outOfMemory := errors.New("out of memory")
	h := newHarness(t)
	h.mustSpawn(nil)
	h.instance.AnswerNext(recording.CallKindCreateBuffer, recording.Answer{Error: outOfMemory})

	h.readDevice(func(device gfx.Device) {
		buffer, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 16})
		if !errors.Is(err, outOfMemory) || buffer != (gfx.Buffer{}) {
			t.Fatalf("CreateBuffer answered %+v, %v, want the zero handle and %v", buffer, err, outOfMemory)
		}
		created, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 16})
		if err != nil {
			t.Fatal(err)
		}
		if created.Index() != 0 {
			t.Errorf("the next buffer is at index %d, want the failed one's 0", created.Index())
		}
	})
}

func TestTextureIsCreatedAndReleasedWithItsDescriptor(t *testing.T) {
	descriptor := gfx.TextureDescriptor{
		Label:         "albedo",
		Size:          gfx.Extent3D{Width: 64, Height: 64, DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gfx.TextureDimension2D,
		Format:        gfx.TextureFormatRGBA8Unorm,
		Usage:         gfx.TextureUsageFlagTextureBinding,
		ViewFormats:   []gfx.TextureFormat{gfx.TextureFormatRGBA8UnormSrgb},
	}
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		texture, err := device.CreateTexture(descriptor)
		if err != nil {
			t.Fatalf("CreateTexture answered %v", err)
		}
		if got := device.TextureDescriptor(texture); !reflect.DeepEqual(got, descriptor) {
			t.Errorf("TextureDescriptor = %+v, want %+v", got, descriptor)
		}
		created := recording.Call{Kind: recording.CallKindCreateTexture, Arguments: recording.CreateTextureArguments{Texture: texture, Descriptor: descriptor}}
		if call := lastCall(h); !reflect.DeepEqual(call, created) {
			t.Errorf("last call = %+v, want %+v", call, created)
		}

		device.ReleaseTexture(texture)
		want := recording.Call{Kind: recording.CallKindReleaseTexture, Arguments: texture}
		if call := lastCall(h); call != want {
			t.Errorf("last call = %+v, want %+v", call, want)
		}
		if got := device.TextureDescriptor(texture); !reflect.DeepEqual(got, gfx.TextureDescriptor{}) {
			t.Errorf("TextureDescriptor of a released texture = %+v, want the zero descriptor", got)
		}
		device.ReleaseTexture(texture)
	})
	if logged := h.takeLogged(); len(logged) != 1 || !errors.Is(logged[0], gfx.ErrReleased) {
		t.Fatalf("logged %v, want one ErrReleased", logged)
	}
}

func TestTextureDescriptorsViewFormatsAreCopied(t *testing.T) {
	viewFormats := []gfx.TextureFormat{gfx.TextureFormatRGBA8UnormSrgb}
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		texture, err := device.CreateTexture(gfx.TextureDescriptor{
			Size:          gfx.Extent3D{Width: 4, Height: 4, DepthOrArrayLayers: 1},
			MipLevelCount: 1,
			SampleCount:   1,
			Format:        gfx.TextureFormatRGBA8Unorm,
			ViewFormats:   viewFormats,
		})
		if err != nil {
			t.Fatal(err)
		}
		viewFormats[0] = gfx.TextureFormatBGRA8Unorm
		device.TextureDescriptor(texture).ViewFormats[0] = gfx.TextureFormatBGRA8Unorm

		want := []gfx.TextureFormat{gfx.TextureFormatRGBA8UnormSrgb}
		if got := device.TextureDescriptor(texture).ViewFormats; !slices.Equal(got, want) {
			t.Errorf("TextureDescriptor's ViewFormats = %v, want %v", got, want)
		}
		created, _ := lastCall(h).Arguments.(recording.CreateTextureArguments)
		if !slices.Equal(created.Descriptor.ViewFormats, want) {
			t.Errorf("the recorded ViewFormats = %v, want %v", created.Descriptor.ViewFormats, want)
		}
	})
}

// lastCall answers the last call h's instance recorded.
func lastCall(h *harness) recording.Call {
	calls := h.instance.Recording().Calls
	return calls[len(calls)-1]
}
