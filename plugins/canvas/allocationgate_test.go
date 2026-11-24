package canvas_test

import (
	"image/color"
	"maps"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
)

// The paths of the files makeGateFiles answers, besides the quadrants', the
// panel's and the dissolve shader's.
const (
	gateSolidPath = "gate/solid.png"
	gateTreePath  = "gate/tree.mesh"
)

// gateSecondShaderPath is the shader of the material a gate's sprites switch
// to: canvas's default material's, under a material of its own.
const gateSecondShaderPath = "cog.canvas/default.wgsl"

// gateWindowSize is the size of the window the gate's frames and the
// benchmarks draw into.
var gateWindowSize = platform.Size{Width: 320, Height: 240}

// makeGateFiles answers the files the gate's frames draw: the dissolve
// mounter's, the quadrants, the panel, a solid image and the tree mesh
// file.
func makeGateFiles(t testing.TB) fstest.MapFS {
	t.Helper()
	files := newDissolveMounter(t).files.(fstest.MapFS)
	maps.Copy(files, makeQuadrantsFiles(t))
	addPanelFile(t, files)
	addSolidFile(t, files, gateSolidPath, 8, 8, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
	files[gateTreePath] = &fstest.MapFile{Data: encodeMeshFile(t, gateTreePath, makeSquareTriangles(10))}
	return files
}

// gateWarmFrameCount is how many frames the gate steps before it measures,
// for canvas's tables, atlases and draw state, render's pools, gfx's and the
// kernel's to grow, and every first use to have loaded.
const gateWarmFrameCount = 4

// gateMeasuredFrameCount is how many frames the gate measures, besides
// AllocsPerRun's own warm-up frame: its count a frame is rounded down, so
// fewer allocations than this over them all, made by the runtime or the
// test framework meanwhile, count none.
const gateMeasuredFrameCount = 10

// spawnGateHarness spawns h with a concurrentUser calling records, each in a
// recording task of its own, and the gate's files mounted, and opens a
// window of gateWindowSize.
func spawnGateHarness(t testing.TB, h *canvastest.Harness, records ...recordFunc) {
	t.Helper()
	h.Spawn(nil, nil, &fileMounter{files: makeGateFiles(t)}, &concurrentUser{records: records})
	h.OpenWindow(gateWindowSize)
}

func TestTheGatesFrameDrawsEveryKindOfEntryThroughCamerasOfTwoOrdersAndCullsHalfItsSprites(t *testing.T) {
	// Two tasks each record 16 sprites, 8 of them far right of every view,
	// through two cameras splitting the window, of order 0, and a UI camera
	// of order 1 drawing layer 0.
	h := canvastest.NewHarness(t)
	spawnGateHarness(t, h, makeGateRecords(gateScenario{spriteCount: 32, taskCount: 2, cameraCount: 3, switchPeriod: 4})...)
	h.Step()
	h.Step()

	frame := h.Frames()[1]
	if len(frame.Cameras) != 3 {
		t.Fatalf("the frame drew through %d cameras, want 3", len(frame.Cameras))
	}
	if lists := findCanvasLists(frame.Render); len(lists) != 2 {
		t.Errorf("canvas recorded %d command lists, want one for each of the cameras' two orders", len(lists))
	}
	for index, camera := range frame.Cameras {
		kinds := map[canvas.EntryKind]bool{}
		materialLabels := map[string]bool{}
		for _, batch := range camera.Batches {
			materialLabels[batch.MaterialLabel] = true
			for _, entry := range batch.Entries {
				kinds[entry.Kind] = true
			}
		}
		for _, kind := range []canvas.EntryKind{canvas.EntryKindShape, canvas.EntryKindSprite, canvas.EntryKindText, canvas.EntryKindTriangles, canvas.EntryKindMesh} {
			if !kinds[kind] {
				t.Errorf("camera %d drew no entry of kind %d", index, kind)
			}
		}
		if !materialLabels["second"] || !materialLabels["dissolve"] {
			t.Errorf("camera %d drew batches of materials %v, want the second and the dissolve among them", index, materialLabels)
		}
		culledSprites := 0
		for _, entry := range camera.Culled {
			if entry.Kind == canvas.EntryKindSprite {
				culledSprites++
			}
		}
		if culledSprites != 16 {
			t.Errorf("camera %d culled %d sprites, want the 16 far right", index, culledSprites)
		}
	}
}

// makeGateScenarios answers the gate's scenarios: 16, 1,024 and 16,384
// sprites, split among 1, 4 and 16 recording tasks, drawn through 1, 2 and
// 4 cameras, each with one material in one layer, and with a switch every
// 16 sprites over 16 layers. Under the race detector, it leaves out the
// scenarios of 16,384 sprites, which it slows past use.
func makeGateScenarios() []gateScenario {
	var scenarios []gateScenario
	for _, spriteCount := range []int{16, 1024, 16384} {
		if raceEnabled && spriteCount > 1024 {
			continue
		}
		for _, taskCount := range []int{1, 4, 16} {
			for _, cameraCount := range []int{1, 2, 4} {
				for _, drawing := range []struct{ switchPeriod, layerCount int }{{0, 1}, {16, 16}} {
					scenarios = append(scenarios, gateScenario{spriteCount: spriteCount, taskCount: taskCount, cameraCount: cameraCount, switchPeriod: drawing.switchPeriod, layerCount: drawing.layerCount})
				}
			}
		}
	}
	return scenarios
}

func TestASteadyFrameAllocatesNothingOnceWarm(t *testing.T) {
	for _, scenario := range makeGateScenarios() {
		t.Run(scenario.String(), func(t *testing.T) {
			h := canvastest.NewHarness(t)
			h.RecordNothing()
			spawnGateHarness(t, h, makeGateRecords(scenario)...)
			for range gateWarmFrameCount {
				h.Step()
			}
			allocations := testing.AllocsPerRun(gateMeasuredFrameCount, h.Step)
			if allocations != 0 && !raceEnabled {
				t.Errorf("a warm frame allocated %v times, want none", allocations)
			}
		})
	}
}
