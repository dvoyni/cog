//go:build !js

package internal

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/slots/gfx"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/gogpu/wgpu/core"
	"github.com/gogpu/wgpu/hal/noop"
)

// noopDeviceProvider hands attach a real wgpu device built on the noop HAL, so
// pipeline creation runs wgpu's own validation without a GPU.
type noopDeviceProvider struct{ device *wgpu.Device }

func (p noopDeviceProvider) Device() *wgpu.Device { return p.device }
func (p noopDeviceProvider) Queue() *wgpu.Queue   { return p.device.Queue() }
func (p noopDeviceProvider) SurfaceFormat() gputypes.TextureFormat {
	return gputypes.TextureFormatBGRA8Unorm
}

// newNoopGfxBackend attaches a fresh backend to a noop-HAL device. wgpu skips
// the noop backend when it enumerates adapters, so the device is opened from
// the noop HAL directly and wrapped with NewDeviceFromHAL.
func newNoopGfxBackend(t *testing.T) *gfxBackend {
	t.Helper()
	instance, err := noop.NewBackend().CreateInstance(nil)
	if err != nil {
		t.Fatalf("noop instance: %v", err)
	}
	adapters := instance.EnumerateAdapters(nil)
	if len(adapters) == 0 {
		t.Fatal("noop instance exposes no adapter")
	}
	open, err := adapters[0].Adapter.Open(0, gputypes.DefaultLimits())
	if err != nil {
		t.Fatalf("noop adapter open: %v", err)
	}
	device, err := wgpu.NewDeviceFromHAL(open.Device, open.Queue, 0, gputypes.DefaultLimits(), "noop")
	if err != nil {
		t.Fatalf("NewDeviceFromHAL: %v", err)
	}
	b := newGfxBackend()
	if err := b.attach(noopDeviceProvider{device: device}, "noop"); err != nil {
		t.Fatalf("attach: %v", err)
	}
	return b
}

const oneInputWGSL = `
@vertex fn vs_main(@location(0) position: vec3<f32>) -> @builtin(position) vec4<f32> {
	return vec4<f32>(position, 1.0);
}

@fragment fn fs_main() -> @location(0) vec4<f32> {
	return vec4<f32>(1.0);
}
`

// A stride that is not a multiple of 4 succeeds on Vulkan and fails in the
// browser and on GLES. gfx refuses it before the backend is asked
// (ErrVertexStrideAlignment), so this calls the backend's NewPipeline directly:
// it proves the second line of defence, the pinned wgpu fork, now fails the
// pipeline on every native backend instead of letting each one decide.
// Before the pin the same call returned no error (dvoyni/cog#47).
func TestAMisalignedVertexStrideFailsThePipelineOnTheNativePath(t *testing.T) {
	b := newNoopGfxBackend(t)
	shader, err := b.NewShader(gfx.ShaderDesc{Label: "stride.wgsl", Code: []byte(oneInputWGSL)})
	if err != nil {
		t.Fatalf("NewShader: %v", err)
	}
	desc := gfx.PipelineDesc{
		Shader:        shader,
		Topology:      gfx.TopologyTriangleList,
		NoDepthTarget: true,
		Attributes:    []gfx.VertexAttribute{{Offset: 0, Type: gfx.Float32x3, Location: 0}},
		Label:         "stride",
	}

	desc.Stride = 12
	if _, err := b.NewPipeline(desc); err != nil {
		t.Fatalf("stride 12: NewPipeline failed on the noop device, so the stride 30 failure below proves nothing: %v", err)
	}

	desc.Stride = 30
	_, err = b.NewPipeline(desc)
	var pipelineErr *core.CreateRenderPipelineError
	if !errors.As(err, &pipelineErr) {
		t.Fatalf("stride 30: err = %v (%T), want a *core.CreateRenderPipelineError", err, err)
	}
	if pipelineErr.Kind != core.CreateRenderPipelineErrorVertexStrideMisaligned {
		t.Fatalf("stride 30: kind = %v (%v), want VertexStrideMisaligned", pipelineErr.Kind, err)
	}
	if pipelineErr.ArrayStride != 30 {
		t.Errorf("stride 30: reported arrayStride = %d", pipelineErr.ArrayStride)
	}
}
