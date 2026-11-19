package canvas_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/platform"
)

// fileMounter is a test plugin binding a read mount of files held in memory,
// so a test's images load through platform's FS.
type fileMounter struct {
	testPlugin
	files fstest.MapFS
}

func (*fileMounter) Name() kernel.PluginName { return "file mounter" }

func (f *fileMounter) Init(configurator *kernel.Configurator, _ any) error {
	configurator.BindSocket(platform.ReadMountSocket{FS: f.files})
	return nil
}

// quadrantsPath is the path makeQuadrantsFiles puts the quadrants at.
const quadrantsPath = "quadrants.png"

// makeQuadrantsFiles answers the files of quadrantsPath, a PNG of 4×4 texels
// whose quadrants are opaque red at the top left, green of alpha 128 at the
// top right, opaque blue at the bottom left and opaque white at the bottom
// right, so where a sprite draws each shows how it is placed and turned.
func makeQuadrantsFiles(t testing.TB) fstest.MapFS {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			quadrant := [2][2]color.NRGBA{
				{{R: 255, A: 255}, {G: 255, A: 128}},
				{{B: 255, A: 255}, {R: 255, G: 255, B: 255, A: 255}},
			}[y/2][x/2]
			picture.SetNRGBA(x, y, quadrant)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatalf("encoding the quadrants: %v", err)
	}
	return fstest.MapFS{quadrantsPath: {Data: encoded.Bytes()}}
}
