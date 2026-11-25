package canvas_test

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/libs/meshfile"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

func TestAfterADeviceLossTheNextFrameDrawsItsSpritesTextAndMeshesAgain(t *testing.T) {
	// Two sprites, one of an image past OwnTextureSize, a label and a mesh,
	// drawn before a loss and again after render's recovery, whose textures
	// and buffers render made again empty: the frame after packs the
	// sprites' images, the large one into a new texture of its own, the old
	// one released, copies the label's glyphs where they were, copies the
	// white texel, put again as canvas started afresh, where it was, and
	// loads the mesh's file again, then draws the four as before; the frame
	// after that loads nothing.
	const meshPath = "meshes/tree.mesh"
	files := makeAtlasFiles(t)
	square := makeSquareVertices(10)
	tree := canvas.Triangles[canvas.Vertex]{Vertices: square, Indices: squareIndices, Topology: meshfile.TopologyTriangleList, Bounds: meshfile.CalculateBounds(square, canvas.Vertex.Position3)}
	files[meshPath] = &fstest.MapFile{Data: encodeMeshFile(t, meshPath, tree)}
	red := canvas.ImageFromFile(redPath)
	blue := canvas.ImageFromFile(bluePath)
	record := func(list canvas.DrawList) {
		list.DrawSprite(canvas.Sprite{Image: red, Transform: m.Translation2x3(-60, 0)})
		list.DrawSprite(canvas.Sprite{Image: blue, Transform: m.Translation2x3(-100, 0), Order: 1})
		list.DrawText(canvas.Text{String: "ab", Order: 2})
		list.DrawMesh(canvas.Mesh{File: canvas.MeshFileFromFile(meshPath), Transform: m.Translation2x3(60, 0), Order: 3})
	}
	h := canvastest.NewHarness(t)
	describer := &textureDescriber{}
	h.Spawn(nil, &canvas.Config{OwnTextureSize: 16}, &fileMounter{files: files}, describer)
	h.OpenWindow(platform.Size{Width: 320, Height: 240})
	h.Record(record)
	h.Step()
	labels := findTextureLabels(h.Frames()[0].Render)
	packedBefore := findPackings(t, h.Frames()[0].Render, labels)
	if len(packedBefore) != 2 {
		t.Fatalf("the frame before the loss packed %+v, want both sprites' images", packedBefore)
	}
	describer.describe(packedBefore[1].atlas)

	h.LoseDevice()
	h.Record(record)
	h.Step()
	h.Record(record)
	h.Step()

	frames := h.Frames()
	if len(frames) != 3 {
		t.Fatalf("the harness inspected %d frames, want the one before the loss and the two after", len(frames))
	}
	before, after, steady := frames[0], frames[1], frames[2]
	labels = findTextureLabels(before.Render, after.Render, steady.Render)
	for name, frame := range map[string]canvas.Inspection{"before the loss": before, "after the recovery": after} {
		if got := findPackings(t, frame.Render, labels); len(got) != 2 || got[0].source != redPath || labels[got[0].atlas] != spriteAtlasLabel || got[1].source != bluePath || labels[got[1].atlas] != bluePath {
			t.Errorf("the frame %s packed %+v, want the red image packed into the sprite atlas and the blue into a texture of its own", name, got)
		}
		if got := findMadeBufferLabels(frame.Render); !slices.Equal(got, []string{meshPath, meshPath}) {
			t.Errorf("the frame %s made buffers %v, want the mesh file's vertices and indices", name, got)
		}
	}
	// The sRGB atlas emptied, the white texel is copied where it was.
	if got, want := findCopyRegions(after.Render, labels, spriteAtlasLabel), findCopyRegions(before.Render, labels, spriteAtlasLabel); len(want) != 1 || !slices.Equal(got, want) {
		t.Errorf("the frame after the recovery copied into %+v of the sprite atlas, want the white texel's cell where the frame before the loss copied it, %+v", got, want)
	}
	// The glyph atlas emptied, the label's glyphs are packed where they were.
	if got, want := findCopyRegions(after.Render, labels, glyphAtlasLabel), findCopyRegions(before.Render, labels, glyphAtlasLabel); len(want) != 2 || !slices.Equal(got, want) {
		t.Errorf("the frame after the recovery copied glyphs into %+v of the glyph atlas, want the label's 2 copied where the frame before the loss copied them, %+v", got, want)
	}
	if packedAfter := findPackings(t, after.Render, labels); len(packedAfter) == 2 && packedAfter[1].atlas == packedBefore[1].atlas {
		t.Errorf("the frame after the recovery packed the blue image into its texture of before the loss, want a new one")
	}
	if described := describer.describedTextures(); len(described) != 1 || described[0] != (render.TextureDescriptor{}) {
		t.Errorf("the blue image's texture of before the loss is described as %+v after the recovery, want it released", described)
	}
	if got := findBatchKinds(after); !slices.Equal(got, findBatchKinds(before)) || len(got) != 4 {
		t.Errorf("the frame after the recovery drew batches of %v, want each sprite's, the label's and the mesh's, as before the loss: %v", got, findBatchKinds(before))
	}
	if packings, copies, buffers := findPackings(t, steady.Render, labels), findCopyRegions(steady.Render, labels, glyphAtlasLabel), findMadeBufferLabels(steady.Render); len(packings) != 0 || len(copies) != 0 || len(buffers) != 0 {
		t.Errorf("the frame after that packed %+v, copied glyphs into %+v and made buffers %v, want nothing loaded again", packings, copies, buffers)
	}
}

// findCopyRegions answers the region of each copy frame's canvas lists made
// into the atlas labelled atlasLabel, its texture labelled as labels has it.
func findCopyRegions(frame render.Inspection, labels map[render.Texture]string, atlasLabel string) []render.TextureRegion {
	var regions []render.TextureRegion
	for _, list := range findCanvasLists(frame) {
		for _, inspectedCopy := range list.Copies {
			if labels[inspectedCopy.DestinationTexture] == atlasLabel {
				regions = append(regions, inspectedCopy.DestinationRegion)
			}
		}
	}
	return regions
}

// findMadeBufferLabels answers the label of each buffer frame made.
func findMadeBufferLabels(frame render.Inspection) []string {
	var made []string
	for _, asset := range frame.MadeAssets {
		if asset.Buffer != (render.Buffer{}) {
			made = append(made, asset.Label)
		}
	}
	return made
}

// findBatchKinds answers the kind of the first entry of each batch of
// frame's first camera.
func findBatchKinds(frame canvas.Inspection) []canvas.EntryKind {
	var kinds []canvas.EntryKind
	for _, inspectedBatch := range frame.Cameras[0].Batches {
		kinds = append(kinds, inspectedBatch.Entries[0].Kind)
	}
	return kinds
}
