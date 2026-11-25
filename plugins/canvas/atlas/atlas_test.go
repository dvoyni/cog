package atlas_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas/atlas"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

// atlasListLabel labels the command list atlasUser records into.
const atlasListLabel = "atlas user"

// atlasUser is the test's plugin, listed after render, that makes atlases
// and calls them as a user's plugin would: in each frame, its task runs
// frame with render's Inventory and a command list of its own, labelled
// atlasListLabel, which it ends after.
type atlasUser struct {
	frame func(frameIndex int, inventory render.Inventory, list *render.CommandList)
	// frameCount is how many frames the handler of render.FrameEvent has
	// scheduled the task for.
	frameCount int
}

func (*atlasUser) Name() kernel.PluginName { return "atlas user" }

func (*atlasUser) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

func (u *atlasUser) Init(configurator *kernel.Configurator, _ any) error {
	record := configurator.RegisterTask2(func(k *kernel.Kernel, frameIndex int, inventory render.Inventory, frame render.Frame) kernel.NoResult {
		list := frame.BeginCommandList(atlasListLabel, 1)
		u.frame(frameIndex, inventory, &list)
		list.End()
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		out := ex.Join(in, ex.Schedule(record, u.frameCount, event.Ready).Handle())
		u.frameCount++
		return event, out
	})
	return nil
}

func (*atlasUser) Start(*kernel.Kernel) error { return nil }

func (*atlasUser) Stop(*kernel.Kernel) error { return nil }

func (*atlasUser) Destroy() error { return nil }

// runAtlasFrames spawns render with an atlasUser running frame, opens a
// window, and steps frameCount frames, answering h for reading them.
func runAtlasFrames(t *testing.T, frameCount int, frame func(frameIndex int, inventory render.Inventory, list *render.CommandList)) *rendertest.Harness {
	t.Helper()
	h := rendertest.NewHarness(t)
	h.Spawn(nil, &atlasUser{frame: frame})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	for range frameCount {
		h.Step()
	}
	return h
}

// findAtlasList answers the list atlasUser recorded in frame.
func findAtlasList(t *testing.T, frame render.Inspection) render.InspectedCommandList {
	t.Helper()
	for _, list := range frame.CommandLists {
		if list.Label == atlasListLabel {
			return list
		}
	}
	t.Fatalf("the frame recorded no list labelled %q: %+v", atlasListLabel, frame.CommandLists)
	return render.InspectedCommandList{}
}

// mipPass is a pass render recorded generating mips: the mip level and the
// page it drew.
type mipPass struct {
	mipLevel, layer uint32
}

// findMipPasses answers each pass of list that generated mips, in the order
// recorded.
func findMipPasses(list render.InspectedCommandList) []mipPass {
	var passes []mipPass
	for _, pass := range list.Passes {
		if strings.HasPrefix(pass.Label, "render: mip level ") {
			passes = append(passes, mipPass{mipLevel: pass.ColorTargets[0].MipLevel, layer: pass.ColorTargets[0].Layer})
		}
	}
	return passes
}

func TestAnAtlasPacksEachImageOnTheAtlasGridItsMipLevelsSetItsCellReachingTheGutterPastItsRounding(t *testing.T) {
	// At 3 levels the atlas grid is 4, and a gutter of 2 is raised to it:
	// a texel's cell is 12, the image of 6 rounds up to 8 for a cell of 16,
	// cut as a shelf of its own below, the row of 4 by 1 rounds up to 4 by
	// 4, beside the texel on its shelf, and an image of 16 takes a cell of
	// 24. Each image lies a gutter in from its cell, on the atlas grid. At
	// one level the gutter is 2 on no atlas grid, so the cells are the
	// images and their gutter.
	sizes := [][2]uint32{{1, 1}, {6, 6}, {4, 1}, {16, 16}}
	for _, test := range []struct {
		name          string
		mipLevelCount uint32
		cells         [][2]uint32
		want          []render.TextureRegion
	}{
		{"of 3 levels", 3, [][2]uint32{{12, 12}, {16, 16}, {12, 12}, {24, 24}}, []render.TextureRegion{
			{X: 4, Y: 4, Width: 1, Height: 1},
			{X: 4, Y: 16, Width: 6, Height: 6},
			{X: 16, Y: 4, Width: 4, Height: 1},
			{X: 4, Y: 32, Width: 16, Height: 16},
		}},
		{"of one level", 1, [][2]uint32{{5, 5}, {10, 10}, {8, 5}, {20, 20}}, []render.TextureRegion{
			{X: 2, Y: 2, Width: 1, Height: 1},
			{X: 2, Y: 7, Width: 6, Height: 6},
			{X: 7, Y: 2, Width: 4, Height: 1},
			{X: 2, Y: 17, Width: 16, Height: 16},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var cells [][2]uint32
			var got []render.TextureRegion
			var errs []error
			runAtlasFrames(t, 1, func(_ int, inventory render.Inventory, list *render.CommandList) {
				testedAtlas := atlas.New(inventory, atlas.Descriptor{Label: "grid", Format: render.TextureFormatRGBA8Unorm, PageSize: 64, MipLevelCount: test.mipLevelCount, Gutter: 2})
				for _, size := range sizes {
					region, err := testedAtlas.Pack(inventory, size[0], size[1])
					errs = append(errs, err)
					cell := testedAtlas.Cell(region)
					cells = append(cells, [2]uint32{cell.Width, cell.Height})
					got = append(got, region)
				}
				testedAtlas.Release(inventory)
			})

			if !slices.Equal(errs, make([]error, len(sizes))) {
				t.Fatalf("packing the images answered %v, want each testedAtlas on the page of 64", errs)
			}
			if !slices.Equal(cells, test.cells) {
				t.Errorf("the cells are %v, want %v", cells, test.cells)
			}
			if !slices.Equal(got, test.want) {
				t.Errorf("the images were testedAtlas at %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestPackAnswersErrTooLargeForACellNoPageHoldsAndErrFullAtItsCeilingUntilARegionIsFreed(t *testing.T) {
	// A page of 8 texels of one level, its gutter 1, holds one cell of 8:
	// an image of 7 has a cell of 9, and one 9 wide is wider than a page,
	// each too large. Two images of 6 fill the two pages of the ceiling, so
	// a third finds it full, until the first is freed and the third takes
	// its cell.
	type packAnswer struct {
		region render.TextureRegion
		err    error
	}
	var got []packAnswer
	var pageCount uint32
	runAtlasFrames(t, 1, func(_ int, inventory render.Inventory, list *render.CommandList) {
		testedAtlas := atlas.New(inventory, atlas.Descriptor{Label: "small", Format: render.TextureFormatR8Unorm, PageSize: 8, Gutter: 1, LayerCeiling: 2})
		pack := func(width, height uint32) render.TextureRegion {
			region, err := testedAtlas.Pack(inventory, width, height)
			got = append(got, packAnswer{region, err})
			return region
		}
		pack(7, 7)
		pack(9, 1)
		first := pack(6, 6)
		pack(6, 6)
		pack(6, 6)
		pageCount = testedAtlas.PageCount()
		testedAtlas.Free(first)
		pack(6, 6)
		testedAtlas.RecordCopies(list, inventory)
	})

	want := []packAnswer{
		{err: atlas.ErrTooLarge},
		{err: atlas.ErrTooLarge},
		{region: render.TextureRegion{X: 1, Y: 1, Width: 6, Height: 6}},
		{region: render.TextureRegion{Layer: 1, X: 1, Y: 1, Width: 6, Height: 6}},
		{err: atlas.ErrFull},
		{region: render.TextureRegion{X: 1, Y: 1, Width: 6, Height: 6}},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Pack answered %+v, want %+v", got, want)
	}
	if pageCount != 2 {
		t.Errorf("the full atlas has %d pages, want its ceiling of 2", pageCount)
	}
}

func TestTwoGrowthsBeforeARecordCopyEveryMipLevelOfTheFirstPagesOnceIntoTheLastTexture(t *testing.T) {
	// A page of 8 texels of 2 levels, on an atlas grid and gutter of 2,
	// holds one cell of 6, so a second image grows the atlas to 2 pages and
	// a third to 4 in one frame: the one page it had is copied at each of
	// its levels, once, into the texture of 4, and the texture between is
	// released uncopied, as is the first once copied.
	var textures []render.Texture
	var held []bool
	var layerCount uint32
	h := runAtlasFrames(t, 3, func(frameIndex int, inventory render.Inventory, list *render.CommandList) {
		switch frameIndex {
		case 0:
			testedAtlas := atlas.New(inventory, atlas.Descriptor{Label: "growing", Format: render.TextureFormatRGBA8Unorm, PageSize: 8, MipLevelCount: 2, LayerCeiling: 4})
			textures = append(textures, testedAtlas.Texture())
			for range 3 {
				if _, err := testedAtlas.Pack(inventory, 2, 2); err != nil {
					t.Errorf("packing an image of 2 answered %v, want it testedAtlas", err)
				}
				textures = append(textures, testedAtlas.Texture())
			}
			testedAtlas.RecordCopies(list, inventory)
		case 2:
			// The textures replaced were freed once the frame releasing them
			// was drawn.
			for _, texture := range []render.Texture{textures[0], textures[2], textures[3]} {
				descriptor, isHeld := inventory.TextureDescriptor(texture)
				held = append(held, isHeld)
				layerCount = descriptor.LayerCount
			}
		}
	})

	if len(textures) != 4 || textures[0] != textures[1] || textures[1] == textures[2] || textures[2] == textures[3] {
		t.Fatalf("the atlas's texture was %v, packing after packing, want the first kept by the first image and replaced by each growth", textures)
	}
	type pageCopy struct {
		source, destination render.Texture
		mipLevel, layer     uint32
	}
	var copies []pageCopy
	for _, copied := range findAtlasList(t, h.Frames()[0]).Copies {
		if copied.SourceRegion != copied.DestinationRegion {
			t.Errorf("the frame copied %+v, want each page's level into the same of the grown texture", copied)
		}
		copies = append(copies, pageCopy{copied.SourceTexture, copied.DestinationTexture, copied.DestinationRegion.MipLevel, copied.DestinationRegion.Layer})
	}
	if want := []pageCopy{{textures[0], textures[3], 0, 0}, {textures[0], textures[3], 1, 0}}; !slices.Equal(copies, want) {
		t.Errorf("the frame copied %+v, want the first page at each of its 2 levels, from the first texture into the last: %+v", copies, want)
	}
	if !slices.Equal(held, []bool{false, false, true}) || layerCount != 4 {
		t.Errorf("render holds the first, the second and the last texture: %v, the last of %d pages, want the last alone, of 4", held, layerCount)
	}
}

func TestFreeDropsAPutStillToRecordAndGivesBackItsCell(t *testing.T) {
	// Two images are put, and the first freed before the copies are
	// recorded: only the second's cell is copied, and a third image testedAtlas
	// after takes the first's cell.
	var cells []render.TextureRegion
	h := runAtlasFrames(t, 1, func(_ int, inventory render.Inventory, list *render.CommandList) {
		testedAtlas := atlas.New(inventory, atlas.Descriptor{Label: "freeing", Format: render.TextureFormatR8Unorm, PageSize: 16, Gutter: 1})
		var regions []render.TextureRegion
		for range 2 {
			region, _ := testedAtlas.Pack(inventory, 2, 2)
			testedAtlas.Put(region, []byte{1, 2, 3, 4}, atlas.WrapNone)
			regions = append(regions, region)
			cells = append(cells, testedAtlas.Cell(region))
		}
		testedAtlas.Free(regions[0])
		third, _ := testedAtlas.Pack(inventory, 2, 2)
		cells = append(cells, testedAtlas.Cell(third))
		testedAtlas.RecordCopies(list, inventory)
	})

	var copied []render.TextureRegion
	for _, bufferCopy := range findAtlasList(t, h.Frames()[0]).Copies {
		copied = append(copied, bufferCopy.DestinationRegion)
	}
	if want := cells[1:2]; !slices.Equal(copied, want) {
		t.Errorf("the frame copied into %+v, want the second image's cell alone, %+v", copied, want)
	}
	if cells[2] != cells[0] {
		t.Errorf("the third image's cell is %+v, want the first's, freed: %+v", cells[2], cells[0])
	}
}

func TestPutFillsTheGutterFromTheOppositeEdgeAlongAnAxisItWrapsTheEdgeContinuedAlongTheOtherOrTransparentWhenCleared(t *testing.T) {
	// An image of 2 by 2, its rows 1 2 and 3 4, in a cell of 6 at one level
	// with a gutter of 2, wrapped along x and continued along y; then, under
	// ClearGutter, its gutter transparent whatever its wrap. A texel of a
	// 3-level atlas, on an atlas grid of 4, fills its whole cell of 12, the
	// rounding to the atlas grid included.
	for _, test := range []struct {
		name       string
		descriptor atlas.Descriptor
		side       uint32
		texels     []byte
		wrap       atlas.Wrap
		want       []byte
	}{
		{"wrapped along x", atlas.Descriptor{PageSize: 16, Gutter: 2}, 2, []byte{1, 2, 3, 4}, atlas.WrapX, []byte{
			1, 2, 1, 2, 1, 2,
			1, 2, 1, 2, 1, 2,
			1, 2, 1, 2, 1, 2,
			3, 4, 3, 4, 3, 4,
			3, 4, 3, 4, 3, 4,
			3, 4, 3, 4, 3, 4,
		}},
		{"wrapped along y", atlas.Descriptor{PageSize: 16, Gutter: 2}, 2, []byte{1, 2, 3, 4}, atlas.WrapY, []byte{
			1, 1, 1, 2, 2, 2,
			3, 3, 3, 4, 4, 4,
			1, 1, 1, 2, 2, 2,
			3, 3, 3, 4, 4, 4,
			1, 1, 1, 2, 2, 2,
			3, 3, 3, 4, 4, 4,
		}},
		{"cleared", atlas.Descriptor{PageSize: 16, Gutter: 2, ClearGutter: true}, 2, []byte{1, 2, 3, 4}, atlas.WrapBoth, []byte{
			0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0,
			0, 0, 1, 2, 0, 0,
			0, 0, 3, 4, 0, 0,
			0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0,
		}},
		{"rounded up to the atlas grid", atlas.Descriptor{PageSize: 16, MipLevelCount: 3}, 1, []byte{9}, atlas.WrapNone, slices.Repeat([]byte{9}, 144)},
	} {
		t.Run(test.name, func(t *testing.T) {
			var region, cell render.TextureRegion
			var got []byte
			runAtlasFrames(t, 1, func(_ int, inventory render.Inventory, list *render.CommandList) {
				descriptor := test.descriptor
				descriptor.Label = "put"
				descriptor.Format = render.TextureFormatR8Unorm
				testedAtlas := atlas.New(inventory, descriptor)
				region, _ = testedAtlas.Pack(inventory, test.side, test.side)
				testedAtlas.Put(region, test.texels, test.wrap)
				cell, got = testedAtlas.FindStagedCell(0)
				testedAtlas.RecordCopies(list, inventory)
			})

			if !slices.Equal(got, test.want) {
				t.Errorf("Put of %v at %+v filled its cell %+v with %v, want %v", test.texels, region, cell, got, test.want)
			}
		})
	}
}

func TestRecordMipsMakesTheMipsOfEachPagePackedIntoOnceAndNothingWhenNothingIsPending(t *testing.T) {
	// A page of 32 texels of 3 levels holds four cells of 12, so five
	// images fill the first page and grow a second: each page's levels 1
	// and 2 are drawn once. A sixth image, in the next frame, lands on the
	// second page, whose mips alone are drawn again. A third frame packs
	// nothing and records nothing, as does a new atlas's first.
	var mippedAtlas *atlas.Atlas
	h := runAtlasFrames(t, 4, func(frameIndex int, inventory render.Inventory, list *render.CommandList) {
		switch frameIndex {
		case 0:
			freshAtlas := atlas.New(inventory, atlas.Descriptor{Label: "freshAtlas", Format: render.TextureFormatRGBA8Unorm, PageSize: 32, MipLevelCount: 3})
			freshAtlas.RecordCopies(list, inventory)
			freshAtlas.RecordMips(list)
			freshAtlas.Release(inventory)
			return
		case 1:
			mippedAtlas = atlas.New(inventory, atlas.Descriptor{Label: "mippedAtlas", Format: render.TextureFormatRGBA8Unorm, PageSize: 32, MipLevelCount: 3, LayerCeiling: 2})
			for range 5 {
				if _, err := mippedAtlas.Pack(inventory, 4, 4); err != nil {
					t.Errorf("packing an image of 4 answered %v, want it testedAtlas", err)
				}
			}
		case 2:
			if region, _ := mippedAtlas.Pack(inventory, 4, 4); region.Layer != 1 {
				t.Errorf("the sixth image was testedAtlas on page %d, want the second", region.Layer)
			}
		}
		mippedAtlas.RecordCopies(list, inventory)
		mippedAtlas.RecordMips(list)
	})

	for index, want := range [][]mipPass{nil, {{1, 0}, {2, 0}, {1, 1}, {2, 1}}, {{1, 1}, {2, 1}}, nil} {
		list := findAtlasList(t, h.Frames()[index])
		if got := findMipPasses(list); !slices.Equal(got, want) {
			t.Errorf("frame %d drew mips %+v, want %+v", index, got, want)
		}
		if want == nil && (len(list.Passes) != 0 || len(list.Copies) != 0) {
			t.Errorf("frame %d recorded %+v, want nothing", index, list)
		}
	}
}
