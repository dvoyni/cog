package types

// Throwaway research benchmarks for bundles/scene/docs/research/decoder-seam-cost.md.
// They measure what a raw-data decoder seam would cost the parse half of a model
// load, against real models from the sibling cog-examples repository. Every
// benchmark skips when those assets are absent.
//
// Point COG_EXAMPLES_ASSETS at cog-examples/assets when it is not the sibling
// of this checkout's usual home.

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

var seamModels = []string{
	"BoxVertexColors/BoxVertexColors.glb",
	"Fox/Fox.glb",
	"CesiumMilkTruck/CesiumMilkTruck.glb",
	"WaterBottle/WaterBottle.glb",
	"MorphStressTest/MorphStressTest.glb",
	"AlphaBlendModeTest/AlphaBlendModeTest.glb",
	"CompareBaseColor/CompareBaseColor.glb",
	"AnimatedMorphCube/AnimatedMorphCube.glb",
}

func seamAssets() string {
	if dir := os.Getenv("COG_EXAMPLES_ASSETS"); dir != "" {
		return dir
	}
	return "C:/Repos/cog-games/cog-examples/assets"
}

func seamRead(tb testing.TB, name string) ([]byte, string) {
	full := filepath.Join(seamAssets(), name)
	data, err := os.ReadFile(full)
	if err != nil {
		tb.Skipf("model not available: %v", err)
	}
	return data, full
}

func seamDecode(tb testing.TB, data []byte, full string) *gltf.Document {
	doc := new(gltf.Document)
	decoder := gltf.NewDecoderFS(bytes.NewReader(data), os.DirFS(filepath.Dir(full)))
	if err := decoder.Decode(doc); err != nil {
		tb.Fatal(err)
	}
	return doc
}

func seamParse(tb testing.TB, data []byte, full string) *LoadedModel {
	loaded, err := parseModel(assets.NewBlob(data), filepath.Base(full), os.DirFS(filepath.Dir(full)), 60)
	if err != nil {
		tb.Fatal(err)
	}
	return loaded
}

// TestSeamModelStats prints what each candidate model carries.
func TestSeamModelStats(t *testing.T) {
	for _, name := range seamModels {
		data, full := seamRead(t, name)
		loaded := seamParse(t, data, full)
		vertices, indices, skinned, morphed := 0, 0, 0, 0
		for _, g := range loaded.geometries {
			vertices += len(g.vertices)
			indices += len(g.indices)
			if g.skinned {
				skinned++
			}
			if g.morph.morphed() {
				morphed++
			}
		}
		t.Logf("%-45s %8d bytes  geoms=%d prims=%d verts=%d idx=%d skinnedGeoms=%d morphedGeoms=%d joints=%d clips=%d poseRows=%d morphWords=%d textures=%d materials=%d",
			name, len(data), len(loaded.geometries), len(loaded.primitives), vertices, indices,
			skinned, morphed, loaded.animation.jointCount, len(loaded.animation.clips),
			len(loaded.animation.poses), len(loaded.morphDeltas), len(loaded.textures), len(loaded.materials))
	}
}

// BenchmarkSeamParse is the whole parse half: bytes in memory to LoadedModel.
func BenchmarkSeamParse(b *testing.B) {
	for _, name := range seamModels {
		data, full := seamRead(b, name)
		b.Run(filepath.Base(name), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				seamParse(b, data, full)
			}
		})
	}
}

// BenchmarkSeamDecode is the qmuntal/gltf document decode alone.
func BenchmarkSeamDecode(b *testing.B) {
	for _, name := range seamModels {
		data, full := seamRead(b, name)
		b.Run(filepath.Base(name), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				seamDecode(b, data, full)
			}
		})
	}
}

// BenchmarkSeamReadFile is the disk read the parse is handed, for scale. The
// file is warm in the OS cache after the first iteration.
func BenchmarkSeamReadFile(b *testing.B) {
	for _, name := range seamModels {
		_, full := seamRead(b, name)
		b.Run(filepath.Base(name), func(b *testing.B) {
			for b.Loop() {
				if _, err := os.ReadFile(full); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkSeamInstallPack is the CPU side of the upload half's geometry: the
// in-place pack and the index narrowing bakeModelGeometry does before handing
// bytes to the resource queue. The copy that gives each iteration fresh
// vertices is outside the timer.
func BenchmarkSeamInstallPack(b *testing.B) {
	for _, name := range seamModels {
		data, full := seamRead(b, name)
		loaded := seamParse(b, data, full)
		b.Run(filepath.Base(name), func(b *testing.B) {
			scratch := make([][]skinnedVertex, len(loaded.geometries))
			for i, g := range loaded.geometries {
				scratch[i] = make([]skinnedVertex, len(g.vertices))
			}
			for b.Loop() {
				b.StopTimer()
				for i, g := range loaded.geometries {
					copy(scratch[i], g.vertices)
				}
				b.StartTimer()
				for i, g := range loaded.geometries {
					uv := meshRecordFor(g.uv0, g.uv1)
					packOverAuthored(scratch[i], uv, g.skinnedLayout)
					if len(g.indices) > 0 {
						indexBytes(g.indices, indexWidthFor(len(g.vertices)))
					}
				}
			}
		})
	}
}

// BenchmarkSeamImageDecode decodes every embedded image the parse named, for
// scale: the parse only slices these bytes, and the texture cache decodes them
// later, on the upload side of the load.
func BenchmarkSeamImageDecode(b *testing.B) {
	for _, name := range seamModels {
		data, full := seamRead(b, name)
		loaded := seamParse(b, data, full)
		b.Run(filepath.Base(name), func(b *testing.B) {
			for b.Loop() {
				for _, descr := range loaded.textures {
					if descr.Blob.Len() == 0 {
						continue
					}
					if _, _, err := image.Decode(bytes.NewReader(descr.Blob.Data())); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// seamPrimitives lists every primitive of every mesh in the document once.
func seamPrimitives(doc *gltf.Document) []*gltf.Primitive {
	var out []*gltf.Primitive
	for _, mesh := range doc.Meshes {
		out = append(out, mesh.Primitives...)
	}
	return out
}

// BenchmarkSeamVertex compares the shipped one-pass vertex read against a
// two-pass one that first lands every accessor in a plain slice - the shape a
// raw-data decoder would hand across the seam - and then fills the conversion
// vertices from those slices. Everything else convertPrimitive does (indices,
// morph targets, topology, generated normals and tangents) is identical in both
// arms, so the difference between them is the thin cut's added pass and
// allocations. The document is decoded once, outside the timer.
//
// Run the arms interleaved: -bench 'SeamVertex/.*/onepass' then 'twopass', and
// repeat.
func BenchmarkSeamVertex(b *testing.B) {
	for _, name := range seamModels {
		data, full := seamRead(b, name)
		doc := seamDecode(b, data, full)
		primitives := seamPrimitives(doc)
		for _, arm := range []struct {
			name    string
			convert func(*gltf.Document, *gltf.Primitive, bool) (gltfGeometry, error)
		}{{"onepass", convertPrimitive}, {"twopass", convertPrimitiveTwoPass}, {"twopassalias", convertPrimitiveAlias}} {
			b.Run(filepath.Base(name)+"/"+arm.name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					for _, p := range primitives {
						if _, err := arm.convert(doc, p, false); err != nil && err != errPointTopology {
							b.Fatal(err)
						}
					}
				}
			})
		}
	}
}

// TestSeamTwoPassMatches holds the two-pass arm to the shipped one's output, so
// the benchmark compares two ways of computing the same thing.
func TestSeamTwoPassMatches(t *testing.T) {
	for _, name := range seamModels {
		data, full := seamRead(t, name)
		doc := seamDecode(t, data, full)
		for i, p := range seamPrimitives(doc) {
			one, err1 := convertPrimitive(doc, p, true)
			two, err2 := convertPrimitiveTwoPass(doc, p, true)
			three, err3 := convertPrimitiveAlias(doc, p, true)
			if fmt.Sprint(err1) != fmt.Sprint(err3) || len(one.vertices) != len(three.vertices) {
				t.Fatalf("%s primitive %d: alias arm differs", name, i)
			}
			for v := range one.vertices {
				if one.vertices[v] != three.vertices[v] {
					t.Fatalf("%s primitive %d vertex %d differs in the alias arm", name, i, v)
				}
			}
			if one.uv0 != three.uv0 || one.uv1 != three.uv1 {
				t.Fatalf("%s primitive %d: alias uv ranges differ", name, i)
			}
			if fmt.Sprint(err1) != fmt.Sprint(err2) {
				t.Fatalf("%s primitive %d: errors differ: %v vs %v", name, i, err1, err2)
			}
			if len(one.vertices) != len(two.vertices) {
				t.Fatalf("%s primitive %d: vertex counts differ", name, i)
			}
			for v := range one.vertices {
				if one.vertices[v] != two.vertices[v] {
					t.Fatalf("%s primitive %d vertex %d differs", name, i, v)
				}
			}
			if one.uv0 != two.uv0 || one.uv1 != two.uv1 {
				t.Fatalf("%s primitive %d: uv ranges differ", name, i)
			}
		}
	}
}

// convertPrimitiveTwoPass is convertPrimitive with readVertexAttributes
// replaced by the raw-then-fill pair below; the rest is a verbatim copy.
func convertPrimitiveTwoPass(doc *gltf.Document, primitive *gltf.Primitive, needTangents bool) (gltfGeometry, error) {
	return convertPrimitiveVia(doc, primitive, needTangents, func(doc *gltf.Document, primitive *gltf.Primitive, g *gltfGeometry) error {
		raw, err := readRawAttributes(doc, primitive)
		if err != nil {
			return err
		}
		fillFromRaw(g, &raw)
		return nil
	})
}

// convertPrimitiveAlias is the cheapest thin cut: the raw slices are
// modeler's own decoded accessors wherever those are already float32, so the
// seam adds no allocation over the shipped path and only the fill becomes a
// separate pass. Quantised accessors are widened into a fresh slice.
func convertPrimitiveAlias(doc *gltf.Document, primitive *gltf.Primitive, needTangents bool) (gltfGeometry, error) {
	return convertPrimitiveVia(doc, primitive, needTangents, func(doc *gltf.Document, primitive *gltf.Primitive, g *gltfGeometry) error {
		raw, err := readAliasAttributes(doc, primitive)
		if err != nil {
			return err
		}
		fillFromAlias(g, &raw)
		return nil
	})
}

func convertPrimitiveVia(
	doc *gltf.Document, primitive *gltf.Primitive, needTangents bool,
	fill func(*gltf.Document, *gltf.Primitive, *gltfGeometry) error,
) (gltfGeometry, error) {
	position, ok := attributeAccessor(doc, primitive.Attributes, gltf.POSITION)
	if !ok {
		return gltfGeometry{}, fmt.Errorf("it has no POSITION attribute")
	}
	geometry := gltfGeometry{vertices: make([]skinnedVertex, position.Count)}
	if err := fill(doc, primitive, &geometry); err != nil {
		return gltfGeometry{}, err
	}
	geometry.box, geometry.hasBox = accessorBox(position)
	geometry.morph = readMorphTargets(doc, primitive, len(geometry.vertices))
	geometry.expandBoxByMorph()

	indices, err := readIndices(doc, primitive)
	if err != nil {
		return gltfGeometry{}, err
	}
	geometry.topology, geometry.indices, err = convertTopology(primitive.Mode, indices, len(geometry.vertices))
	if err != nil {
		return gltfGeometry{}, err
	}
	if geometry.topology != gfx.TopologyTriangleList {
		return geometry, nil
	}
	if _, has := primitive.Attributes[gltf.NORMAL]; !has {
		var source []uint32
		geometry.vertices, geometry.indices, source = unweld(geometry.vertices, geometry.indices)
		geometry.morph.remap(source)
		generateFlatNormals(geometry.vertices)
	}
	if _, has := primitive.Attributes[gltf.TANGENT]; !has && needTangents {
		generateTangents(geometry.vertices, geometry.indices)
	}
	return geometry, nil
}

// rawAttributes is the decoder's output under the thin cut: one plain slice per
// attribute, in scene's float types where the accessor needed widening and in
// modeler's own shape where modeler already widened it.
type rawAttributes struct {
	positions, normals []m.Vec3
	tangents           []m.Vec4
	uv0, uv1           []m.Vec2
	colors             [][4]uint8
	joints             [][4]uint16
	weights            [][4]float32
}

func readRawAttributes(doc *gltf.Document, primitive *gltf.Primitive) (rawAttributes, error) {
	var raw rawAttributes
	position, _ := attributeAccessor(doc, primitive.Attributes, gltf.POSITION)
	raw.positions = make([]m.Vec3, position.Count)
	if err := readAttribute(doc, position, func(i int, v attrValue) {
		raw.positions[i] = m.Vec3{X: v[0], Y: v[1], Z: v[2]}
	}); err != nil {
		return raw, fmt.Errorf("POSITION: %w", err)
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.NORMAL); ok {
		raw.normals = make([]m.Vec3, accessor.Count)
		if err := readAttribute(doc, accessor, func(i int, v attrValue) {
			raw.normals[i] = m.Vec3{X: v[0], Y: v[1], Z: v[2]}
		}); err != nil {
			return raw, fmt.Errorf("NORMAL: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.TANGENT); ok {
		raw.tangents = make([]m.Vec4, accessor.Count)
		if err := readAttribute(doc, accessor, func(i int, v attrValue) {
			raw.tangents[i] = m.Vec4{X: v[0], Y: v[1], Z: v[2], W: v[3]}
		}); err != nil {
			return raw, fmt.Errorf("TANGENT: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.TEXCOORD_0); ok {
		raw.uv0 = make([]m.Vec2, accessor.Count)
		if err := readAttribute(doc, accessor, func(i int, v attrValue) {
			raw.uv0[i] = m.Vec2{X: v[0], Y: v[1]}
		}); err != nil {
			return raw, fmt.Errorf("TEXCOORD_0: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.TEXCOORD_1); ok {
		raw.uv1 = make([]m.Vec2, accessor.Count)
		if err := readAttribute(doc, accessor, func(i int, v attrValue) {
			raw.uv1[i] = m.Vec2{X: v[0], Y: v[1]}
		}); err != nil {
			return raw, fmt.Errorf("TEXCOORD_1: %w", err)
		}
	}
	var err error
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.COLOR_0); ok {
		if raw.colors, err = modeler.ReadColor(doc, accessor, nil); err != nil {
			return raw, fmt.Errorf("COLOR_0: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.JOINTS_0); ok {
		if raw.joints, err = modeler.ReadJoints(doc, accessor, nil); err != nil {
			return raw, fmt.Errorf("JOINTS_0: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.WEIGHTS_0); ok {
		if raw.weights, err = modeler.ReadWeights(doc, accessor, nil); err != nil {
			return raw, fmt.Errorf("WEIGHTS_0: %w", err)
		}
	}
	return raw, nil
}

// fillFromRaw is the thin cut's second pass: the conversion vertices built from
// the raw slices, with the normalise and the UV ranges the one-pass read does
// inside its callbacks.
func fillFromRaw(geometry *gltfGeometry, raw *rawAttributes) {
	vertices := geometry.vertices
	for i := range vertices {
		v := &vertices[i]
		v.Position = raw.positions[i]
		if raw.normals != nil {
			v.Normal = raw.normals[i].Normalize()
		}
		if raw.tangents != nil {
			v.Tangent = raw.tangents[i]
		}
		if raw.uv0 != nil {
			v.UV0 = raw.uv0[i]
			geometry.uv0.add(v.UV0)
		}
		if raw.uv1 != nil {
			v.UV1 = raw.uv1[i]
			geometry.uv1.add(v.UV1)
		}
		if raw.colors != nil {
			const scale = 1.0 / unorm8CodeMax
			c := raw.colors[i]
			v.Color = m.NewColorLinear(float32(c[0])*scale, float32(c[1])*scale,
				float32(c[2])*scale, float32(c[3])*scale)
		} else {
			v.Color = m.White
		}
		if raw.joints != nil {
			v.Joints = raw.joints[i]
		}
		if raw.weights != nil {
			w := raw.weights[i]
			v.Weights = m.Vec4{X: w[0], Y: w[1], Z: w[2], W: w[3]}
		}
	}
}

// aliasAttributes is rawAttributes in modeler's own array shapes, so a float32
// accessor's decoded slice is the raw slice itself.
type aliasAttributes struct {
	positions, normals [][3]float32
	tangents           [][4]float32
	uv0, uv1           [][2]float32
	colors             [][4]uint8
	joints             [][4]uint16
	weights            [][4]float32
}

func alias2(doc *gltf.Document, accessor *gltf.Accessor) ([][2]float32, error) {
	data, err := modeler.ReadAccessor(doc, accessor, nil)
	if err != nil {
		return nil, err
	}
	if v, ok := data.([][2]float32); ok {
		return v, nil
	}
	out := make([][2]float32, accessor.Count)
	err = readAttribute(doc, accessor, func(i int, v attrValue) { out[i] = [2]float32{v[0], v[1]} })
	return out, err
}

func alias3(doc *gltf.Document, accessor *gltf.Accessor) ([][3]float32, error) {
	data, err := modeler.ReadAccessor(doc, accessor, nil)
	if err != nil {
		return nil, err
	}
	if v, ok := data.([][3]float32); ok {
		return v, nil
	}
	out := make([][3]float32, accessor.Count)
	err = readAttribute(doc, accessor, func(i int, v attrValue) { out[i] = [3]float32{v[0], v[1], v[2]} })
	return out, err
}

func alias4(doc *gltf.Document, accessor *gltf.Accessor) ([][4]float32, error) {
	data, err := modeler.ReadAccessor(doc, accessor, nil)
	if err != nil {
		return nil, err
	}
	if v, ok := data.([][4]float32); ok {
		return v, nil
	}
	out := make([][4]float32, accessor.Count)
	err = readAttribute(doc, accessor, func(i int, v attrValue) { out[i] = [4]float32(v) })
	return out, err
}

func readAliasAttributes(doc *gltf.Document, primitive *gltf.Primitive) (aliasAttributes, error) {
	var raw aliasAttributes
	var err error
	position, _ := attributeAccessor(doc, primitive.Attributes, gltf.POSITION)
	if raw.positions, err = alias3(doc, position); err != nil {
		return raw, fmt.Errorf("POSITION: %w", err)
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.NORMAL); ok {
		if raw.normals, err = alias3(doc, accessor); err != nil {
			return raw, fmt.Errorf("NORMAL: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.TANGENT); ok {
		if raw.tangents, err = alias4(doc, accessor); err != nil {
			return raw, fmt.Errorf("TANGENT: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.TEXCOORD_0); ok {
		if raw.uv0, err = alias2(doc, accessor); err != nil {
			return raw, fmt.Errorf("TEXCOORD_0: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.TEXCOORD_1); ok {
		if raw.uv1, err = alias2(doc, accessor); err != nil {
			return raw, fmt.Errorf("TEXCOORD_1: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.COLOR_0); ok {
		if raw.colors, err = modeler.ReadColor(doc, accessor, nil); err != nil {
			return raw, fmt.Errorf("COLOR_0: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.JOINTS_0); ok {
		if raw.joints, err = modeler.ReadJoints(doc, accessor, nil); err != nil {
			return raw, fmt.Errorf("JOINTS_0: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.WEIGHTS_0); ok {
		if raw.weights, err = modeler.ReadWeights(doc, accessor, nil); err != nil {
			return raw, fmt.Errorf("WEIGHTS_0: %w", err)
		}
	}
	return raw, nil
}

func fillFromAlias(geometry *gltfGeometry, raw *aliasAttributes) {
	vertices := geometry.vertices
	for i := range vertices {
		v := &vertices[i]
		p := raw.positions[i]
		v.Position = m.Vec3{X: p[0], Y: p[1], Z: p[2]}
		if raw.normals != nil {
			n := raw.normals[i]
			v.Normal = m.Vec3{X: n[0], Y: n[1], Z: n[2]}.Normalize()
		}
		if raw.tangents != nil {
			t := raw.tangents[i]
			v.Tangent = m.Vec4{X: t[0], Y: t[1], Z: t[2], W: t[3]}
		}
		if raw.uv0 != nil {
			v.UV0 = m.Vec2{X: raw.uv0[i][0], Y: raw.uv0[i][1]}
			geometry.uv0.add(v.UV0)
		}
		if raw.uv1 != nil {
			v.UV1 = m.Vec2{X: raw.uv1[i][0], Y: raw.uv1[i][1]}
			geometry.uv1.add(v.UV1)
		}
		if raw.colors != nil {
			const scale = 1.0 / unorm8CodeMax
			c := raw.colors[i]
			v.Color = m.NewColorLinear(float32(c[0])*scale, float32(c[1])*scale,
				float32(c[2])*scale, float32(c[3])*scale)
		} else {
			v.Color = m.White
		}
		if raw.joints != nil {
			v.Joints = raw.joints[i]
		}
		if raw.weights != nil {
			w := raw.weights[i]
			v.Weights = m.Vec4{X: w[0], Y: w[1], Z: w[2], W: w[3]}
		}
	}
}
