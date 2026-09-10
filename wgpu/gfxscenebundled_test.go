package wgpu

import (
	"os"
	"testing"

	cgfx "github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/storage"
)

// bundledSceneShader flattens one variant of scene's shader off disk rather
// than importing the scene package. wgpu is the only tree with a WGSL front end,
// so the check has to live here, and reading the sources keeps the driver from
// depending on a plugin that sits above it.
//
// SCENE_MAX_LIGHTS is deliberately not supplied: the array's size below is the
// includee's own default, which is what an unsupplied #const means.
func bundledSceneShader(t *testing.T, opts ...cgfx.ShaderOption) string {
	t.Helper()
	filesystem := storage.NewFileSystem("scene", os.DirFS("../scene"))
	text, _, err := cgfx.FlattenShader(filesystem, cgfx.ShaderWithResource("builtin/scene/scene.wgsl", opts...))
	if err != nil {
		t.Fatalf("flatten the bundled scene shader: %v", err)
	}
	return text
}

// sceneVariant is the supply for one of the four variants scene draws.
func sceneVariant(defines ...string) []cgfx.ShaderOption {
	opts := make([]cgfx.ShaderOption, 0, len(defines))
	for _, name := range defines {
		opts = append(opts, cgfx.ShaderDefine(name))
	}
	return opts
}

// everyFeature is the variant a skinned, morphed draw gets. With both defines
// supplied the reflected layout must match what the single unsplit module
// declared, which is what makes the split falsifiable against one number.
func everyFeature() []cgfx.ShaderOption { return sceneVariant("SCENE_SKIN", "SCENE_MORPH") }

// The bundled shader is the one module every scene draw goes through, and a
// binding it declares but scene does not bind takes the whole frame's command
// buffer down silently. So the bindings it declares are asserted here, where
// they can be read without a GPU.
func TestBundledSceneShaderDeclaresItsGroupZeroAndOneBindings(t *testing.T) {
	layout, err := reflectShaderLayout(bundledSceneShader(t, everyFeature()...))
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
		{name: "sceneAnim", group: 0, binding: 2},
		{name: "scenePbrMaterial", group: 1, binding: 0},
		// Group 2 is per model, and this is the everything variant, so all
		// three are declared. A draw that reads none of them declares none of
		// them - see TestBundledSceneShaderVariantsDeclareOnlyWhatTheyRead.
		{name: "scenePoses", group: 2, binding: 0},
		{name: "sceneSkinJoints", group: 2, binding: 1},
		{name: "sceneMorphDeltas", group: 2, binding: 2},
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
	// Five slots, each a texture and its own sampler, all in the per-material
	// group. Scene binds every one of them on every draw, with a white texel
	// and a flat normal where the material names no texture.
	for _, slot := range []string{
		"baseColor", "metallicRoughness", "normal", "occlusion", "emissive",
	} {
		texture, ok := resources[slot+"Texture"]
		if !ok {
			t.Fatalf("%sTexture is not reflected; an unreflected binding is an unbound one", slot)
		}
		if texture.Sampler || texture.StorageBuffer || texture.Group != 1 {
			t.Errorf("%sTexture is %+v, want a group 1 texture", slot, texture)
		}
		sampler, ok := resources[slot+"Sampler"]
		if !ok {
			t.Fatalf("%sSampler is not reflected; five slots means five samplers", slot)
		}
		if !sampler.Sampler || sampler.Comparison || sampler.Group != 1 {
			t.Errorf("%sSampler is %+v, want a group 1 filtering sampler", slot, sampler)
		}
	}
	if len(layout.Resources) != 17 {
		t.Fatalf("the scene shader declares %d bindings, want the 17 asserted above: %+v",
			len(layout.Resources), layout.Resources)
	}
}

// The record offsets are a contract between the shader and scene's Go structs:
// scene packs bytes, the shader reads them, and a mismatch renders a plausible
// wrong picture. This half pins what the shader declares, through the same gfx
// reflection the driver binds by, and the strides and counts with it;
// scene's own TestEveryUploadedRecordMatchesItsShaderStruct pins the Go structs
// against the same members, which is the half wgpu cannot reach from here.
func TestBundledSceneShaderRecordsMatchTheirPackedOffsets(t *testing.T) {
	layout, err := reflectShaderLayout(bundledSceneShader(t, everyFeature()...))
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
			"view": 0, "projection": 64, "viewProjection": 128,
			"cameraPosition": 192, "viewDirection": 208,
			"sunDirection": 224, "sunColor": 240, "ambientSky": 256, "ambientGround": 272,
			"lightCount": 288, "lights": 304,
		},
		// The per-slot metadata is flat named members rather than an array,
		// because array members are not name-addressable through
		// OverrideParams - and animating baseColorTransform per frame is UV
		// scrolling, which the array form forecloses permanently.
		"scenePbrMaterial": {
			"baseColorFactor": 0, "emissiveFactor": 16,
			"baseColorTransform": 32, "metallicRoughnessTransform": 48,
			"normalTransform": 64, "occlusionTransform": 80, "emissiveTransform": 96,
			"baseColorRotation": 112, "metallicRoughnessRotation": 116,
			"normalRotation": 120, "occlusionRotation": 124, "emissiveRotation": 128,
			"metallicFactor": 132, "roughnessFactor": 136, "normalScale": 140,
			"occlusionStrength": 144, "alphaCutoff": 148, "uvSets": 152,
		},
	} {
		for member, offset := range want {
			if got, ok := offsets[binding][member]; !ok || got != offset {
				t.Errorf("%s.%s is at offset %d (present: %v), want %d", binding, member, got, ok, offset)
			}
		}
	}
	// The instance array's stride is the record size scene pads its arena to.
	for _, member := range membersOf(t, layout, "sceneInstances") {
		if member.Name == "data" && member.Stride != 64 {
			t.Errorf("sceneInstances.data has stride %d, want the 64-byte record", member.Stride)
		}
	}
	// The light array is the fixed cap of 48-byte records, which is the whole
	// reason the cap is a constant rather than a knob.
	for _, member := range membersOf(t, layout, "sceneFrame") {
		if member.Name == "lights" && (member.Stride != 48 || member.Count != 16) {
			t.Errorf("sceneFrame.lights is %d x %d bytes, want 16 x 48", member.Count, member.Stride)
		}
	}
	// The three animation strides are the whole addressing contract. A pose
	// row is indexed as clipBase + frame*jointCount + joint and a joint record
	// by the same joint index, so a stride that drifts from scene's Go structs
	// reads every joint after the first from the wrong place.
	for _, want := range []struct {
		binding string
		stride  int
	}{
		// 112 is exactly seven vec4s with no tail padding, which is what the
		// explicit-column form buys over mat4x3 and mat3x3.
		{binding: "scenePoses", stride: 48},
		{binding: "sceneSkinJoints", stride: 112},
		// sceneAnim is a raw vec4 arena, because animOffset counts vec4s, and
		// sceneMorphDeltas is one too: a delta record is 16 * popcount(mask)
		// bytes, so the array element is the slot rather than the record.
		{binding: "sceneAnim", stride: 16},
		{binding: "sceneMorphDeltas", stride: 16},
	} {
		for _, member := range membersOf(t, layout, want.binding) {
			if member.Name == "data" && member.Stride != want.stride {
				t.Errorf("%s.data has stride %d, want %d",
					want.binding, member.Stride, want.stride)
			}
		}
	}
}

func membersOf(t *testing.T, layout cgfx.ShaderLayout, name string) []cgfx.StorageMember {
	t.Helper()
	for _, resource := range layout.Resources {
		if resource.Name == name {
			return resource.Members
		}
	}
	t.Fatalf("%s is not reflected", name)
	return nil
}

// Scene's storage-buffer budget has no headroom on the browser floor, and every
// reflected binding costs a slot in both stages because reflection never
// consults entry points.
func TestBundledSceneShaderFitsTheWebStorageBudget(t *testing.T) {
	layout, err := reflectShaderLayout(bundledSceneShader(t, everyFeature()...))
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

// A draw declares only the bindings it actually uses, and the numbers below are
// the whole argument for the split: a static draw drops from seven storage
// buffers to three. scene.wgsl recorded that sceneMorphDeltas was "the seventh
// and last one this module may ever declare - the eighth stays reserved",
// against the browser core adapter's floor of eight per stage. The budget was
// one binding from exhausted for every draw, including a debug line that reads
// none of them.
func TestBundledSceneShaderVariantsDeclareOnlyWhatTheyRead(t *testing.T) {
	for _, want := range []struct {
		name     string
		defines  []string
		bindings int
		storage  int
	}{
		{name: "debug line or static prop", defines: nil, bindings: 13, storage: 3},
		{name: "morph only, a face", defines: []string{"SCENE_MORPH"}, bindings: 15, storage: 5},
		{name: "skinned, no morph", defines: []string{"SCENE_SKIN"}, bindings: 16, storage: 6},
		{name: "everything", defines: []string{"SCENE_SKIN", "SCENE_MORPH"}, bindings: 17, storage: 7},
	} {
		layout, err := reflectShaderLayout(bundledSceneShader(t, sceneVariant(want.defines...)...))
		if err != nil {
			t.Fatalf("%s: reflect: %v", want.name, err)
		}
		storage := 0
		for _, resource := range layout.Resources {
			if resource.StorageBuffer {
				storage++
			}
		}
		if len(layout.Resources) != want.bindings || storage != want.storage {
			t.Errorf("%s declares %d bindings and %d storage buffers, want %d and %d",
				want.name, len(layout.Resources), storage, want.bindings, want.storage)
		}
	}
}

// The morph-only variant declares group 2 binding 2 with no bindings 0 or 1.
// WebGPU permits non-contiguous binding numbers and gfx builds its layouts from
// reflection rather than by counting, so the gap costs nothing - but it is the
// first time a group's shape varies by variant, and anything assuming density
// would break on it.
func TestMorphOnlyVariantLeavesAGapInGroupTwo(t *testing.T) {
	layout, err := reflectShaderLayout(bundledSceneShader(t, sceneVariant("SCENE_MORPH")...))
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}
	var groupTwo []int
	for _, resource := range layout.Resources {
		if resource.Group == 2 {
			groupTwo = append(groupTwo, resource.Binding)
		}
	}
	if len(groupTwo) != 1 || groupTwo[0] != 2 {
		t.Fatalf("group 2 holds bindings %v, want just binding 2", groupTwo)
	}
	// buildShaderLayouts keys every entry on the reflected binding number and
	// sizes the group array by the highest group it saw, so the gap is carried
	// rather than counted over. That it needs a device to run is why the shape is
	// asserted here and the pipeline is built by the examples.
	for _, resource := range layout.Resources {
		if resource.Group == 2 && resource.Name != "sceneMorphDeltas" {
			t.Errorf("group 2 also declares %q in the morph-only variant", resource.Name)
		}
	}
}

// The includee declares its own default and Go overrides it, which is what
// keeps scene's cap and the shader's array in step instead of duplicated. The
// span moves because the array is the record's tail.
func TestSceneMaxLightsOverrideResizesTheFrameRecord(t *testing.T) {
	for _, want := range []struct {
		supplied string
		span     int
	}{
		{supplied: "", span: 1072},
		{supplied: "4", span: 496},
	} {
		opts := everyFeature()
		if want.supplied != "" {
			opts = append(opts, cgfx.ShaderConst("SCENE_MAX_LIGHTS", want.supplied))
		}
		layout, err := reflectShaderLayout(bundledSceneShader(t, opts...))
		if err != nil {
			t.Fatalf("supplied %q: reflect: %v", want.supplied, err)
		}
		span := 0
		for _, member := range membersOf(t, layout, "sceneFrame") {
			if member.Name == "lights" {
				span = member.Offset + member.Count*member.Stride
			}
		}
		if span != want.span {
			t.Errorf("with SCENE_MAX_LIGHTS %q the frame record spans %d bytes, want %d",
				want.supplied, span, want.span)
		}
	}
}
