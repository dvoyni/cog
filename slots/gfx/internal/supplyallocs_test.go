package internal

import (
	"testing"

	"github.com/dvoyni/cog/kernel"
)

// A shader with a supply is named by its root and its supply together, and
// that label is spelled once per variant: a steady frame drawing the variant
// many times allocates nothing for it in translate.
func TestSuppliedShaderDrawAllocatesNothing(t *testing.T) {
	backend := &fakeBackend{}
	translator := newTranslator()
	queue := testOpQueue(backend)
	queue.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto()})
	mesh := Mesh(
		BakedBuffer(1, 3*28),
		TopologyTriangleList,
		Attr(0, Float32x3), Attr(12, Float32x4),
	)
	material := Material(ShaderWithText("//test", ShaderDefine("HQ"), ShaderConst("N", "4")))
	const draws = 100
	for range draws {
		queue.Draw(mesh, material)
	}
	translate := func() {
		if _, err := translator.translate(kernel.Kernel{}, &queue, nil, backend, noFiles, CaptureDesc{}, false); err != nil {
			t.Fatalf("translate: %v", err)
		}
	}
	translate()
	out, _ := translator.translate(kernel.Kernel{}, &queue, nil, backend, noFiles, CaptureDesc{}, false)
	backend.Execute(out)
	if got := countOps(backend.lastOps, testOpDraw); got != draws {
		t.Fatalf("draw ops = %d, want %d: the frame must reach the draws it measures", got, draws)
	}

	if allocs := testing.AllocsPerRun(20, translate); allocs != 0 {
		t.Errorf("translate of %d supplied-shader draws allocated %v times, want 0", draws, allocs)
	}
}
