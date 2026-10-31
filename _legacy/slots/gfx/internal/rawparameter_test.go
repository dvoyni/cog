package internal

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/libs/m"
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
	param := types.ShaderParameterRaw("record", value)
	bytes, ok := param.AppendValueTo(nil)
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
	types.ShaderParameterRaw("record", badlyPacked{})
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
	types.ShaderParameterRaw("record", withPointer{})
}

func TestRawParameterAcceptsArraysAtTheirWGSLStride(t *testing.T) {
	type sixteen struct{ Values [4]m.Vec4 }
	if bytes, _ := types.ShaderParameterRaw("record", sixteen{}).AppendValueTo(nil); len(bytes) != 64 {
		t.Fatalf("array record = %d bytes, want 64", len(bytes))
	}
}

func TestFingerprintParamsSeparatesValuesAndIgnoresBacking(t *testing.T) {
	one := []types.ShaderParameterDescr{types.ShaderParameterFloat("fade", 0.25), types.ShaderParameterColor("tint", m.Color{R: 1, A: 1})}
	same := []types.ShaderParameterDescr{types.ShaderParameterFloat("fade", 0.25), types.ShaderParameterColor("tint", m.Color{R: 1, A: 1})}
	if FingerprintParams(one) != FingerprintParams(same) {
		t.Fatal("equal parameter slices in different backings fingerprinted differently")
	}
	different := []types.ShaderParameterDescr{types.ShaderParameterFloat("fade", 0.5), types.ShaderParameterColor("tint", m.Color{R: 1, A: 1})}
	if FingerprintParams(one) == FingerprintParams(different) {
		t.Fatal("a changed value did not change the fingerprint")
	}
	reordered := []types.ShaderParameterDescr{one[1], one[0]}
	if FingerprintParams(one) == FingerprintParams(reordered) {
		t.Fatal("reordered parameters fingerprinted the same")
	}
	// A kind change with the same name is the case a hand-written comparison
	// misses, and it is the one that merges two draws that differ.
	kindChanged := []types.ShaderParameterDescr{types.ShaderParameterVec4("fade", m.Vec4{}), one[1]}
	if FingerprintParams(one) == FingerprintParams(kindChanged) {
		t.Fatal("a changed kind did not change the fingerprint")
	}
	if FingerprintParams(nil) != FingerprintParams([]types.ShaderParameterDescr{}) {
		t.Fatal("nil and empty parameter slices fingerprinted differently")
	}
}

// A record past the inline size is borrowed, not copied: the descriptor reads
// the caller's value, and building one per batch allocates nothing.
func TestRawParameterRefBorrowsALargeRecord(t *testing.T) {
	value := &wellPacked{Amount: m.Vec4{X: 1}}
	param := types.ShaderParameterRawRef("record", value)
	value.Amount.X = 7
	bytes, _ := param.AppendValueTo(nil)
	if len(bytes) != 96 || math.Float32frombits(binary.LittleEndian.Uint32(bytes)) != 7 {
		t.Fatalf("borrowed record = %d bytes starting %v, want 96 reading the caller's 7", len(bytes), bytes[:4])
	}
	if allocs := testing.AllocsPerRun(20, func() { param = types.ShaderParameterRawRef("record", value) }); allocs != 0 {
		t.Fatalf("ShaderParameterRawRef allocated %v times, want 0", allocs)
	}
	// A record that fits is carried inline, as ShaderParameterRaw's is, so its
	// descriptor no longer reads the caller's value.
	small := &struct{ A m.Vec4 }{A: m.Vec4{X: 1}}
	inline := types.ShaderParameterRawRef("small", small)
	small.A.X = 9
	if got, _ := inline.AppendValueTo(nil); math.Float32frombits(binary.LittleEndian.Uint32(got)) != 1 {
		t.Fatalf("inline record read %v, want the 1 it was built from", got[:4])
	}
}
