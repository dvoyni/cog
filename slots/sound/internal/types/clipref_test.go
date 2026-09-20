package types

import (
	"testing"

	"github.com/dvoyni/cog/libs/assets"
)

// Equal is Clip identity rather than field identity, which is what makes it a
// method worth having: a Clip with a path is named by that path, so two refs
// spelling one path are one Clip - the Library's own key rule, met where a
// caller meets it.
func TestEqualIsClipIdentity(t *testing.T) {
	bytes := assets.NewBlobFromString("ogg bytes")
	other := assets.NewBlobFromString("other bytes")

	cases := []struct {
		name  string
		left  ClipRef
		right ClipRef
		want  bool
	}{
		{"one path twice", ClipWithResource("bell.ogg"), ClipWithResource("bell.ogg"), true},
		{"two paths", ClipWithResource("bell.ogg"), ClipWithResource("drum.ogg"), false},
		{"one run of bytes twice", ClipWithBytes(bytes), ClipWithBytes(bytes), true},
		{"two runs of bytes", ClipWithBytes(bytes), ClipWithBytes(other), false},
		{"a path against bytes", ClipWithResource("bell.ogg"), ClipWithBytes(bytes), false},
		{"two zero refs", ClipRef{}, ClipRef{}, true},
	}
	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			if got := each.left.Equal(each.right); got != each.want {
				t.Fatalf("Equal = %v, want %v", got, each.want)
			}
			if got := each.right.Equal(each.left); got != each.want {
				t.Fatalf("Equal the other way round = %v, want %v", got, each.want)
			}
		})
	}
}

// The same bytes in two allocations are two Blobs and therefore two Clips,
// which is assets.Blob's identity rule reaching all the way up to a ClipRef.
//
// Both refs are kept alive in a slice rather than compared straight out of two
// calls, because the compiler reuses one stack slot across such a comparison
// and the comparison then reports an equality the heap does not have. This is
// the shape libs/assets' own identity test uses, for the same reason.
func TestBytesAreIdentifiedByTheirRunAndNotTheirContents(t *testing.T) {
	refs := make([]ClipRef, 0, 2)
	for range 2 {
		refs = append(refs, ClipWithBytes(assets.NewBlob([]byte("ogg"))))
	}

	if refs[0].Equal(refs[1]) {
		t.Fatal("two allocations holding the same bytes named one Clip")
	}
}
