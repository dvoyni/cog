package rendertest_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

// everyCallScene is what the inspection tests make and draw with, beside a
// passParameterScene: material.wgsl's material, whose tint is a uniform
// block of its own; instanced.wgsl's and fullscreen.wgsl's; cull.wgsl's
// compute shader with its objects and visible buffers; a buffer copies go
// into; and the vertex buffers of instances.
type everyCallScene struct {
	passParameterScene
	lit, instanced, fullscreen render.Material
	cull                       render.Shader
	objects, visible, copied   render.Buffer
	offsets, weights           render.Buffer
}

// makeEveryCallScene makes an everyCallScene through inventory.
func makeEveryCallScene(inventory render.Inventory) everyCallScene {
	scene := everyCallScene{
		passParameterScene: makePassParameterScene(inventory),
		cull:               inventory.AcquireShader("cull.wgsl"),
		objects:            inventory.MakeBuffer("objects", render.BufferUsageFlagStorage, make([]byte, 32)),
		visible:            inventory.MakeBuffer("visible", render.BufferUsageFlagStorage|render.BufferUsageFlagCopySrc, make([]byte, 16)),
		copied:             inventory.MakeBuffer("copied", render.BufferUsageFlagCopySrc, make([]byte, 16)),
		offsets:            inventory.MakeBuffer("offsets", render.BufferUsageFlagVertex, make([]float32, 16)),
		weights:            inventory.MakeBuffer("weights", render.BufferUsageFlagVertex, make([]float32, 4)),
	}
	litState := render.State{CullMode: render.CullModeBack, Depth: render.DepthState{Compare: render.CompareFunctionLess, Write: true}}
	litState.ColorTargets[0].Blend.Color = render.BlendComponent{SourceFactor: render.BlendFactorSourceAlpha}
	scene.lit = inventory.MakeMaterial(render.MaterialDescriptor{Label: "lit", Shader: inventory.AcquireShader("material.wgsl"), State: litState, FragmentEntryPoint: "fragment_main"})
	scene.instanced = inventory.MakeMaterial(render.MaterialDescriptor{Label: "instanced", Shader: inventory.AcquireShader("instanced.wgsl")})
	scene.fullscreen = inventory.MakeMaterial(render.MaterialDescriptor{Label: "fullscreen", Shader: inventory.AcquireShader("fullscreen.wgsl")})
	return scene
}

// recordEveryCall records, through inventory and frame, a frame using every
// kind of call render has: it loads a texture with Mipmaps and a buffer,
// writes a buffer and a texture, takes a temporary texture, buffer and
// mesh, and records into one list a pass of pass parameters, draw
// parameters, a material versioned between two draws, instance buffers and
// a draw with no mesh, a second pass of no pass parameters, a compute pass,
// a copy of each kind, a buffer clear, a readback of each kind, and a copy
// refused, reported. It answers what it took from the pools and the
// materials it drew with, in order.
func recordEveryCall(scene everyCallScene, inventory render.Inventory, frame render.Frame) (render.Texture, []render.BufferRange, render.Mesh, []render.Material) {
	inventory.AcquireTexture("mask.png", render.TextureOptions{Mipmaps: true})
	data := inventory.AcquireBuffer("data.bin", render.BufferUsageFlagCopySrc)
	inventory.WriteBuffer(scene.lightGrid, 0, []float32{1, 2, 3, 4})
	inventory.WriteTexture(scene.albedo, render.TextureRegion{X: 1, Y: 2, Width: 2, Height: 1}, make([]byte, 8))
	inventory.SetMaterialTexture(scene.material, "albedo", scene.albedo)
	inventory.SetMaterialUniform(scene.lit, "tint", tint{Color: [4]float32{1, 0, 0, 1}})
	temporary := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "bloom", Width: 8, Height: 8, Format: render.TextureFormatRGBA8Unorm})

	list := frame.BeginCommandList("everything", drawOrder)
	rows := list.AcquireTemporaryBuffer(make([]float32, 4))
	quadVertices := list.AcquireTemporaryBuffer(make([]float32, 12))
	quads := list.AcquireTemporaryMesh(render.MeshDescriptor{Label: "quads", VertexBuffers: []render.VertexBuffer{{Range: quadVertices, Layout: positionLayout}}})
	pass := list.BeginPass(scene.colorPass("scene"))
	pass.SetPassUniform("camera", [16]float32{1})
	pass.SetPassBuffer("light_grid", render.BufferRange{Buffer: scene.lightGrid})
	pass.SetPassTexture("environment", scene.albedo)
	pass.SetPassSampler("environment_sampler", render.SamplerDescriptor{MagFilter: render.FilterModeLinear})
	pass.SetMaterial(scene.material)
	pass.SetUniform("object", [4]float32{1})
	pass.SetBuffer("rows", rows)
	pass.SetTextureView("mask", render.TextureView{Texture: scene.layers, BaseLayer: 1, LayerCount: 2, Dimension: render.TextureViewDimension2DArray})
	pass.Draw(scene.indexedMesh, render.DrawArgs{InstanceCount: 2})
	pass.SetUniform("object", [4]float32{2})
	pass.Draw(quads, render.DrawArgs{})
	pass.SetMaterial(scene.lit)
	pass.Draw(scene.indexedMesh, render.DrawArgs{})
	inventory.SetMaterialUniform(scene.lit, "tint", tint{Color: [4]float32{0, 1, 0, 1}})
	pass.Draw(scene.plainMesh, render.DrawArgs{FirstInstance: 1})
	pass.SetMaterial(scene.instanced)
	pass.SetInstanceBuffer(2, render.InstanceBuffer{Range: render.BufferRange{Buffer: scene.weights}, Layout: weightsLayout})
	pass.SetInstanceBuffer(0, render.InstanceBuffer{Range: render.BufferRange{Buffer: scene.offsets, Offset: 16}, Layout: offsetsLayout})
	pass.Draw(scene.indexedMesh, render.DrawArgs{InstanceCount: 3})
	pass.SetMaterial(scene.fullscreen)
	pass.DrawVertices(render.PrimitiveTopologyTriangleList, 3, render.DrawArgs{})
	pass.End()
	kept := list.BeginPass(scene.makeKeptPass("kept"))
	kept.SetMaterial(scene.lit)
	kept.Draw(scene.indexedMesh, render.DrawArgs{})
	kept.End()

	computePass := list.BeginComputePass("cull")
	computePass.SetShader(scene.cull, "cull")
	computePass.SetUniform("camera", cameraBlock{Position: [4]float32{1, 2, 3, 1}})
	computePass.SetBuffer("objects", render.BufferRange{Buffer: scene.objects})
	computePass.SetBuffer("visible", render.BufferRange{Buffer: scene.visible})
	computePass.Dispatch(4, 1, 1)
	computePass.End()

	list.CopyBuffer(render.BufferRange{Buffer: scene.visible, Offset: 4, Size: 8}, render.BufferRange{Buffer: scene.copied})
	list.CopyTexture(scene.albedo, render.TextureRegion{}, temporary, render.TextureRegion{X: 4, Y: 4})
	list.CopyBufferToTexture(render.BufferRange{Buffer: data}, scene.albedo, render.TextureRegion{Width: 1, Height: 1})
	list.CopyTextureToBuffer(scene.albedo, render.TextureRegion{X: 1, Width: 1, Height: 1}, render.BufferRange{Buffer: scene.copied})
	list.ClearBuffer(render.BufferRange{Buffer: scene.copied, Offset: 8})
	list.ReadBuffer(render.BufferRange{Buffer: scene.visible, Size: 8})
	list.ReadTexture(scene.albedo, render.TextureRegion{Width: 2, Height: 2})
	list.CopyBuffer(render.BufferRange{Buffer: scene.visible}, render.BufferRange{Buffer: scene.visible})
	list.End()
	materials := []render.Material{scene.material, scene.material, scene.lit, scene.lit, scene.instanced, scene.fullscreen, scene.lit}
	return temporary, []render.BufferRange{rows, quadVertices}, quads, materials
}

// spawnInspectionHarness spawns h with the draw tests' shaders,
// parameters.wgsl, material.wgsl and cull.wgsl beside their declarations, a
// PNG with Mipmaps to load and a buffer's file, running duringFrame in each
// frame's recording task and sending each inspection delivered on
// inspections, and opens a window.
func spawnInspectionHarness(t *testing.T, h *rendertest.Harness, duringFrame func(frameIndex int, inventory render.Inventory, frame render.Frame), inspections chan<- render.Inspection) {
	t.Helper()
	files := makeDrawFiles(t)
	for name, declaration := range map[string]string{"parameters.wgsl": parametersDeclaration, "material.wgsl": materialDeclaration, "cull.wgsl": cullDeclaration} {
		putFile(t, files, name, []byte("// "+name))
		putFile(t, files, name+".json", []byte(declaration))
	}
	mask, _ := makePNG(t, 6, 4, 255)
	putFile(t, files, "mask.png", mask)
	putFile(t, files, "data.bin", make([]byte, 16))
	h.Spawn(nil, &loaderPlugin{
		files:       files,
		duringFrame: duringFrame,
		onInspected: func(event render.InspectedEvent, frame render.Frame) {
			inspection, err := frame.ReadInspection(event)
			if err != nil {
				t.Errorf("reading the inspection of beat %d: %v", event.Beat.Tick, err)
			}
			if inspection.Beat != event.Beat {
				t.Errorf("the inspection of beat %d is of beat %d", event.Beat.Tick, inspection.Beat.Tick)
			}
			inspections <- inspection
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
}

// receiveInspection answers the next inspection inspections delivers,
// failing the test when none comes within a second.
func receiveInspection(t *testing.T, inspections <-chan render.Inspection) render.Inspection {
	t.Helper()
	select {
	case inspection := <-inspections:
		return inspection
	case <-time.After(time.Second):
		t.Fatal("no inspection was delivered")
		return render.Inspection{}
	}
}

// eraseRendersOwn answers inspection without what render/rendertest's
// reader cannot read from gfx's recording: the temporaries, the versioned
// materials, each draw's Material and the pass timings.
func eraseRendersOwn(inspection render.Inspection) render.Inspection {
	inspection.PassTimings = nil
	inspection.TemporaryTextures = nil
	inspection.TemporaryBuffers = nil
	inspection.TemporaryMeshes = nil
	inspection.VersionedMaterials = nil
	lists := slices.Clone(inspection.CommandLists)
	for listIndex := range lists {
		passes := slices.Clone(lists[listIndex].Passes)
		for passIndex := range passes {
			draws := slices.Clone(passes[passIndex].Draws)
			for drawIndex := range draws {
				draws[drawIndex].Material.Material = render.Material{}
			}
			passes[passIndex].Draws = draws
		}
		lists[listIndex].Passes = passes
	}
	inspection.CommandLists = lists
	return inspection
}

// compareInspections fails the test on each part of got that differs from
// want, naming it.
func compareInspections(t *testing.T, got, want render.Inspection) {
	t.Helper()
	compare := func(part string, got, want any) {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the inspection's %s are\n%+v\nwhere the reader's are\n%+v", part, got, want)
		}
	}
	compare("beat", got.Beat, want.Beat)
	if len(got.CommandLists) != len(want.CommandLists) {
		t.Errorf("the inspection has %d command lists, where the reader's has %d", len(got.CommandLists), len(want.CommandLists))
	}
	for index := range min(len(got.CommandLists), len(want.CommandLists)) {
		gotList := got.CommandLists[index]
		wantList := want.CommandLists[index]
		compare("list labels", gotList.Label, wantList.Label)
		if len(gotList.Passes) != len(wantList.Passes) {
			t.Errorf("list %q has %d passes, where the reader's has %d", gotList.Label, len(gotList.Passes), len(wantList.Passes))
		}
		for passIndex := range min(len(gotList.Passes), len(wantList.Passes)) {
			gotPass := gotList.Passes[passIndex]
			wantPass := wantList.Passes[passIndex]
			compare("pass targets", []any{gotPass.Position, gotPass.Label, gotPass.ColorTargets, gotPass.DepthStencil}, []any{wantPass.Position, wantPass.Label, wantPass.ColorTargets, wantPass.DepthStencil})
			if len(gotPass.Draws) != len(wantPass.Draws) {
				t.Errorf("pass %q has %d draws, where the reader's has %d", gotPass.Label, len(gotPass.Draws), len(wantPass.Draws))
			}
			for drawIndex := range min(len(gotPass.Draws), len(wantPass.Draws)) {
				compare("draws", gotPass.Draws[drawIndex], wantPass.Draws[drawIndex])
			}
		}
		compare("compute passes", gotList.ComputePasses, wantList.ComputePasses)
		compare("copies", gotList.Copies, wantList.Copies)
		compare("readbacks", gotList.Readbacks, wantList.Readbacks)
	}
	compare("made assets", got.MadeAssets, want.MadeAssets)
	compare("writes", got.Writes, want.Writes)
	compare("made pipelines", got.MadePipelines, want.MadePipelines)
	compare("made bind groups", got.MadeBindGroups, want.MadeBindGroups)
	compare("reports", got.Reports, want.Reports)
	if !reflect.DeepEqual(got, want) {
		t.Error("the inspection differs from the reader's view of the frame")
	}
}

func TestAnInspectionOfAFrameUsingEveryKindOfCallEqualsTheReadersViewOfItAndListsWhatItMadeAndReported(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindCopyRefused)
	inspections := make(chan render.Inspection, 4)
	var temporaryTexture render.Texture
	var temporaryBuffers []render.BufferRange
	var temporaryMesh render.Mesh
	var drawnMaterials []render.Material
	var scene everyCallScene
	spawnInspectionHarness(t, h, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		switch frameIndex {
		case 0:
			frame.RequestInspection()
		case 1:
			scene = makeEveryCallScene(inventory)
			temporaryTexture, temporaryBuffers, temporaryMesh, drawnMaterials = recordEveryCall(scene, inventory, frame)
		}
	}, inspections)

	h.Step()
	h.Step()
	got := receiveInspection(t, inspections)
	h.Step()

	want := h.Frames()[1]
	var labels []string
	var drawCount, copyCount, readbackCount, dispatchCount int
	for _, list := range want.CommandLists {
		labels = append(labels, list.Label)
		for _, pass := range list.Passes {
			drawCount += len(pass.Draws)
		}
		for _, pass := range list.ComputePasses {
			dispatchCount += len(pass.Dispatches)
		}
		copyCount += len(list.Copies)
		readbackCount += len(list.Readbacks)
	}
	if !slices.Equal(labels, []string{"render: mips", "loader", "everything"}) || drawCount != 9 || dispatchCount != 1 || copyCount != 5 || readbackCount != 2 ||
		len(want.MadeAssets) == 0 || len(want.Writes) == 0 || len(want.MadePipelines) == 0 || len(want.MadeBindGroups) == 0 || len(want.Reports) != 1 {
		t.Fatalf("the reader read lists %q with %d draws, %d dispatches, %d copies and %d readbacks, %d assets made, %d writes, %d pipelines and %d bind groups made and %d reports, "+
			"want the mips', the loader's and the frame's, 7 draws and 2 of mips, a dispatch, 5 copies, 2 readbacks, some of each made and written and a report",
			labels, drawCount, dispatchCount, copyCount, readbackCount, len(want.MadeAssets), len(want.Writes), len(want.MadePipelines), len(want.MadeBindGroups), len(want.Reports))
	}
	compareInspections(t, eraseRendersOwn(got), want)
	if !slices.Equal(got.TemporaryTextures, []render.Texture{temporaryTexture}) || !slices.Equal(got.TemporaryBuffers, temporaryBuffers) || !slices.Equal(got.TemporaryMeshes, []render.Mesh{temporaryMesh}) {
		t.Errorf("the inspection took temporary textures %v, buffers %v and meshes %v from the pools, want %v, %v and %v",
			got.TemporaryTextures, got.TemporaryBuffers, got.TemporaryMeshes, temporaryTexture, temporaryBuffers, temporaryMesh)
	}
	if !slices.Equal(got.VersionedMaterials, []render.Material{scene.lit}) {
		t.Errorf("the inspection versioned materials %v, want %v, the one changed after its draw", got.VersionedMaterials, scene.lit)
	}
	var materials []render.Material
	for _, list := range got.CommandLists {
		for _, pass := range list.Passes {
			for _, draw := range pass.Draws {
				if draw.Material.Shader != (render.Shader{}) {
					materials = append(materials, draw.Material.Material)
				}
			}
		}
	}
	if !slices.Equal(materials, drawnMaterials) {
		t.Errorf("the inspection's draws drew with materials %v, want %v", materials, drawnMaterials)
	}
	if len(got.MadePipelines) != 7 {
		t.Errorf("the inspection lists %d pipelines made, want 7: the mip shader's, the scene's two meshes', lit's, instanced's, fullscreen's and cull's: %+v", len(got.MadePipelines), got.MadePipelines)
	}
	if !slices.ContainsFunc(got.MadePipelines, func(pipeline render.InspectedPipeline) bool {
		return pipeline.Shader == scene.cull && pipeline.ComputeEntryPoint == "cull"
	}) {
		t.Errorf("the inspection lists pipelines %+v, none cull's compute pipeline", got.MadePipelines)
	}
	kinds := map[render.BindGroupKind]int{}
	for _, bindGroup := range got.MadeBindGroups {
		kinds[bindGroup.Kind]++
	}
	for _, kind := range []render.BindGroupKind{render.BindGroupKindParameters, render.BindGroupKindMaterial, render.BindGroupKindDefault, render.BindGroupKindMips} {
		if kinds[kind] == 0 {
			t.Errorf("the inspection lists bind groups made of kinds %v, none of kind %d", kinds, kind)
		}
	}
	if len(got.Reports) != 1 || got.Reports[0].Kind != render.ReportKindCopyRefused {
		t.Errorf("the inspection lists reports %v, want the one copy refused", got.Reports)
	}
}

func TestOneInspectionIsRecordedAtATimeAndARequestWhileOneIsPendingJoinsIt(t *testing.T) {
	h := rendertest.NewHarness(t)
	inspections := make(chan render.Inspection, 8)
	spawnInspectionHarness(t, h, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		switch frameIndex {
		case 0:
			frame.RequestInspection()
			frame.RequestInspection()
		case 1, 4:
			frame.RequestInspection()
		}
		list := frame.BeginCommandList("frame", drawOrder)
		list.End()
	}, inspections)

	for range 7 {
		h.Step()
	}

	frames := h.Frames()
	for _, frameIndex := range []int{1, 5} {
		if got := receiveInspection(t, inspections); got.Beat != frames[frameIndex].Beat {
			t.Errorf("an inspection of beat %d was delivered, want one of beat %d, frame %d's", got.Beat.Tick, frames[frameIndex].Beat.Tick, frameIndex)
		}
	}
	select {
	case got := <-inspections:
		t.Errorf("an inspection of beat %d was delivered, want two alone: of the frame after the first requests, which the request during it joined, and of the frame after the last", got.Beat.Tick)
	default:
	}
}

func TestAnInspectionIsReadOnlyWhileItsEventsJobsRun(t *testing.T) {
	h := rendertest.NewHarness(t)
	inspections := make(chan render.Inspection, 4)
	var delivered render.InspectedEvent
	var readErr error
	files := makeDrawFiles(t)
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
			switch frameIndex {
			case 0:
				frame.RequestInspection()
			case 4:
				_, readErr = frame.ReadInspection(delivered)
			}
		},
		onInspected: func(event render.InspectedEvent, frame render.Frame) {
			delivered = event
			inspection, _ := frame.ReadInspection(event)
			inspections <- inspection
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 5 {
		h.Step()
	}

	receiveInspection(t, inspections)
	if readErr == nil {
		t.Error("reading an inspection after its event's jobs ran answered no error")
	}
}

func TestAFrameNotInspectedAllocatesNothingForTheInspection(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.RecordNothing()
	inspections := make(chan render.Inspection, 4)
	const drawCount = 100
	const frameCount = 6
	var allocations [frameCount]float64
	var scene passParameterScene
	spawnInspectionHarness(t, h, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makePassParameterScene(inventory)
			inventory.SetMaterialTexture(scene.material, "albedo", scene.albedo)
		}
		if frameIndex == 2 {
			frame.RequestInspection()
		}
		list := frame.BeginCommandList("draws", drawOrder)
		pass := list.BeginPass(scene.colorPass("scene"))
		pass.SetPassUniform("camera", [16]float32{1})
		pass.SetMaterial(scene.material)
		pass.SetTexture("mask", scene.layers)
		draw := func() { pass.Draw(scene.indexedMesh, render.DrawArgs{}) }
		if frameIndex < 2 {
			for range drawCount + 1 {
				draw()
			}
		} else {
			allocations[frameIndex] = testing.AllocsPerRun(drawCount, draw)
		}
		pass.End()
		list.End()
	}, inspections)

	for range frameCount {
		h.Step()
	}

	receiveInspection(t, inspections)
	for frameIndex, count := range allocations {
		switch {
		case frameIndex < 2:
		case frameIndex == 3:
			if count == 0 {
				t.Errorf("frame %d, inspected, allocated nothing for its draws: the test cannot tell an inspection's allocations", frameIndex)
			}
		case count != 0:
			t.Errorf("frame %d, not inspected, allocated %v times a draw, want none", frameIndex, count)
		}
	}
}
