package wgpu

import (
	"os"
	"testing"

	cgfx "github.com/dvoyni/cog/gfx"
)

// bundledSceneShader reads scene's shader off disk rather than importing the
// scene package. wgpu is the only tree with a WGSL front end, so the check has
// to live here, and reading the file keeps the driver from depending on a
// plugin that sits above it.
func bundledSceneShader(t *testing.T) string {
	t.Helper()
	source, err := os.ReadFile("../scene/builtin/scene/scene.wgsl")
	if err != nil {
		t.Fatalf("read the bundled scene shader: %v", err)
	}
	return string(source)
}

// The bundled shader is the one module every scene draw goes through, and a
// binding it declares but scene does not bind takes the whole frame's command
// buffer down silently. So the bindings it declares are asserted here, where
// they can be read without a GPU.
func TestBundledSceneShaderDeclaresItsGroupZeroAndOneBindings(t *testing.T) {
	layout, err := reflectShaderLayout(bundledSceneShader(t))
	if err != nil {
		t.Fatalf("reflect the bundled scene shader: %v", err)
	}
	if layout.UniformSize != 0 {
		t.Fatalf("the scene shader declares a %d-byte uniform block; all scene data is storage", layout.UniformSize)
	}
	resources := map[string]cgfx.ShaderResource{}
	for _, resource := range layout.Resources {
		resources[resource.Name] = resource
	}
	for _, want := range []struct {
		name    string
		group   int
		binding int
	}{
		{name: "sceneFrame", group: 0, binding: 0},
		{name: "sceneInstances", group: 0, binding: 1},
		{name: "scenePbrMaterial", group: 1, binding: 0},
	} {
		got, ok := resources[want.name]
		if !ok {
			t.Fatalf("%s is not reflected; an unreflected binding is an unbound one", want.name)
		}
		if !got.StorageBuffer || got.WritableBuffer {
			t.Errorf("%s is %+v, want a read-only storage buffer", want.name, got)
		}
		if got.Group != want.group || got.Binding != want.binding {
			t.Errorf("%s is at group %d binding %d, want %d/%d",
				want.name, got.Group, got.Binding, want.group, want.binding)
		}
	}
	if len(layout.Resources) != 3 {
		t.Fatalf("the scene shader declares %d bindings, want the 3 asserted above: %+v",
			len(layout.Resources), layout.Resources)
	}
}

// The record offsets are a contract between this file and scene's Go structs,
// with nothing between them to catch a drift: scene packs bytes, the shader
// reads them, and a mismatch renders a plausible wrong picture.
func TestBundledSceneShaderRecordsMatchTheirPackedOffsets(t *testing.T) {
	layout, err := reflectShaderLayout(bundledSceneShader(t))
	if err != nil {
		t.Fatalf("reflect the bundled scene shader: %v", err)
	}
	offsets := map[string]map[string]int{}
	for _, resource := range layout.Resources {
		members := map[string]int{}
		for _, member := range resource.Members {
			members[member.Name] = member.Offset
		}
		offsets[resource.Name] = members
	}
	for binding, want := range map[string]map[string]int{
		"sceneFrame": {
			"view": 0, "projection": 64, "viewProjection": 128, "cameraPosition": 192,
		},
		"scenePbrMaterial": {"baseColorFactor": 0},
	} {
		for member, offset := range want {
			if got, ok := offsets[binding][member]; !ok || got != offset {
				t.Errorf("%s.%s is at offset %d (present: %v), want %d", binding, member, got, ok, offset)
			}
		}
	}
	// The instance array's stride is the record size scene pads its arena to.
	for _, member := range instancesMembers(t, layout) {
		if member.Name == "data" && member.Stride != 64 {
			t.Errorf("sceneInstances.data has stride %d, want the 64-byte record", member.Stride)
		}
	}
}

func instancesMembers(t *testing.T, layout cgfx.ShaderLayout) []cgfx.StorageMember {
	t.Helper()
	for _, resource := range layout.Resources {
		if resource.Name == "sceneInstances" {
			return resource.Members
		}
	}
	t.Fatal("sceneInstances is not reflected")
	return nil
}

// Scene's storage-buffer budget has no headroom on the browser floor, and every
// reflected binding costs a slot in both stages because reflection never
// consults entry points.
func TestBundledSceneShaderFitsTheWebStorageBudget(t *testing.T) {
	layout, err := reflectShaderLayout(bundledSceneShader(t))
	if err != nil {
		t.Fatalf("reflect the bundled scene shader: %v", err)
	}
	storage := 0
	for _, resource := range layout.Resources {
		if resource.StorageBuffer {
			storage++
		}
	}
	if floor := cgfx.DefaultLimits.MaxStorageBuffersPerShaderStage; storage > floor {
		t.Fatalf("the bundled scene shader declares %d storage buffers, past the web floor of %d", storage, floor)
	}
}
