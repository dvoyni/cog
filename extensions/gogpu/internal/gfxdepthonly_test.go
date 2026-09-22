package internal

import (
	"testing"

	cgogpu "github.com/dvoyni/cog/extensions/gogpu"
	"github.com/dvoyni/cog/slots/gfx"
)

func TestOnlyAPassWithDepthAndNoColourIsDeclined(t *testing.T) {
	// The shape matters in both directions. Declining too much would drop a
	// screen pass; declining too little leaves the fault in place, and the
	// fault is a segfault several frames inside the driver rather than an
	// error anything can catch.
	cases := []struct {
		name string
		desc gfx.PassDesc
		want bool
	}{
		{"the screen", gfx.PassDesc{Screen: true, DepthAuto: true}, false},
		{"a texture target with pooled depth", gfx.PassDesc{Target: 7, DepthAuto: true}, false},
		{"a texture target with its own depth", gfx.PassDesc{Target: 7, Depth: 9}, false},
		{"a colour pass with no depth", gfx.PassDesc{Target: 7}, false},
		{"a depth-only pass", gfx.PassDesc{NoColor: true, Depth: 9}, true},
		{"a depth-only pass on the pooled texture", gfx.PassDesc{NoColor: true, DepthAuto: true}, true},
		{"no attachments at all", gfx.PassDesc{NoColor: true}, false},
		// The case NoColor exists for: a temporary target on its first frame
		// has no view yet, so its id is zero exactly as a colourless pass's is.
		// It is a colour pass with nothing to render into, not a depth-only
		// pass, and reporting it would fire on the first frame of every app
		// that uses a render target.
		{"a texture target whose view is not created yet", gfx.PassDesc{DepthAuto: true}, false},
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
	err := cgogpu.ErrDepthOnlyPassUnsupported{Pass: "scene.camera-200.depth", Backend: "Pure Go (GLES)"}
	text := err.Error()
	for _, want := range []string{"scene.camera-200.depth", "Pure Go (GLES)", "skipped", "untouched"} {
		if !contains(text, want) {
			t.Errorf("the refusal does not mention %q: %s", want, text)
		}
	}
}

func TestOnlyABackendKnownToEncodeADepthOnlyPassIsAllowedOne(t *testing.T) {
	// This replaced a build-tag constant, and the reason is the interesting
	// part: a native build can select Vulkan or GLES, gogpu/wgpu#353 fixed only
	// Vulkan, and the GLES HAL fails silently rather than faulting - it binds no
	// framebuffer and draws into whatever was bound last. So an unrecognised
	// backend must come back false. Refusing a pass that would have worked
	// reports itself; encoding one that does not is a wrong picture nobody sees.
	cases := []struct {
		backend string
		want    bool
	}{
		{"Pure Go (Vulkan)", true},
		{"Browser WebGPU", true},
		{"Pure Go (GLES)", false},
		{"Pure Go (GL)", false},
		{"Pure Go (Software)", false},
		{"Pure Go (Metal)", false},
		{"Pure Go (DX12)", false},
		// The name gogpu reports before an adapter has been selected, and the
		// one it reports for a HAL nobody here has tried.
		{"Pure Go (Auto)", false},
		{"", false},
	}
	for _, c := range cases {
		if got := depthOnlyPassesWork(c.backend); got != c.want {
			t.Errorf("depthOnlyPassesWork(%q) = %v, want %v", c.backend, got, c.want)
		}
	}
}

func TestADepthOnlyPassBeginsWithoutAColourAttachment(t *testing.T) {
	// The refusal was lifted for Vulkan, but a second guard behind it still
	// skipped every pass that resolved no colour view - and a depth-only pass
	// never resolves one. So on a backend allowed the pass it was dropped
	// without a report: no clear, no depth written, and a later pass loading
	// that texture rendered against whatever was in it. Only a missing colour
	// view on a pass that declared colour is a pass with nothing to render
	// into; a pass that declared none needs only its depth.
	cases := []struct {
		name          string
		desc          gfx.PassDesc
		colour, depth bool
		want          bool
	}{
		{"a colour pass", gfx.PassDesc{Target: 7, DepthAuto: true}, true, true, true},
		{"a colour pass with no depth", gfx.PassDesc{Target: 7}, true, false, true},
		{"a depth-only pass", gfx.PassDesc{NoColor: true, Depth: 9}, false, true, true},
		// A temporary depth texture has no view on its first frame either.
		{"a depth-only pass whose depth view is not created yet", gfx.PassDesc{NoColor: true, Depth: 9}, false, false, false},
		// Pooled depth takes its size from the colour target, and there is none.
		{"a depth-only pass on the pooled texture", gfx.PassDesc{NoColor: true, DepthAuto: true}, false, false, false},
		{"a colour target whose view is not created yet", gfx.PassDesc{DepthAuto: true}, false, true, false},
		{"no attachments at all", gfx.PassDesc{NoColor: true}, false, false, false},
	}
	for _, c := range cases {
		if got := passBegins(c.desc, c.colour, c.depth); got != c.want {
			t.Errorf("%s: passBegins = %v, want %v", c.name, got, c.want)
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
