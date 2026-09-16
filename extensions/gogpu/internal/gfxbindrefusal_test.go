package internal

import (
	"testing"

	cgogpu "github.com/dvoyni/cog/extensions/gogpu"
)

// Every way a bind group can come up short converges on one refused group, and
// the backend is the only place that knows it was refused - including the one
// gfx cannot see, a SetBuffer for a buffer no longer baked. Reporting it is
// what keeps that case from being as silent as the bug this backstop was added
// for.
func TestARefusedBindGroupIsNamedOncePerShaderAndGroup(t *testing.T) {
	b := newGfxBackend()
	shader := &gfxbShader{label: "scene.wgsl"}
	other := &gfxbShader{label: "canvas.wgsl"}

	err := b.noteRefusedBindGroup(shader, 2)
	refused, ok := err.(cgogpu.ErrBindGroupRefused)
	if !ok {
		t.Fatalf("first refusal = %v, want ErrBindGroupRefused", err)
	}
	if refused.Shader != "scene.wgsl" || refused.Group != 2 {
		t.Errorf("refusal = %+v, want scene.wgsl group 2", refused)
	}
	if again := b.noteRefusedBindGroup(shader, 2); again != nil {
		t.Errorf("second refusal of the same group = %v, want silence", again)
	}
	// A refused bind group is a property of the material, not of the HAL the
	// way a declined depth-only pass is, so a second broken site is a second
	// thing worth hearing about.
	if err := b.noteRefusedBindGroup(shader, 3); err == nil {
		t.Error("a different group of the same shader was swallowed")
	}
	if err := b.noteRefusedBindGroup(other, 2); err == nil {
		t.Error("the same group of a different shader was swallowed")
	}
}

// A shader recompiled after an edit is a new chance to get it right, and the
// map must not hold the old one alive to say otherwise.
func TestFreeingAShaderForgetsItsRefusedGroups(t *testing.T) {
	b := newGfxBackend()
	shader := &gfxbShader{label: "scene.wgsl"}
	kept := &gfxbShader{label: "canvas.wgsl"}
	b.noteRefusedBindGroup(shader, 2)
	b.noteRefusedBindGroup(kept, 2)

	b.forgetRefusedBindGroups(shader)

	if err := b.noteRefusedBindGroup(shader, 2); err == nil {
		t.Error("a recompiled shader's refusal stayed latched")
	}
	if err := b.noteRefusedBindGroup(kept, 2); err != nil {
		t.Errorf("another shader's latch was dropped with it: %v", err)
	}
}
