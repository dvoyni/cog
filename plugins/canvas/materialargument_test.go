package canvas_test

import (
	"image/color"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
)

// dissolveShaderPath is the dissolve test material's shader, in testdata,
// which compiles only with CANVAS_ARGUMENT: an entry's image times its tint
// times its argument's colour, dissolved from its left edge by its
// argument's amount.
const dissolveShaderPath = "testdata/dissolve.wgsl"

// dissolve is the dissolve test material's argument, the shader's struct
// Argument: its colour at offset 16, past the padding WGSL lays after a
// vec4<f32>'s alignment.
type dissolve struct {
	Amount float32
	_      [3]float32
	Colour [4]float32
}

// unpaddedDissolve has dissolve's fields with no padding, its colour at
// offset 4, where the shader's struct Argument has none.
type unpaddedDissolve struct {
	Amount float32
	Colour [4]float32
}

// newDissolveMounter answers a file mounter of the test materials' shaders,
// the dissolve shader among them, and a white image, "white.png".
func newDissolveMounter(t testing.TB) *fileMounter {
	t.Helper()
	mounter := newMaterialMounter(t)
	files := mounter.files.(fstest.MapFS)
	addModuleFile(t, files, dissolveShaderPath)
	addModuleFile(t, files, dissolveShaderPath+".json")
	addSolidFile(t, files, "white.png", 4, 4, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	return mounter
}

// recordDissolvedEntries records, with material, the dissolve material, five
// entries 40 units square in a row along the x axis, all of one batch: a
// fill given a red argument dissolving none of it, centred at -130; a fill
// given a green one dissolving its left half, at -70; a sprite of white
// given a blue one dissolving its left half by DrawSpritesWith, at -10; a
// sprite given a yellow one dissolving all of it by DrawSpriteWith, at 50;
// and a fill given none, the zero value, of no colour, at 110.
func recordDissolvedEntries(list canvas.CommandList, material canvas.Material, white canvas.Image) {
	list.DrawShapeWith(canvas.Shape{Rect: m.Rect{X: -150, Y: -20, Width: 40, Height: 40}, Tint: m.White, Material: material}, dissolve{Colour: [4]float32{1, 0, 0, 1}})
	list.DrawShapeWith(canvas.Shape{Rect: m.Rect{X: -90, Y: -20, Width: 40, Height: 40}, Tint: m.White, Material: material}, dissolve{Amount: 0.5, Colour: [4]float32{0, 1, 0, 1}})
	list.DrawSpritesWith([]canvas.Sprite{{Image: white, Transform: m.Translation2x3(-10, 0), Size: m.Vec2{X: 40, Y: 40}, Material: material}}, []dissolve{{Amount: 0.5, Colour: [4]float32{0, 0, 1, 1}}})
	list.DrawSpriteWith(canvas.Sprite{Image: white, Transform: m.Translation2x3(50, 0), Size: m.Vec2{X: 40, Y: 40}, Material: material}, dissolve{Amount: 1, Colour: [4]float32{1, 1, 0, 1}})
	list.DrawShape(canvas.Shape{Rect: m.Rect{X: 90, Y: -20, Width: 40, Height: 40}, Tint: m.White, Material: material})
}

func TestADissolveMaterialReadsEachEntrysOwnArgument(t *testing.T) {
	white := canvas.ImageFromFile("white.png")
	recorder := &materialRecorder{
		descriptors: []canvas.MaterialDescriptor{{Label: "dissolve", Shader: dissolveShaderPath, Argument: canvas.ArgumentOf[dissolve]()}},
		record: func(list canvas.CommandList, assets canvas.Assets, materials []canvas.Material, frame int) {
			recordDissolvedEntries(list, materials[0], white)
		},
	}

	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(canvas.CommandList) {}, recorder, newDissolveMounter(t))

	picture.expect(t, []probe{
		{"the red fill's left", m.Vec2{X: -140}, opaqueRed},
		{"the red fill's right", m.Vec2{X: -120}, opaqueRed},
		{"the green fill's dissolved left", m.Vec2{X: -80}, opaqueBlack},
		{"the green fill's right", m.Vec2{X: -60}, opaqueGreen},
		{"the blue sprite's dissolved left", m.Vec2{X: -20}, opaqueBlack},
		{"the blue sprite's right", m.Vec2{X: 0}, opaqueBlue},
		{"the yellow sprite, dissolved whole", m.Vec2{X: 50}, opaqueBlack},
		{"the fill of the zero argument, of no colour", m.Vec2{X: 110}, opaqueBlack},
	})
}

func TestEntriesOfAMaterialTakingAnArgumentDrawAsOneBatchOfItsStride(t *testing.T) {
	// The five dissolved entries, however each gave its argument or none,
	// are one batch, one draw of five instances, from a buffer of rows of
	// 176 bytes: canvas's 144, then the argument's 32.
	white := canvas.ImageFromFile("white.png")
	recorder := &materialRecorder{
		descriptors: []canvas.MaterialDescriptor{{Label: "dissolve", Shader: dissolveShaderPath, Argument: canvas.ArgumentOf[dissolve]()}},
		record: func(list canvas.CommandList, assets canvas.Assets, materials []canvas.Material, frame int) {
			recordDissolvedEntries(list, materials[0], white)
		},
	}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, recorder, newDissolveMounter(t))
	h.OpenWindow(platform.Size{Width: 320, Height: 240})

	h.Step()

	frame := h.Frames()[0]
	batches := frame.Cameras[0].Batches
	if len(batches) != 1 || len(batches[0].Entries) != 5 {
		t.Fatalf("the dissolved entries drew as batches %+v, want one of five", batches)
	}
	draws := findPass(t, findCanvasLists(frame.Render)[0], "canvas camera").Draws
	if len(draws) != 1 || draws[0].InstanceCount != 5 || draws[0].FirstInstance != 0 {
		t.Fatalf("the camera's pass drew %+v, want one draw of 5 instances from 0", draws)
	}
	if instances := findParameter(t, draws[0].DrawParameters, "canvas_instances"); instances.Buffer.Size != 5*176 {
		t.Errorf("the draw's instance data is %d bytes, want 5 rows of 176, %d", instances.Buffer.Size, 5*176)
	}
}

func TestAnArgumentItsMaterialDoesNotTakeIsRefusedReportedOnceForTheMaterialAndType(t *testing.T) {
	// Each frame draws a fill of the dissolve material given a uint32, one
	// of the default material given a dissolve, both skipped, and one of
	// the dissolve material given a dissolve, drawn.
	recorder := &materialRecorder{
		descriptors: []canvas.MaterialDescriptor{{Label: "dissolve", Shader: dissolveShaderPath, Argument: canvas.ArgumentOf[dissolve]()}},
		record: func(list canvas.CommandList, assets canvas.Assets, materials []canvas.Material, frame int) {
			list.DrawShapeWith(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}, Material: materials[0]}, uint32(1))
			list.DrawShapeWith(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}}, dissolve{})
			list.DrawShapeWith(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}, Material: materials[0]}, dissolve{})
		},
	}
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindArgumentRefused)
	h.Spawn(nil, nil, recorder, newDissolveMounter(t))
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	for index, frame := range h.Frames() {
		batches := frame.Cameras[0].Batches
		if len(batches) != 1 || len(batches[0].Entries) != 1 || batches[0].Material != recorder.madeMaterials()[0] {
			t.Errorf("frame %d drew batches %+v, want one, of the dissolve material's one entry given a dissolve", index, batches)
		}
	}
	if count := countReports(h, canvas.ReportKindArgumentRefused); count != 2 {
		t.Errorf("two frames of two refused arguments reported %d refusals, want 2, one for each material and type", count)
	}
}

func TestAnArgumentTypeWhoseLayoutMismatchesItsShadersIsReportedOnceAndItsMaterialsEntriesSkipped(t *testing.T) {
	// Two materials of the dissolve shader take an argument of no padding,
	// whose colour lies at offset 4, not 16: the mismatch is reported once,
	// for the shader and the type, and neither material's entries draw.
	descriptor := canvas.MaterialDescriptor{Label: "unpadded", Shader: dissolveShaderPath, Argument: canvas.ArgumentOf[unpaddedDissolve]()}
	recorder := &materialRecorder{
		descriptors: []canvas.MaterialDescriptor{descriptor, descriptor},
		record: func(list canvas.CommandList, assets canvas.Assets, materials []canvas.Material, frame int) {
			list.DrawShapeWith(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}, Material: materials[0]}, unpaddedDissolve{})
			list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}, Material: materials[1]})
			list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}})
		},
	}
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindArgumentLayoutMismatch)
	h.Spawn(nil, nil, recorder, newDissolveMounter(t))
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()

	batches := h.Frames()[0].Cameras[0].Batches
	if len(batches) != 1 || batches[0].Material != (canvas.Material{}) {
		t.Errorf("the frame drew batches %+v, want one, of the default material's fill alone", batches)
	}
	if count := countReports(h, canvas.ReportKindArgumentLayoutMismatch); count != 1 {
		t.Errorf("two materials of a mismatched argument reported %d mismatches, want one, for the shader and the type", count)
	}
}

func TestParallelSlicesOfDifferentLengthsAreRefusedRecordingNoneReportedOnceForTheCommandList(t *testing.T) {
	// Each frame gives DrawSpritesWith two sprites and one argument, then
	// draws a fill of the default material.
	white := canvas.ImageFromFile("white.png")
	recorder := &materialRecorder{
		descriptors: []canvas.MaterialDescriptor{{Label: "dissolve", Shader: dissolveShaderPath, Argument: canvas.ArgumentOf[dissolve]()}},
		record: func(list canvas.CommandList, assets canvas.Assets, materials []canvas.Material, frame int) {
			sprite := canvas.Sprite{Image: white, Material: materials[0]}
			list.DrawSpritesWith([]canvas.Sprite{sprite, sprite}, []dissolve{{}})
			list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}})
		},
	}
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindSliceLengthsDiffer)
	h.Spawn(nil, nil, recorder, newDissolveMounter(t))
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	for index, frame := range h.Frames() {
		batches := frame.Cameras[0].Batches
		if len(batches) != 1 || len(batches[0].Entries) != 1 || batches[0].Entries[0].Kind != canvas.EntryKindShape {
			t.Errorf("frame %d drew batches %+v, want one, of the fill alone", index, batches)
		}
	}
	if count := countReports(h, canvas.ReportKindSliceLengthsDiffer); count != 1 {
		t.Errorf("two frames of uneven slices reported %d refusals, want one, for the command list", count)
	}
}

func TestArgumentOfPanicsForATypeHoldingAPointer(t *testing.T) {
	type labelled struct {
		Amount float32
		Label  [1]struct{ Name string }
	}
	canvas.ArgumentOf[dissolve]()
	canvas.ArgumentOf[float32]()
	for name, argumentOf := range map[string]func() canvas.ArgumentType{
		"*float32":             canvas.ArgumentOf[*float32],
		"[]float32":            canvas.ArgumentOf[[]float32],
		"a string in an array": canvas.ArgumentOf[labelled],
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("ArgumentOf of %s answered, want a panic", name)
				}
			}()
			argumentOf()
		}()
	}
}
