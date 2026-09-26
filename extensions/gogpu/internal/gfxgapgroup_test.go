//go:build !js

package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx"
)

// On a device, a shader whose groups skip an index gets a layout for the gap,
// and the empty bind group flushBinds sets there is one the device creates.
func TestAGapGroupGetsAnEmptyLayoutAndBindGroupOnTheDevice(t *testing.T) {
	b := newNoopGfxBackend(t)
	shader := b.ReserveShader()
	if err := b.CreateShader(shader, gfx.ShaderDesc{Label: "gap.wgsl", Code: []byte(gapGroupWGSL)}); err != nil {
		t.Fatalf("CreateShader: %v", err)
	}
	compiled := b.shaders[shader]
	if len(compiled.bgLayouts) != 3 || compiled.bgLayouts[1] == nil {
		t.Fatalf("layouts = %v, want three with the gap at group 1 filled", compiled.bgLayouts)
	}
	if compiled.declaredEntries(1) != 0 {
		t.Fatalf("the gap declares %d entries, want none", compiled.declaredEntries(1))
	}
	if b.bindGroups.get(compiled, 1, nil) == nil {
		t.Fatal("the device refused an empty bind group over the gap's layout")
	}
}

const gapGroupWGSL = `
@group(0) @binding(0) var<uniform> frame: vec4<f32>;
@group(2) @binding(0) var<uniform> tint: vec4<f32>;

@vertex fn vs_main(@location(0) position: vec3<f32>) -> @builtin(position) vec4<f32> {
	return vec4<f32>(position, 1.0) * frame;
}

@fragment fn fs_main() -> @location(0) vec4<f32> {
	return tint;
}
`
