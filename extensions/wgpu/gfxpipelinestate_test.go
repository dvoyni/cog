package wgpu

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx"
	"github.com/gogpu/gputypes"
)

func TestCompareFuncMapsTheWholeSet(t *testing.T) {
	cases := []struct {
		from gfx.CompareFunc
		want gputypes.CompareFunction
	}{
		{gfx.CompareAlways, gputypes.CompareFunctionAlways},
		{gfx.CompareNever, gputypes.CompareFunctionNever},
		{gfx.CompareLess, gputypes.CompareFunctionLess},
		{gfx.CompareLessEqual, gputypes.CompareFunctionLessEqual},
		{gfx.CompareGreater, gputypes.CompareFunctionGreater},
		{gfx.CompareGreaterEqual, gputypes.CompareFunctionGreaterEqual},
		{gfx.CompareEqual, gputypes.CompareFunctionEqual},
		{gfx.CompareNotEqual, gputypes.CompareFunctionNotEqual},
	}
	for _, c := range cases {
		if got := compareFunc(c.from); got != c.want {
			t.Errorf("compareFunc(%v) = %v, want %v", c.from, got, c.want)
		}
	}
}

func TestCullAndWindingZeroValuesAreTheWebGPUDefaults(t *testing.T) {
	if got := cullMode(gfx.CullNone); got != gputypes.CullModeNone {
		t.Errorf("cullMode(CullNone) = %v, want CullModeNone", got)
	}
	if got := cullMode(gfx.CullBack); got != gputypes.CullModeBack {
		t.Errorf("cullMode(CullBack) = %v, want CullModeBack", got)
	}
	if got := cullMode(gfx.CullFront); got != gputypes.CullModeFront {
		t.Errorf("cullMode(CullFront) = %v, want CullModeFront", got)
	}
	if got := frontFace(gfx.FrontCCW); got != gputypes.FrontFaceCCW {
		t.Errorf("frontFace(FrontCCW) = %v, want FrontFaceCCW", got)
	}
	if got := frontFace(gfx.FrontCW); got != gputypes.FrontFaceCW {
		t.Errorf("frontFace(FrontCW) = %v, want FrontFaceCW", got)
	}
}

func TestStripTopologiesDeclareTheirIndexFormat(t *testing.T) {
	// An indexed strip draw is invalid under WebGPU unless the pipeline says
	// which index format cuts the strip, and with two widths in the engine the
	// format a strip declares is the mesh's own.
	for _, c := range []struct {
		width gfx.IndexWidth
		want  gputypes.IndexFormat
	}{
		{gfx.IndexUint16, gputypes.IndexFormatUint16},
		{gfx.IndexUint32, gputypes.IndexFormatUint32},
	} {
		strip := stripIndexFormat(gfx.TopologyTriangleStrip, c.width)
		if strip == nil || *strip != c.want {
			t.Fatalf("stripIndexFormat(TriangleStrip, %v) = %v, want %v", c.width, strip, c.want)
		}
	}
	// Every other topology has no strip to cut, and WebGPU forbids declaring a
	// format for one - whatever width the mesh's own indices happen to be.
	for _, topology := range []gfx.PrimitiveTopology{gfx.TopologyTriangleList, gfx.TopologyLineList} {
		for _, width := range []gfx.IndexWidth{gfx.IndexUint16, gfx.IndexUint32} {
			if got := stripIndexFormat(topology, width); got != nil {
				t.Errorf("stripIndexFormat(%v, %v) = %v, want nil for a non-strip topology", topology, width, got)
			}
		}
	}
}
