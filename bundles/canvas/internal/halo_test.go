package internal

import (
	"bytes"
	"encoding/binary"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/spirv"
	"github.com/gogpu/naga/wgsl"
)

// haloProfileOf reads a profile back out of the bytes a halo binding carries,
// which are HaloProfile's own.
func haloProfileOf(t *testing.T, data []byte) HaloProfile {
	t.Helper()
	if len(data) != 12 {
		t.Fatalf("halo binding = %d bytes, want HaloProfile's 12", len(data))
	}
	return HaloProfile{Reach: floatAt(data, 0), Plateau: floatAt(data, 4), Exponent: floatAt(data, 8)}
}

// The profile came off painted art rather than being guessed: 84% of the halo
// pixels in feuds' militiaman.png are exactly #ae9f8d, with alpha holding near
// 0.82 for the first fifth of the band and then falling almost exactly
// linearly. These three numbers are that measurement, and a caller edits the
// filled struct rather than assembling one from zeroes.
func TestDefaultHaloProfileIsTheProfileMeasuredOffTheArt(t *testing.T) {
	want := HaloProfile{Reach: 6, Plateau: 0.18, Exponent: 1}
	if got := DefaultHaloProfile(); got != want {
		t.Fatalf("DefaultHaloProfile() = %+v, want %+v", got, want)
	}
}

// The set carries the whole profile as one per-batch parameter on its Params,
// which is the only frequency it has: every valued parameter constructor sets
// HasValue, and shadeSprite sends a draw's valued parameter to the per-sprite
// arrays while keeping a scope's for the batch. Two reaches are two scopes.
func TestTheHaloSetCarriesTheWholeProfilePerBatch(t *testing.T) {
	profile := HaloProfile{Reach: 12, Plateau: 0.25, Exponent: 2}
	set := HaloMaterialSet(profile)
	if len(set.Params) != 1 || set.Params[0].Name != HaloSlot || !set.Params[0].Kind.IsValue() {
		t.Fatalf("set parameters = %+v, want the one %q value", set.Params, HaloSlot)
	}
	data, _ := set.Params[0].AppendValueTo(nil)
	if got := haloProfileOf(t, data); got != profile {
		t.Fatalf("set profile = %+v, want %+v", got, profile)
	}
}

// A complete profile, never a parameter per non-zero field: Plateau 0 is a
// legitimate value - no plateau, pure falloff - that a sentinel would read as
// the default.
func TestAZeroPlateauReachesTheShaderAsZero(t *testing.T) {
	k, _, backend := testKernel(t, fstest.MapFS{}, haloConfig(), func(write *OpQueue) {
		write.FillRect(0, m.Rect{Width: 8, Height: 8}, ShapeDraw{Color: m.Color{R: 1, A: 1}})
		write.SetLayerMaterial(0, HaloMaterialSet(HaloProfile{Reach: 6, Plateau: 0, Exponent: 1}))
	})
	runFrame(k)
	if got := haloProfileOf(t, backend.uniformOf(0, HaloSlot)); got.Plateau != 0 || got.Reach != 6 {
		t.Fatalf("bound profile = %+v, want the zero plateau the caller asked for", got)
	}
}

// Triangles and Texture stay nil, so a DrawTriangles recorded on the halo layer
// paints as itself rather than as a band. The layer is meant to be dedicated to
// the marks being haloed, and a nil slot is what says so.
func TestTheHaloSetLeavesTheOtherTwoFamiliesAlone(t *testing.T) {
	set := HaloMaterialSet(DefaultHaloProfile())
	if set.Sprite == nil {
		t.Fatal("the halo set names no sprite material")
	}
	if set.Triangles != nil || set.Texture != nil {
		t.Fatalf("the halo set fills the triangles or texture slot: %+v", set)
	}
}

// The material is a singleton whose parameter is baked at construction, and
// Material.Fingerprint hashes it. Two sets at two profiles must therefore name
// the same material and differ only in their scope parameters - otherwise
// every profile would be a second set.
func TestEveryHaloSetNamesOneMaterialAtOneFingerprint(t *testing.T) {
	wide := HaloMaterialSet(HaloProfile{Reach: 30, Plateau: 0.5, Exponent: 3})
	narrow := HaloMaterialSet(DefaultHaloProfile())
	if wide.Sprite != narrow.Sprite {
		t.Fatal("two profiles produced two materials; the profile rides the scope, not the material")
	}
	if wide.Sprite.Fingerprint() != narrow.Sprite.Fingerprint() {
		t.Fatal("the halo material's fingerprint moved; its parameters were mutated")
	}
}

// The default lives on the material's own parameter, which is its set's own
// value, and a scope's is the frame's version over it - so the material's is a
// default a scope overrides by name. This is what makes a hand-assembled set
// with no parameters render at reach 6 rather than rendering nothing.
func TestAHandAssembledHaloSetRendersAtTheMaterialsOwnDefaults(t *testing.T) {
	bare := MaterialSet{Sprite: HaloMaterialSet(DefaultHaloProfile()).Sprite}
	k, _, backend := testKernel(t, fstest.MapFS{}, haloConfig(), func(write *OpQueue) {
		write.FillRect(0, m.Rect{Width: 8, Height: 8}, ShapeDraw{Color: m.Color{R: 1, A: 1}})
		write.SetLayerMaterial(0, bare)
	})
	runFrame(k)
	if len(backend.drawParams) != 1 {
		t.Fatalf("draws = %d, want 1", len(backend.drawParams))
	}
	if got := haloProfileOf(t, backend.uniformOf(0, HaloSlot)); got != DefaultHaloProfile() {
		t.Fatalf("bound profile = %+v, want the material's own default %+v", got, DefaultHaloProfile())
	}
}

// And a scope that names a profile overrides those defaults by name, which is
// the whole of how a caller says how wide the band is.
func TestAScopesProfileOverridesTheMaterialsDefaults(t *testing.T) {
	profile := HaloProfile{Reach: 12, Plateau: 0.25, Exponent: 2}
	k, _, backend := testKernel(t, fstest.MapFS{}, haloConfig(), func(write *OpQueue) {
		write.FillRect(0, m.Rect{Width: 8, Height: 8}, ShapeDraw{Color: m.Color{R: 1, A: 1}})
		write.SetLayerMaterial(0, HaloMaterialSet(profile))
	})
	runFrame(k)
	if len(backend.drawParams) != 1 {
		t.Fatalf("draws = %d, want 1", len(backend.drawParams))
	}
	if got := haloProfileOf(t, backend.uniformOf(0, HaloSlot)); got != profile {
		t.Fatalf("bound profile = %+v, want the scope's %+v", got, profile)
	}
}

// The headline: one SetLayerMaterial over a halo layer haloes a Text number, a
// Sprite icon, a FillRect, a StrokeRect and a Line together, each in its own
// colour. They are not one batch - glyphs come from the font atlas and the rest
// from the sprite atlas - but every one of them shades with the halo material,
// which is what "together" means here.
func TestOneLayerSetHaloesEveryShapeInTheSpriteFamily(t *testing.T) {
	ink := m.Color{R: 0.68, G: 0.62, B: 0.55, A: 0.85}
	k, _, backend := testKernel(t, fstest.MapFS{}, Config{}, func(write *OpQueue) {
		write.Text(0, "", "42", TextDraw{Position: m.Vec2{X: 10, Y: 40}, Size: 16, Color: ink})
		write.Sprite(0, "", SpriteTransform{Size: m.Vec2{X: 8, Y: 8}}, nil,
			gfx.ShaderParameterColor(TintSlot, ink))
		write.FillRect(0, m.Rect{X: 20, Width: 8, Height: 8}, ShapeDraw{Color: ink})
		write.StrokeRect(0, m.Rect{X: 40, Width: 8, Height: 8}, ShapeDraw{Color: ink, Thickness: 1})
		write.Line(0, m.Vec2{X: 60}, m.Vec2{X: 68, Y: 8}, ShapeDraw{Color: ink, Thickness: 1})
		write.SetLayerMaterial(0, HaloMaterialSet(DefaultHaloProfile()))
	})
	runFrame(k)
	if len(backend.pipelines) == 0 {
		t.Fatal("nothing was drawn")
	}
	for i := range backend.pipelines {
		if !strings.Contains(backend.pipelineShader(i), "fn haloFalloff") {
			t.Fatalf("pipeline %d was not built from the halo material", i)
		}
	}
	// Every instance wears the draw's own colour, because the band's colour and
	// its peak alpha are tint - a frozen field of the instance record rather
	// than a storage array, so it is per sprite and costs nothing.
	instances := spriteInstances(backend)
	if len(instances) < 2 {
		t.Fatalf("instance buffers = %d, want the glyph run and the sprite/shape run", len(instances))
	}
	for _, buffer := range instances {
		for i := 0; i < len(buffer)/testInstanceSize; i++ {
			record := instanceAt(buffer, i)
			got := m.Color{
				R: floatAt(record, 48), G: floatAt(record, 52),
				B: floatAt(record, 56), A: floatAt(record, 60),
			}
			if got != ink {
				t.Fatalf("instance tint = %+v, want the draw's own colour %+v", got, ink)
			}
		}
	}
}

// A shape draw on the halo layer expands its quad in the vertex stage, and
// nothing in canvas reads a sprite's extent - no clip intersection, no
// batch-key field, no culling - so the instance record it batches is the
// ordinary one. This is the record the expansion is computed from, so a
// divergence would move the band rather than fail.
func TestTheHaloBatchesTheOrdinaryInstanceRecord(t *testing.T) {
	k, _, backend := testKernel(t, fstest.MapFS{}, haloConfig(), func(write *OpQueue) {
		write.FillRect(0, m.Rect{X: 4, Y: 6, Width: 8, Height: 10}, ShapeDraw{Color: m.Color{A: 1}})
		write.SetLayerMaterial(0, HaloMaterialSet(DefaultHaloProfile()))
	})
	runFrame(k)
	instances := spriteInstances(backend)
	if len(instances) != 1 {
		t.Fatalf("instance buffers = %d, want 1", len(instances))
	}
	record := instanceAt(instances[0], 0)
	if x, y := floatAt(record, 8), floatAt(record, 12); x != 8 || y != 10 {
		t.Fatalf("transform0.zw = %v,%v, want the fill's own 8x10 size", x, y)
	}
	// The white texel's frame is a degenerate point, which is what the fragment
	// stage branches on to pick the analytic distance-to-rect over the ring
	// kernel.
	if floatAt(record, 32) != floatAt(record, 40) || floatAt(record, 36) != floatAt(record, 44) {
		t.Fatal("a fill's frame is not a degenerate point; the analytic branch would never be taken")
	}
}

func TestHaloShaderParses(t *testing.T) {
	assertBuiltinShaderLowers(t, HaloShaderPath)
}

// The halo's profile is set whole as one HaloProfile, so the WGSL struct and the
// Go one are one layout for the same reason the canvas block is.
func TestTheHaloBindingMatchesHaloProfile(t *testing.T) {
	module := lowerBuiltinShader(t, HaloShaderPath)
	goType := reflect.TypeFor[HaloProfile]()
	members := uniformMembers(t, module, HaloSlot)
	if len(members) != goType.NumField() {
		t.Fatalf("halo profile has %d members, want HaloProfile's %d", len(members), goType.NumField())
	}
	for i, member := range members {
		field := goType.Field(i)
		if !strings.EqualFold(member.Name, field.Name) || int(member.Offset) != int(field.Offset) {
			t.Fatalf("halo member %d = %q@%d, want HaloProfile.%s@%d", i, member.Name, member.Offset, field.Name, field.Offset)
		}
	}
	if size := uniformSize(t, module, HaloSlot); size != int(goType.Size()) {
		t.Fatalf("halo profile is %d bytes, want HaloProfile's %d", size, goType.Size())
	}
}

// The halo is the first material to widen its own inter-stage struct, and
// nothing in cog checks the WebGPU floor on those: checkWebLimits counts
// storage buffers, bind groups and uniform size only. The floor is 16
// inter-stage variables and 60 components, and a material that exceeds it
// passes every other test cog has and then fails at pipeline creation on the
// web.
func TestTheHaloInterStageStructFitsTheWebGPUFloor(t *testing.T) {
	const maxLocations, maxComponents = 16, 60
	module := lowerBuiltinShader(t, HaloShaderPath)
	var record ir.StructType
	for _, typ := range module.Types {
		if structure, ok := typ.Inner.(ir.StructType); ok && typ.Name == "HaloVertexOut" {
			record = structure
		}
	}
	if record.Members == nil {
		t.Fatal("the halo shader declares no HaloVertexOut; it must not reuse the published VertexOut")
	}
	locations, components := 0, 0
	for _, member := range record.Members {
		if member.Binding == nil {
			continue
		}
		if _, ok := (*member.Binding).(ir.LocationBinding); !ok {
			continue
		}
		locations++
		components += componentCount(t, module, member.Type)
	}
	if locations > maxLocations || components > maxComponents {
		t.Fatalf("HaloVertexOut spends %d locations / %d components against a floor of %d / %d",
			locations, components, maxLocations, maxComponents)
	}
	if locations != 6 || components != 12 {
		t.Fatalf("HaloVertexOut spends %d locations / %d components, want 6 / 12 - the shape the design settled on",
			locations, components)
	}
}

// The halo's frame rejection is a vector comparison - any(tap < lo) - and that
// is the form naga's SPIR-V backend used to refuse: it lowered cleanly to IR and
// then died at pipeline creation with "unsupported expression kind:
// ir.ExprRelational". Writing it the natural way is safe only because go.mod
// overrides naga with the fork carrying the fix (cog#227).
//
// This test is what stands between that override and a device. Drop the replace
// directive before a fixed naga is released and the halo stops compiling here,
// loudly, rather than on the one machine that runs Vulkan.
func TestTheHaloVectorComparisonsReachTheSPIRVBinary(t *testing.T) {
	blob, err := spirv.NewBackend(spirv.DefaultOptions()).Compile(lowerBuiltinShader(t, HaloShaderPath))
	if err != nil {
		t.Fatalf("compile %q to SPIR-V: %v", HaloShaderPath, err)
	}

	// OpAny reduces the frame rejection, OpAll the silhouette test underneath it.
	const opAny, opAll = 154, 155
	for _, op := range []struct {
		name string
		code uint32
	}{{"OpAny", opAny}, {"OpAll", opAll}} {
		if !spirvCarriesOpcode(blob, op.code) {
			t.Errorf("the halo's SPIR-V carries no %s, so its vector comparisons are no longer "+
				"vector comparisons - if the shader was spelled out component-wise again, that "+
				"workaround is obsolete", op.name)
		}
	}
}

// spirvCarriesOpcode walks the instruction stream past the five-word header and
// reports whether any instruction has the given opcode.
func spirvCarriesOpcode(blob []byte, opcode uint32) bool {
	const headerWords = 5
	if len(blob)%4 != 0 || len(blob) < headerWords*4 {
		return false
	}
	for at := headerWords * 4; at+4 <= len(blob); {
		word := binary.LittleEndian.Uint32(blob[at:])
		words := int(word >> 16)
		if words == 0 || at+words*4 > len(blob) {
			return false
		}
		if word&0xFFFF == opcode {
			return true
		}
		at += words * 4
	}
	return false
}

// The halo includes spritebindings.wgsl alone rather than declining the
// bindings and hand-declaring them: declining would create a third, untested
// copy of the FROZEN SpriteInstance record, which only
// TestSpriteInstanceMatchesTheShaderRecord guards and only inside cog.
func TestTheHaloDeclaresNoCopyOfTheFrozenRecord(t *testing.T) {
	for _, path := range []string{HaloShaderPath, HaloBandPath} {
		if strings.Contains(builtinSourceCode(t, path), "struct SpriteInstance") {
			t.Fatalf("%s declares its own SpriteInstance; include %s and read the published one", path, SpriteBindingsPath)
		}
	}
	if got := strings.Count(flattenBuiltinShader(t, HaloShaderPath), "struct SpriteInstance"); got != 1 {
		t.Fatalf("the halo flattens to %d SpriteInstance declarations, want 1", got)
	}
}

// It replaces both entry points, so it must include the bindings without
// spritevertex.wgsl - which declares a vs_main that does not expand the quad.
func TestTheHaloDeclaresItsOwnVertexStage(t *testing.T) {
	code := builtinSourceCode(t, HaloBandPath)
	if strings.Contains(code, SpriteVertexPath) {
		t.Fatalf("%s includes %s; a material that expands the quad writes its own vs_main", HaloBandPath, SpriteVertexPath)
	}
	if !strings.Contains(code, SpriteBindingsPath) {
		t.Fatalf("%s does not include %s", HaloBandPath, SpriteBindingsPath)
	}
	if got := strings.Count(flattenBuiltinShader(t, HaloShaderPath), "fn vs_main"); got != 1 {
		t.Fatalf("the halo flattens to %d vs_main declarations, want 1", got)
	}
}

// The halo never reads the mark's colour - it samples the atlas for alpha only
// and returns vec4(tint.rgb, coverage * tint.a) - so it has no use for the
// key-colour ramp, and carrying it would cost a location in the inter-stage
// struct for nothing.
func TestTheHaloDoesNotIncludeTheKeyColourRamp(t *testing.T) {
	if strings.Contains(flattenBuiltinShader(t, HaloShaderPath), "fn keyColorRamp") {
		t.Fatalf("%s includes the ramp; the halo paints a band in one colour and never reads the mark's", HaloShaderPath)
	}
}

// Groups are numbered by what a binding is, and the halo binds what the
// built-in sprite material binds plus its own profile in group 0: it declares no
// parameter array of its own and claims nothing in group 3.
func TestTheHaloNumbersItsGroupsByKind(t *testing.T) {
	want := map[string][2]uint32{
		"u": {0, 0}, "halo": {0, 1}, "canvasSampler": {1, 0}, "canvasTexture": {1, 1}, "instances": {2, 0},
	}
	seen := map[string][2]uint32{}
	for _, variable := range lowerBuiltinShader(t, HaloShaderPath).GlobalVariables {
		if variable.Binding == nil {
			continue
		}
		seen[variable.Name] = [2]uint32{variable.Binding.Group, variable.Binding.Binding}
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("halo bindings = %v, want %v", seen, want)
	}
}

// The material is unexported deliberately and its root is named by no exported
// constant, but the band it is made of is published as HaloBandPath. Both are
// mounted: the root for canvas's own material, the band for that root and for
// any material an app builds on it.
func TestTheHaloRootAndItsBandAreMounted(t *testing.T) {
	k, _, _ := testKernel(t, fstest.MapFS{}, Config{}, func(*OpQueue) {})
	for _, path := range []string{HaloShaderPath, HaloBandPath} {
		got := k.ExecuteCommand[readFileProbeCmd](readFileProbeRequest{Name: path})
		if !bytes.Equal(got.Data, readBuiltinSource(t, path)) {
			t.Fatalf("%s is not mounted; a halo material's shader would not resolve", path)
		}
	}
}

// The root is the band and nothing else: an include and an fs_main that returns
// haloBand. That is what makes the published source the whole halo rather than
// most of it - a material built on HaloBandPath that passes the band through
// untouched is canvas's own halo.
func TestTheHaloRootIsItsBandUntouched(t *testing.T) {
	code := builtinSourceCode(t, HaloShaderPath)
	if !strings.Contains(code, "//#include "+HaloBandPath) {
		t.Fatalf("%s does not include %s", HaloShaderPath, HaloBandPath)
	}
	for _, declared := range []string{"struct ", "var<", "fn vs_main", "fn haloBand"} {
		if strings.Contains(code, declared) {
			t.Fatalf("%s declares %q itself; it belongs in %s, where a material built on the band gets it too", HaloShaderPath, declared, HaloBandPath)
		}
	}
	if !strings.Contains(code, "return haloBand(in);") {
		t.Fatalf("%s does not return the band untouched", HaloShaderPath)
	}
}

// What the band is published for: a material that includes it, declares a
// uniform of its own beside the profile and passes haloBand through a function
// of its own. It has to lower as the halo does, with the profile still at the
// binding HaloMaterialSet's parameter reaches and exactly one of each stage.
func TestAMaterialBuiltOnTheBandLowers(t *testing.T) {
	const source = "//#include " + HaloBandPath + `
@group(0) @binding(2) var<uniform> fade: f32;

@fragment
fn fs_main(in: HaloVertexOut) -> @location(0) vec4<f32> {
    let band = haloBand(in);
    return vec4<f32>(band.rgb, band.a * fade);
}
`
	text, err := flattenShader(t, builtinMountID, builtinFS, gfx.ShaderWithText(source))
	if err != nil {
		t.Fatalf("flatten a material built on %s: %v", HaloBandPath, err)
	}
	for _, stage := range []string{"fn vs_main", "fn fs_main"} {
		if got := strings.Count(text, stage); got != 1 {
			t.Fatalf("a material built on the band flattens to %d %q declarations, want 1", got, stage)
		}
	}
	parsed, err := naga.Parse(text)
	if err != nil {
		t.Fatalf("parse a material built on %s: %v", HaloBandPath, err)
	}
	module, err := wgsl.Lower(parsed)
	if err != nil {
		t.Fatalf("lower a material built on %s: %v", HaloBandPath, err)
	}
	want := map[string][2]uint32{
		"u": {0, 0}, "halo": {0, 1}, "fade": {0, 2}, "canvasSampler": {1, 0}, "canvasTexture": {1, 1}, "instances": {2, 0},
	}
	seen := map[string][2]uint32{}
	for _, variable := range module.GlobalVariables {
		if variable.Binding == nil {
			continue
		}
		seen[variable.Name] = [2]uint32{variable.Binding.Group, variable.Binding.Binding}
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("bindings of a material built on the band = %v, want %v", seen, want)
	}
}

// haloConfig is the small atlas the shape tests draw into: no artwork, so the
// generated white texel is all the atlas ever holds.
func haloConfig() Config {
	return Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
}

// readBuiltinSource reads one embedded source as it ships, before the
// preprocessor resolves anything - which is what a test about what a source
// declares itself has to look at.
func readBuiltinSource(t *testing.T, path string) []byte {
	t.Helper()
	source, err := fs.ReadFile(builtinFS, path)
	if err != nil {
		t.Fatalf("read embedded shader %q: %v", path, err)
	}
	return source
}

// builtinSourceCode is one embedded source with its prose removed: everything
// after a "//" that is not an "//#include" directive. A shader's header comment
// names the very things these tests forbid the code from doing - it says why the
// halo does not include keycolor.wgsl, and why it declares no SpriteInstance of
// its own - so a scan of the raw bytes reads its own explanation as a violation.
func builtinSourceCode(t *testing.T, path string) string {
	t.Helper()
	var code strings.Builder
	for _, line := range strings.Split(string(readBuiltinSource(t, path)), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//#") {
			code.WriteString(line)
		} else if comment := strings.Index(line, "//"); comment >= 0 {
			code.WriteString(line[:comment])
		} else {
			code.WriteString(line)
		}
		code.WriteByte('\n')
	}
	return code.String()
}

// componentCount reports how many inter-stage components one type occupies: the
// unit the WebGPU floor of 60 is counted in.
func componentCount(t *testing.T, module *ir.Module, handle ir.TypeHandle) int {
	t.Helper()
	switch inner := module.Types[handle].Inner.(type) {
	case ir.ScalarType:
		return 1
	case ir.VectorType:
		return int(inner.Size)
	}
	t.Fatalf("an inter-stage member is neither a scalar nor a vector: %T", module.Types[handle].Inner)
	return 0
}
