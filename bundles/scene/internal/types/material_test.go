package types

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// A model material's key is taken from the fingerprint its load recorded, and
// it is the key MaterialKeyOf gives the forward material wrapped around the same
// descr, whether that wrapper spells the forward tag or leaves it zero. Keying
// from the load therefore changes what the flush pays and never what batches.
func TestAForwardKeyFromAFingerprintIsTheKeyOfTheWrappedMaterial(t *testing.T) {
	descr := gfx.MaterialWithState(gfx.ShaderWithResource("builtin/test.wgsl"), gfx.StateOpaque3D(),
		gfx.ColorParam("tint", m.Color{R: 1, A: 1}))
	got := ForwardMaterialKey(descr.Fingerprint())
	if want := MaterialKeyOf(Material{{Tag: TagForward, Descr: descr}}); got != want {
		t.Errorf("forward key = %x, want the wrapped material's %x", got, want)
	}
	if want := MaterialKeyOf(Material{{Descr: descr}}); got != want {
		t.Errorf("forward key = %x, want the untagged material's %x", got, want)
	}
	other := gfx.MaterialWithState(gfx.ShaderWithResource("builtin/test.wgsl"), gfx.StateOpaque3D(),
		gfx.ColorParam("tint", m.Color{G: 1, A: 1}))
	if ForwardMaterialKey(other.Fingerprint()) == got {
		t.Error("two forward materials differing in a parameter share a key")
	}
}
