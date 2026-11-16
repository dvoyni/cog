package rendertest_test

import (
	"fmt"
	"testing"

	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/platform/filesystems"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

// gateWarmFrameCount is how many frames the gate steps before it measures,
// for render's pools and tables, gfx's and the kernel's to grow.
const gateWarmFrameCount = 4

// gateMeasuredFrameCount is how many frames the gate measures, besides
// AllocsPerRun's own warm-up frame: its count a frame is rounded down, so
// fewer allocations than this over them all, made by the runtime or the
// test framework meanwhile, count none.
const gateMeasuredFrameCount = 10

// spawnGateHarness spawns h with scenario's gatePlugin and makeGateFiles'
// files mounted, and opens a window.
func spawnGateHarness(t testing.TB, h *rendertest.Harness, scenario gateScenario) {
	t.Helper()
	h.Spawn(nil, &gatePlugin{files: makeGateFiles(t), scenario: scenario})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
}

// makeGateFiles answers the draw tests' files with the gate's shaders and
// passreading.wgsl, each beside its declaration.
func makeGateFiles(t testing.TB) *filesystems.MemoryFS {
	t.Helper()
	return makeDeclaredFiles(t, map[string]string{
		"gatemesh.wgsl":     gateMeshDeclaration,
		"gatevertices.wgsl": gateVerticesDeclaration,
		"gatetexture.wgsl":  gateTextureDeclaration,
		"gatecompute.wgsl":  gateComputeDeclaration,
		"passreading.wgsl":  passReadingDeclaration,
	})
}

// measureFrameAllocations spawns a harness as spawnGateHarness does, steps
// it gateWarmFrameCount frames, and answers how many times a frame
// allocates after them, on gfx's recording adapter recording nothing.
// Under the race detector, whose runtime allocates on its own, it steps the
// frames and answers none.
func measureFrameAllocations(t *testing.T, scenario gateScenario) float64 {
	t.Helper()
	h := rendertest.NewHarness(t)
	h.DisableRecording()
	spawnGateHarness(t, h, scenario)
	for range gateWarmFrameCount {
		h.Step()
	}
	allocations := testing.AllocsPerRun(gateMeasuredFrameCount, h.Step)
	if raceEnabled {
		return 0
	}
	return allocations
}

func TestEachListOfTheGateDrawsEveryDrawAndDispatchItRecords(t *testing.T) {
	for _, shape := range []gateShape{gateShapeMesh, gateShapeVertices, gateShapeTexture} {
		scenario := gateScenario{shape: shape, drawCount: 16, listCount: 2, passCount: 2, switchPeriod: 4, versionCount: 2, temporaryPassParameterCount: 3, temporaryRangeMoves: true}
		t.Run(scenario.String(), func(t *testing.T) {
			h := rendertest.NewHarness(t)
			spawnGateHarness(t, h, scenario)
			h.Step()
			h.Step()

			frames := h.Frames()
			lists := frames[len(frames)-1].CommandLists
			if len(lists) != scenario.listCount {
				t.Fatalf("the frame recorded %d lists, want %d", len(lists), scenario.listCount)
			}
			for index, list := range lists {
				drawCount := 0
				for _, pass := range list.Passes {
					drawCount += len(pass.Draws)
				}
				// Each list draws its draws and the temporary mesh, and list 0
				// the versioned material before and after each version, and
				// the passes given lights.
				wantDrawCount := scenario.drawCount + 1
				if index == 0 {
					wantDrawCount += 1 + scenario.versionCount + scenario.temporaryPassParameterCount
				}
				if drawCount != wantDrawCount {
					t.Errorf("list %d drew %d draws, want %d", index, drawCount, wantDrawCount)
				}
				if len(list.ComputePasses) != 1 || len(list.ComputePasses[0].Dispatches) != 1 {
					t.Errorf("list %d dispatched in compute passes %+v, want one dispatch in one", index, list.ComputePasses)
				}
				if len(list.Copies) != 2 {
					t.Errorf("list %d recorded %d copies and clears, want a copy and a clear", index, len(list.Copies))
				}
			}
		})
	}
}

// makeGateScenarios answers the gate's scenarios: each shape at 16, 256 and
// 4096 draws in each of 1, 4 and 16 lists, each drawn in one pass with the
// material switched every draw, in 8 passes with it switched every 16
// draws, and in one pass with it set once. Under the race detector, it
// leaves out the scenarios of 4096 draws, which it slows past use.
func makeGateScenarios() []gateScenario {
	var scenarios []gateScenario
	for _, shape := range []gateShape{gateShapeMesh, gateShapeVertices, gateShapeTexture} {
		for _, drawCount := range []int{16, 256, 4096} {
			if raceEnabled && drawCount > 256 {
				continue
			}
			for _, listCount := range []int{1, 4, 16} {
				for _, drawing := range []struct{ passCount, switchPeriod int }{{1, 1}, {8, 16}, {1, drawCount}} {
					scenarios = append(scenarios, gateScenario{shape: shape, drawCount: drawCount, listCount: listCount, passCount: drawing.passCount, switchPeriod: drawing.switchPeriod})
				}
			}
		}
	}
	return scenarios
}

func TestEveryPerFrameOperationAllocatesNothingOnceWarm(t *testing.T) {
	for _, scenario := range makeGateScenarios() {
		t.Run(scenario.String(), func(t *testing.T) {
			if allocations := measureFrameAllocations(t, scenario); allocations != 0 {
				t.Errorf("a warm frame allocated %v times, want none", allocations)
			}
		})
	}
}

// The fixed counts of what may allocate in a warm frame: a material version
// allocates gfx's copies of its bind group's entries and of what the group
// depends on, and its material's own bind group, made again in the next
// frame, as many; a pass given a temporary range as a @group(0) storage
// parameter, where none sat in the frames before, allocates its bind group,
// gfx's two copies, and render's key and entry for it, with the buffers the
// entry names.
const (
	versionAllocationCount                     = 2
	versionedMaterialAllocationCount           = 2
	movedTemporaryPassParameterAllocationCount = 5
)

func TestEachMaterialVersionAllocatesItsFixedCount(t *testing.T) {
	for _, versionCount := range []int{1, 8} {
		t.Run(fmt.Sprint(versionCount), func(t *testing.T) {
			scenario := gateScenario{drawCount: 16, listCount: 1, passCount: 1, switchPeriod: 16, versionCount: versionCount}
			want := float64(versionCount*versionAllocationCount + versionedMaterialAllocationCount)
			if raceEnabled {
				want = 0
			}
			if allocations := measureFrameAllocations(t, scenario); allocations != want {
				t.Errorf("a warm frame of %d versions of a material allocated %v times, want %v", versionCount, allocations, want)
			}
		})
	}
}

func TestEachPassGivenATemporaryRangeInGroupZeroAllocatesItsFixedCountWhereTheRangeMoved(t *testing.T) {
	for _, test := range []struct {
		passCount  int
		rangeMoves bool
	}{
		{1, false},
		{8, false},
		{1, true},
		{8, true},
	} {
		t.Run(fmt.Sprintf("passes=%d/moves=%t", test.passCount, test.rangeMoves), func(t *testing.T) {
			scenario := gateScenario{drawCount: 16, listCount: 1, passCount: 1, switchPeriod: 16, temporaryPassParameterCount: test.passCount, temporaryRangeMoves: test.rangeMoves}
			want := 0.0
			if test.rangeMoves && !raceEnabled {
				want = float64(test.passCount * movedTemporaryPassParameterAllocationCount)
			}
			if allocations := measureFrameAllocations(t, scenario); allocations != want {
				t.Errorf("a warm frame of %d passes given a temporary range of lights allocated %v times, want %v", test.passCount, allocations, want)
			}
		})
	}
}

// heldCallCount is how many times the test of the calls on held assets
// makes each, besides AllocsPerRun's own warm-up call.
const heldCallCount = 100

func TestASecondAcquireOfAHeldKeyAnyReleaseAndMakeMeshAllocateNothingOnceWarm(t *testing.T) {
	h := rendertest.NewHarness(t)
	files := makeDrawFiles(t)
	png, _ := makePNG(t, 4, 4, 255)
	putFile(t, files, "texture.png", png)
	for index := range heldCallCount + 1 {
		putFile(t, files, fmt.Sprintf("buffer %d.bin", index), make([]byte, 16))
	}
	// madeBuffers, madeTextures, loadedBuffers, meshes and materials are
	// what the second frame makes and acquires, each held once, and the
	// third releases.
	var madeBuffers, loadedBuffers [heldCallCount + 1]render.Buffer
	var madeTextures [heldCallCount + 1]render.Texture
	var meshes [heldCallCount + 1]render.Mesh
	var materials [heldCallCount + 1]render.Material
	var vertices render.Buffer
	var shader render.Shader
	allocations := map[string]float64{}
	measure := func(name string, call func(index int)) {
		index := 0
		allocations[name] = testing.AllocsPerRun(heldCallCount, func() {
			call(index)
			index++
		})
	}
	acquireHeld := func(inventory render.Inventory) {
		inventory.AcquireTexture("texture.png", render.TextureOptions{})
		inventory.AcquireBuffer("buffer 0.bin", render.BufferUsageFlagStorage)
		inventory.AcquireShader("plain.wgsl")
		inventory.AcquireShader("plain.wgsl", render.ShaderDefine{Name: "SKINNED"})
	}
	makeMesh := func(inventory render.Inventory) render.Mesh {
		return inventory.MakeMesh(render.MeshDescriptor{Label: "mesh", VertexBuffers: []render.VertexBuffer{{Range: render.BufferRange{Buffer: vertices}, Layout: positionLayout}}})
	}
	h.Spawn(nil, &loaderPlugin{files: files, duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		switch frameIndex {
		case 0:
			vertices = inventory.MakeBuffer("vertices", render.BufferUsageFlagVertex, make([]float32, 12))
			shader = inventory.AcquireShader("plain.wgsl")
			acquireHeld(inventory)
			for range heldCallCount + 1 {
				inventory.ReleaseMesh(makeMesh(inventory))
			}
		case 1:
			measure("a second acquire of each held key", func(int) { acquireHeld(inventory) })
			measure("a release of each held key", func(int) {
				inventory.ReleaseTexture(inventory.AcquireTexture("texture.png", render.TextureOptions{}))
				inventory.ReleaseBuffer(inventory.AcquireBuffer("buffer 0.bin", render.BufferUsageFlagStorage))
				inventory.ReleaseShader(inventory.AcquireShader("plain.wgsl"))
				inventory.ReleaseShader(inventory.AcquireShader("plain.wgsl", render.ShaderDefine{Name: "SKINNED"}))
			})
			measure("MakeMesh, and its release", func(int) { inventory.ReleaseMesh(makeMesh(inventory)) })
			for index := range heldCallCount + 1 {
				madeBuffers[index] = inventory.MakeBuffer("made", render.BufferUsageFlagStorage, make([]byte, 16))
				madeTextures[index] = inventory.MakeTexture[byte](render.TextureDescriptor{Label: "made", Width: 4, Height: 4, Format: render.TextureFormatRGBA8Unorm}, nil)
				loadedBuffers[index] = inventory.AcquireBuffer(fmt.Sprintf("buffer %d.bin", index), render.BufferUsageFlagStorage)
				meshes[index] = makeMesh(inventory)
				materials[index] = inventory.MakeMaterial(render.MaterialDescriptor{Label: "made", Shader: shader})
			}
		case 2:
			measure("the last release of a made buffer", func(index int) { inventory.ReleaseBuffer(madeBuffers[index]) })
			measure("the last release of a made texture", func(index int) { inventory.ReleaseTexture(madeTextures[index]) })
			measure("the last release of a loaded buffer", func(index int) { inventory.ReleaseBuffer(loadedBuffers[index]) })
			measure("the last release of a mesh", func(index int) { inventory.ReleaseMesh(meshes[index]) })
			measure("the last release of a material", func(index int) { inventory.ReleaseMaterial(materials[index]) })
		}
	}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	for range 4 {
		h.Step()
	}

	if len(allocations) != 8 {
		t.Fatalf("the test measured %d calls, want 8", len(allocations))
	}
	for name, count := range allocations {
		if count != 0 {
			t.Errorf("%s allocated %v times, want none", name, count)
		}
	}
}
