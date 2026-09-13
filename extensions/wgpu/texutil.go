package wgpu

import (
	"errors"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// errDeviceNotReady is returned when the GPU device/queue is not yet available
// (the WebGPU device is created asynchronously on the browser).
var errDeviceNotReady = errors.New("wgpu: GPU device not ready")

// createTextureView uploads straight RGBA8 pixels into a new 2D texture and
// returns a view of it.
func createTextureView(device *wgpu.Device, queue *wgpu.Queue, w, h int, pixels []byte) (view *wgpu.TextureView, err error) {
	tex, err := device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "gfx.texture",
		Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        gputypes.TextureFormatRGBA8Unorm,
		Usage:         gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopyDst,
	})
	if err != nil {
		return nil, err
	}

	if err := queue.WriteTexture(
		&wgpu.ImageCopyTexture{Texture: tex, MipLevel: 0, Origin: wgpu.Origin3D{}, Aspect: gputypes.TextureAspectAll},
		pixels,
		&wgpu.ImageDataLayout{Offset: 0, BytesPerRow: uint32(w * 4), RowsPerImage: uint32(h)},
		&wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
	); err != nil {
		return nil, err
	}

	// A non-nil descriptor is required: the browser backend's CreateTextureView
	// dereferences desc without a nil check (native tolerates nil).
	view, err = device.CreateTextureView(tex, &wgpu.TextureViewDescriptor{
		Label:           "gfx.textureView",
		Format:          gputypes.TextureFormatRGBA8Unorm,
		Dimension:       gputypes.TextureViewDimension2D,
		Aspect:          gputypes.TextureAspectAll,
		MipLevelCount:   1,
		ArrayLayerCount: 1,
	})
	if err != nil {
		return nil, err
	}
	return view, nil
}
