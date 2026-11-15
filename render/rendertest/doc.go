// Package rendertest runs render in a real kernel on gfx's recording adapter,
// for render's own tests and those of the plugins that draw with it.
//
// A test makes a Harness, spawns it with render's config and its own plugins,
// which are listed after render, and steps one beat at a time:
//
//	h := rendertest.NewHarness(t)
//	h.Spawn(nil, myplugin.NewPlugin())
//	h.Step()
//	frames := h.Frames()
//
// Frames reads back each frame gfx drew in render's terms, as a
// render.Inspection, and Reports answers the reports render logged.
//
// The harness fails the test on any error the kernel logs, a render.Report
// among them, unless it is a report of a kind the test named with
// ExpectReport.
//
// # Binding declarations
//
// gfx's recording adapter parses no WGSL, so the harness answers a shader's
// reflection, its bindings and its vertex entry points' inputs, from a binding
// declaration: a JSON file beside the shader, named for it with .json
// appended, lit.wgsl.json beside lit.wgsl, read from the files the shader is
// preprocessed from. render's cmd/declarebindings generates it from the
// shader on wgpu's software adapter, with a variant for each define set
// named with -variant, run from a //go:generate line beside the shaders:
//
//	//go:generate go run github.com/dvoyni/cog/render/cmd/declarebindings -variant SKINNED,JOINTS=4 lit.wgsl A shader compiled with no declaration, or with one that
// fails to parse, fails the test, naming its path. An answer a test of
// rendertest's own queues on the recording adapter wins over the
// declaration's.
//
// A declaration holds the shader's bindings, the inputs of each vertex entry
// point by name, and a variant for each define set whose bindings or inputs
// differ:
//
//	{
//		"bindings": [
//			{"group": 0, "binding": 0, "name": "camera", "buffer": {"type": "uniform", "size": 80,
//				"members": [{"name": "view", "offset": 0, "size": 64}, {"name": "time", "offset": 64, "size": 4}]},
//				"entryPoints": ["vertex_main", "fragment_main"]},
//			{"group": 1, "binding": 0, "name": "albedo", "texture": {"sampleType": "float", "viewDimension": "2d"},
//				"entryPoints": ["fragment_main"]},
//			{"group": 1, "binding": 1, "name": "albedo_sampler", "sampler": {"type": "filtering"},
//				"entryPoints": ["fragment_main"]}
//		],
//		"vertexInputs": {"vertex_main": [{"location": 0, "type": "vec3<f32>"}]},
//		"variants": [
//			{"defines": {"SKINNED": "", "JOINTS": "4"}, "bindings": [...], "vertexInputs": {...}}
//		]
//	}
//
// Each binding has its group, number and name, the entry points that use it,
// directly or through the functions they call, and exactly one kind, as
// gfx.ShaderBinding holds it, spelt as WebGPU spells its values:
//
//   - "buffer": its "type", uniform, storage or read-only-storage, its
//     "size", that of its type as WGSL lays it out, a runtime-sized array's
//     counting one element, and its struct's "members", each with its name,
//     offset and size;
//   - "sampler": its "type", filtering, non-filtering or comparison;
//   - "texture": its "sampleType", float, unfilterable-float, depth, sint or
//     uint, its "viewDimension", 1d, 2d, 2d-array, cube, cube-array or 3d,
//     and whether it is "multisampled", false when left out;
//   - "storageTexture": its "access", write-only, read-only or read-write, its
//     "format", one WebGPU allows a storage texture, such as rgba8unorm, and
//     its "viewDimension".
//
// A vertex input has its location and its type as WGSL spells it, f32, f16,
// i32 or u32, or a vector of two to four of them, vec3<f32>; builtins are no
// inputs. A variant applies when its "defines", each name with its value,
// empty for none, are exactly those the shader is preprocessed with, in any
// order; its bindings and inputs then replace the declaration's whole. Any
// other define set takes the declaration's own.
//
// The harness appends a comment naming the shader and its defines to the text
// it answers gfx's PreprocessShader with, so it knows each module's shader
// whichever variants share their text; the recording adapter compiles the
// text without it.
package rendertest
