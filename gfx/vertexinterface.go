package gfx

// CheckVertexInterface reports the first way a vertex layout fails the shader
// about to be drawn with it, and nil when the pair is legal. Three things can
// be wrong: an @location the layout does not supply at all, one it supplies at
// a different type, and a stride WebGPU refuses outright.
//
// **The type rule is equality of (kind, count), deliberately stricter than
// WebGPU.** WebGPU fills a shader's missing components with (0, 0, 0, 1) and
// drops the extra ones, so `vec3<f32>` over a two-component unorm yields
// (x, y, 0) - a plausible unit-ish direction lying in the XY plane. That is not
// a black screen and not garbage triangles; it is wrong shading that looks like
// art, in an engine with no pixel readback anywhere. Presence-only and
// base-type-compatible were both considered and both let exactly that case
// through, because a two-component unorm decodes to f32 and `vec3<f32>` is f32.
// The cost of forbidding WebGPU's legal widening and narrowing is zero against
// the tree: every @location in scene/builtin and canvas/builtin is an exact
// match already.
//
// **The check is one-directional.** A layout supplying an attribute the shader
// does not read is legal and common - scene's bundled vertex struct declares six
// of eight under the no-skin variant. The direction that fails is a shader input
// no attribute supplies, never the other way round.
//
// It is an exported plain function for the reason FlattenShader is: it is the
// test surface for a rule that otherwise could only be exercised through a
// backend, and a package that owns both halves of a pair - scene holds its
// layout and its shader - can ask the same question gfx will ask at draw time,
// through the same call.
func CheckVertexInterface(shader string, layout ShaderLayout, attrs []VertexAttr) error {
	stride := 0
	for i := range attrs {
		if end := attrs[i].offset + attrs[i].typ.size(); end > stride {
			stride = end
		}
	}
	// WebGPU requires this unconditionally, and a 30-byte stride succeeds on
	// Vulkan, Apple-silicon Metal and D3D12 while failing on js/wasm, on GLES and
	// on older Apple GPUs: green on the dev machine, broken in the browser. Both
	// named scene layouts satisfy it by construction, so what this guards is the
	// custom-layout path.
	if stride%4 != 0 {
		return ErrVertexStrideAlignment{Shader: shader, Stride: stride}
	}
	for _, input := range layout.VertexInputs {
		// An attribute's @location is its index in the layout, which is the same
		// mapping the pipeline descriptor is built with.
		if input.Location < 0 || input.Location >= len(attrs) {
			return ErrVertexInputUnsupplied{
				Shader: shader, Input: input.Name, Location: input.Location,
				Declared: input.Kind.wgsl(input.Count),
			}
		}
		kind, count := attrs[input.Location].typ.decode()
		if kind != input.Kind || count != input.Count {
			return ErrVertexInputMismatch{
				Shader: shader, Input: input.Name, Location: input.Location,
				Declared: input.Kind.wgsl(input.Count), Supplied: kind.wgsl(count),
			}
		}
	}
	return nil
}
