package canvas

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

func TestAFramesRectangleIsItsSheetsCellOfItsSourceRectOrNoneWhenOutOfBounds(t *testing.T) {
	// Each case is of a file of 16 by 8 texels.
	for _, test := range []struct {
		name       string
		options    ImageOptions
		frame      uint32
		want       m.Recti
		withinFile bool
	}{
		{"the whole file", ImageOptions{}, 0, m.Recti{Width: 16, Height: 8}, true},
		{"the last frame of a sheet of 2 by 2", ImageOptions{Sheet: Grid{Columns: 2, Rows: 2}}, 3, m.Recti{X: 8, Y: 4, Width: 8, Height: 4}, true},
		{"a frame of a strip of 4 columns and no rows", ImageOptions{Sheet: Grid{Columns: 4}}, 2, m.Recti{X: 8, Width: 4, Height: 8}, true},
		{"a frame of a sheet of 3 columns leaving a texel over", ImageOptions{Sheet: Grid{Columns: 3}}, 2, m.Recti{X: 10, Width: 5, Height: 8}, true},
		{"a source rectangle", ImageOptions{SourceRect: m.Recti{X: 4, Y: 2, Width: 8, Height: 4}}, 0, m.Recti{X: 4, Y: 2, Width: 8, Height: 4}, true},
		{"a frame of a sheet cutting a source rectangle", ImageOptions{SourceRect: m.Recti{X: 4, Y: 2, Width: 8, Height: 4}, Sheet: Grid{Columns: 2, Rows: 1}}, 1, m.Recti{X: 8, Y: 2, Width: 4, Height: 4}, true},
		{"a frame past the sheet's last", ImageOptions{Sheet: Grid{Columns: 2, Rows: 2}}, 4, m.Recti{}, false},
		{"the frame of an image of one past it", ImageOptions{}, 1, m.Recti{}, false},
		{"a source rectangle reaching past the file", ImageOptions{SourceRect: m.Recti{X: 12, Width: 8, Height: 8}}, 0, m.Recti{}, false},
		{"a source rectangle left of the file", ImageOptions{SourceRect: m.Recti{X: -1, Width: 4, Height: 4}}, 0, m.Recti{}, false},
		{"a frame of no texels", ImageOptions{Sheet: Grid{Columns: 32}}, 0, m.Recti{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, withinFile := test.options.findFrameRect(16, 8, test.frame)
			if got != test.want || withinFile != test.withinFile {
				t.Errorf("frame %d is %+v, within the file %v, want %+v, %v", test.frame, got, withinFile, test.want, test.withinFile)
			}
		})
	}
}
