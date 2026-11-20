package canvas

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/plugins/render"
)

func TestAnAtlasPacksEachImageOnTheAtlasGridItsMipLevelsSetItsCellReachingTheGutterPastItsRounding(t *testing.T) {
	// At 3 levels the atlas grid and the gutter are 4: the white texel's cell is
	// 12, the green of 6 rounds up to 8 for a cell of 16, cut as a shelf of
	// its own below, the row of 4 by 1 rounds up to 4 by 4, beside the white
	// texel on its shelf, and an image of 16 takes a cell of 24. Each image
	// lies a gutter in from its cell, on the atlas grid. At one level the gutter
	// is 2 on no atlas grid, so the cells are the images and their gutter.
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
			// A sprite atlas of one page of 64 texels, holding no texture.
			spriteAtlas := &atlas{width: 64, height: 64, mipLevelCount: test.mipLevelCount, pages: []shelfAllocator{makeShelfAllocator(64, 64)}}
			var cells [][2]uint32
			var got []render.TextureRegion
			for _, size := range [][2]uint32{{1, 1}, {6, 6}, {4, 1}, {16, 16}} {
				cellWidth, cellHeight := spriteAtlas.findCellSize(size[0], size[1])
				cells = append(cells, [2]uint32{cellWidth, cellHeight})
				region, fits := spriteAtlas.pack(size[0], size[1])
				if !fits {
					t.Fatalf("an image of %v did not fit a page of 64", size)
				}
				got = append(got, region)
			}
			if !slices.Equal(cells, test.cells) {
				t.Errorf("the cells are %v, want %v", cells, test.cells)
			}
			if !slices.Equal(got, test.want) {
				t.Errorf("the images were packed at %+v, want %+v", got, test.want)
			}
		})
	}
}
