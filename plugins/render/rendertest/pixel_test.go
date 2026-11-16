package rendertest_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

//go:generate go run github.com/dvoyni/cog/plugins/render/cmd/declarebindings testdata/pixels/encoded.wgsl testdata/pixels/defaults.wgsl testdata/pixels/block.wgsl

// pixelBlock is block.wgsl's uniform block as Go lays it out: offset's
// alignment of 8 needs no padding after scale, tint's of 16 needs a word
// after count.
type pixelBlock struct {
	Color  [3]float32
	Scale  float32
	Offset [2]float32
	Count  uint32
	_      uint32
	Tint   [4]float32
}

func TestEachMipLevelOfALoadedTextureAveragesTheFourTexelsAboveItExactly(t *testing.T) {
	h := rendertest.NewHarness(t)
	// Each channel is a multiple of 16, so every level's averages are whole.
	ramp := func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(64 * x), G: uint8(64 * y), B: uint8(16 * ((x + 3*y) % 4)), A: 255}
	}
	rampPNG, rampTexels := encodePNG(t, 4, 4, ramp)
	files := makePixelFiles(t, map[string][]byte{"ramp.png": rampPNG})

	read := drawPixels(t, h, files, func(inventory render.Inventory, list *render.CommandList) []render.Readback {
		texture := inventory.AcquireTexture("ramp.png", render.TextureOptions{ColorSpace: render.ColorSpaceLinear, Mipmaps: true})
		return []render.Readback{
			list.ReadTexture(texture, render.TextureRegion{MipLevel: 1}),
			list.ReadTexture(texture, render.TextureRegion{MipLevel: 2}),
		}
	})

	levels := [][]byte{rampTexels}
	for side := 2; side >= 1; side /= 2 {
		levels = append(levels, averageTexels(levels[len(levels)-1], side))
	}
	for index, side := range []int{2, 1} {
		if got := readTexelRows(read[index], side, side, 4); !bytes.Equal(got, levels[index+1]) {
			t.Errorf("mip level %d holds %v, want %v", index+1, got, levels[index+1])
		}
	}
}

func TestEachBindingLeftUnboundDrawsItsFixedDefault(t *testing.T) {
	h := rendertest.NewHarness(t)

	read := drawPixels(t, h, makePixelFiles(t, nil), func(inventory render.Inventory, list *render.CommandList) []render.Readback {
		material := inventory.MakeMaterial(render.MaterialDescriptor{Label: "defaults", Shader: inventory.AcquireShader("defaults.wgsl")})
		ramp := inventory.MakeTexture(render.TextureDescriptor{Label: "ramp", Width: 2, Height: 1, Format: render.TextureFormatRGBA8Unorm}, []byte{0, 0, 0, 255, 204, 0, 0, 255})
		inventory.SetMaterialTexture(material, "ramp", ramp)
		return []render.Readback{drawCovering(inventory, list, material, 2, 1, render.TextureFormatRGBA8Unorm)}
	})

	// White and depth 1.0 read whole; the zeros leave blue a quarter; a
	// linear filter averages the ramp's 0 and 204 at its middle, and
	// clamping to the edge reads its first texel left of it.
	want := []byte{255, 255, 64, 102, 255, 255, 64, 0}
	if got := readTexelRows(read[0], 2, 1, 4); !bytes.Equal(got, want) {
		t.Errorf("the draw with every binding but the ramp unbound wrote %v, want %v", got, want)
	}
}

func TestATextureThatFailsToLoadDrawsMagentaAtOneTexelForAHeaderFailureAndItsOwnSizeForABodyFailure(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindLoadFailed)
	whole, _ := encodePNG(t, 3, 2, func(x, y int) color.NRGBA { return color.NRGBA{R: 10, G: 20, B: 30, A: 255} })
	paths := []string{"missing.png", "truncated.png"}
	// The signature and the header chunk, 33 bytes, decode as a 3×2 picture
	// whose body is missing.
	files := makePixelFiles(t, map[string][]byte{"truncated.png": whole[:33]})

	read := drawPixels(t, h, files, func(inventory render.Inventory, list *render.CommandList) []render.Readback {
		var readbacks []render.Readback
		for _, path := range paths {
			material := inventory.MakeMaterial(render.MaterialDescriptor{Label: path, Shader: inventory.AcquireShader("encoded.wgsl")})
			inventory.SetMaterialTexture(material, "picture", inventory.AcquireTexture(path, render.TextureOptions{}))
			readbacks = append(readbacks, drawCovering(inventory, list, material, 3, 2, render.TextureFormatRGBA8Unorm))
		}
		return readbacks
	})

	magenta := bytes.Repeat([]byte{255, 0, 255, 255}, 6)
	for index, path := range paths {
		if got := readTexelRows(read[index], 3, 2, 4); !bytes.Equal(got, magenta) {
			t.Errorf("the draw of %s wrote %v, want magenta everywhere", path, got)
		}
	}
}

func TestTheLastPassEncodesTheLinearColourItSamplesAsSRGBIntoABGRA8UnormTarget(t *testing.T) {
	h := rendertest.NewHarness(t)
	texels := []color.NRGBA{{R: 0, G: 1, B: 13, A: 255}, {R: 64, G: 100, B: 137, A: 255}, {R: 188, G: 200, B: 230, A: 255}, {R: 250, G: 254, B: 255, A: 255}}
	picture := func(x, y int) color.NRGBA { return texels[x] }
	picturePNG, _ := encodePNG(t, 4, 1, picture)
	files := makePixelFiles(t, map[string][]byte{"picture.png": picturePNG})

	read := drawPixels(t, h, files, func(inventory render.Inventory, list *render.CommandList) []render.Readback {
		material := inventory.MakeMaterial(render.MaterialDescriptor{Label: "encoded", Shader: inventory.AcquireShader("encoded.wgsl")})
		inventory.SetMaterialTexture(material, "picture", inventory.AcquireTexture("picture.png", render.TextureOptions{ColorSpace: render.ColorSpaceSRGB}))
		return []render.Readback{drawCovering(inventory, list, material, 4, 1, render.TextureFormatBGRA8Unorm)}
	})

	// The texture decodes sRGB as it is sampled, and the shader encodes it
	// again, so each texel's bytes come back as the file holds them, in
	// BGRA order.
	var want []byte
	for _, texel := range texels {
		want = append(want, texel.B, texel.G, texel.R, texel.A)
	}
	if got := readTexelRows(read[0], 4, 1, 4); !bytes.Equal(got, want) {
		t.Errorf("the last pass wrote %v, want the file's sRGB bytes %v", got, want)
	}
}

func TestAGoStructMatchingItsWGSLBlockReachesTheShaderMemberByMember(t *testing.T) {
	h := rendertest.NewHarness(t)

	read := drawPixels(t, h, makePixelFiles(t, nil), func(inventory render.Inventory, list *render.CommandList) []render.Readback {
		material := inventory.MakeMaterial(render.MaterialDescriptor{Label: "block", Shader: inventory.AcquireShader("block.wgsl")})
		inventory.SetMaterialUniform(material, "block", pixelBlock{
			Color:  [3]float32{10.0 / 255, 20.0 / 255, 30.0 / 255},
			Scale:  40.0 / 255,
			Offset: [2]float32{50.0 / 255, 60.0 / 255},
			Count:  70,
			Tint:   [4]float32{80.0 / 255, 90.0 / 255, 100.0 / 255, 110.0 / 255},
		})
		return []render.Readback{drawCovering(inventory, list, material, 3, 1, render.TextureFormatRGBA8Unorm)}
	})

	want := []byte{10, 20, 30, 40, 50, 60, 70, 255, 80, 90, 100, 110}
	if got := readTexelRows(read[0], 3, 1, 4); !bytes.Equal(got, want) {
		t.Errorf("the block's members drew %v, want %v", got, want)
	}
}

func TestAReadbackAnswersTheBytesOfItsBufferRangeAndItsTextureRegion(t *testing.T) {
	h := rendertest.NewHarness(t)
	texels := make([]byte, 3*2*4)
	for index := range texels {
		texels[index] = byte(10 + index)
	}

	read := drawPixels(t, h, makePixelFiles(t, nil), func(inventory render.Inventory, list *render.CommandList) []render.Readback {
		numbers := inventory.MakeBuffer("numbers", render.BufferUsageFlagCopySrc, []uint32{1, 2, 3, 4, 5, 6, 7, 8})
		picture := inventory.MakeTexture(render.TextureDescriptor{Label: "picture", Width: 3, Height: 2, Format: render.TextureFormatRGBA8Unorm}, texels)
		return []render.Readback{
			list.ReadBuffer(render.BufferRange{Buffer: numbers, Offset: 8, Size: 12}),
			list.ReadTexture(picture, render.TextureRegion{X: 1, Width: 2, Height: 2}),
		}
	})

	if want := []byte{3, 0, 0, 0, 4, 0, 0, 0, 5, 0, 0, 0}; !bytes.Equal(read[0], want) {
		t.Errorf("the buffer's readback answered %v, want %v", read[0], want)
	}
	want := slices.Concat(texels[4:12], texels[16:24])
	if got := readTexelRows(read[1], 2, 2, 4); !bytes.Equal(got, want) {
		t.Errorf("the texture's readback answered %v, want %v", got, want)
	}
}

func TestADeclarationThatDiffersFromTheRealReflectionFailsNamingTheShaderAndWhatDiffersFirst(t *testing.T) {
	declaration, err := os.ReadFile("testdata/pixels/block.wgsl.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, declared, stale, want string
	}{
		{"of a block's size", `"size": 48`, `"size": 64`, "binding \"block\" at group 1, binding 0, is declared\n"},
		{"of a binding's name", `"name": "block"`, `"name": "settings"`, "binding \"block\" at group 1, binding 0, is declared\n"},
		{"of a binding's group", `"group": 1`, `"group": 0`, `binding "block" at group 0, binding 0, is declared and not reflected`},
		{"of an entry point's stage", `"stage": "fragment"`, `"stage": "compute"`, "the entry points are declared"},
		{"of a vertex entry point's inputs", `"vertex_main": []`, `"vertex_main": [{"location": 0, "type": "f32"}]`, `the inputs of vertex entry point "vertex_main" are declared`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if strings.Count(string(declaration), test.declared) != 1 {
				t.Fatalf("block.wgsl.json holds %q other than once", test.declared)
			}
			stale := strings.Replace(string(declaration), test.declared, test.stale, 1)
			fake := &fakeTest{TB: t}
			h := rendertest.NewHarness(fake)
			h.UseSoftwareAdapter()
			files := makePixelFiles(t, map[string][]byte{"block.wgsl.json": []byte(stale)})
			h.Spawn(nil, &loaderPlugin{files: files, duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
				if frameIndex == 0 {
					inventory.AcquireShader("block.wgsl")
				}
			}})

			h.Step()

			failures := fake.takeFailures()
			if len(failures) != 1 || !strings.Contains(failures[0], "block.wgsl.json is stale for block.wgsl:") || !strings.Contains(failures[0], test.want) {
				t.Errorf("the harness failed the test with %q, want one failure naming block.wgsl and saying %q", failures, test.want)
			}
		})
	}
}

// makePixelFiles answers the pixel tests' shaders and their declarations,
// read from testdata/pixels, with extra beside them, an extra file of the
// same name in their place.
func makePixelFiles(t *testing.T, extra map[string][]byte) fstest.MapFS {
	t.Helper()
	shaders := os.DirFS("testdata/pixels")
	files := fstest.MapFS{}
	paths, err := fs.Glob(shaders, "*.wgsl*")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		data, err := fs.ReadFile(shaders, path)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = &fstest.MapFile{Data: data}
	}
	for path, data := range extra {
		files[path] = &fstest.MapFile{Data: data}
	}
	return files
}

// drawPixels runs h on the software adapter, with files mounted, until every
// readback record answers in the first frame's recording, given a command
// list of its own, has been delivered, and answers the bytes of each in the
// order record answered them. A readback not delivered ready within ten
// beats fails the test.
func drawPixels(t *testing.T, h *rendertest.Harness, files fs.FS, record func(inventory render.Inventory, list *render.CommandList) []render.Readback) [][]byte {
	t.Helper()
	h.UseSoftwareAdapter()
	var mutex sync.Mutex
	delivered := map[render.Readback][]byte{}
	plugin := newReadbackPlugin()
	plugin.onRead = func(event render.ReadbackEvent, frame render.Frame) {
		data, err := frame.ReadbackData(event)
		if err != nil {
			t.Errorf("reading readback %v: %v", event.Readback, err)
			return
		}
		mutex.Lock()
		defer mutex.Unlock()
		delivered[event.Readback] = slices.Clone(data)
	}
	var readbacks []render.Readback
	h.Spawn(nil, &loaderPlugin{files: files, duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			list := frame.BeginCommandList("pixels", drawOrder)
			readbacks = record(inventory, &list)
			list.End()
		}
	}}, plugin)
	for range 10 {
		h.Step()
		if len(plugin.readReadbacks()) >= len(readbacks) {
			break
		}
	}
	mutex.Lock()
	defer mutex.Unlock()
	read := make([][]byte, len(readbacks))
	for index, readback := range readbacks {
		data, ok := delivered[readback]
		if !ok {
			t.Fatalf("readback %d, %v, was not delivered ready within ten beats", index, readback)
		}
		read[index] = data
	}
	return read
}

// drawCovering draws material with one triangle covering a texture of width
// by height texels in format that list makes, cleared to transparent black,
// in a pass of its own, and answers the readback of the texture.
func drawCovering(inventory render.Inventory, list *render.CommandList, material render.Material, width, height uint32, format render.TextureFormat) render.Readback {
	target := inventory.MakeTexture[byte](render.TextureDescriptor{Label: "target", Width: width, Height: height, Format: format}, nil)
	pass := list.BeginPass(render.PassDescriptor{Label: "covering", ColorTargets: []render.ColorTarget{{Texture: target, Clear: true}}})
	pass.SetMaterial(material)
	pass.DrawVertices(render.PrimitiveTopologyTriangleList, 3, render.DrawArgs{})
	pass.End()
	return list.ReadTexture(target, render.TextureRegion{})
}

// encodePNG answers a PNG of width by height texels, each texel's colour,
// and its texels in RGBA order, row by row, as render writes them.
func encodePNG(t *testing.T, width, height int, texel func(x, y int) color.NRGBA) ([]byte, []byte) {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			picture.SetNRGBA(x, y, texel(x, y))
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes(), picture.Pix
}

// averageTexels answers the RGBA texels of the level of side by side texels
// below level, each channel the average of the four texels it halves.
func averageTexels(level []byte, side int) []byte {
	averaged := make([]byte, side*side*4)
	for y := range side {
		for x := range side {
			for channel := range 4 {
				sum := 0
				for _, corner := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
					sum += int(level[((2*y+corner[1])*2*side+2*x+corner[0])*4+channel])
				}
				averaged[(y*side+x)*4+channel] = byte(sum / 4)
			}
		}
	}
	return averaged
}

// readTexelRows answers the texels of a readback of width by height texels
// of texelSize bytes, its rows each starting a multiple of 256 bytes after
// the one before, without the padding between them.
func readTexelRows(data []byte, width, height, texelSize int) []byte {
	rowSize := width * texelSize
	bytesPerRow := (rowSize + 255) / 256 * 256
	var texels []byte
	for row := range height {
		start := row * bytesPerRow
		if start+rowSize > len(data) {
			return append(texels, data[min(start, len(data)):]...)
		}
		texels = append(texels, data[start:start+rowSize]...)
	}
	return texels
}
