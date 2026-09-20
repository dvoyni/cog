package internal

import (
	"errors"

	"github.com/gogpu/wgpu"
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

// A draw whose bind group was refused used to encode anyway, with that group
// left unbound - a second validation error stacked on the first, and the one
// the driver reports is not the one that explains anything. Dropping the draw
// is what gfx already does for an unfilled storage binding, for the same
// reason.
//
// The encoder is nil here on purpose: the assertion is that nothing is encoded
// against it, and a draw that did encode would fault on it.
func TestARefusedBindGroupDropsItsDraw(t *testing.T) {
	b := newGfxBackend()
	b.bindGroups = newGfxBindGroupCache(
		func(*wgpu.BindGroupLayout, []wgpu.BindGroupEntry) (*wgpu.BindGroup, error) {
			return nil, errors.New("refused")
		},
		func(*wgpu.BindGroup) {},
	)
	shader := &gfxbShader{label: "canvas.wgsl", bgLayouts: []*wgpu.BindGroupLayout{{}}}
	b.addEntry(0, gfxbBindEntry{key: gfxbBindingKey{kind: gfxbBindTexture, binding: 1, id: 7}})

	pass := &gfxRenderPass{backend: b, shader: shader}
	pass.Draw(0, 3, 1, 0, false)

	if b.refusal == nil {
		t.Fatal("a refused bind group went unreported")
	}
	if len(b.acc[0]) != 0 {
		t.Errorf("pending entries after the draw = %d, want the accumulator reset", len(b.acc[0]))
	}
}

// A group the shader declares entries for and nothing filled used to be
// skipped, not refused: flushBinds looped over the accumulator, so a group with
// no pending entries was not looked at, and the draw encoded with that group
// left unset. It is the quietest way a draw can be wrong - the refusal
// machinery that catches a *short* group never fires - and the uniform arena
// can reach it, because a slot it cannot hand out leaves group 0 empty rather
// than short.
func TestADeclaredGroupNothingFilledIsRefused(t *testing.T) {
	b := newGfxBackend()
	shader := &gfxbShader{
		label:      "canvas.wgsl",
		bgLayouts:  []*wgpu.BindGroupLayout{{}},
		groupSizes: []int{1},
	}

	if b.flushBinds(nil, shader) {
		t.Fatal("a declared group nothing filled bound, want the draw dropped")
	}
	if b.refusal == nil {
		t.Error("an unfilled group went unreported")
	}
}

// The inverse, and the reason the check cannot be "the layout is non-nil and
// the accumulator is empty": buildShaderLayouts creates a layout for every
// group index below the highest one a shader uses, so an unused middle group
// has a non-nil layout with no entries in it. A scene shader composed from
// frame (group 0) and morph (group 2) but not material (group 1) is exactly
// that, and refusing it would drop every draw of it forever, because a refusal
// is latched once and dropped always.
func TestAGroupDeclaringNoEntriesIsSkipped(t *testing.T) {
	b := newGfxBackend()
	bg := &wgpu.BindGroup{}
	b.bindGroups = newGfxBindGroupCache(
		func(*wgpu.BindGroupLayout, []wgpu.BindGroupEntry) (*wgpu.BindGroup, error) {
			return bg, nil
		},
		func(*wgpu.BindGroup) {},
	)
	shader := &gfxbShader{
		label:      "scene.wgsl",
		bgLayouts:  []*wgpu.BindGroupLayout{{}, {}, {}},
		groupSizes: []int{1, 0, 1},
	}
	b.addEntry(0, gfxbBindEntry{key: gfxbBindingKey{kind: gfxbBindBuffer, binding: 0, id: 4}})
	b.addEntry(2, gfxbBindEntry{key: gfxbBindingKey{kind: gfxbBindBuffer, binding: 0, id: 9}})
	// The encoder is nil, so the two groups that do bind are seeded as already
	// bound: the redundant-bind filter suppresses their SetBindGroup calls, and
	// what this observes is the loop's verdict rather than its encoding.
	b.bound = []*wgpu.BindGroup{bg, nil, bg}

	if !b.flushBinds(nil, shader) {
		t.Fatal("a group declaring no entries was refused, want it skipped")
	}
	if b.refusal != nil {
		t.Errorf("an empty declared group was reported: %v", b.refusal)
	}
}
