package scene

import (
	"encoding/binary"
	"io/fs"
	"math"
	"regexp"
	"strings"
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
// drops a module-scope vector used as an expression operand and hands the
// shader (0, 0, 0) — no parse error, no validation error, no warning — so a
// zeroed F0 is indistinguishable from a deliberately dark material. The bundled
// shader therefore holds F0 in a function-local let, and this pins that it
// survives all the way into the binary.
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
	t.Errorf("the SPIR-V binary carries no %v, so every dielectric reflects nothing", dielectricF0)
}

// moduleScopeVector matches a module-scope `const` or `var<private>` holding a
// vector or matrix — the declaration form the SPIR-V backend loses.
var moduleScopeVector = regexp.MustCompile(`(?m)^\s*(const|var<private>)\s+\w+\s*:\s*(vec|mat)`)

// A module-scope vector is the trap, not the value: the next one written into
// these sources would be zeroed just as quietly. Guard the whole bundled shader
// rather than the one declaration that was found the hard way.
func TestTheBundledShaderHoldsNoModuleScopeVector(t *testing.T) {
	names, err := fs.Glob(shaderFS, "builtin/scene/*.wgsl")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, name := range names {
		source, err := shaderFS.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, line := range moduleScopeVector.FindAllString(string(source), -1) {
			t.Errorf("%s declares %q at module scope; naga's SPIR-V backend zeroes it "+
				"when it is used as an operand — hold it in a function-local let instead",
				name, strings.TrimSpace(line))
		}
	}
}
