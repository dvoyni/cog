package rendertest_test

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

// copyScene is what the copy tests copy between, beside a drawScene: a
// source buffer of 512 bytes and a destination one of 1024, and two 8×8
// colour textures.
type copyScene struct {
	drawScene
	source, destination render.Buffer
	image, copiedImage  render.Texture
}

// makeCopyScene makes a copyScene through inventory.
func makeCopyScene(inventory render.Inventory) copyScene {
	return copyScene{
		drawScene:   makeDrawScene(inventory),
		source:      inventory.MakeBuffer("source", render.BufferUsageFlagCopySrc|render.BufferUsageFlagStorage, make([]uint32, 128)),
		destination: inventory.MakeBuffer("destination", render.BufferUsageFlagCopySrc|render.BufferUsageFlagStorage, make([]uint32, 256)),
		image:       inventory.MakeTexture[byte](render.TextureDescriptor{Label: "image", Width: 8, Height: 8, Format: render.TextureFormatRGBA8Unorm}, nil),
		copiedImage: inventory.MakeTexture[byte](render.TextureDescriptor{Label: "copied image", Width: 8, Height: 8, Format: render.TextureFormatRGBA8Unorm}, nil),
	}
}

// findListCommands answers the commands of each command list gfx
// submitted, frame after frame.
func findListCommands(h *rendertest.Harness) [][]recording.Command {
	var lists [][]recording.Command
	for _, frame := range h.RecordedFrames() {
		for _, buffer := range readSubmittedBuffers(frame) {
			lists = append(lists, buffer.Commands)
		}
	}
	return lists
}

func TestCopiesAndBufferClearsRecordAtTheirPointAmongPasses(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene copyScene
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeCopyScene(inventory)
		list := frame.BeginCommandList("copies", drawOrder)
		list.CopyBuffer(render.BufferRange{Buffer: scene.source, Offset: 16, Size: 32}, render.BufferRange{Buffer: scene.destination, Offset: 64})
		pass := list.BeginPass(scene.colorPass("scene"))
		pass.End()
		list.CopyTexture(scene.image, render.TextureRegion{X: 2, Y: 2, Width: 4, Height: 4}, scene.copiedImage, render.TextureRegion{X: 1})
		computePass := list.BeginComputePass("cull")
		computePass.End()
		list.CopyBufferToTexture(render.BufferRange{Buffer: scene.source}, scene.image, render.TextureRegion{Width: 4, Height: 2})
		list.CopyTextureToBuffer(scene.image, render.TextureRegion{Y: 4, Width: 2, Height: 2}, render.BufferRange{Buffer: scene.destination, Offset: 256})
		list.ClearBuffer(render.BufferRange{Buffer: scene.destination, Offset: 512})
		list.End()
	})

	lists := h.Frames()[0].CommandLists
	if len(lists) != 2 {
		t.Fatalf("the frame has %d command lists, want render's own and the copies'", len(lists))
	}
	list := lists[1]
	if len(list.Passes) != 1 || list.Passes[0].Position != 1 || list.Passes[0].Label != "scene" {
		t.Errorf("the list's passes are %+v, want the scene's at position 1", list.Passes)
	}
	if want := []render.InspectedComputePass{{Position: 3, Label: "cull"}}; !reflect.DeepEqual(list.ComputePasses, want) {
		t.Errorf("the list's compute passes are %+v, want %+v", list.ComputePasses, want)
	}
	want := []render.InspectedCopy{
		{Position: 0, SourceBuffer: render.BufferRange{Buffer: scene.source, Offset: 16, Size: 32}, DestinationBuffer: render.BufferRange{Buffer: scene.destination, Offset: 64, Size: 32}},
		{Position: 2, SourceTexture: scene.image, SourceRegion: render.TextureRegion{X: 2, Y: 2, Width: 4, Height: 4}, DestinationTexture: scene.copiedImage, DestinationRegion: render.TextureRegion{X: 1, Width: 4, Height: 4}},
		{Position: 4, SourceBuffer: render.BufferRange{Buffer: scene.source}, DestinationTexture: scene.image, DestinationRegion: render.TextureRegion{Width: 4, Height: 2}},
		{Position: 5, SourceTexture: scene.image, SourceRegion: render.TextureRegion{Y: 4, Width: 2, Height: 2}, DestinationBuffer: render.BufferRange{Buffer: scene.destination, Offset: 256}},
		{Position: 6, DestinationBuffer: render.BufferRange{Buffer: scene.destination, Offset: 512, Size: 512}},
	}
	if !reflect.DeepEqual(list.Copies, want) {
		t.Errorf("the list's copies are\n%+v\nwant\n%+v", list.Copies, want)
	}
	for _, command := range findListCommands(h)[1] {
		if arguments, ok := command.Arguments.(recording.CopyBufferToTextureArguments); ok {
			if layout := arguments.Regions[0].BufferLayout; layout != (gfx.ImageDataLayout{BytesPerRow: 256, RowsPerImage: 2}) {
				t.Errorf("the copy into the texture laid its rows out %+v, want 256 bytes apart", layout)
			}
		}
	}
}

func TestACopyOrClearThatDoesNotFitIsRefusedReportedAndRecordsNothing(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindCopyRefused)
	var scene copyScene
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeCopyScene(inventory)
		multisampled := inventory.MakeTexture[byte](render.TextureDescriptor{Label: "multisampled", Width: 8, Height: 8, SampleCount: 4, Format: render.TextureFormatRGBA8Unorm}, nil)
		list := frame.BeginCommandList("copies", drawOrder)
		// A buffer made without the copy source usage.
		list.CopyBuffer(render.BufferRange{Buffer: scene.vertices}, render.BufferRange{Buffer: scene.destination})
		// An offset no multiple of 4.
		list.CopyBuffer(render.BufferRange{Buffer: scene.source, Offset: 2, Size: 8}, render.BufferRange{Buffer: scene.destination})
		// Past the destination's end.
		list.CopyBuffer(render.BufferRange{Buffer: scene.source}, render.BufferRange{Buffer: scene.destination, Offset: 1020})
		// Within one buffer.
		list.CopyBuffer(render.BufferRange{Buffer: scene.source, Size: 64}, render.BufferRange{Buffer: scene.source, Offset: 256})
		// Of different formats.
		list.CopyTexture(scene.image, render.TextureRegion{}, scene.depth, render.TextureRegion{})
		// Into a destination region smaller than the copy.
		list.CopyTexture(scene.image, render.TextureRegion{Width: 4, Height: 4}, scene.copiedImage, render.TextureRegion{X: 6})
		// Past the source level's edge.
		list.CopyTexture(scene.image, render.TextureRegion{X: 6, Width: 4}, scene.copiedImage, render.TextureRegion{})
		// Into a texture of several samples.
		list.CopyBufferToTexture(render.BufferRange{Buffer: scene.source}, multisampled, render.TextureRegion{Width: 1, Height: 1})
		// Rows past the destination buffer's end.
		list.CopyTextureToBuffer(scene.image, render.TextureRegion{Height: 2}, render.BufferRange{Buffer: scene.destination, Offset: 768})
		// A size no multiple of 4.
		list.ClearBuffer(render.BufferRange{Buffer: scene.destination, Size: 6})
		// A buffer render does not hold.
		list.ClearBuffer(render.BufferRange{})
		list.End()
	})

	if count := countReports(h, render.ReportKindCopyRefused); count != 11 {
		t.Errorf("render reported %d refused copies, want eleven", count)
	}
	if copies := h.Frames()[0].CommandLists[1].Copies; len(copies) != 0 {
		t.Errorf("the list recorded copies %+v, want none", copies)
	}
}
