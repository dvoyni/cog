package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx"
)

func TestOnlyAPassWithDepthAndNoColourIsDeclined(t *testing.T) {
	// The shape matters in both directions. Declining too much would drop a
	// screen pass; declining too little leaves the fault in place, and the
	// fault is a segfault several frames inside the driver rather than an
	// error anything can catch.
	cases := []struct {
		name string
		desc gfx.PassDescr
		want bool
	}{
		{"the screen", pass(screen, auto), false},
		{"a texture target with pooled depth", pass(texture, auto), false},
		{"a texture target with its own depth", pass(texture, depthTexture), false},
		{"a colour pass with no depth", pass(texture, noDepth), false},
		{"a depth-only pass", pass(noColour, depthTexture), true},
		{"a depth-only pass on the pooled texture", pass(noColour, auto), true},
		{"no attachments at all", pass(noColour, noDepth), false},
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
	err := ErrDepthOnlyPassUnsupported{Pass: "scene.camera-200.depth", Backend: "Pure Go (GLES)"}
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
		desc          gfx.PassDescr
		colour, depth bool
		want          bool
	}{
		{"a colour pass", pass(texture, auto), true, true, true},
		{"a colour pass with no depth", pass(texture, noDepth), true, false, true},
		{"a depth-only pass", pass(noColour, depthTexture), false, true, true},
		// A depth texture the backend does not hold resolves no view.
		{"a depth-only pass whose depth view does not resolve", pass(noColour, depthTexture), false, false, false},
		// Pooled depth takes its size from the colour target, and there is none.
		{"a depth-only pass on the pooled texture", pass(noColour, auto), false, false, false},
		{"a colour target whose view does not resolve", pass(texture, auto), false, true, false},
		{"no attachments at all", pass(noColour, noDepth), false, false, false},
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

// The attachments the tables combine: which a pass declares is all isDepthOnly
// and passBegins read.
var (
	screen       = gfx.TargetDescrScreen()
	texture      = gfx.TargetDescr{Kind: gfx.TargetTexture, Texture: 7}
	noColour     = gfx.TargetDescrNone()
	auto         = gfx.DepthDescrAuto()
	depthTexture = gfx.DepthDescr{Kind: gfx.DepthKindTexture, Texture: 9}
	noDepth      = gfx.DepthDescrNone()
)

func pass(target gfx.TargetDescr, depth gfx.DepthDescr) gfx.PassDescr {
	return gfx.PassDescr{Target: target, Depth: depth}
}
