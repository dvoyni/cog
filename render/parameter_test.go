package render_test

import (
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

// registerTask registers a lockProbe's task, such as one of registerHolder.
type registerTask = func(p *lockProbe, configurator *kernel.Configurator) *kernel.Task[kernel.NoArg, kernel.NoResult]

func TestParametersLockWhatTheyDeclare(t *testing.T) {
	frame := (*lockProbe).registerHolder[render.Frame]
	inventory := (*lockProbe).registerHolder[render.Inventory]
	surface := (*lockProbe).registerHolder[render.Surface]
	for _, test := range []struct {
		name            string
		registerProbed  registerTask
		registerOther   registerTask
		wantRanTogether bool
	}{
		{name: "Frame beside a writer of render's frame state", registerProbed: frame, registerOther: (*lockProbe).registerHolder[kernel.Write[*render.FrameState]], wantRanTogether: false},
		{name: "Frame beside a writer of render's asset table", registerProbed: frame, registerOther: (*lockProbe).registerHolder[kernel.Write[*render.AssetTable]], wantRanTogether: false},
		{name: "Frame beside a writer of gfx's Device", registerProbed: frame, registerOther: (*lockProbe).registerHolder[kernel.Write[gfx.Device]], wantRanTogether: false},
		{name: "Frame beside another Frame task", registerProbed: frame, registerOther: frame, wantRanTogether: true},
		{name: "Inventory beside a writer of render's asset table", registerProbed: inventory, registerOther: (*lockProbe).registerHolder[kernel.Write[*render.AssetTable]], wantRanTogether: false},
		{name: "Inventory beside a writer of gfx's Device", registerProbed: inventory, registerOther: (*lockProbe).registerHolder[kernel.Write[gfx.Device]], wantRanTogether: false},
		{name: "Inventory beside a writer of platform's FS", registerProbed: inventory, registerOther: (*lockProbe).registerHolder[kernel.Write[platform.FS]], wantRanTogether: false},
		{name: "Inventory beside a writer of render's frame state", registerProbed: inventory, registerOther: (*lockProbe).registerHolder[kernel.Write[*render.FrameState]], wantRanTogether: true},
		{name: "Inventory beside a Frame task", registerProbed: inventory, registerOther: frame, wantRanTogether: true},
		{name: "Inventory beside another Inventory task", registerProbed: inventory, registerOther: inventory, wantRanTogether: true},
		{name: "Surface beside a reader of gfx's Device", registerProbed: surface, registerOther: (*lockProbe).registerHolder[kernel.Read[gfx.Device]], wantRanTogether: false},
		{name: "Surface beside a Frame task", registerProbed: surface, registerOther: frame, wantRanTogether: false},
		{name: "Surface beside an Inventory task", registerProbed: surface, registerOther: inventory, wantRanTogether: false},
		{name: "Surface beside a writer of render's frame state", registerProbed: surface, registerOther: (*lockProbe).registerHolder[kernel.Write[*render.FrameState]], wantRanTogether: true},
		{name: "Surface beside a writer of render's asset table", registerProbed: surface, registerOther: (*lockProbe).registerHolder[kernel.Write[*render.AssetTable]], wantRanTogether: true},
		{name: "Surface beside a writer of platform's FS", registerProbed: surface, registerOther: (*lockProbe).registerHolder[kernel.Write[platform.FS]], wantRanTogether: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := rendertest.NewHarness(t)
			probe := &lockProbe{registerProbed: test.registerProbed, registerOther: test.registerOther}
			h.Spawn(nil, probe)
			h.OpenWindow(platform.Size{Width: 64, Height: 48})

			h.Step()

			if got := probe.ranTogether.Load(); got != test.wantRanTogether {
				t.Errorf("the probed task ran together with the other: %v, want %v", got, test.wantRanTogether)
			}
		})
	}
}
