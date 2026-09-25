package internal

import (
	"errors"
	"strings"
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/bundles/mcp"
)

func TestGfxOffersItsTwoCapabilitiesAsReadOnly(t *testing.T) {
	offered := provider{}.Capabilities()
	if len(offered) != 2 {
		t.Fatalf("capabilities = %d, want the two gfx implements", len(offered))
	}
	want := []struct {
		name, description string
	}{
		{captureName, captureDescription},
		{frameName, frameDescription},
	}
	for i, expected := range want {
		capability := offered[i]
		if err := capability.Err(); err != nil {
			t.Fatalf("%s failed construction: %v", expected.name, err)
		}
		if capability.Name() != expected.name {
			t.Fatalf("name = %q, want %q", capability.Name(), expected.name)
		}
		if !capability.ReadOnly() {
			t.Errorf("%s changes nothing in the game, and is read-only in cog's reading", expected.name)
		}
		if capability.Description() != expected.description {
			t.Errorf("the description an agent reads for %s is not the one the spec reproduces",
				expected.name)
		}
	}
}

func TestDepthAndOtherFormatsAreNotAnImage(t *testing.T) {
	for _, format := range []descriptors.TextureFormat{descriptors.FormatDepth32F, descriptors.TextureFormat(99)} {
		capture := Capture{
			Pixels: make([]byte, 256), Width: 2, Height: 2, Format: format, BytesPerRow: 256,
		}
		if capture.Image() != nil {
			t.Fatalf("%s produced an image; only 8-bit RGBA can be one", format.String())
		}
	}
	words := captureRefusal(ErrCaptureUnsupported{Format: descriptors.FormatDepth32F}, 1, 1)
	var unavailable mcp.Unavailable
	if !errors.As(words, &unavailable) {
		t.Fatalf("depth refusal = %T, want words an agent can act on", words)
	}
	if !strings.Contains(unavailable.Reason, "depth") {
		t.Fatalf("depth refusal = %q, want it to name what was refused", unavailable.Reason)
	}
}

// A refusal reaches an agent as words it can act on rather than as a fault.
func TestACaptureRefusalReadsAsWords(t *testing.T) {
	refusal := captureRefusal(types.ErrCaptureBusy{}, 1, 1)
	var unavailable mcp.Unavailable
	if !errors.As(refusal, &unavailable) {
		t.Fatalf("refusal = %T, want words an agent can act on", refusal)
	}
}
