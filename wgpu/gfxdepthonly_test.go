package wgpu

import (
	"testing"

	cgfx "github.com/dvoyni/cog/gfx"
)

func TestOnlyAPassWithDepthAndNoColourIsDeclined(t *testing.T) {
	// The shape matters in both directions. Declining too much would drop a
	// screen pass; declining too little leaves the fault in place, and the
	// fault is a segfault several frames inside the driver rather than an
	// error anything can catch.
	cases := []struct {
		name string
		desc cgfx.GpuPassDesc
		want bool
	}{
		{"the screen", cgfx.GpuPassDesc{Screen: true, DepthAuto: true}, false},
		{"a texture target with pooled depth", cgfx.GpuPassDesc{Target: 7, DepthAuto: true}, false},
		{"a texture target with its own depth", cgfx.GpuPassDesc{Target: 7, Depth: 9}, false},
		{"a colour pass with no depth", cgfx.GpuPassDesc{Target: 7}, false},
		{"a depth-only pass", cgfx.GpuPassDesc{NoColor: true, Depth: 9}, true},
		{"a depth-only pass on the pooled texture", cgfx.GpuPassDesc{NoColor: true, DepthAuto: true}, true},
		{"no attachments at all", cgfx.GpuPassDesc{NoColor: true}, false},
		// The case NoColor exists for: a temporary target on its first frame
		// has no view yet, so its id is zero exactly as a colourless pass's is.
		// It is a colour pass with nothing to render into, not a depth-only
		// pass, and reporting it would fire on the first frame of every app
		// that uses a render target.
		{"a texture target whose view is not created yet", cgfx.GpuPassDesc{DepthAuto: true}, false},
	}
	for _, c := range cases {
		if got := isDepthOnly(c.desc); got != c.want {
			t.Errorf("%s: isDepthOnly = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTheRefusalNamesThePassAndSaysWhatWasSkipped(t *testing.T) {
	// A caller reading this in a log has to be able to tell which pass went
	// missing and that its depth texture was left as it found it, because the
	// visible symptom is a later pass rendering against undefined depth rather
	// than anything about the pass that was skipped.
	err := ErrDepthOnlyPassUnsupported{Pass: "scene.camera-200.depth"}
	text := err.Error()
	for _, want := range []string{"scene.camera-200.depth", "skipped", "untouched"} {
		if !contains(text, want) {
			t.Errorf("the refusal does not mention %q: %s", want, text)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
