package gfx_test

import (
	"strings"
	"testing"

	"github.com/dvoyni/cog/gfx"
)

// Nothing recovers from a preprocessor error, so it reads as one printed line
// naming the shader and every location.
func TestErrShaderSourceReadsAsOneLine(t *testing.T) {
	err := gfx.ErrShaderSource{
		Shader:  "scene.wgsl [SKIN]",
		At:      []gfx.ShaderSourceLocation{{Source: "a.wgsl", Line: 4}, {Source: "b.wgsl", Line: 9}},
		Message: "#const \"N\" is declared twice",
	}
	got := err.Error()
	if strings.Contains(got, "\n") {
		t.Fatalf("Error() spans lines: %q", got)
	}
	for _, want := range []string{"scene.wgsl [SKIN]", "a.wgsl:4", "b.wgsl:9", "declared twice"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, missing %q", got, want)
		}
	}
}
