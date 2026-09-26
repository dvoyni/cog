package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/kernel"
)

// A shader with a supply is named by its root and its supply together, and
// that label is spelled once per variant: a steady frame drawing the variant
// many times allocates nothing for it in translate.
func TestSuppliedShaderDrawAllocatesNothing(t *testing.T) {
	backend := &fakeBackend{}
	translator := newTranslator()
	queue := testOpQueue(backend)
	queue.Pass(descriptors.PassDescr{Target: descriptors.ScreenTarget(), Depth: descriptors.DepthAuto()})
	mesh := descriptors.Mesh(
		descriptors.BakedBuffer(1, 3*28),
		types.TopologyTriangleList,
		descriptors.Attr(0, descriptors.Float32x3), descriptors.Attr(12, descriptors.Float32x4),
	)
	material := descriptors.Material(shader.ShaderWithText("//test", shader.ShaderDefine("HQ"), shader.ShaderConst("N", "4")))
	const draws = 100
	for range draws {
		queue.Draw(mesh, material, 1, 0)
	}
	translate := func() {
		if _, err := translator.translate(kernel.Kernel{}, &queue, nil, backend, noFiles, types.CaptureDesc{}, false); err != nil {
			t.Fatalf("translate: %v", err)
		}
	}
	translate()
	out, _ := translator.translate(kernel.Kernel{}, &queue, nil, backend, noFiles, types.CaptureDesc{}, false)
	backend.Execute(out)
	if got := countOps(backend.lastOps, testOpDraw); got != draws {
		t.Fatalf("draw ops = %d, want %d: the frame must reach the draws it measures", got, draws)
	}

	if allocs := testing.AllocsPerRun(20, translate); allocs != 0 {
		t.Errorf("translate of %d supplied-shader draws allocated %v times, want 0", draws, allocs)
	}
}
