package rendertest_test

import (
	"strings"
	"testing"

	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

// The layout tests' shader, layout.wgsl: a uniform block scene, of an
// array<Light, 4> and a u32; a texture albedo; and a storage buffer rows, a
// runtime-sized array<Row>, each Row a mat3x2<f32>, a u32 and an Argument of
// a vec4<f32> and an f32.
const layoutDeclaration = `{
	"bindings": [
		{"group": 1, "binding": 0, "name": "scene", "buffer": {"type": "uniform", "size": 80, "members": [
			{"name": "lights", "offset": 0, "size": 64, "element": {"size": 16, "members": [{"name": "position", "offset": 0, "size": 12}, {"name": "range", "offset": 12, "size": 4}]}, "elementCount": 4, "elementStride": 16},
			{"name": "count", "offset": 64, "size": 4}
		]}, "entryPoints": ["fragment_main"]},
		{"group": 1, "binding": 1, "name": "albedo", "texture": {"sampleType": "float", "viewDimension": "2d"}, "entryPoints": ["fragment_main"]},
		{"group": 2, "binding": 0, "name": "rows", "buffer": {"type": "read-only-storage", "size": 64, "element": {"size": 64, "members": [
			{"name": "transform", "offset": 0, "size": 24},
			{"name": "layer", "offset": 24, "size": 4},
			{"name": "argument", "offset": 32, "size": 32, "members": [{"name": "tint", "offset": 0, "size": 16}, {"name": "strength", "offset": 16, "size": 4}]}
		]}, "elementStride": 64}, "entryPoints": ["vertex_main"]}
	],
	"entryPoints": [{"name": "vertex_main", "stage": "vertex"}, {"name": "fragment_main", "stage": "fragment"}],
	"vertexInputs": {"vertex_main": [{"location": 0, "type": "vec3<f32>"}]}
}`

// rowArgument mirrors layout.wgsl's Argument: a vec4<f32> and an f32, padded
// to its 32 bytes.
type rowArgument struct {
	Tint     [4]float32
	Strength float32
	_        [12]byte
}

// unpaddedArgument is rowArgument without its padding, of 20 bytes.
type unpaddedArgument struct {
	Tint     [4]float32
	Strength float32
}

// sceneLight mirrors layout.wgsl's Light: a vec3<f32> and an f32.
type sceneLight struct {
	Position [3]float32
	Range    float32
}

// swappedLight is sceneLight with its fields swapped, of its size but not
// its member offsets.
type swappedLight struct {
	Range    float32
	Position [3]float32
}

// sceneBlock mirrors layout.wgsl's uniform block scene: four lights and a
// count, padded to its 80 bytes.
type sceneBlock struct {
	Lights [4]sceneLight
	Count  uint32
	_      [12]byte
}

func TestAGoTypeIsCheckedAgainstTheTypeAtAPathWithinABinding(t *testing.T) {
	// Each check is given layout.wgsl's shader and one of a missing file,
	// and answers an error naming each of its wanted parts, or none for
	// none.
	checks := []struct {
		name      string
		check     func(inventory render.Inventory, shader, missing render.Shader) error
		wantParts []string
	}{
		{name: "an element's member of a runtime-sized array", check: func(inventory render.Inventory, shader, _ render.Shader) error {
			return inventory.CheckLayout[rowArgument](shader, "rows", "[].argument")
		}},
		{name: "an element of a fixed array within a uniform block", check: func(inventory render.Inventory, shader, _ render.Shader) error {
			return inventory.CheckLayout[sceneLight](shader, "scene", "lights[]")
		}},
		{name: "a uniform block whole", check: func(inventory render.Inventory, shader, _ render.Shader) error {
			return inventory.CheckLayout[sceneBlock](shader, "scene", "")
		}},
		{name: "a type of another size", wantParts: []string{"rows", `"[].argument"`, "unpaddedArgument", "20 bytes", "32"}, check: func(inventory render.Inventory, shader, _ render.Shader) error {
			return inventory.CheckLayout[unpaddedArgument](shader, "rows", "[].argument")
		}},
		{name: "a type of other offsets", wantParts: []string{"scene", `"lights[]"`, "swappedLight", "Position", "offset 4", "range", "12"}, check: func(inventory render.Inventory, shader, _ render.Shader) error {
			return inventory.CheckLayout[swappedLight](shader, "scene", "lights[]")
		}},
		{name: "a member the type lacks", wantParts: []string{"rows", `colour, which is no member of the type at "[]"`}, check: func(inventory render.Inventory, shader, _ render.Shader) error {
			return inventory.CheckLayout[rowArgument](shader, "rows", "[].colour")
		}},
		{name: "an element of no array", wantParts: []string{"rows", `the type at "[].argument", which is no array`}, check: func(inventory render.Inventory, shader, _ render.Shader) error {
			return inventory.CheckLayout[rowArgument](shader, "rows", "[].argument[]")
		}},
		{name: "a binding the shader lacks", wantParts: []string{"missing"}, check: func(inventory render.Inventory, shader, _ render.Shader) error {
			return inventory.CheckLayout[rowArgument](shader, "missing", "")
		}},
		{name: "a binding of no buffer", wantParts: []string{"albedo", "texture"}, check: func(inventory render.Inventory, shader, _ render.Shader) error {
			return inventory.CheckLayout[rowArgument](shader, "albedo", "")
		}},
		{name: "a shader that failed", wantParts: []string{"failed"}, check: func(inventory render.Inventory, _, missing render.Shader) error {
			return inventory.CheckLayout[rowArgument](missing, "rows", "[].argument")
		}},
		{name: "a shader render does not hold", wantParts: []string{"does not hold"}, check: func(inventory render.Inventory, _, _ render.Shader) error {
			return inventory.CheckLayout[rowArgument](render.Shader{}, "rows", "[].argument")
		}},
	}
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindLoadFailed)
	answers := make([]error, len(checks))
	spawnDeclaredHarness(t, h, nil, map[string]string{"layout.wgsl": layoutDeclaration}, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		shader := inventory.AcquireShader("layout.wgsl")
		missing := inventory.AcquireShader("missing.wgsl")
		for index, check := range checks {
			answers[index] = check.check(inventory, shader, missing)
		}
	})

	for index, check := range checks {
		err := answers[index]
		if check.wantParts == nil {
			if err != nil {
				t.Errorf("%s: answered %v, want it to fit", check.name, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: answered no error, want one naming %q", check.name, check.wantParts)
			continue
		}
		for _, part := range check.wantParts {
			if !strings.Contains(err.Error(), part) {
				t.Errorf("%s: answered %q, want it to name %q", check.name, err, part)
			}
		}
	}
}
