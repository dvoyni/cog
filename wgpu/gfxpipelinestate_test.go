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
	// which index format cuts the strip, and with two widths in the engine the
	// format a strip declares is the mesh's own.
	for _, c := range []struct {
		width cgfx.IndexWidth
		want  gputypes.IndexFormat
	}{
		{cgfx.IndexUint16, gputypes.IndexFormatUint16},
		{cgfx.IndexUint32, gputypes.IndexFormatUint32},
	} {
		strip := stripIndexFormat(cgfx.TopologyTriangleStrip, c.width)
		if strip == nil || *strip != c.want {
			t.Fatalf("stripIndexFormat(TriangleStrip, %v) = %v, want %v", c.width, strip, c.want)
		}
	}
	// Every other topology has no strip to cut, and WebGPU forbids declaring a
	// format for one - whatever width the mesh's own indices happen to be.
	for _, topology := range []cgfx.PrimitiveTopology{cgfx.TopologyTriangleList, cgfx.TopologyLineList} {
		for _, width := range []cgfx.IndexWidth{cgfx.IndexUint16, cgfx.IndexUint32} {
			if got := stripIndexFormat(topology, width); got != nil {
				t.Errorf("stripIndexFormat(%v, %v) = %v, want nil for a non-strip topology", topology, width, got)
			}
		}
	}
}
