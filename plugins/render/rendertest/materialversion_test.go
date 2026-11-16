package rendertest_test

import (
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

// materialBindGroupLabel labels the bind groups of a material's own values.
const materialBindGroupLabel = "render: a material's bind group"

// findMaterialGroups answers the bind group each pass labelled label set as
// @group(1), each time it set one, pass by pass.
func findMaterialGroups(h *rendertest.Harness, label string) [][]gfx.BindGroup {
	var passes [][]gfx.BindGroup
	for _, commands := range findEveryPassCommands(h, label) {
		var groups []gfx.BindGroup
		for _, arguments := range findSetBindGroups(commands, 1) {
			groups = append(groups, arguments.Group)
		}
		passes = append(passes, groups)
	}
	return passes
}

// findEntryBufferLabel answers the label of the buffer group, a bind group
// calls created, binds at binding number binding; empty when it binds none.
func findEntryBufferLabel(calls []recording.Call, group gfx.BindGroup, binding uint32) string {
	buffer := findEntry(calls, group, binding).Buffer
	for _, call := range calls {
		if arguments, ok := call.Arguments.(recording.CreateBufferArguments); ok && arguments.Buffer == buffer {
			return arguments.Descriptor.Label
		}
	}
	return ""
}

// findEntry answers the entry group, a bind group calls created, has at
// binding number binding; the zero entry when it has none.
func findEntry(calls []recording.Call, group gfx.BindGroup, binding uint32) gfx.BindGroupEntry {
	for _, call := range calls {
		arguments, ok := call.Arguments.(recording.CreateBindGroupArguments)
		if !ok || arguments.Group != group {
			continue
		}
		if index := slices.IndexFunc(arguments.Descriptor.Entries, func(entry gfx.BindGroupEntry) bool { return entry.Binding == binding }); index >= 0 {
			return arguments.Descriptor.Entries[index]
		}
	}
	return gfx.BindGroupEntry{}
}

// readStrength answers the strength of the tint draw's material binds.
func readStrength(draw render.InspectedDraw) float32 {
	parameter, _ := findParameter(draw, "tint")
	if len(parameter.Uniform) < 20 {
		return -1
	}
	return readFloat32(parameter.Uniform[16:])
}

func TestTwoDrawsOfAMaterialWithAChangeBetweenThemReadTheOldValuesThenTheNew(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene parameterScene
	spawnParameterHarness(t, h, 2, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makeParameterScene(inventory)
		}
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(scene.material)
			if frameIndex == 0 {
				inventory.SetMaterialUniform(scene.material, "tint", tint{Strength: 1})
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
				inventory.SetMaterialUniform(scene.material, "tint", tint{Strength: 2})
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
				inventory.SetMaterialUniform(scene.material, "tint", tint{Strength: 3})
				inventory.SetMaterialUniform(scene.material, "tint", tint{Strength: 4})
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
			}
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
	})

	passes := findPassDraws(h, "scene")
	if len(passes) != 2 {
		t.Fatalf("the reader read %d passes, want 2", len(passes))
	}
	strengths := [][]float32{makeStrengths(passes[0]), makeStrengths(passes[1])}
	if want := [][]float32{{1, 2, 4, 4, 4}, {4}}; !slices.EqualFunc(strengths, want, slices.Equal[[]float32]) {
		t.Errorf("the draws read strengths %v, want %v: each the value set before it, the next frame the last", strengths, want)
	}
	calls := h.Calls()
	groups := findMaterialGroups(h, "scene")
	if len(groups[0]) != 3 || groups[0][0] == groups[0][1] || groups[0][1] == groups[0][2] {
		t.Fatalf("the first frame set @group(1) %v, want three bind groups, one for each version", groups[0])
	}
	chunks := findChunks(calls)
	for index, group := range groups[0] {
		if _, inArena := chunks[findEntry(calls, group, 2).Buffer]; inArena != (index > 0) {
			t.Errorf("version %d binds tint in buffer %q, want the material's own for the first and the arena for the later", index, findEntryBufferLabel(calls, group, 2))
		}
	}
	if label := findEntryBufferLabel(calls, groups[1][0], 2); !strings.Contains(label, "uniform block tint") {
		t.Errorf("the next frame binds tint in buffer %q, want the material's own", label)
	}
}

func TestAVersionsUniformBlockIsCopiedOnceAndKeptByALaterVersionChangingAnotherValue(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene parameterScene
	spawnParameterHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeParameterScene(inventory)
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(scene.material)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
			inventory.SetMaterialUniform(scene.material, "tint", tint{Strength: 1})
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
			inventory.SetMaterialTexture(scene.material, "albedo", scene.albedo)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
	})

	draws := findPassDraws(h, "scene")[0]
	if strengths := []float32{readStrength(draws[1]), readStrength(draws[2])}; !slices.Equal(strengths, []float32{1, 1}) {
		t.Errorf("the later versions read strengths %v, want 1 in both", strengths)
	}
	groups := findMaterialGroups(h, "scene")[0]
	if len(groups) != 3 {
		t.Fatalf("the pass set @group(1) %v, want three bind groups, one for each version", groups)
	}
	calls := h.Calls()
	first := findEntry(calls, groups[1], 2)
	second := findEntry(calls, groups[2], 2)
	if first != second {
		t.Errorf("the versions bind tint at %+v, then at %+v, want one range of the arena, copied once", first, second)
	}
}

func TestAUniformBlockSetToTheBytesItHoldsMakesNoVersion(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene parameterScene
	spawnParameterHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeParameterScene(inventory)
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(scene.material)
			for range 3 {
				inventory.SetMaterialUniform(scene.material, "tint", tint{Strength: 1})
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
			}
		})
	})

	if groups := findMaterialGroups(h, "scene")[0]; len(groups) != 1 {
		t.Errorf("the pass set @group(1) %v, want one bind group, the same bytes set again making no version", groups)
	}
	if strengths := makeStrengths(findPassDraws(h, "scene")[0]); !slices.Equal(strengths, []float32{1, 1, 1}) {
		t.Errorf("the draws read strengths %v, want 1 in each", strengths)
	}
}

// makeStrengths answers the strength of the tint each of draws binds.
func makeStrengths(draws []render.InspectedDraw) []float32 {
	var strengths []float32
	for _, draw := range draws {
		strengths = append(strengths, readStrength(draw))
	}
	return strengths
}

func TestAChangeBeforeAMaterialsFirstDrawInTheFrameMakesNoVersion(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene parameterScene
	spawnParameterHarness(t, h, 3, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makeParameterScene(inventory)
			inventory.SetMaterialTexture(scene.material, "albedo", scene.albedo)
		}
		inventory.SetMaterialUniform(scene.material, "tint", tint{Strength: float32(frameIndex)})
		if frameIndex == 2 {
			inventory.SetMaterialTexture(scene.material, "albedo", scene.layers)
		}
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(scene.material)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
	})

	draws := findOnlyDraw(t, h, "scene")
	for frameIndex, draw := range draws {
		if strength := readStrength(draw); strength != float32(frameIndex) {
			t.Errorf("frame %d: the draw read strength %v, want %d, set before it", frameIndex, strength, frameIndex)
		}
	}
	groups := findMaterialGroups(h, "scene")
	if groups[0][0] != groups[1][0] {
		t.Errorf("the second frame set @group(1) %v, after %v, want the same bind group, its block written in place", groups[1][0], groups[0][0])
	}
	if groups[2][0] == groups[1][0] {
		t.Error("the third frame set the second's @group(1), want a new bind group for its new texture")
	}
	if label := findEntryBufferLabel(h.Calls(), groups[2][0], 2); !strings.Contains(label, "uniform block tint") {
		t.Errorf("the third frame binds tint in buffer %q, want the material's own", label)
	}
	if frame, ok := findReleaseFrame(h.Calls(), recording.CallKindReleaseBindGroup, groups[1][0]); !ok || frame != 2 {
		t.Errorf("the bind group the third frame's change replaced was released after %d submits (%v), want 2, at the change, before the frame drew", frame, ok)
	}
}

func TestVersionsAreFreedAtTheFramesEndAfterThePresent(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene parameterScene
	spawnParameterHarness(t, h, 2, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makeParameterScene(inventory)
		}
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(scene.material)
			if frameIndex == 0 {
				inventory.SetMaterialUniform(scene.material, "tint", tint{Strength: 1})
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
				inventory.SetMaterialUniform(scene.material, "tint", tint{Strength: 2})
				inventory.SetMaterialTexture(scene.material, "albedo", scene.albedo)
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
			}
		})
	})

	calls := h.Calls()
	created := findCreatedBindGroups(calls, materialBindGroupLabel)
	groups := findMaterialGroups(h, "scene")[0]
	if len(groups) != 2 {
		t.Fatalf("the first frame set @group(1) %v, want two bind groups", groups)
	}
	presents := findCallIndices(calls, recording.CallKindPresent)
	for index, group := range groups {
		if created[group] != 0 {
			t.Errorf("version %d was made after %d submits, want 0", index, created[group])
		}
		releaseIndex := slices.IndexFunc(calls, func(call recording.Call) bool {
			return call.Kind == recording.CallKindReleaseBindGroup && call.Arguments == any(group)
		})
		if releaseIndex < 0 || len(presents) == 0 || releaseIndex < presents[0] || (len(presents) > 1 && releaseIndex > presents[1]) {
			t.Errorf("version %d was released at call %d, want between the first frame's present, at %v, and the next", index, releaseIndex, presents)
		}
	}
}

func TestAMaterialChangedWhileListsRecordItDrawsAValueSetBeforeEachDraw(t *testing.T) {
	const listCount = 4
	const drawCount = 16
	h := rendertest.NewHarness(t)
	var made sync.Once
	var scene parameterScene
	var nextOrder atomic.Int32
	files := makeDrawFiles(t)
	putFile(t, files, "material.wgsl", []byte("// material.wgsl"))
	putFile(t, files, "material.wgsl.json", []byte(materialDeclaration))
	h.Spawn(&render.Config{TemporaryChunkSize: 1024}, &loaderPlugin{files: files, recordingTaskCount: listCount, duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		made.Do(func() { scene = makeParameterScene(inventory) })
		order := nextOrder.Add(1)
		list := frame.BeginCommandList("draws", drawOrder+render.Order(order))
		pass := list.BeginPass(render.PassDescriptor{Label: "scene", ColorTargets: []render.ColorTarget{{Texture: scene.color}}})
		pass.SetMaterial(scene.material)
		for index := range drawCount {
			inventory.SetMaterialUniform(scene.material, "tint", tint{Strength: float32(1 + index)})
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		}
		pass.End()
		list.End()
	}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	passes := findPassDraws(h, "scene")
	if len(passes) != 2*listCount {
		t.Fatalf("the reader read %d passes, want %d", len(passes), 2*listCount)
	}
	for passIndex, draws := range passes {
		if len(draws) != drawCount {
			t.Errorf("pass %d drew %d times, want %d", passIndex, len(draws), drawCount)
		}
		for drawIndex, draw := range draws {
			if strength := readStrength(draw); strength < 1 || strength > drawCount {
				t.Errorf("pass %d, draw %d: tint read strength %v, want one a list set", passIndex, drawIndex, strength)
			}
		}
	}
}
