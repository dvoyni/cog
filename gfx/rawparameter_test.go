package gfx

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/dvoyni/cog/m"
)

type wellPacked struct {
	Amount  m.Vec4
	Warp    m.Mat4
	Scalars m.Vec4
}

// badlyPacked is the counterexample the layout check exists for: Go aligns it to
// 4 and lays it out in 16 bytes, WGSL aligns the vec3 to 16 and lays it out in
// 32. It passes any size % 16 check and reads the wrong memory from the second
// element on.
type badlyPacked struct {
	A float32
	B m.Vec3
}

func TestRawParameterCopiesTheValueBytes(t *testing.T) {
	value := wellPacked{Amount: m.Vec4{X: 1, Y: 2, Z: 3, W: 4}}
	param := RawParameter("record", value)
	bytes, ok := param.AppendValue(nil)
	if !ok {
		t.Fatal("a raw parameter reported no value")
	}
	if len(bytes) != 96 {
		t.Fatalf("raw value = %d bytes, want 96", len(bytes))
	}
	for i, want := range []float32{1, 2, 3, 4} {
		if got := math.Float32frombits(binary.LittleEndian.Uint32(bytes[i*4:])); got != want {
			t.Fatalf("raw value word %d = %v, want %v", i, got, want)
		}
	}
}

func TestRawParameterPanicsOnAGoLayoutWGSLDoesNotShare(t *testing.T) {
	defer func() {
		recovered := recover()
		message, ok := recovered.(string)
		if !ok {
			t.Fatalf("recovered %v, want a layout panic", recovered)
		}
		// The message has to name the field and both offsets, because the
		// author's next move is inserting the padding it reports.
		for _, want := range []string{"field B", "Go offset 4", "WGSL offset 16", "padding"} {
			if !strings.Contains(message, want) {
				t.Fatalf("panic %q does not mention %q", message, want)
			}
		}
	}()
	RawParameter("record", badlyPacked{})
	t.Fatal("a badly packed struct did not panic")
}

func TestRawParameterRejectsAMemberThatIsNotShaderData(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a pointer member did not panic")
		}
	}()
	type withPointer struct {
		A m.Vec4
		B *float32
	}
	RawParameter("record", withPointer{})
}

func TestRawParameterAcceptsArraysAtTheirWGSLStride(t *testing.T) {
	type sixteen struct{ Values [4]m.Vec4 }
	if bytes, _ := RawParameter("record", sixteen{}).AppendValue(nil); len(bytes) != 64 {
		t.Fatalf("array record = %d bytes, want 64", len(bytes))
	}
}

func TestFingerprintParamsSeparatesValuesAndIgnoresBacking(t *testing.T) {
	one := []ParameterDescr{FloatParam("fade", 0.25), ColorParam("tint", m.Color{R: 1, A: 1})}
	same := []ParameterDescr{FloatParam("fade", 0.25), ColorParam("tint", m.Color{R: 1, A: 1})}
	if FingerprintParams(one) != FingerprintParams(same) {
		t.Fatal("equal parameter slices in different backings fingerprinted differently")
	}
	different := []ParameterDescr{FloatParam("fade", 0.5), ColorParam("tint", m.Color{R: 1, A: 1})}
	if FingerprintParams(one) == FingerprintParams(different) {
		t.Fatal("a changed value did not change the fingerprint")
	}
	reordered := []ParameterDescr{one[1], one[0]}
	if FingerprintParams(one) == FingerprintParams(reordered) {
		t.Fatal("reordered parameters fingerprinted the same")
	}
	// A kind change with the same name is the case a hand-written comparison
	// misses, and it is the one that merges two draws that differ.
	kindChanged := []ParameterDescr{VecParam("fade", m.Vec4{}), one[1]}
	if FingerprintParams(one) == FingerprintParams(kindChanged) {
		t.Fatal("a changed kind did not change the fingerprint")
	}
	if FingerprintParams(nil) != FingerprintParams([]ParameterDescr{}) {
		t.Fatal("nil and empty parameter slices fingerprinted differently")
	}
}
