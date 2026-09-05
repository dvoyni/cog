package wgpu

import (
	"testing"

	cgfx "github.com/dvoyni/cog/gfx"
	"github.com/gogpu/gputypes"
)

func TestCompareFuncMapsTheWholeSet(t *testing.T) {
	cases := []struct {
		from cgfx.CompareFunc
		want gputypes.CompareFunction
	}{
		{cgfx.CompareAlways, gputypes.CompareFunctionAlways},
		{cgfx.CompareNever, gputypes.CompareFunctionNever},
		{cgfx.CompareLess, gputypes.CompareFunctionLess},
		{cgfx.CompareLessEqual, gputypes.CompareFunctionLessEqual},
		{cgfx.CompareGreater, gputypes.CompareFunctionGreater},
		{cgfx.CompareGreaterEqual, gputypes.CompareFunctionGreaterEqual},
		{cgfx.CompareEqual, gputypes.CompareFunctionEqual},
		{cgfx.CompareNotEqual, gputypes.CompareFunctionNotEqual},
	}
	for _, c := range cases {
		if got := compareFunc(c.from); got != c.want {
			t.Errorf("compareFunc(%v) = %v, want %v", c.from, got, c.want)
		}
	}
}

func TestCullAndWindingZeroValuesAreTheWebGPUDefaults(t *testing.T) {
	if got := cullMode(cgfx.CullNone); got != gputypes.CullModeNone {
		t.Errorf("cullMode(CullNone) = %v, want CullModeNone", got)
	}
	if got := cullMode(cgfx.CullBack); got != gputypes.CullModeBack {
		t.Errorf("cullMode(CullBack) = %v, want CullModeBack", got)
	}
	if got := cullMode(cgfx.CullFront); got != gputypes.CullModeFront {
		t.Errorf("cullMode(CullFront) = %v, want CullModeFront", got)
	}
	if got := frontFace(cgfx.FrontCCW); got != gputypes.FrontFaceCCW {
		t.Errorf("frontFace(FrontCCW) = %v, want FrontFaceCCW", got)
	}
	if got := frontFace(cgfx.FrontCW); got != gputypes.FrontFaceCW {
		t.Errorf("frontFace(FrontCW) = %v, want FrontFaceCW", got)
	}
}

func TestStripTopologiesDeclareTheirIndexFormat(t *testing.T) {
	// An indexed strip draw is invalid under WebGPU unless the pipeline says
	// which index format cuts the strip, and index buffers are always uint32.
	strip := stripIndexFormat(cgfx.TopologyTriangleStrip)
	if strip == nil || *strip != gputypes.IndexFormatUint32 {
		t.Fatalf("stripIndexFormat(TriangleStrip) = %v, want Uint32", strip)
	}
	for _, topology := range []cgfx.PrimitiveTopology{cgfx.TopologyTriangleList, cgfx.TopologyLineList} {
		if got := stripIndexFormat(topology); got != nil {
			t.Errorf("stripIndexFormat(%v) = %v, want nil for a non-strip topology", topology, got)
		}
	}
}
