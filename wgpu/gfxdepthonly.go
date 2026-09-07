package wgpu

import (
	"fmt"

	cgfx "github.com/dvoyni/cog/gfx"
)

// A depth-only render pass - no colour attachment, one depth texture - is the
// shape a shadow map and a depth prepass both take, and this backend cannot
// encode one.
//
// The reason is upstream and it is not a validation failure, it is a fault.
// gogpu's Vulkan HAL returns a render-pass encoder without beginning a render
// pass at all when a descriptor names no colour attachment:
//
//	if e.active == 0 || len(desc.ColorAttachments) == 0 {
//	    return rpe
//	}
//
// and RenderPassEncoder.End then calls vkCmdEndRenderPass unconditionally, on a
// pass that was never begun. The process dies inside the driver with an access
// violation, several frames of stack away from anything that names a pass. It
// is present in the pinned version and still present in the newest published
// one, so it is a live gap rather than a stale pin.
//
// gfx builds the pass correctly, and a browser's WebGPU runs it correctly, so
// the feature is real everywhere except here. What this backend can do is
// decline the pass it cannot encode and say so once, which turns a segfault
// into a reported gap. The draws in it are dropped: gfx already skips a pass
// whose BeginPass returned nil.
//
// The consequence for a caller is worth stating, because it is not "the pass is
// missing" but "the texture it would have written is untouched": a later pass
// that loads that depth with LoadPreserve loads whatever was in it. A depth
// prepass whose colour pass depends on it therefore renders against undefined
// depth on this backend, and one whose output nothing consumes renders the same
// frame it always did.

// ErrDepthOnlyPassUnsupported reports a pass this backend declined to encode.
type ErrDepthOnlyPassUnsupported struct{ Pass string }

func (e ErrDepthOnlyPassUnsupported) Error() string {
	return fmt.Sprintf(
		"wgpu: pass %q has a depth attachment and no colour attachment, which this backend cannot encode: "+
			"gogpu's Vulkan HAL never begins a render pass with no colour attachments and then faults ending it. "+
			"The pass is skipped and its depth texture is left untouched",
		e.Pass)
}

// hasDepthAttachment reports whether a pass names a depth attachment, either
// the pooled automatic one or a texture of its own.
func hasDepthAttachment(desc cgfx.GpuPassDesc) bool {
	return desc.DepthAuto || desc.Depth != 0
}

// isDepthOnly reports the pass shape this backend declines: a pass that
// declares no colour attachment and does declare a depth one.
//
// It reads NoColor rather than a zero Target, and that distinction is the whole
// reason NoColor exists. A texture target whose view has not been created yet
// resolves to zero as well - which every temporary target does on its first
// frame, because the allocation is a bake the backend replays after these
// descriptors were built - and that pass is not a depth-only pass, it is a
// colour pass with nothing to render into yet. Both are skipped, but only one
// of them is worth reporting.
//
// A pass with neither attachment is not this case either: it is nothing at all,
// and BeginPass already returns nil for it.
func isDepthOnly(desc cgfx.GpuPassDesc) bool {
	return desc.NoColor && hasDepthAttachment(desc)
}

// takeRefusal returns the pending refusal, if any, and clears it. It exists
// because the backend runs on the render thread with no kernel handle: the
// plugin takes it and reports it where a report can be made.
func (b *gfxBackend) takeRefusal() error {
	err := b.refusal
	b.refusal = nil
	return err
}
