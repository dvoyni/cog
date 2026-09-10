package canvas

import (
	"bytes"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
	"github.com/gogpu/naga/ir"
)

// Where the halo's three knobs sit inside the test backend's hand-written union
// layout. It is one union standing in for every shader, so these are the
// union's offsets and not the halo shader's - the real ones are asserted
// against naga in TestTheHaloBlockIsThePublishedPrefixPlusThreeKnobs.
const (
	testHaloReachOffset    = 192
	testHaloPlateauOffset  = 196
	testHaloExponentOffset = 200
)

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

// The set carries the whole profile as per-batch parameters on its Params,
// which is the only frequency the knobs have: every valued parameter
// constructor sets HasValue, and shadeSprite sends a draw's valued parameter to
// the per-sprite arrays while appending a scope's to shared. Two reaches are two
// scopes.
func TestTheHaloSetCarriesTheWholeProfilePerBatch(t *testing.T) {
	set := HaloMaterialSet(HaloProfile{Reach: 12, Plateau: 0.25, Exponent: 2})
	want := map[string]float32{"haloReach": 12, "haloPlateau": 0.25, "haloExponent": 2}
	got := map[string]float32{}
	for _, param := range set.Params {
		value, ok := param.FloatValue()
		if !ok {
			t.Fatalf("parameter %q is not a float; a knob that is not a value cannot be per batch", param.Name())
		}
		if !param.HasValue() {
			t.Fatalf("parameter %q carries no value", param.Name())
		}
		got[param.Name()] = value
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("set parameters = %v, want %v", got, want)
	}
}

// A complete profile, never a struct emitting a parameter per non-zero field:
// Plateau 0 is a legitimate value - no plateau, pure falloff - that a sentinel
// would read as the default.
func TestAZeroPlateauReachesTheShaderAsZero(t *testing.T) {
	set := HaloMaterialSet(HaloProfile{Reach: 6, Plateau: 0, Exponent: 1})
	for _, param := range set.Params {
		if param.Name() != "haloPlateau" {
			continue
		}
		if value, _ := param.FloatValue(); value != 0 {
			t.Fatalf("haloPlateau = %v, want the zero the caller asked for", value)
		}
		return
	}
	t.Fatal("the set carries no haloPlateau; a zero field was dropped rather than sent")
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

// The material is a singleton whose parameters are baked at construction, and
// MaterialDescr.Fingerprint hashes those parameters. Two sets at two profiles
// must therefore name the same material and differ only in their scope
// parameters - otherwise every profile would be a second pipeline.
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

// The defaults live on the material's own parameters, because parameterRefFor
// searches the draw's first and the material's second - so the material's are
// defaults a scope overrides by name. This is what makes a hand-assembled set
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
	if got := floatAt(backend.drawParams[0], testHaloReachOffset); got != defaultHaloReach {
		t.Fatalf("haloReach = %v, want the material's own default %v", got, defaultHaloReach)
	}
	if got := floatAt(backend.drawParams[0], testHaloPlateauOffset); got != defaultHaloPlateau {
		t.Fatalf("haloPlateau = %v, want %v", got, defaultHaloPlateau)
	}
}

// And a scope that names a profile overrides those defaults by name, which is
// the whole of how a caller says how wide the band is.
func TestAScopesProfileOverridesTheMaterialsDefaults(t *testing.T) {
	k, _, backend := testKernel(t, fstest.MapFS{}, haloConfig(), func(write *OpQueue) {
		write.FillRect(0, m.Rect{Width: 8, Height: 8}, ShapeDraw{Color: m.Color{R: 1, A: 1}})
		write.SetLayerMaterial(0, HaloMaterialSet(HaloProfile{Reach: 12, Plateau: 0.25, Exponent: 2}))
	})
	runFrame(k)
	if len(backend.drawParams) != 1 {
		t.Fatalf("draws = %d, want 1", len(backend.drawParams))
	}
	if got := floatAt(backend.drawParams[0], testHaloReachOffset); got != 12 {
		t.Fatalf("haloReach = %v, want the scope's 12", got)
	}
	if got := floatAt(backend.drawParams[0], testHaloExponentOffset); got != 2 {
		t.Fatalf("haloExponent = %v, want the scope's 2", got)
	}
}

// The headline: one SetLayerMaterial over a halo layer haloes a Text number, a
// Sprite icon, a FillRect, a StrokeRect and a Line together, each in its own
// colour. They are not one batch - glyphs come from the font atlas and the rest
// from the sprite atlas - but every one of them shades with the halo material,
// which is what "together" means here.
func TestOneLayerSetHaloesEveryShapeInTheSpriteFamily(t *testing.T) {
	ink := m.Color{R: 0.68, G: 0.62, B: 0.55, A: 0.85}
	k, _, backend := testKernel(t, fstest.MapFS{}, DefaultConfig(), func(write *OpQueue) {
		write.Text(0, "", "42", TextDraw{Position: m.Vec2{X: 10, Y: 40}, Size: 16, Color: ink})
		write.Sprite(0, "", SpriteTransform{Size: m.Vec2{X: 8, Y: 8}}, nil,
			gfx.ColorParam(TintSlot, ink))
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
	assertBuiltinShaderLowers(t, haloShaderPath)
}

// The halo extends the uniform block with its three knobs, which is the
// mechanism a custom material declares per-batch parameters with. The canvas
// prefix has to stay exactly what uniforms.wgsl declares, because an appended
// member after a drifted prefix is silently misaddressed.
func TestTheHaloBlockIsThePublishedPrefixPlusThreeKnobs(t *testing.T) {
	want := []struct {
		name   string
		offset uint32
	}{
		{"canvasViewport", 0}, {"canvasLayer", 16}, {"canvasClip", 80},
		{"haloReach", 96}, {"haloPlateau", 100}, {"haloExponent", 104},
	}
	members := uniformBlockMembers(t, lowerBuiltinShader(t, haloShaderPath))
	if len(members) != len(want) {
		t.Fatalf("the halo block has %d members, want %d: %+v", len(members), len(want), members)
	}
	for i, member := range members {
		if member.Name != want[i].name || member.Offset != want[i].offset {
			t.Fatalf("member %d = %q@%d, want %q@%d", i, member.Name, member.Offset, want[i].name, want[i].offset)
		}
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
	module := lowerBuiltinShader(t, haloShaderPath)
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

// naga's SPIR-V backend cannot lower ir.ExprRelational: any() and all() over a
// vector of bools compile as WGSL and then die at pipeline creation with
// "unsupported expression kind". Every vector comparison in a canvas shader is
// spelled out component-wise because of it, and the halo is the shader most
// tempted to write any(tap < lo).
func TestTheHaloSpellsEveryVectorComparisonComponentWise(t *testing.T) {
	code := builtinSourceCode(t, haloShaderPath)
	for _, banned := range []string{"any(", "all("} {
		if strings.Contains(code, banned) {
			t.Errorf("%s calls %s...); naga's SPIR-V backend cannot lower it, spell the comparison out component-wise",
				haloShaderPath, banned)
		}
	}
}

// The halo includes spritebindings.wgsl alone rather than declining the
// bindings and hand-declaring them: declining would create a third, untested
// copy of the FROZEN SpriteInstance record, which only
// TestSpriteInstanceMatchesTheShaderRecord guards and only inside cog.
func TestTheHaloDeclaresNoCopyOfTheFrozenRecord(t *testing.T) {
	if strings.Contains(builtinSourceCode(t, haloShaderPath), "struct SpriteInstance") {
		t.Fatalf("%s declares its own SpriteInstance; include %s and read the published one", haloShaderPath, SpriteBindingsPath)
	}
	if got := strings.Count(flattenBuiltinShader(t, haloShaderPath), "struct SpriteInstance"); got != 1 {
		t.Fatalf("the halo flattens to %d SpriteInstance declarations, want 1", got)
	}
}

// It replaces both entry points, so it must include the bindings without
// spritevertex.wgsl - which declares a vs_main that does not expand the quad.
func TestTheHaloDeclaresItsOwnVertexStage(t *testing.T) {
	code := builtinSourceCode(t, haloShaderPath)
	if strings.Contains(code, SpriteVertexPath) {
		t.Fatalf("%s includes %s; a material that expands the quad writes its own vs_main", haloShaderPath, SpriteVertexPath)
	}
	if !strings.Contains(code, SpriteBindingsPath) {
		t.Fatalf("%s does not include %s", haloShaderPath, SpriteBindingsPath)
	}
	if got := strings.Count(flattenBuiltinShader(t, haloShaderPath), "fn vs_main"); got != 1 {
		t.Fatalf("the halo flattens to %d vs_main declarations, want 1", got)
	}
}

// The halo never reads the mark's colour - it samples the atlas for alpha only
// and returns vec4(tint.rgb, coverage * tint.a) - so it has no use for the
// key-colour ramp, and carrying it would cost a location in the inter-stage
// struct for nothing.
func TestTheHaloDoesNotIncludeTheKeyColourRamp(t *testing.T) {
	if strings.Contains(builtinSourceCode(t, haloShaderPath), "keycolor.wgsl") {
		t.Fatalf("%s includes the ramp; the halo paints a band in one colour and never reads the mark's", haloShaderPath)
	}
}

// Groups are numbered by what a binding is, and the halo binds exactly what the
// built-in sprite material binds: it declares no parameter array of its own and
// claims nothing in group 3.
func TestTheHaloNumbersItsGroupsByKind(t *testing.T) {
	want := map[string][2]uint32{
		"u": {0, 0}, "canvasSampler": {1, 0}, "canvasTexture": {1, 1}, "instances": {2, 0},
	}
	seen := map[string][2]uint32{}
	for _, variable := range lowerBuiltinShader(t, haloShaderPath).GlobalVariables {
		if variable.Binding == nil {
			continue
		}
		seen[variable.Name] = [2]uint32{variable.Binding.Group, variable.Binding.Binding}
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("halo bindings = %v, want %v", seen, want)
	}
}

// The material is unexported deliberately, and the WGSL is not published:
// keycolor.wgsl is published because a custom triangles material must reproduce
// the ramp or key every texel against black, and nothing has to reproduce the
// halo. This is the test that the source is mounted for canvas's own use and
// named by no exported constant.
func TestTheHaloSourceIsMountedButNotPublished(t *testing.T) {
	k, _, _ := testKernel(t, fstest.MapFS{}, DefaultConfig(), func(*OpQueue) {})
	got, _ := k.ExecuteCommand[readFileProbeCmd](readFileProbeRequest{Name: haloShaderPath})
	if !bytes.Equal(got.Data, readBuiltinSource(t, haloShaderPath)) {
		t.Fatalf("%s is not mounted; the material's own shader would not resolve", haloShaderPath)
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
// halo spells its comparisons out rather than calling any(), and why it does not
// include keycolor.wgsl - so a scan of the raw bytes reads its own explanation
// as a violation.
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
