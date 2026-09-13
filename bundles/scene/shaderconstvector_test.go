package scene

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/spirv"
	"github.com/gogpu/naga/wgsl"
)

// dielectricF0 is the normal-incidence reflectance the BRDF lerps toward the
// base colour with metallic. Every dielectric in the 3D path reflects this
// much, so a zero here is a picture-wide bug that renders as plausibly-dark.
const dielectricF0 = 0.04

// The Vulkan path is the one that can lose this value. naga's SPIR-V backend
// dropped a module-scope vector used as an expression operand and handed the
// shader (0, 0, 0) — no parse error, no validation error, no warning — so a
// zeroed F0 is indistinguishable from a deliberately dark material.
//
// SCENE_DIELECTRIC_F0 is declared in exactly that form, which is safe only
// because go.mod overrides naga with the fork carrying the fix
// (gogpu/naga#92). This test is what stands between that override and the
// picture: drop the replace directive before a fixed naga is released and it
// fails here, loudly, rather than in a frame nobody can read.
func TestTheDielectricF0ReachesTheSPIRVBinary(t *testing.T) {
	text := flattenedSceneShader(t)

	parsed, err := naga.Parse(text)
	if err != nil {
		t.Fatalf("parse the bundled scene shader: %v", err)
	}
	module, err := wgsl.Lower(parsed)
	if err != nil {
		t.Fatalf("lower the bundled scene shader: %v", err)
	}
	blob, err := naga.GenerateSPIRV(module, spirv.Options{})
	if err != nil {
		t.Fatalf("generate SPIR-V for the bundled scene shader: %v", err)
	}

	want := math.Float32bits(dielectricF0)
	for i := 0; i+4 <= len(blob); i += 4 {
		if binary.LittleEndian.Uint32(blob[i:]) == want {
			return
		}
	}
	t.Errorf("the SPIR-V binary carries no %v, so every dielectric reflects nothing. "+
		"If go.mod no longer overrides naga, that is why: the fix for a module-scope "+
		"vector used as an operand (gogpu/naga#92) has not shipped in a release yet",
		dielectricF0)
}
