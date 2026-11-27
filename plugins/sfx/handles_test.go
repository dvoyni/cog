package sfx_test

import (
	"testing"

	"github.com/dvoyni/cog/plugins/sfx"
)

func TestVoiceRendersBothHalves(t *testing.T) {
	if got := sfx.NoVoice.String(); got != "NoVoice" {
		t.Fatalf("NoVoice renders %s", got)
	}
	if got := sfx.Voice(2<<32 | 7).String(); got != "Voice(7v2)" {
		t.Fatalf("index 7 at generation 2 renders %s, want Voice(7v2)", got)
	}
}
