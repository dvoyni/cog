package canvas_test

//go:generate go run github.com/dvoyni/cog/plugins/render/cmd/declarebindings testdata/tint.wgsl testdata/rawcolour.wgsl

import (
	"image/color"
	"os"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// The test materials' shaders, in testdata, each including canvas's
// include from the assets folder beside it: tintShaderPath's draws an
// entry's image times its tint times its material_tint, and
// rawColourShaderPath's writes its raw_colour as it is, unencoded.
const (
	tintShaderPath      = "testdata/tint.wgsl"
	rawColourShaderPath = "testdata/rawcolour.wgsl"
)

// newMaterialMounter answers a file mounter of the test materials' shaders,
// their binding declarations and canvas's include they include, at their
// paths in the module, which the include's path is relative to.
func newMaterialMounter(t testing.TB) *fileMounter {
	t.Helper()
	files := fstest.MapFS{}
	for _, path := range []string{tintShaderPath, tintShaderPath + ".json", rawColourShaderPath, rawColourShaderPath + ".json", "assets/canvas.wgsl"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = &fstest.MapFile{Data: data}
	}
	return &fileMounter{files: files}
}

// findBatchMaterials answers the material of each batch camera drew, in the
// order it drew them.
func findBatchMaterials(camera canvas.InspectedCamera) []canvas.Material {
	var materials []canvas.Material
	for _, inspectedBatch := range camera.Batches {
		materials = append(materials, inspectedBatch.Material)
	}
	return materials
}

func TestAFragmentOnlyMaterialTintsSpritesAndShapesAlike(t *testing.T) {
	// White entries drawn with a material tinting them cyan, through
	// canvas's vertex stage, show cyan; one drawn with the default
	// material between them stays white.
	mounter := newMaterialMounter(t)
	addSolidFile(t, mounter.files.(fstest.MapFS), "white.png", 4, 4, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	white := canvas.ImageFromFile("white.png")
	recorder := &materialRecorder{
		descriptors: []canvas.MaterialDescriptor{{Label: "tint", Shader: tintShaderPath}},
		record: func(list canvas.DrawList, assets canvas.Assets, materials []canvas.Material, frame int) {
			if frame == 0 {
				assets.SetMaterialUniform(materials[0], "material_tint", [4]float32{0, 1, 1, 1})
			}
			list.DrawShape(canvas.Shape{Rect: m.Rect{X: -100, Y: -20, Width: 40, Height: 40}, Tint: m.White, Material: materials[0]})
			list.DrawShape(canvas.Shape{Rect: m.Rect{X: -20, Y: -20, Width: 40, Height: 40}, Tint: m.White})
			list.DrawSprite(canvas.Sprite{Image: white, Transform: m.Translation2x3(80, 0), Size: m.Vec2{X: 40, Y: 40}, Material: materials[0]})
		},
	}

	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(canvas.DrawList) {}, recorder, mounter)

	picture.expect(t, []probe{
		{"the shape of the material", m.Vec2{X: -80}, opaqueCyan},
		{"the shape of the default material", m.Vec2{}, opaqueWhite},
		{"the sprite of the material", m.Vec2{X: 80}, opaqueCyan},
		{"the background", m.Vec2{X: 40}, opaqueBlack},
	})
}

func TestEachBlendConstructorBlendsAsItsNameSaysOverAKnownColour(t *testing.T) {
	// A fill of raw (0.2, 0.4, 0, 0.6), premultiplied, is drawn in a column
	// of each blend over an opaque fill of raw (0.8, 0.2, 1), unencoded, so
	// each column shows its blend's sum in bytes: normal, source + 0.4 of
	// the destination; additive, their sum; multiply, their product + 0.4
	// of the destination; screen, source + the destination × (1 − source);
	// opaque, the source; and the zero blend, normal's.
	source := [4]float32{0.2, 0.4, 0, 0.6}
	blends := []struct {
		name  string
		blend render.BlendState
		want  color.NRGBA
	}{
		{"normal", canvas.BlendNormal(), color.NRGBA{R: 133, G: 122, B: 102, A: 255}},
		{"additive", canvas.BlendAdditive(), color.NRGBA{R: 255, G: 153, B: 255, A: 255}},
		{"multiply", canvas.BlendMultiply(), color.NRGBA{R: 122, G: 41, B: 102, A: 255}},
		{"screen", canvas.BlendScreen(), color.NRGBA{R: 214, G: 133, B: 255, A: 255}},
		{"opaque", canvas.BlendOpaque(), color.NRGBA{R: 51, G: 102, A: 255}},
		{"zero", render.BlendState{}, color.NRGBA{R: 133, G: 122, B: 102, A: 255}},
	}
	descriptors := []canvas.MaterialDescriptor{{Label: "destination", Shader: rawColourShaderPath, Blend: canvas.BlendOpaque()}}
	for _, blend := range blends {
		descriptors = append(descriptors, canvas.MaterialDescriptor{Label: blend.name, Shader: rawColourShaderPath, Blend: blend.blend})
	}
	// columnX is the left of each blend's column.
	columnX := func(index int) float32 { return -150 + 50*float32(index) }
	mounter := newMaterialMounter(t)
	recorder := &materialRecorder{
		descriptors: descriptors,
		record: func(list canvas.DrawList, assets canvas.Assets, materials []canvas.Material, frame int) {
			if frame == 0 {
				assets.SetMaterialUniform(materials[0], "raw_colour", [4]float32{0.8, 0.2, 1, 1})
				for _, material := range materials[1:] {
					assets.SetMaterialUniform(material, "raw_colour", source)
				}
			}
			list.DrawShape(canvas.Shape{Rect: m.Rect{X: -160, Y: -40, Width: 320, Height: 80}, Material: materials[0]})
			for index, material := range materials[1:] {
				list.DrawShape(canvas.Shape{Rect: m.Rect{X: columnX(index), Y: -20, Width: 40, Height: 40}, Material: material, Order: 1})
			}
		},
	}

	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(canvas.DrawList) {}, recorder, mounter)

	probes := []probe{{"the destination between the columns", m.Vec2{X: columnX(0) + 45}, color.NRGBA{R: 204, G: 51, B: 255, A: 255}}}
	for index, blend := range blends {
		probes = append(probes, probe{blend.name + "'s column", m.Vec2{X: columnX(index) + 20}, blend.want})
	}
	picture.expect(t, probes)
}

func TestAMaterialParameterSetByATaskShowsInTheFrameCanvasDrawsAfterIt(t *testing.T) {
	// Each frame's task records a white fill of the tint material, then
	// sets its tint to the frame's colour, cyan and yellow in turn, and
	// records a fill of the default material tinted the same: whichever
	// frame the capture holds, the two fills show one colour, the
	// material's tint set after its entry was recorded.
	mounter := newMaterialMounter(t)
	recorder := &materialRecorder{
		descriptors: []canvas.MaterialDescriptor{{Label: "tint", Shader: tintShaderPath, FragmentEntryPoint: "tint_fragment"}},
		record: func(list canvas.DrawList, assets canvas.Assets, materials []canvas.Material, frame int) {
			colour := m.Color{G: 1, B: 1, A: 1}
			if frame%2 == 1 {
				colour = m.Color{R: 1, G: 1, A: 1}
			}
			list.DrawShape(canvas.Shape{Rect: m.Rect{X: -60, Y: -20, Width: 40, Height: 40}, Tint: m.White, Material: materials[0]})
			assets.SetMaterialUniform(materials[0], "material_tint", [4]float32{colour.R, colour.G, colour.B, colour.A})
			list.DrawShape(canvas.Shape{Rect: m.Rect{X: 20, Y: -20, Width: 40, Height: 40}, Tint: colour})
		},
	}

	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(canvas.DrawList) {}, recorder, mounter)

	material := picture.at(m.Vec2{X: -40})
	tinted := picture.at(m.Vec2{X: 40})
	if material != tinted || (material != opaqueCyan && material != opaqueYellow) {
		t.Errorf("the fill of the material shows %v and the fill tinted alike %v, want both cyan or both yellow, the material's tint set in the frame that drew it", material, tinted)
	}
}

func TestFillsOfTwoMaterialsInterleavedDrawAsTheirSortOrderSaysBreakingBatchesOnlyWhereTheMaterialChanges(t *testing.T) {
	// The first layer's fills, of the first, the second and the first
	// material again, draw in their order; the regrouped layer's, of the
	// second and the first in turn at one key, draw grouped by material,
	// the second's first, joining no batch before them: five batches, each
	// drawn from its own first instance, through canvas's vertex stage,
	// blending normally.
	mounter := newMaterialMounter(t)
	recorder := &materialRecorder{
		descriptors: []canvas.MaterialDescriptor{{Label: "first", Shader: tintShaderPath}, {Label: "second", Shader: tintShaderPath}},
		record: func(list canvas.DrawList, assets canvas.Assets, materials []canvas.Material, frame int) {
			for order, material := range []canvas.Material{materials[0], materials[1], materials[0]} {
				list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}, Material: material, Order: int32(order)})
			}
			for _, material := range []canvas.Material{materials[1], materials[0], materials[1], materials[0]} {
				list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}, Material: material, Layer: 1})
			}
		},
	}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, &canvas.Config{RegroupedLayers: []int32{1}}, recorder, mounter)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()

	materials := recorder.madeMaterials()
	first := materials[0]
	second := materials[1]
	frame := h.Frames()[0]
	batchMaterials := findBatchMaterials(frame.Cameras[0])
	if want := []canvas.Material{first, second, first, second, first}; !slices.Equal(batchMaterials, want) {
		t.Fatalf("the fills drew as batches of materials %v, want %v", batchMaterials, want)
	}
	draws := findPass(t, findCanvasLists(frame.Render)[0], "canvas camera").Draws
	if len(draws) != 5 {
		t.Fatalf("the camera's pass drew %d draws, want one for each of 5 batches: %+v", len(draws), draws)
	}
	wantFirstInstances := []uint32{0, 1, 2, 3, 5}
	wantInstanceCounts := []uint32{1, 1, 1, 2, 2}
	firstRendered := draws[0].Material.Material
	secondRendered := draws[1].Material.Material
	if firstRendered == secondRendered {
		t.Errorf("the two materials drew with one render material, %v", firstRendered)
	}
	// render's inspection spells each value of a state that is its default
	// zero, so normal blending's source factor, One, and its adding are.
	normalComponent := render.BlendComponent{DestinationFactor: render.BlendFactorOneMinusSourceAlpha}
	wantBlend := render.BlendState{Color: normalComponent, Alpha: normalComponent}
	for index, draw := range draws {
		wantRendered := firstRendered
		if index%2 == 1 {
			wantRendered = secondRendered
		}
		if draw.Material.Material != wantRendered || draw.FirstInstance != wantFirstInstances[index] || draw.InstanceCount != wantInstanceCounts[index] {
			t.Errorf("draw %d is of render material %v, %d instances from %d, want %v, %d from %d", index, draw.Material.Material, draw.InstanceCount, draw.FirstInstance, wantRendered, wantInstanceCounts[index], wantFirstInstances[index])
		}
		if draw.Material.VertexEntryPoint != "canvas_sprite_vertex" || draw.Material.State.ColorTargets[0].Blend != wantBlend {
			t.Errorf("draw %d's material has vertex stage %q and blend %+v, want canvas's own, canvas_sprite_vertex, and BlendNormal()'s, %+v", index, draw.Material.VertexEntryPoint, draw.Material.State.ColorTargets[0].Blend, wantBlend)
		}
	}
}

func TestAReleasedMaterialDrawsTheFrameOfItsReleaseAndItsLaterEntriesAreSkippedReportedOnce(t *testing.T) {
	// The second frame records a fill of the material, then releases it,
	// and releases it again; the third and fourth record a fill of it, and
	// one of the default material.
	mounter := newMaterialMounter(t)
	recorder := &materialRecorder{
		descriptors: []canvas.MaterialDescriptor{{Label: "tint", Shader: tintShaderPath}},
		record: func(list canvas.DrawList, assets canvas.Assets, materials []canvas.Material, frame int) {
			list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}, Material: materials[0]})
			if frame == 1 {
				assets.ReleaseMaterial(materials[0])
				assets.ReleaseMaterial(materials[0])
			}
			if frame >= 2 {
				list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}, Order: 1})
			}
		},
	}
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindMaterialNotHeld)
	h.Spawn(nil, nil, recorder, mounter)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 4 {
		h.Step()
	}

	material := recorder.madeMaterials()[0]
	for index, frame := range h.Frames() {
		batchMaterials := findBatchMaterials(frame.Cameras[0])
		want := []canvas.Material{material}
		if index >= 2 {
			want = []canvas.Material{{}}
		}
		if !slices.Equal(batchMaterials, want) {
			t.Errorf("frame %d drew batches of materials %v, want %v", index, batchMaterials, want)
		}
	}
	if count := countReports(h, canvas.ReportKindMaterialNotHeld); count != 1 {
		t.Errorf("releasing the material twice and drawing it after reported %d refusals, want one, for the material", count)
	}
}

func TestAMaterialsCullMarginKeepsItsEntriesOutsideTheViewByLessThanIt(t *testing.T) {
	// Two fills lie 5 units past the right of the view, 32 units from the
	// centre: the one of a material of a cull margin of 6 is drawn, and the
	// one of the default material culled.
	mounter := newMaterialMounter(t)
	recorder := &materialRecorder{
		descriptors: []canvas.MaterialDescriptor{{Label: "swaying", Shader: tintShaderPath, CullMargin: 6}},
		record: func(list canvas.DrawList, assets canvas.Assets, materials []canvas.Material, frame int) {
			list.DrawShape(canvas.Shape{Rect: m.Rect{X: 37, Width: 4, Height: 4}, Material: materials[0]})
			list.DrawShape(canvas.Shape{Rect: m.Rect{X: 37, Width: 4, Height: 4}, Order: 1})
		},
	}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, recorder, mounter)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()

	batches := h.Frames()[0].Cameras[0].Batches
	if len(batches) != 1 || batches[0].Material != recorder.madeMaterials()[0] {
		t.Errorf("the fills past the view drew as batches %+v, want one, of the material of a cull margin", batches)
	}
}
