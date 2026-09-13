package wgpu

import (
	"testing"

	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/gogpu/gputypes"
)

func TestCompareFuncMapsTheWholeSet(t *testing.T) {
	cases := []struct {
		from gpu.CompareFunc
		want gputypes.CompareFunction
	}{
		{gpu.CompareAlways, gputypes.CompareFunctionAlways},
		{gpu.CompareNever, gputypes.CompareFunctionNever},
		{gpu.CompareLess, gputypes.CompareFunctionLess},
		{gpu.CompareLessEqual, gputypes.CompareFunctionLessEqual},
		{gpu.CompareGreater, gputypes.CompareFunctionGreater},
		{gpu.CompareGreaterEqual, gputypes.CompareFunctionGreaterEqual},
		{gpu.CompareEqual, gputypes.CompareFunctionEqual},
		{gpu.CompareNotEqual, gputypes.CompareFunctionNotEqual},
	}
	for _, c := range cases {
		if got := compareFunc(c.from); got != c.want {
			t.Errorf("compareFunc(%v) = %v, want %v", c.from, got, c.want)
		}
	}
}

func TestCullAndWindingZeroValuesAreTheWebGPUDefaults(t *testing.T) {
	if got := cullMode(gpu.CullNone); got != gputypes.CullModeNone {
		t.Errorf("cullMode(CullNone) = %v, want CullModeNone", got)
	}
	if got := cullMode(gpu.CullBack); got != gputypes.CullModeBack {
		t.Errorf("cullMode(CullBack) = %v, want CullModeBack", got)
	}
	if got := cullMode(gpu.CullFront); got != gputypes.CullModeFront {
		t.Errorf("cullMode(CullFront) = %v, want CullModeFront", got)
	}
	if got := frontFace(gpu.FrontCCW); got != gputypes.FrontFaceCCW {
		t.Errorf("frontFace(FrontCCW) = %v, want FrontFaceCCW", got)
	}
	if got := frontFace(gpu.FrontCW); got != gputypes.FrontFaceCW {
		t.Errorf("frontFace(FrontCW) = %v, want FrontFaceCW", got)
	}
}

func TestStripTopologiesDeclareTheirIndexFormat(t *testing.T) {
	// An indexed strip draw is invalid under WebGPU unless the pipeline says
	// which index format cuts the strip, and with two widths in the engine the
	// format a strip declares is the mesh's own.
	for _, c := range []struct {
		width gpu.IndexWidth
		want  gputypes.IndexFormat
	}{
		{gpu.IndexUint16, gputypes.IndexFormatUint16},
		{gpu.IndexUint32, gputypes.IndexFormatUint32},
	} {
		strip := stripIndexFormat(gpu.TopologyTriangleStrip, c.width)
		if strip == nil || *strip != c.want {
			t.Fatalf("stripIndexFormat(TriangleStrip, %v) = %v, want %v", c.width, strip, c.want)
		}
	}
	// Every other topology has no strip to cut, and WebGPU forbids declaring a
	// format for one - whatever width the mesh's own indices happen to be.
	for _, topology := range []gpu.PrimitiveTopology{gpu.TopologyTriangleList, gpu.TopologyLineList} {
		for _, width := range []gpu.IndexWidth{gpu.IndexUint16, gpu.IndexUint32} {
			if got := stripIndexFormat(topology, width); got != nil {
				t.Errorf("stripIndexFormat(%v, %v) = %v, want nil for a non-strip topology", topology, width, got)
			}
		}
	}
}
