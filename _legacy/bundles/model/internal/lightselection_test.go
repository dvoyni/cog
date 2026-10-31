package internal

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// pointLights packs n white point lights in front of the origin, the i-th at
// z = -(i+1), so the nearest to the eye contribute most.
func pointLights(n int) []Light {
	lights := make([]Light, n)
	for i := range lights {
		lights[i], _ = PackLight(LightDescr{
			Position: m.Vec3{Z: -float32(i + 1)}, Color: m.NewColorLinear(1, 1, 1, 1), Kind: LightPoint,
		})
	}
	return lights
}

// Past MaxLights, the lights kept are the ones contributing most at the eye,
// not the first offered. Twenty lights, offered nearest first, then reversed:
// the same set survives either way.
func TestTheCapKeepsTheBrightestNotTheFirstOffered(t *testing.T) {
	nearestFirst := pointLights(20)
	farthestFirst := make([]Light, 20)
	for i := range nearestFirst {
		farthestFirst[19-i] = nearestFirst[i]
	}
	for _, order := range [][]Light{nearestFirst, farthestFirst} {
		var selection LightSelection
		for i := range order {
			selection.Offer(&order[i], ContributionAt(&order[i], m.Vec3{}))
		}
		if selection.Count() != MaxLights || len(selection.Lights()) != MaxLights {
			t.Fatalf("kept %d lights, want the cap of %d", selection.Count(), MaxLights)
		}
		for _, light := range selection.Lights() {
			if z := light.Position.Z; z < -16 {
				t.Errorf("kept a light at z %v; the four beyond -16 contribute least and should be the ones dropped", z)
			}
		}
	}
}

// Reset empties the selection for the next pass, so a pass never inherits the
// last one's lights.
func TestResetEmptiesTheSelection(t *testing.T) {
	var selection LightSelection
	lights := pointLights(3)
	for i := range lights {
		selection.Offer(&lights[i], 1)
	}
	selection.Reset()
	if selection.Count() != 0 || len(selection.Lights()) != 0 {
		t.Errorf("a reset selection holds %d lights, want none", selection.Count())
	}
}

// The selection is a fixed insertion: a steady pass allocates nothing.
func TestASteadySelectionAllocatesNothing(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	lights := pointLights(40)
	var selection LightSelection
	allocations := testing.AllocsPerRun(50, func() {
		selection.Reset()
		for i := range lights {
			selection.Offer(&lights[i], ContributionAt(&lights[i], m.Vec3{}))
		}
	})
	if allocations != 0 {
		t.Fatalf("a steady selection allocated %v times", allocations)
	}
}
