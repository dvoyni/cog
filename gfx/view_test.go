package gfx

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/m"
)

// The view types are the vocabulary three tools share, so the properties worth
// pinning are the two an agent would be misled by: a tagged union that leaks
// the arm it is not, and a descriptor that carries a megabyte of pixels into a
// reply.

func TestAParameterSerializesToExactlyOneValue(t *testing.T) {
	pixels := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	cases := []struct {
		name      string
		parameter ParameterDescr
		kind      string
		// field is the one key besides name and kind the JSON is allowed to
		// carry.
		field string
	}{
		{"float", FloatParam("alpha", 0.5), "float", "value"},
		{"color", ColorParam("tint", m.Color{R: 1, A: 1}), "color", "value"},
		{"vec4", VecParam("offset", m.Vec4{X: 1, Y: 2}), "vec4", "value"},
		{"mat4", MatParam("mvp", m.NewMat4()), "mat4", "value"},
		{"sampler", SamplerParam("smp", SamplerDesc{}), "sampler", "sampler"},
		{"buffer", BufferParam("items", BufferWithBytes([]byte{1, 2, 3, 4}, false)), "buffer", "buffer"},
		{
			"texture",
			TextureParam("albedo", TextureWithBytes(2, 1, FormatRGBA8, pixels, false, false)),
			"texture", "texture",
		},
		{"none", ParameterDescr{}, "none", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			document := marshalToMap(t, ParameterViewOf(test.parameter))
			if document["kind"] != test.kind {
				t.Fatalf("kind = %v, want %q", document["kind"], test.kind)
			}
			delete(document, "name")
			delete(document, "kind")
			if test.field != "" {
				if _, present := document[test.field]; !present {
					t.Fatalf("the %s parameter carries no %q", test.name, test.field)
				}
				delete(document, test.field)
			}
			// Whatever is left is the dead half of the union: nine wrong
			// values beside the right one is worse than none, because an agent
			// will read them.
			for key := range document {
				t.Errorf("a %s parameter also serialized %q, which belongs to another kind",
					test.name, key)
			}
		})
	}
}

func TestAParameterCarriesItsValueInShaderOrder(t *testing.T) {
	color := ParameterViewOf(ColorParam("tint", m.Color{R: 0.1, G: 0.2, B: 0.3, A: 0.4}))
	if want := []float32{0.1, 0.2, 0.3, 0.4}; !equalFloats(color.Value, want) {
		t.Errorf("color value = %v, want %v", color.Value, want)
	}
	vec := ParameterViewOf(VecParam("offset", m.Vec4{X: 1, Y: 2, Z: 3, W: 4}))
	if want := []float32{1, 2, 3, 4}; !equalFloats(vec.Value, want) {
		t.Errorf("vec4 value = %v, want %v", vec.Value, want)
	}
	if mat := ParameterViewOf(MatParam("mvp", m.NewMat4())); len(mat.Value) != 16 {
		t.Errorf("mat4 value has %d components, want 16", len(mat.Value))
	}
	if single := ParameterViewOf(FloatParam("alpha", 0.5)); !equalFloats(single.Value, []float32{0.5}) {
		t.Errorf("float value = %v, want [0.5]", single.Value)
	}
}

func TestATextureWithInlinePixelsReportsTheirSizeAndNotThem(t *testing.T) {
	// A recognisable run of bytes, so a leak shows up as itself rather than as
	// a length that happens to match.
	pixels := make([]byte, 64)
	for i := range pixels {
		pixels[i] = byte(i + 1)
	}
	parameter := TextureParam("albedo", TextureWithBytes(4, 4, FormatRGBA8, pixels, false, true))

	document, err := json.Marshal(ParameterViewOf(parameter))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	view := ParameterViewOf(parameter)
	if view.Texture == nil {
		t.Fatal("a texture parameter carries no texture view")
	}
	if view.Texture.Bytes != len(pixels) {
		t.Errorf("bytes = %d, want the %d the descriptor carries", view.Texture.Bytes, len(pixels))
	}
	if view.Texture.Width != 4 || view.Texture.Height != 4 {
		t.Errorf("size = %dx%d, want 4x4", view.Texture.Width, view.Texture.Height)
	}
	if !view.Texture.Mipmaps || view.Texture.Format != formatName(FormatRGBA8) {
		t.Errorf("texture = %+v, want the format and mipmap flag it was built with", view.Texture)
	}
	// base64 of the run, and the run itself, both absent: a whole texture in a
	// reply is a debug facility that costs more than the bug.
	text := string(document)
	if strings.Contains(text, "AQIDBAUGBwg") || strings.Contains(text, "pixels") {
		t.Fatalf("the response carries the texture's pixels: %s", text)
	}
	if len(document) > 256 {
		t.Fatalf("a 64-byte texture serialized to %d bytes, so something bulky travelled", len(document))
	}
}

func TestARawParameterReportsItsLengthAndNotItsBytes(t *testing.T) {
	raw := RawParameter("block", struct {
		A float32
		B float32
	}{1, 2})
	view := ParameterViewOf(raw)
	if view.Kind != "raw" {
		t.Fatalf("kind = %q, want raw", view.Kind)
	}
	if size, _ := raw.RawLen(); view.Bytes != size || size == 0 {
		t.Fatalf("bytes = %d, want the %d the parameter carries", view.Bytes, size)
	}
	if view.Value != nil || view.Texture != nil || view.Buffer != nil || view.Sampler != nil {
		t.Fatalf("a raw parameter serialized another kind's value: %+v", view)
	}
}

func TestABufferParameterCarriesTheRangeItBinds(t *testing.T) {
	buffer := BufferWithBytes(make([]byte, 512), false)
	view := ParameterViewOf(BufferRangeParam("items", buffer, 128, 64))
	if view.Buffer == nil {
		t.Fatal("a buffer parameter carries no buffer view")
	}
	if view.Buffer.Offset != 128 || view.Buffer.Range != 64 {
		t.Errorf("range = %d+%d, want 128+64", view.Buffer.Offset, view.Buffer.Range)
	}
	if view.Buffer.Size != 512 || view.Buffer.Bytes != 512 {
		t.Errorf("buffer = %+v, want the 512 bytes it was built from", view.Buffer)
	}
	whole := ParameterViewOf(BufferParam("items", buffer))
	if whole.Buffer.Offset != 0 || whole.Buffer.Range != 0 {
		t.Errorf("a whole-buffer binding reports range %d+%d, want 0+0 - which is how it says whole",
			whole.Buffer.Offset, whole.Buffer.Range)
	}
}

func TestAMaterialViewNamesItsShaderVariantAndState(t *testing.T) {
	material := MaterialWithState(
		ShaderWithResource("shaders/pbr.wgsl", ShaderDefine("SKINNED"), ShaderConst("LIGHTS", "4")),
		StateOpaque3D,
		ColorParam("tint", m.White),
	)
	view := MaterialViewOf(material)
	if view.Shader.Path != "shaders/pbr.wgsl" || view.Shader.Inline {
		t.Errorf("shader = %+v, want the resource path it names", view.Shader)
	}
	// One path under two supplies is two shaders, so the supply is part of the
	// name rather than a detail beside it.
	if !strings.Contains(view.Shader.Supply, "SKINNED") || !strings.Contains(view.Shader.Supply, "LIGHTS=4") {
		t.Errorf("supply = %q, want the defines and consts the descriptor carries", view.Shader.Supply)
	}
	if view.State.DepthCompare != "less" || !view.State.DepthWrite || view.State.Cull != "back" {
		t.Errorf("state = %+v, want the opaque 3D state named rather than numbered", view.State)
	}
	if len(view.Parameters) != 1 || view.Parameters[0].Name != "tint" {
		t.Errorf("parameters = %+v, want the material's own", view.Parameters)
	}
	inline := MaterialViewOf(Material(ShaderWithText("// wgsl")))
	if !inline.Shader.Inline || inline.Shader.Path != "" {
		t.Errorf("inline shader = %+v, want no path and the inline flag", inline.Shader)
	}
}

func TestASnapshotViewCarriesAllThreeCoordinateSizes(t *testing.T) {
	view := SnapshotViewOf(app.Viewport{
		Width: 800, Height: 600,
		WindowWidth: 400, WindowHeight: 300,
		FramebufferWidth: 1600, FramebufferHeight: 1200,
	})
	switch {
	case view.PixelWidth != 1600 || view.PixelHeight != 1200:
		t.Errorf("pixels = %dx%d, want the framebuffer's 1600x1200", view.PixelWidth, view.PixelHeight)
	case view.WindowWidth != 400 || view.WindowHeight != 300:
		t.Errorf("window = %vx%v, want 400x300", view.WindowWidth, view.WindowHeight)
	case view.ViewportWidth != 800 || view.ViewportHeight != 600:
		// The one nothing else reports: a capture omits it on purpose, and a
		// snapshot's own coordinates are in it.
		t.Errorf("viewport = %vx%v, want the logical 800x600", view.ViewportWidth, view.ViewportHeight)
	}
}

func marshalToMap(t *testing.T, value any) map[string]any {
	t.Helper()
	document, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return decoded
}

func equalFloats(got, want []float32) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
