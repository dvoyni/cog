package canvas_test

import (
	"cmp"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/dvoyni/cog/plugins/canvas"
)

func TestOrderOfOrdersEveryFloatAsItSortsNegativeZeroAsZeroAndNaNAfterEveryNumber(t *testing.T) {
	ascending := []float32{
		float32(math.Inf(-1)), -math.MaxFloat32, -1, -math.SmallestNonzeroFloat32,
		0, math.SmallestNonzeroFloat32, 1, math.MaxFloat32, float32(math.Inf(1)),
	}
	for index := 1; index < len(ascending); index++ {
		if below, above := canvas.OrderOf(ascending[index-1]), canvas.OrderOf(ascending[index]); below >= above {
			t.Errorf("OrderOf(%g) is %d, not under OrderOf(%g), %d", ascending[index-1], below, ascending[index], above)
		}
	}
	if got := canvas.OrderOf(float32(math.Copysign(0, -1))); got != 0 {
		t.Errorf("OrderOf(−0) is %d, want 0, as +0", got)
	}
	if got := canvas.OrderOf(-math.SmallestNonzeroFloat32); got != -1 {
		t.Errorf("OrderOf of the negative float nearest zero is %d, want -1, just under 0", got)
	}
	if got, want := canvas.OrderOf(1), int32(1065353216); got != want {
		t.Errorf("OrderOf(1) is %d, want %d", got, want)
	}
	if got, want := canvas.OrderOf(math.Nextafter32(1, 0)), canvas.OrderOf(1)-1; got != want {
		t.Errorf("OrderOf of the float just under 1 is %d, want OrderOf(1)-1, %d", got, want)
	}
	for _, bits := range []uint32{0x7fc00000, 0x7f800001, 0xffc00000, 0xffffffff} {
		nan := math.Float32frombits(bits)
		if got := canvas.OrderOf(nan); got != math.MaxInt32 {
			t.Errorf("OrderOf of the NaN %#x is %d, want %d, after every number", bits, got, math.MaxInt32)
		}
	}
}

func TestOrderOfKeepsTheOrderOfAnyTwoNumbers(t *testing.T) {
	generator := rand.New(rand.NewPCG(1, 2))
	for range 100000 {
		a := math.Float32frombits(generator.Uint32())
		b := math.Float32frombits(generator.Uint32())
		if a != a || b != b {
			continue
		}
		if got, want := cmp.Compare(canvas.OrderOf(a), canvas.OrderOf(b)), cmp.Compare(a, b); got != want {
			t.Fatalf("OrderOf(%g) and OrderOf(%g) compare as %d, want %d, as the floats", a, b, got, want)
		}
	}
}
