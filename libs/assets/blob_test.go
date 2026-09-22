package assets_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dvoyni/cog/libs/assets"
)

// The identity rule in one test: the same backing array wrapped twice is one
// Blob, and a fresh copy of the same bytes is another. Nothing here looks at
// what the bytes say.
//
// Every count below goes through a map rather than a == between two calls,
// because the compiler reuses one stack slot across such a comparison and the
// comparison then reports an equality the heap does not have. Only keeping the
// identities alive tells the truth.
func TestBlobIdentityIsTheRunOfBytesNotTheirContents(t *testing.T) {
	source := []byte("shader source")
	copied := []byte("shader source")

	if assets.NewBlob(source) != assets.NewBlob(source) {
		t.Fatal("re-wrapping the same slice produced two identities")
	}
	if got := distinct(assets.NewBlob(source), assets.NewBlob(copied)); got != 2 {
		t.Fatalf("two backing arrays holding the same bytes are %d identities, want 2", got)
	}
}

// distinct counts how many identities a set of Blobs holds between them. It is
// a map rather than a chain of ==, so the values are live at once and no two of
// them can share a stack slot.
func distinct(blobs ...assets.Blob) int {
	seen := make(map[assets.Blob]struct{}, len(blobs))
	for _, blob := range blobs {
		seen[blob] = struct{}{}
	}
	return len(seen)
}

// Blob{} is the canonical empty, which is what makes a path-named Descr legal:
// every empty compares equal to every other, and reading one back is nil rather
// than a panic.
func TestEmptyBlobsAreOneIdentity(t *testing.T) {
	empties := []assets.Blob{
		{},
		assets.NewBlob(nil),
		assets.NewBlob([]byte{}),
		assets.NewBlobFromString(""),
	}
	for i, blob := range empties {
		if blob != (assets.Blob{}) {
			t.Fatalf("empty %d is not Blob{}", i)
		}
		if blob.Data() != nil {
			t.Fatalf("empty %d yielded %v, want nil", i, blob.Data())
		}
		if blob.String() != "" {
			t.Fatalf("empty %d yielded %q, want the empty string", i, blob.String())
		}
		if blob.Len() != 0 {
			t.Fatalf("empty %d has length %d", i, blob.Len())
		}
	}
}

// Data and String hand the same run back out, in either direction, without a
// copy - so a Blob built from bytes reads as text and one built from a literal
// reads as bytes.
func TestBlobReadsBackOutBothWays(t *testing.T) {
	const literal = "// custom"

	fromBytes := assets.NewBlob([]byte(literal))
	if got := fromBytes.String(); got != literal {
		t.Fatalf("String() = %q, want %q", got, literal)
	}
	if got := fromBytes.Len(); got != len(literal) {
		t.Fatalf("Len() = %d, want %d", got, len(literal))
	}

	fromString := assets.NewBlobFromString(literal)
	if got := string(fromString.Data()); got != literal {
		t.Fatalf("Data() = %q, want %q", got, literal)
	}
	if got := fromString.Len(); got != len(literal) {
		t.Fatalf("Len() = %d, want %d", got, len(literal))
	}
}

// A string literal resolves to one rodata address every evaluation, so the
// built once and kept rule is satisfied by an ordinary Go constant with no var
// ceremony. A []byte conversion of that same literal is a fresh array each time.
func TestAStringLiteralIsOneIdentityAndItsByteConversionIsNot(t *testing.T) {
	const literal = "// custom"

	wraps := make([]assets.Blob, 0, 5)
	conversions := make([]assets.Blob, 0, 5)
	computed := make([]assets.Blob, 0, 5)
	for range 5 {
		wraps = append(wraps, assets.NewBlobFromString(literal))
		conversions = append(conversions, assets.NewBlob([]byte(literal)))
		computed = append(computed, assets.NewBlobFromString(strings.Clone(literal)))
	}

	if got := distinct(wraps...); got != 1 {
		t.Fatalf("five wraps of one string literal are %d identities, want 1", got)
	}
	if got := distinct(conversions...); got != 5 {
		t.Fatalf("five []byte conversions of that literal are %d identities, want 5", got)
	}
	if got := distinct(computed...); got != 5 {
		t.Fatalf("five computed strings are %d identities, want 5", got)
	}
}

// A Blob crosses JSON as its length and never its bytes: it may be
// texture-sized, and whoever encodes it may be holding a lock every System
// waits on.
func TestABlobEncodesAsItsLengthAlone(t *testing.T) {
	encoded, err := json.Marshal(assets.Blob{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := string(encoded), `{"len":0}`; got != want {
		t.Errorf("Blob{} encoded as %s, want %s", got, want)
	}
	encoded, err = json.Marshal(struct{ Pixels assets.Blob }{assets.NewBlobFromString("secretpixels")})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := string(encoded), `{"Pixels":{"len":12}}`; got != want {
		t.Errorf("a populated Blob encoded as %s, want %s", got, want)
	}
	if strings.Contains(string(encoded), "secret") {
		t.Errorf("the encoding %s carries the bytes", encoded)
	}
}
