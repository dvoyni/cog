package render_test

import (
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

func TestFrameLocksRendersFrameStateAndAssetTableAndGfxsDeviceAllForRead(t *testing.T) {
	for _, test := range []struct {
		name            string
		registerOther   func(p *lockProbe, configurator *kernel.Configurator) *kernel.Task[kernel.NoArg, kernel.NoResult]
		wantRanTogether bool
	}{
		{name: "beside a writer of render's frame state", registerOther: (*lockProbe).registerWriter[*render.FrameState], wantRanTogether: false},
		{name: "beside a writer of render's asset table", registerOther: (*lockProbe).registerWriter[*render.AssetTable], wantRanTogether: false},
		{name: "beside a writer of gfx's Device", registerOther: (*lockProbe).registerWriter[gfx.Device], wantRanTogether: false},
		{name: "beside another Frame task", registerOther: nil, wantRanTogether: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := rendertest.NewHarness(t)
			probe := &lockProbe{registerOther: test.registerOther}
			h.Spawn(nil, probe)
			h.OpenWindow(platform.Size{Width: 64, Height: 48})

			h.Step()

			if got := probe.ranTogether.Load(); got != test.wantRanTogether {
				t.Errorf("a task holding Frame ran together with the other: %v, want %v", got, test.wantRanTogether)
			}
		})
	}
}
