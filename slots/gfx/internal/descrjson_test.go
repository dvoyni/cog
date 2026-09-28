package internal

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dvoyni/cog/libs/assets"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/libs/m"
)

// The descriptors marshal themselves for the snapshots three tools share, so
// the properties worth pinning are the two an agent would be misled by: a
// tagged union that leaks the arm it is not, and a descriptor that carries a
// megabyte of pixels into a reply.

// parameterDocument is a marshalled parameter read back: what an agent sees.
type parameterDocument struct {
	Name    string    `json:"name"`
	Kind    string    `json:"kind"`
	Value   []float32 `json:"value"`
	Texture *struct {
		Source  string `json:"source"`
		Width   int    `json:"width"`
		Height  int    `json:"height"`
		Format  string `json:"format"`
		Mipmaps bool   `json:"mipmaps"`
		Bytes   int    `json:"bytes"`
	} `json:"texture"`
	Sampler *struct{} `json:"sampler"`
	Buffer  *struct {
		Size  int `json:"size"`
		Bytes int `json:"bytes"`
	} `json:"buffer"`
	BufferOffset int `json:"bufferOffset"`
	BufferSize   int `json:"bufferSize"`
	Bytes        int `json:"bytes"`
}

func documentOf(t *testing.T, parameter types.ShaderParameterDescr) parameterDocument {
	t.Helper()
	encoded, err := json.Marshal(parameter)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var document parameterDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return document
}

func TestAParameterSerializesToExactlyOneValue(t *testing.T) {
	pixels := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	cases := []struct {
		name      string
		parameter types.ShaderParameterDescr
		kind      string
		// fields are the keys besides name and kind the JSON is allowed to
		// carry.
		fields []string
	}{
		{"float", types.ShaderParameterFloat("alpha", 0.5), "float", []string{"value"}},
		{"color", types.ShaderParameterColor("tint", m.Color{R: 1, A: 1}), "color", []string{"value"}},
		{"vec4", types.ShaderParameterVec4("offset", m.Vec4{X: 1, Y: 2}), "vec4", []string{"value"}},
		{"mat4", types.ShaderParameterMat4("mvp", m.NewMat4()), "mat4", []string{"value"}},
		{"sampler", types.ShaderParameterSampler("smp", types.SamplerDesc{}), "sampler", []string{"sampler"}},
		{
			"buffer",
			types.ShaderParameterBufferRange("items", types.BufferDescrWithBlob(assets.NewBlob([]byte{1, 2, 3, 4}), false), 0, 4),
			"buffer", []string{"buffer", "bufferSize"},
		},
		{
			"texture",
			types.ShaderParameterTexture("albedo", types.TextureWithBytes(2, 1, types.FormatRGBA8, pixels, false, false)),
			"texture", []string{"texture"},
		},
		{"none", types.ShaderParameterDescr{}, "none", nil},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			document := marshalToMap(t, test.parameter)
			if document["kind"] != test.kind {
				t.Fatalf("kind = %v, want %q", document["kind"], test.kind)
			}
			delete(document, "name")
			delete(document, "kind")
			for _, field := range test.fields {
				if _, present := document[field]; !present {
					t.Fatalf("the %s parameter carries no %q", test.name, field)
				}
				delete(document, field)
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
	color := documentOf(t, types.ShaderParameterColor("tint", m.Color{R: 0.1, G: 0.2, B: 0.3, A: 0.4}))
	if want := []float32{0.1, 0.2, 0.3, 0.4}; !equalFloats(color.Value, want) {
		t.Errorf("color value = %v, want %v", color.Value, want)
	}
	vec := documentOf(t, types.ShaderParameterVec4("offset", m.Vec4{X: 1, Y: 2, Z: 3, W: 4}))
	if want := []float32{1, 2, 3, 4}; !equalFloats(vec.Value, want) {
		t.Errorf("vec4 value = %v, want %v", vec.Value, want)
	}
	if mat := documentOf(t, types.ShaderParameterMat4("mvp", m.NewMat4())); len(mat.Value) != 16 {
		t.Errorf("mat4 value has %d components, want 16", len(mat.Value))
	}
	if single := documentOf(t, types.ShaderParameterFloat("alpha", 0.5)); !equalFloats(single.Value, []float32{0.5}) {
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
	parameter := types.ShaderParameterTexture("albedo", types.TextureWithBytes(4, 4, types.FormatRGBA8, pixels, false, true))

	encoded, err := json.Marshal(parameter)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	document := documentOf(t, parameter)
	if document.Texture == nil {
		t.Fatal("a texture parameter carries no texture")
	}
	if document.Texture.Bytes != len(pixels) {
		t.Errorf("bytes = %d, want the %d the descriptor carries", document.Texture.Bytes, len(pixels))
	}
	if document.Texture.Width != 4 || document.Texture.Height != 4 {
		t.Errorf("size = %dx%d, want 4x4", document.Texture.Width, document.Texture.Height)
	}
	if !document.Texture.Mipmaps || document.Texture.Format != types.FormatRGBA8.String() || document.Texture.Source != "bytes" {
		t.Errorf("texture = %+v, want the source, format and mipmap flag it was built with", document.Texture)
	}
	// base64 of the run, and the run itself, both absent: a whole texture in a
	// reply is a debug facility that costs more than the bug.
	text := string(encoded)
	if strings.Contains(text, "AQIDBAUGBwg") || strings.Contains(text, "pixels") {
		t.Fatalf("the response carries the texture's pixels: %s", text)
	}
	if len(encoded) > 256 {
		t.Fatalf("a 64-byte texture serialized to %d bytes, so something bulky travelled", len(encoded))
	}
}

func TestARawParameterReportsItsLengthAndNotItsBytes(t *testing.T) {
	raw := types.ShaderParameterRaw("block", struct {
		A float32
		B float32
	}{1, 2})
	document := documentOf(t, raw)
	if document.Kind != "raw" {
		t.Fatalf("kind = %q, want raw", document.Kind)
	}
	if size := raw.ValueSize(); document.Bytes != size || size == 0 {
		t.Fatalf("bytes = %d, want the %d the parameter carries", document.Bytes, size)
	}
	if document.Value != nil || document.Texture != nil || document.Buffer != nil || document.Sampler != nil {
		t.Fatalf("a raw parameter serialized another kind's value: %+v", document)
	}
}

func TestABufferParameterCarriesTheRangeItBinds(t *testing.T) {
	buffer := types.BufferDescrWithBlob(assets.NewBlob(make([]byte, 512)), false)
	document := documentOf(t, types.ShaderParameterBufferRange("items", buffer, 128, 64))
	if document.Buffer == nil {
		t.Fatal("a buffer parameter carries no buffer")
	}
	if document.BufferOffset != 128 || document.BufferSize != 64 {
		t.Errorf("range = %d+%d, want 128+64", document.BufferOffset, document.BufferSize)
	}
	if document.Buffer.Size != 512 || document.Buffer.Bytes != 512 {
		t.Errorf("buffer = %+v, want the 512 bytes it was built from", document.Buffer)
	}
	whole := documentOf(t, types.ShaderParameterBuffer("items", buffer))
	if whole.BufferOffset != 0 || whole.BufferSize != 0 {
		t.Errorf("a whole-buffer binding reports range %d+%d, want 0+0 - which is how it says whole",
			whole.BufferOffset, whole.BufferSize)
	}
}

func TestAShaderNamesItsVariantAndADrawStateItsState(t *testing.T) {
	document := marshalToMap(t, struct {
		Shader shader.ShaderDescr `json:"shader"`
		State  types.DrawState    `json:"state"`
	}{
		shader.ShaderWithResource("shaders/pbr.wgsl", shader.ShaderDefine("SKINNED"), shader.ShaderConst("LIGHTS", "4")),
		StateOpaque3D(),
	})
	named, _ := document["shader"].(map[string]any)
	if named["path"] != "shaders/pbr.wgsl" || named["inline"] != nil {
		t.Errorf("shader = %v, want the resource path it names", named)
	}
	// One path under two supplies is two shaders, so the supply is part of the
	// name rather than a detail beside it.
	if supply, _ := named["supply"].(string); !strings.Contains(supply, "SKINNED") || !strings.Contains(supply, "LIGHTS=4") {
		t.Errorf("supply = %q, want the defines and consts the descriptor carries", supply)
	}
	state, _ := document["state"].(map[string]any)
	if state["depthCompare"] != "less" || state["depthWrite"] != true || state["cull"] != "back" {
		t.Errorf("state = %v, want the opaque 3D state named rather than numbered", state)
	}
	inline := marshalToMap(t, shader.ShaderWithText("// wgsl"))
	if inline["inline"] != true || inline["path"] != nil {
		t.Errorf("inline shader = %v, want no path and the inline flag", inline)
	}
}

func TestASnapshotViewCarriesAllThreeCoordinateSizes(t *testing.T) {
	view := SnapshotViewOf(types.Viewport{
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
