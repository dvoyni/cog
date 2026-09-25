package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The Batch tests are the only ones here that assert exact Batch counts: a
// Batch is one instanced draw in a pass, so they count the draws a pass
// received and the instances each drew. Every other test asserts which mesh
// drew how many instances with which records, whatever the batching.

// crowd is how many Entities a Batch test spawns, the population the frame
// benches draw.
const crowd = 5_000

// crowdEye is a camera far enough back to see a row of crowd crates half a
// unit apart, and crowdStart where the row starts so it is centred on the
// origin.
var (
	crowdEye   = m.LookAt(m.Vec3{Z: 3000}, m.Vec3{}, m.Vec3{Y: 1})
	crowdStart = m.At(-crowd*0.25, 0, 0)
)

// newCrowdHarness is the drawing harness with a camera that sees the whole
// crowd.
func newCrowdHarness(t *testing.T) *harness {
	t.Helper()
	h := newCameralessHarness(t, crowd+16)
	h.spawn(t, spawnRequest{Place: crowdEye, Camera: &Camera{FovY: 1.0472, Near: 1, Far: 10_000}})
	return h
}

// forwardDraws is every draw the forward pass of camera 0 received.
func (h *harness) forwardDraws(t *testing.T) []recordedDraw {
	t.Helper()
	for _, pass := range h.backend.passes() {
		if pass.desc.Label == "scene.camera0.forward" {
			return pass.draws
		}
	}
	t.Fatalf("the frame has no forward pass for camera 0")
	return nil
}

// instancesOf sums the instances a list of draws drew.
func instancesOf(draws []recordedDraw) int {
	total := 0
	for _, draw := range draws {
		total += draw.instances
	}
	return total
}

// TestIdenticalCratesAreOneBatch is batching's reason to exist: five thousand
// Entities naming the same model with the same material and no Params share a
// key, so they are one instanced draw of five thousand, where the recording
// renderer removed in #573, one call per Entity, drew five thousand. Tinting all of them the same red keeps
// them one Batch, because equal Params batch.
func TestIdenticalCratesAreOneBatch(t *testing.T) {
	for _, arm := range []struct {
		name   string
		params *Params
	}{
		{"untinted", nil},
		{"all tinted red", tint(red)},
	} {
		t.Run(arm.name, func(t *testing.T) {
			h := newCrowdHarness(t)
			h.spawn(t, spawnRequest{
				Count: crowd, Step: 0.5, Place: crowdStart,
				Model: crateModelComponent(), Params: arm.params,
			})
			h.frameUntil(t, "the crowd to draw", func() bool { return len(h.drawn()) == crowd })

			draws := h.forwardDraws(t)
			if len(draws) != 1 || draws[0].instances != crowd {
				t.Fatalf("%d identical crates drew as %d draws of %d instances, want one draw of %d",
					crowd, len(draws), instancesOf(draws), crowd)
			}
			if len(where(h.drawn(), at(crowdStart.Position))) != 1 ||
				len(where(h.drawn(), at(m.Vec3{X: crowdStart.Position.X + (crowd-1)*0.5}))) != 1 {
				t.Errorf("the Batch does not hold both ends of the row: %d instances", len(h.drawn()))
			}
			h.noErrors(t)
		})
	}
}

// TestDistinctTintsAreOneBatchEach is the other end of the key: a Params value
// is part of it, so five thousand crates tinted five thousand ways are five
// thousand Batches of one, and each draws its own tint.
func TestDistinctTintsAreOneBatchEach(t *testing.T) {
	h := newCrowdHarness(t)
	h.spawn(t, spawnRequest{
		Count: crowd, Step: 0.5, Place: crowdStart, Model: crateModelComponent(),
		ParamsEach: func(i int) Params { return *tint(m.Color{R: float32(i) / crowd, A: 1}) },
	})
	h.frameUntil(t, "the crowd to draw", func() bool { return len(h.drawn()) == crowd })

	draws := h.forwardDraws(t)
	if len(draws) != crowd {
		t.Fatalf("%d distinctly tinted crates drew as %d draws, want %d", crowd, len(draws), crowd)
	}
	for i, draw := range draws {
		if draw.instances != 1 {
			t.Fatalf("draw %d drew %d instances, want 1: no two tints are equal", i, draw.instances)
		}
	}
	// Each Batch's properties record carries its own tint: the crate at
	// position i is tinted i/crowd.
	for _, i := range []int{0, 1, crowd / 2, crowd - 1} {
		found := where(h.drawn(), at(m.Vec3{X: crowdStart.Position.X + float32(i)*0.5}))
		if len(found) != 1 {
			t.Fatalf("the crate at index %d drew %d instances, want 1", i, len(found))
		}
		want := float32(i) / crowd
		if got := found[0].param("baseColorFactor"); !nearFloat(got.X, want) {
			t.Errorf("the crate at index %d drew with baseColorFactor %v, want red %v", i, got, want)
		}
	}
	h.noErrors(t)
}

// TestBlendedPanesDrawBackToFrontAroundTheSmoke is the one class that never
// batches. Three panes share a material and so a key, and a puff of smoke with
// a material of its own stands between the nearest and the other two. Blended
// entries stay one draw each, sorted by depth across Batches, so the frame
// draws the two far panes one after the other, each on its own, then the
// smoke, then the near pane.
func TestBlendedPanesDrawBackToFrontAroundTheSmoke(t *testing.T) {
	h := newDrawingHarness(t, 256)
	ref := h.bake(t)
	glass := &Material{Tags: m.NewList(
		MaterialTag{Shader: gfx.ShaderWithText("glass"), State: gfx.StateTransparent3D()})}
	smoke := &Material{Tags: m.NewList(
		MaterialTag{Shader: gfx.ShaderWithText("smoke"), State: gfx.StateTransparent3D()})}
	// The camera stands at Z=30 looking down -Z, so Z=-5 is the farthest.
	farthest, far, middle, near := m.Vec3{X: -2, Z: -5}, m.Vec3{X: -1, Z: -3}, m.Vec3{Z: 0}, m.Vec3{X: 1, Z: 5}
	pane := func(at m.Vec3, material *Material) {
		h.spawn(t, spawnRequest{Place: m.Transform{Position: at}, Mesh: &Mesh{Ref: ref, NeverCull: true}, Material: material})
	}
	pane(near, glass)
	pane(far, glass)
	pane(middle, smoke)
	pane(farthest, glass)

	h.frameUntil(t, "the frame to draw", func() bool { return len(h.drawn()) == 4 })

	draws := h.forwardDraws(t)
	if len(draws) != 4 {
		t.Fatalf("three panes and the smoke drew as %d draws, want 4: a blended entry is a draw of its own", len(draws))
	}
	drawn := h.drawn()
	order := []struct {
		what   string
		where  m.Vec3
		shader string
	}{
		{"the farthest pane", farthest, "glass"}, {"the far pane", far, "glass"},
		{"the smoke", middle, "smoke"}, {"the near pane", near, "glass"},
	}
	for i, want := range order {
		if !nearVec3(drawn[i].position(), want.where) || viewOf(drawn[i]).Shader != want.shader {
			t.Errorf("draw %d is %s at %v, want %s at %v: blended draws go back to front",
				i, viewOf(drawn[i]).Shader, drawn[i].position(), want.what, want.where)
		}
	}
	h.noErrors(t)
}

// TestAnimatedEntitiesShareABatch is the per-instance AnimOffset at work:
// Entities playing different clips at different times share a mesh and a
// material, so they are one Batch, and each instance still points at its own
// sceneAnim block with its own plays.
func TestAnimatedEntitiesShareABatch(t *testing.T) {
	h := newDrawingHarness(t, 256)
	animated := &Model{Ref: model.ModelRef{Path: animatedModel}}
	times := []float32{0, 0.25, 0.5}
	h.spawn(t, spawnRequest{
		Count: len(times), Step: 3, Model: animated,
		AnimationEach: func(i int) Animation {
			return Animation{Plays: [model.MaxClipPlays]model.ClipPlay{
				{Clip: "Walk", Time: times[i], Weight: 1},
			}}
		},
	})

	h.frameUntil(t, "the animated model to become resident", func() bool {
		return len(where(h.drawn(), ofTriangle)) == len(times)
	})

	draws := h.forwardDraws(t)
	if len(draws) != 1 || draws[0].instances != len(times) {
		t.Fatalf("%d animated crates drew as %d draws of %d instances, want one draw of %d",
			len(times), len(draws), instancesOf(draws), len(times))
	}
	offsets := map[uint32]bool{}
	var first uint32
	for i := range times {
		found := where(h.drawn(), at(m.Vec3{X: float32(3 * i)}))
		if len(found) != 1 {
			t.Fatalf("the crate at x=%d drew %d instances, want 1", 3*i, len(found))
		}
		offsets[found[0].animOffset] = true
		plays := playsOf(t, found[0])
		if len(plays) != 1 {
			t.Fatalf("the crate at x=%d packed %d plays, want 1", 3*i, len(plays))
		}
		if i == 0 {
			first = plays[0].row0
		}
		// Sampled at 60 a second, a quarter second is 15 rows.
		if got, want := plays[0].row0-first, uint32(60*times[i]); got != want {
			t.Errorf("the crate at x=%d plays %d rows into Walk, want %d", 3*i, got, want)
		}
	}
	if len(offsets) != len(times) {
		t.Errorf("the Batch's %d instances point at %d sceneAnim blocks, want one each", len(times), len(offsets))
	}
	h.noErrors(t)
}
