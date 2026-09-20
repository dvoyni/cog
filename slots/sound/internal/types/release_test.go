package types

import (
	"testing"

	"github.com/dvoyni/cog/libs/assets"
)

// A release keys on Clip identity and not on field identity. A ref with a path
// is named by that path alone, whatever bytes it carries beside it, which is
// the Library's own key rule and the rule sound's own clip table is spelled
// with - so the two must not disagree about which Voice a release cuts.
//
// This is the one case that separates the two, and it is not constructible from
// the root's surface: ClipWithResource sets a name and ClipWithBytes sets a
// blob, so only here can a ref carry both. A stopClip written with == would
// leave this Voice playing a Clip that has been destroyed.
func TestAReleaseCutsOnClipIdentityRatherThanFieldIdentity(t *testing.T) {
	bytes := assets.NewBlobFromString("ogg bytes the play carried beside its path")
	carrying := ClipRef{name: "bell.ogg", blob: bytes}
	named := ClipWithResource("bell.ogg")

	if carrying == named {
		t.Fatal("the two refs compare equal as fields, so this test proves nothing")
	}

	voices := NewVoices(2)
	var endings []Ending
	voices.start(Operation{Kind: OpPlay, Voice: newVoice(0, 1), Clip: carrying}, clipFacts{}, &endings)
	voices.start(Operation{Kind: OpPlay, Voice: newVoice(1, 1), Clip: ClipWithResource("drum.ogg")}, clipFacts{}, &endings)

	voices.stopClip(named, &endings)

	if len(endings) != 1 {
		t.Fatalf("a release of bell.ogg ended %d Voices, want the one playing it", len(endings))
	}
	if endings[0].Reason != ReasonReleased {
		t.Fatalf("the released Voice ended as %v, want released", endings[0].Reason)
	}
	if voices.Len() != 1 {
		t.Fatalf("%d Voices are live, want the one on the Clip nothing released", voices.Len())
	}
}
