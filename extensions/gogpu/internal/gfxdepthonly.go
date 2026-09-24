package internal

import (
	"strings"

	"github.com/dvoyni/cog/slots/gfx"
)

// A depth-only render pass - no colour attachment, one depth texture - is the
// shape a shadow map and a depth prepass both take, and whether this backend can
// encode one is a property of the HAL that was selected, not of the platform it
// was selected on.
//
// It used to be a build-tag constant, false on every native build and true in a
// browser, because gogpu's Vulkan HAL returned a render-pass encoder without
// beginning a render pass at all when a descriptor named no colour attachment,
// and RenderPassEncoder.End then called vkCmdEndRenderPass on a pass that was
// never begun - a fault several frames inside the driver rather than an error
// anything could catch. That is fixed: gogpu/wgpu#353 landed in v0.34.5, which
// is what this tree pins, and hal/vulkan carries a depth-only render-pass test
// of its own.
//
// **The platform was never the right axis, and the fix is what made that
// visible.** A native build can select Vulkan or GLES - wgpu enumerates both and
// the best adapter wins - and only Vulkan was fixed. The GLES HAL cannot encode
// the pass either, and it fails in the quieter way: hal/gles/command.go's
// BeginRenderPass calls setupColorAttachment only when the descriptor has a
// colour attachment, and that function is the only thing that binds a
// framebuffer or attaches the depth texture. A depth-only pass there binds
// nothing, attaches nothing, and then clears and draws against whatever
// framebuffer was last bound - normally the swapchain. No fault, no error, the
// wrong target, and the depth texture left untouched.
//
// So the question is which backend, and the answer defaults to no. An
// unrecognised name is treated as unable rather than able: the cost of refusing
// a pass that would have worked is a missing shadow map that reports itself,
// and the cost of encoding one that does not is a silently wrong picture on
// someone else's machine.
//
// The consequence for a caller is worth stating, because it is not "the pass is
// missing" but "the texture it would have written is untouched": a later pass
// that loads that depth with LoadPreserve loads whatever was in it. A depth
// prepass whose colour pass depends on it therefore renders against undefined
// depth on a backend that declines, and one whose output nothing consumes
// renders the same frame it always did.

// depthOnlyPassesWork reports whether a backend can encode a pass with a depth
// attachment and no colour attachment.
//
// The argument is gogpu's display name for the active backend - "Pure Go
// (Vulkan)", "Pure Go (GLES)", "Browser WebGPU" - because that is what gogpu
// exposes: Context.Backend() is a string, and the adapter that knows the real
// gputypes.Backend never reaches this tree. Matching a name is not the check
// this wants; it is the check available, which is why the default is deny.
func depthOnlyPassesWork(backend string) bool {
	switch {
	case strings.Contains(backend, "Vulkan"):
		// Fixed by gogpu/wgpu#353, released in v0.34.5.
		return true
	case strings.Contains(backend, "WebGPU"):
		// The browser has always encoded this correctly; it is the reason the
		// feature was known to be real while the desktop could not run it.
		return true
	default:
		// GLES and Software cannot, and Metal and DX12 have never been tried.
		return false
	}
}

// hasDepthAttachment reports whether a pass names a depth attachment, either
// the pooled automatic one or a texture of its own.
func hasDepthAttachment(desc gfx.PassDesc) bool {
	return desc.DepthAuto || desc.Depth != 0
}

// isDepthOnly reports the pass shape a backend may decline: a pass that
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
func isDepthOnly(desc gfx.PassDesc) bool {
	return desc.NoColor && hasDepthAttachment(desc)
}

// passBegins reports whether a pass has the attachments to be opened, given
// whether its colour and depth views resolved. It runs after the refusal, so a
// depth-only pass that reaches it is one this backend can encode.
//
// A pass that declared colour and resolved none is skipped: either there is
// nothing to render at all, or its target's view does not exist yet, as on
// every temporary target's first frame, since its allocation is a bake the same
// Execute replays after the descriptors were built.
//
// A depth-only pass needs only its depth view. This used to sit behind the same
// colour check, which outlived the refusal it duplicated: with the refusal
// lifted for Vulkan the pass was still dropped, silently, so its clear and its
// depth writes never happened and a later pass loading that texture rendered
// against whatever was in it. Pooled depth takes its size from the colour
// target, so a depth-only pass on it resolves no view and is skipped here.
func passBegins(desc gfx.PassDesc, colour, depth bool) bool {
	if isDepthOnly(desc) {
		return depth
	}
	return colour
}

// takeRefusal returns the pending refusal, if any, and clears it. It exists
// because the backend runs on the render thread with no kernel handle: the
// plugin takes it and reports it where a report can be made.
func (b *gfxBackend) takeRefusal() error {
	err := b.refusal
	b.refusal = nil
	return err
}

// gfxbRefusedGroup is one bind group a shader could not have built. The shader
// is held by pointer because that is the only identity the render thread has
// for one, and freeing a shader clears its entries.
type gfxbRefusedGroup struct {
	shader *gfxbShader
	group  int
}

// noteRefusedBindGroup returns the report for a refused bind group the first
// time that shader and group are seen, and nothing afterwards. It is separate
// from flushBinds because the latch is the part with a decision in it, and
// driving flushBinds itself would take a render pass encoder it never reaches.
func (b *gfxBackend) noteRefusedBindGroup(shader *gfxbShader, group int) error {
	key := gfxbRefusedGroup{shader: shader, group: group}
	if _, seen := b.refusedBindGroups[key]; seen {
		return nil
	}
	b.refusedBindGroups[key] = struct{}{}
	return ErrBindGroupRefused{Shader: shader.label, Group: group}
}

// forgetRefusedBindGroups drops a freed shader's latched sites, so the map does
// not hold a released shader alive and a shader recompiled after an edit is
// heard from again.
func (b *gfxBackend) forgetRefusedBindGroups(shader *gfxbShader) {
	for key := range b.refusedBindGroups {
		if key.shader == shader {
			delete(b.refusedBindGroups, key)
		}
	}
}
