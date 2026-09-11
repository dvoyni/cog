package scene

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/m"
	"github.com/qmuntal/gltf"
)

// unpack2x16snorm and unpack4x8snorm are WGSL's own builtins written in Go, and
// they exist because nothing in this tree can run WGSL: builtin/scene/morph.wgsl
// is the shipping decoder and what follows is a transcription of it, kept beside
// the encoder it has to invert.
//
// Both are the specification's definition rather than an approximation of it:
// the divisor is the signed maximum, not the power of two beside it, and the
// low code is clamped so that -32768 and -32767 both decode to -1.
func unpack2x16snorm(word uint32) [2]float32 {
	return [2]float32{
		snormComponent(int32(int16(word)), 32767),
		snormComponent(int32(int16(word>>16)), 32767),
	}
}

func unpack4x8snorm(word uint32) [4]float32 {
	var out [4]float32
	for i := range out {
		out[i] = snormComponent(int32(int8(word>>(8*i))), 127)
	}
	return out
}

func snormComponent(code, codeMax int32) float32 {
	return float32(math.Max(float64(code)/float64(codeMax), -1))
}

// morphBlock is one primitive's packed block read back the way the shader reads
// it: by the arithmetic in morph.wgsl, off the raw words, with nothing of the
// packer's own consulted.
type morphBlockView struct {
	words  []uint32
	base   int
	stride int
}

func viewMorphBlock(words []uint32, base int, stride uint32) morphBlockView {
	return morphBlockView{words: words, base: base, stride: int(stride)}
}

// ranges reads the slot ranges the header carries, in the fixed slot order.
func (b morphBlockView) ranges() morphRanges {
	var out morphRanges
	for slot := range b.stride - 1 {
		at := b.base + slot*morphRangeWords
		out[slot] = m.Vec3{
			X: math.Float32frombits(b.words[at]),
			Y: math.Float32frombits(b.words[at+1]),
			Z: math.Float32frombits(b.words[at+2]),
		}
	}
	return out
}

// span reads one target's header: where its records start and the run of
// vertices they cover.
func (b morphBlockView) span(target int) (base int, span morphSpan) {
	at := b.base + (b.stride-1)*morphRangeWords + target*morphTargetHeaderWords
	return int(b.words[at]), morphSpan{first: int(b.words[at+1]), count: int(b.words[at+2])}
}

// delta decodes one target's delta for one vertex, and reports whether the
// target's span covers that vertex at all - which is the one compare the shader
// spends on the 93% of pairs that used to load three weighted zeros.
func (b morphBlockView) delta(target, vertex int) (position, normal, tangent m.Vec3, covered bool) {
	base, span := b.span(target)
	if vertex < span.first || vertex >= span.first+span.count {
		return m.Vec3{}, m.Vec3{}, m.Vec3{}, false
	}
	scale := b.ranges()
	at := base + (vertex-span.first)*b.stride
	xy, z := unpack2x16snorm(b.words[at]), unpack2x16snorm(b.words[at+1])
	position = m.Vec3{X: xy[0] * scale[0].X, Y: xy[1] * scale[0].Y, Z: z[0] * scale[0].Z}
	if b.stride > 2 {
		code := unpack4x8snorm(b.words[at+2])
		normal = m.Vec3{X: code[0] * scale[1].X, Y: code[1] * scale[1].Y, Z: code[2] * scale[1].Z}
	}
	if b.stride > 3 {
		code := unpack4x8snorm(b.words[at+3])
		tangent = m.Vec3{X: code[0] * scale[2].X, Y: code[1] * scale[2].Y, Z: code[2] * scale[2].Z}
	}
	return position, normal, tangent, true
}

// A record is 8 bytes for position, 4 for normal and 4 for tangent, so the
// prefix sums are 8 / 12 / 16 - distinct, which is the whole of why a per-slot
// width does not break the rule that which slots a record holds is recoverable
// from the stride alone. Three widths that summed to a repeat would make a
// stride ambiguous and the mask would have to be transmitted.
func TestTheRecordPrefixSumsStayDistinctUnderPerSlotWidths(t *testing.T) {
	for _, sample := range []struct {
		mask  morphMask
		bytes int
	}{
		{mask: morphPosition, bytes: 8},
		{mask: morphPosition | morphNormal, bytes: 12},
		{mask: morphPosition | morphNormal | morphTangent, bytes: 16},
	} {
		if got := sample.mask.recordWords() * morphWordSize; got != sample.bytes {
			t.Errorf("mask %b records %d bytes, want %d", sample.mask, got, sample.bytes)
		}
	}
	seen := map[int]morphMask{}
	for _, mask := range []morphMask{
		morphPosition, morphPosition | morphNormal, morphPosition | morphNormal | morphTangent,
	} {
		words := mask.recordWords()
		if other, clash := seen[words]; clash {
			t.Errorf("masks %b and %b both stride %d words", other, mask, words)
		}
		seen[words] = mask
	}
}

// A target stores records only for the span of vertices it moves. Everything
// outside that span is not stored at all, and a vertex there costs the shader
// one compare rather than a record load and three weighted zeros.
func TestATargetStoresOnlyItsLiveSpan(t *testing.T) {
	// Eight vertices, moved at 3 and 5: a span of three inside a primitive of
	// eight, which is the shape the vendored stress test has at scale.
	deltas := make([]m.Vec4, 8)
	deltas[3] = m.Vec4{X: 1}
	deltas[5] = m.Vec4{Y: 2}
	words := packMorphBlock(nil, deltas, morphPosition, 1, 8)
	block := viewMorphBlock(words, 0, uint32(morphPosition.recordWords()))
	_, span := block.span(0)
	if span != (morphSpan{first: 3, count: 3}) {
		t.Fatalf("span = %+v, want vertices 3 through 5", span)
	}
	// Three records of two words, behind three range words and one target
	// header. A dense block of the same primitive is eight records.
	if want := morphRangeWords + morphTargetHeaderWords + 3*2; len(words) != want {
		t.Errorf("the block is %d words, want %d", len(words), want)
	}
	for _, vertex := range []int{0, 1, 2, 6, 7} {
		if _, _, _, covered := block.delta(0, vertex); covered {
			t.Errorf("vertex %d is inside the span, want it outside", vertex)
		}
	}
	for _, want := range []struct {
		vertex   int
		position m.Vec3
	}{
		{vertex: 3, position: m.Vec3{X: 1}},
		{vertex: 4},
		{vertex: 5, position: m.Vec3{Y: 2}},
	} {
		position, _, _, covered := block.delta(0, want.vertex)
		if !covered {
			t.Fatalf("vertex %d is outside the span, want it inside", want.vertex)
		}
		if position != want.position {
			t.Errorf("vertex %d = %v, want %v", want.vertex, position, want.position)
		}
	}
}

// A target that moves nothing keeps its slot. Dropping it at load is tempting
// and wrong: MorphWeights is positional over the flattened slot list, so
// removing one silently renumbers every slot after it and a caller's weight
// lands on the wrong shape.
func TestATargetThatMovesNothingKeepsItsSlotAndStoresNoRecords(t *testing.T) {
	deltas := make([]m.Vec4, 3*3)
	deltas[0] = m.Vec4{X: 1}     // target 0 moves vertex 0
	deltas[2*3+2] = m.Vec4{Z: 4} // target 2 moves vertex 2; target 1 moves nothing
	words := packMorphBlock(nil, deltas, morphPosition, 3, 3)
	block := viewMorphBlock(words, 0, uint32(morphPosition.recordWords()))
	if _, span := block.span(1); span.count != 0 {
		t.Errorf("the dead target stores %d records, want none", span.count)
	}
	// The dead target costs its twelve-byte header and nothing else, and the
	// targets after it keep their own numbers.
	if want := morphRangeWords + 3*morphTargetHeaderWords + 2*2; len(words) != want {
		t.Errorf("the block is %d words, want %d", len(words), want)
	}
	position, _, _, covered := block.delta(2, 2)
	if !covered || position != (m.Vec3{Z: 4}) {
		t.Errorf("target 2 vertex 2 = %v (covered %v), want the delta it authored", position, covered)
	}
}

// Absence is expressed by the span, not by a value, so the bits a narrowed slot
// leaves over need no sentinel and stay reserved: 16 in the position slot and 8
// in each direction slot, written as zero and never read.
func TestTheSpareBitsOfEverySlotStayReserved(t *testing.T) {
	mask := morphPosition | morphNormal | morphTangent
	deltas := []m.Vec4{{X: -1, Y: 1, Z: 0.5}, {X: 0.25, Y: -0.75, Z: 1}, {X: 1, Y: 1, Z: -1}}
	words := packMorphBlock(nil, deltas, mask, 1, 1)
	record := len(words) - mask.recordWords()
	if got := words[record+1] >> 16; got != 0 {
		t.Errorf("the position slot's spare 16 bits are %#x, want reserved", got)
	}
	if got := words[record+2] >> 24; got != 0 {
		t.Errorf("the normal slot's spare 8 bits are %#x, want reserved", got)
	}
	if got := words[record+3] >> 24; got != 0 {
		t.Errorf("the tangent slot's spare 8 bits are %#x, want reserved", got)
	}
}

// The range ends round-trip exactly, which is what a fixed point against a
// measured range buys that a half float does not: the extreme of a target's
// reach is the value an artist can see, and it is the one every candidate is
// judged on.
func TestTheEndsOfEverySlotRangeRoundTripExactly(t *testing.T) {
	mask := morphPosition | morphNormal | morphTangent
	// One vertex, three slots, every component at the range's own end.
	deltas := []m.Vec4{{X: 2, Y: -2, Z: 2}, {X: 0.5, Y: -0.5, Z: 0.5}, {X: -1, Y: 1, Z: -1}}
	words := packMorphBlock(nil, deltas, mask, 1, 1)
	block := viewMorphBlock(words, 0, uint32(mask.recordWords()))
	position, normal, tangent, covered := block.delta(0, 0)
	if !covered {
		t.Fatal("the only vertex is outside its own span")
	}
	for _, want := range []struct {
		slot string
		got  m.Vec3
		want m.Vec4
	}{
		{slot: "position", got: position, want: deltas[0]},
		{slot: "normal", got: normal, want: deltas[1]},
		{slot: "tangent", got: tangent, want: deltas[2]},
	} {
		if want.got != (m.Vec3{X: want.want.X, Y: want.want.Y, Z: want.want.Z}) {
			t.Errorf("%s = %v, want the authored %v exactly", want.slot, want.got, want.want)
		}
	}
}

// The narrowing's error bound is what decided against a half float, so it is
// measured rather than asserted: a position delta lands within half a code of
// the primitive's own range, and a normal delta within half a code of its own -
// about 0.13 degrees on a unit normal, an order below the octahedral rung that
// was stared at and could not be seen.
func TestTheNarrowedDeltasStayInsideHalfACodeOfTheirRange(t *testing.T) {
	mask := morphPosition | morphNormal
	const vertices = 64
	deltas := make([]m.Vec4, vertices*2)
	for i := range vertices {
		// A spread that fills the range rather than clustering near zero, which
		// is what a target's live deltas actually do.
		angle := float64(i) / vertices * 2 * math.Pi
		deltas[i*2] = m.Vec4{
			X: float32(math.Sin(angle)), Y: float32(math.Cos(angle) * 0.5), Z: float32(angle/6 - 0.5),
		}
		deltas[i*2+1] = m.Vec4{
			X: float32(math.Cos(angle) * 0.02), Y: float32(math.Sin(angle) * 0.02), Z: 0.01,
		}
	}
	words := packMorphBlock(nil, deltas, mask, 1, vertices)
	block := viewMorphBlock(words, 0, uint32(mask.recordWords()))
	scale := block.ranges()
	var worstPosition, worstNormal float64
	for i := range vertices {
		position, normal, _, covered := block.delta(0, i)
		if !covered {
			t.Fatalf("vertex %d is outside the span, want every vertex of a live target inside", i)
		}
		worstPosition = math.Max(worstPosition, componentError(position, deltas[i*2], scale[0], morphPositionCodeMax))
		worstNormal = math.Max(worstNormal, componentError(normal, deltas[i*2+1], scale[1], morphDirectionCodeMax))
	}
	// Half a code each, with a rounding slack of a tenth: anything above this is
	// a lost bit rather than a rounding difference.
	if worstPosition > 0.55 {
		t.Errorf("the worst position error is %v codes, want half a code", worstPosition)
	}
	if worstNormal > 0.55 {
		t.Errorf("the worst normal error is %v codes, want half a code", worstNormal)
	}
}

// componentError reports the largest error of one decoded slot in units of its
// own code, which is the only scale a fixed point can be judged on: an absolute
// error means nothing without the range it was quantised against.
func componentError(got m.Vec3, want m.Vec4, scale m.Vec3, codeMax int) float64 {
	worst := float64(0)
	for _, axis := range [...][3]float32{
		{got.X, want.X, scale.X}, {got.Y, want.Y, scale.Y}, {got.Z, want.Z, scale.Z},
	} {
		if axis[2] == 0 {
			continue
		}
		worst = math.Max(worst,
			math.Abs(float64(axis[0]-axis[1]))/float64(axis[2])*float64(codeMax))
	}
	return worst
}

// The narrowed, sparse block cannot be larger than the float layout for any
// primitive with a surface to morph. At position only the block is
// 12 + 12T + 8TV bytes against the float layout's 16TV, so the crossover is
// 12 + 12T <= 8TV - satisfied for every T at three vertices or more, and from
// four targets up at two. Below that is a point- or line-mode primitive, which
// has no surface for a shape to deform.
func TestTheBlockIsNeverLargerThanTheFloatLayoutForAPrimitiveThatDraws(t *testing.T) {
	for _, targets := range []int{1, 8, 100} {
		for _, vertices := range []int{3, 4, 1504} {
			// The worst case for the sparse scheme is a target that moves its
			// first and last vertex, so its span covers the whole primitive and
			// it stores exactly what the dense layout stored.
			deltas := make([]m.Vec4, targets*vertices)
			for target := range targets {
				deltas[target*vertices] = m.Vec4{X: 1}
				deltas[target*vertices+vertices-1] = m.Vec4{X: -1}
			}
			words := packMorphBlock(nil, deltas, morphPosition, targets, vertices)
			dense := targets * vertices * 4
			if got := len(words); got > dense {
				t.Errorf("%d targets over %d vertices pack %d words, want no more than the float layout's %d",
					targets, vertices, got, dense)
			}
		}
	}
}

// The block is the whole addressing contract now: the shader reads the ranges
// and the per-target headers out of the delta buffer itself, so a block laid
// out anywhere else is read from the wrong place with nothing to say so.
//
// The offsets below are spelled out from the layout rather than read back
// through the code that wrote them.
func TestPackMorphBlockLaysOutRangesThenHeadersThenRecords(t *testing.T) {
	doc := testDoc()
	mesh := morphedMesh(doc, true, false, []gltf.PrimitiveAttributes{
		deltaTarget(doc,
			[][3]float32{{1, 0, 0}, {2, 0, 0}, {3, 0, 0}},
			[][3]float32{{0, 1, 0}, {0, 2, 0}, {0, 3, 0}}, nil),
		deltaTarget(doc,
			[][3]float32{{0, 0, 0}, {0, 0, 5}, {0, 0, 6}},
			[][3]float32{{0, 0, 0}, {8, 0, 0}, {9, 0, 0}}, nil),
	})
	morph := readMorphTargets(doc, doc.Meshes[mesh].Primitives[0], 3)
	words := packMorphBlock(nil, morph.deltas, morph.mask, morph.targets, 3)
	// Two slots: six words of range, two headers of three, then the records.
	if got, want := words[0], math.Float32bits(3); got != want {
		t.Errorf("the position range's x is %v, want the largest magnitude 3", math.Float32frombits(got))
	}
	if got, want := words[5], math.Float32bits(0); got != want {
		t.Errorf("the normal range's z is %v, want zero", math.Float32frombits(got))
	}
	headers := 2 * morphRangeWords
	if got := words[headers]; got != uint32(headers+2*morphTargetHeaderWords) {
		t.Errorf("target 0's base is %d, want the first word after the header", got)
	}
	if got, want := [2]uint32{words[headers+1], words[headers+2]}, [2]uint32{0, 3}; got != want {
		t.Errorf("target 0's span is %v, want %v", got, want)
	}
	// Target 1 moves nothing at vertex 0, so its span starts at 1.
	if got, want := [2]uint32{words[headers+4], words[headers+5]}, [2]uint32{1, 2}; got != want {
		t.Errorf("target 1's span is %v, want %v", got, want)
	}
	if got := words[headers+3]; got != uint32(headers+2*morphTargetHeaderWords+3*3) {
		t.Errorf("target 1's base is %d, want it behind target 0's three records", got)
	}
	block := viewMorphBlock(words, 0, uint32(morph.mask.recordWords()))
	for _, want := range []struct {
		target, vertex   int
		position, normal m.Vec3
	}{
		{target: 0, vertex: 2, position: m.Vec3{X: 3}, normal: m.Vec3{Y: 3}},
		{target: 1, vertex: 1, position: m.Vec3{Z: 5}, normal: m.Vec3{X: 8}},
	} {
		position, normal, _, covered := block.delta(want.target, want.vertex)
		if !covered {
			t.Fatalf("target %d vertex %d is outside its span", want.target, want.vertex)
		}
		// Within a code of the slot's own range: only a value sitting on the
		// range's end round-trips exactly, and these do not.
		if !nearDelta(position, want.position, 1e-3) || !nearDelta(normal, want.normal, 0.1) {
			t.Errorf("target %d vertex %d = %v / %v, want %v / %v",
				want.target, want.vertex, position, normal, want.position, want.normal)
		}
	}
}

// nearDelta reports whether two deltas agree to a tolerance, which is what a
// fixed-point round trip can promise anywhere but the range's own ends.
func nearDelta(got, want m.Vec3, tolerance float32) bool {
	return abs(got.X-want.X) <= tolerance &&
		abs(got.Y-want.Y) <= tolerance &&
		abs(got.Z-want.Z) <= tolerance
}
