package types

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// The defaults are glTF's own, so a material that names nothing renders as
// glTF says an empty material renders: white, fully rough, fully metallic, with
// every texture slot multiplying through unchanged.
func TestThePbrValuesDefaultToGltfsOwnDefaults(t *testing.T) {
	record := defaultPbrValues()
	if record.baseColorFactor != (m.Vec4{X: 1, Y: 1, Z: 1, W: 1}) {
		t.Errorf("baseColorFactor is %v, want white", record.baseColorFactor)
	}
	if record.emissiveFactor != (m.Vec4{}) {
		t.Errorf("emissiveFactor is %v, want black", record.emissiveFactor)
	}
	if record.metallicFactor != 1 || record.roughnessFactor != 1 {
		t.Errorf("metallic/roughness are %v/%v, want 1/1", record.metallicFactor, record.roughnessFactor)
	}
	if record.normalScale != 1 || record.occlusionStrength != 1 {
		t.Errorf("normalScale/occlusionStrength are %v/%v, want 1/1", record.normalScale, record.occlusionStrength)
	}
	// Zero rather than glTF's 0.5, because the discard is unconditional in the
	// one bundled module: an opaque material must cut nothing away, and alpha
	// is never below zero. A MASK material sets its own cutoff.
	if record.alphaCutoff != 0 {
		t.Errorf("alphaCutoff is %v, want 0 so the discard is a no-op for an opaque material", record.alphaCutoff)
	}
	for slot, transform := range record.transforms {
		if transform != (m.Vec4{Z: 1, W: 1}) {
			t.Errorf("slot %d transform is %v, want zero offset and unit scale", slot, transform)
		}
	}
	if record.uvSets != 0 {
		t.Errorf("uvSets is %#b, want every slot on TEXCOORD_0", record.uvSets)
	}
}

// UV sets are capped at two, and the selector is one bit per slot.
func TestUVSetSelectionIsOneBitPerSlotAndCapsAtTwo(t *testing.T) {
	record := defaultPbrValues()
	var reported []error
	report := func(err error) { reported = append(reported, err) }

	record.selectUVSet(report, NormalSlot, 1)
	if record.uvSets != 1<<NormalSlot {
		t.Fatalf("uvSets is %#b, want only the normal slot on TEXCOORD_1", record.uvSets)
	}
	if len(reported) != 0 {
		t.Fatalf("selecting TEXCOORD_1 reported %v", reported)
	}

	record.selectUVSet(report, 0, 2)
	if record.uvSets&1 != 0 {
		t.Fatalf("uvSets is %#b, want the out-of-range slot back on TEXCOORD_0", record.uvSets)
	}
	if len(reported) != 1 {
		t.Fatalf("a texCoord past the cap reported %d errors, want 1: %v", len(reported), reported)
	}
	var unsupported ErrTextureUVSetUnsupported
	if !errors.As(reported[0], &unsupported) || unsupported.TexCoord != 2 {
		t.Fatalf("reported %v, want ErrTextureUVSetUnsupported for TEXCOORD_2", reported[0])
	}
}
