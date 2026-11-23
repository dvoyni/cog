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
// render.Inspection, the one render.Frame's RequestInspection asks of
// render, so a test compares the two; and Reports answers the reports render
// logged.
//
// The harness fails the test on any error the kernel logs, a render.Report
// among them, unless it is a report of a kind the test named with
// ExpectReport, or an error a match given ExpectError answers true of, such
// as a report of the test's own plugin.
//
// A test counting what a frame allocates, such as a plugin's allocation
// gate, calls RecordNothing before Spawn: gfx's recording adapter then keeps
// no call, which would allocate, and Frames answers nothing.
//
// A test of what a plugin does on render.RecoveredEvent calls LoseDevice
// with a window open: it loses gfx's device at the next beat's acquire,
// steps the beats losing and recovering it, and returns once every handler
// of render.RecoveredEvent has run, so the next Step draws the frame after
// the recovery. gfx's recording groups the frame lost, which presented
// nothing, with that one.
//
// A pixel test calls UseSoftwareAdapter before Spawn: render then draws on
// wgpu's software adapter, through gogpu's plugin in place of the recording
// adapter, real rendering on the CPU with no window and no GPU. The test
// draws into textures and reads them back through render's readback, with
// exact expected texels; it is skipped under -short. The harness wraps
// gogpu's instance, as it wraps the recording one, with kernel's
// WrapSocket, and checks each shader compiled there against its binding
// declaration, failing the test on a stale one, naming the shader and the
// first binding that differs.
//
// gogpu's HostLoop runs every call on the kernel's main thread, which on
// macOS must be the process's. A test package with pixel tests runs its
// tests through RunMain, which keeps the process's main thread for the
// harness:
//
//	func TestMain(m *testing.M) { rendertest.RunMain(m) }
//
// Without it, the harness locks the test's goroutine to an OS thread in its
// place, which serves on Windows and Linux, and skips a pixel test on macOS.
//
// A test of pass timings calls EnableTimestamps before Spawn: the fake GPU
// then has timestamp queries, which no real backend has until the wgpu fork
// exposes them, and a pass's timings are a nanosecond for each command it
// recorded, its beginning among them.
//
// # Binding declarations
//
// gfx's recording adapter parses no WGSL, so the harness answers a shader's
// reflection, its bindings, its entry points and its vertex entry points'
// inputs, from a binding
// declaration: a JSON file beside the shader, named for it with .json
// appended, lit.wgsl.json beside lit.wgsl, read from the files the shader is
// preprocessed from. render's cmd/declarebindings generates it from the
// shader on wgpu's software adapter, with a variant for each define set
// named with -variant, run from a //go:generate line beside the shaders:
//
//	//go:generate go run github.com/dvoyni/cog/plugins/render/cmd/declarebindings -variant SKINNED,JOINTS=4 lit.wgsl
//
// A shader compiled with no declaration, or with one that fails to parse,
// fails the test, naming its path. An answer a test of
// rendertest's own queues on the recording adapter wins over the
// declaration's.
//
// A declaration holds the shader's bindings, its entry points with their
// stages, the inputs of each vertex entry point by name, and a variant for
// each define set whose bindings, entry points or inputs differ:
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
//		"entryPoints": [{"name": "vertex_main", "stage": "vertex"}, {"name": "fragment_main", "stage": "fragment"}],
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
//   - "buffer": its "type", uniform, storage or read-only-storage, and its
//     type as WGSL lays it out: its "size", a runtime-sized array's counting
//     one element; a struct's "members", each with its "name", "offset" and
//     type; and an array's "element", a type, with its "elementCount", left
//     out for a runtime-sized array, and its "elementStride". A member's or
//     an element's type is spelt as the buffer's: a "size", and "members"
//     or an "element" when it has them;
//   - "sampler": its "type", filtering, non-filtering or comparison;
//   - "texture": its "sampleType", float, unfilterable-float, depth, sint or
//     uint, its "viewDimension", 1d, 2d, 2d-array, cube, cube-array or 3d,
//     and whether it is "multisampled", false when left out;
//   - "storageTexture": its "access", write-only, read-only or read-write, its
//     "format", one WebGPU allows a storage texture, such as rgba8unorm, and
//     its "viewDimension".
//
// An entry point has its name and its stage, vertex, fragment or compute, in
// the order the shader declares them; a declaration with none declares a
// shader with no function a material or dispatch can name.
//
// A vertex input has its location and its type as WGSL spells it, f32, f16,
// i32 or u32, or a vector of two to four of them, vec3<f32>; builtins are no
// inputs. A variant applies when its "defines", each name with its value,
// empty for none, are exactly those the shader is preprocessed with, in any
// order; its bindings, entry points and inputs then replace the
// declaration's whole. Any
// other define set takes the declaration's own. A declaration of variants
// alone, written with declarebindings' -variantsonly for a shader that
// compiles only with defines, such as a 2D material's reading its argument,
// holds "variantsOnly": true and its "variants", with no bindings, entry
// points or inputs of its own, and a define set none of them is fails the
// test.
//
// The harness preprocesses a shader with the gogpu plugin's preprocessor, as
// a device does, so its #include, #define and #if directives work and the
// files it includes are read through the filesystem render hands it. It
// appends a comment naming the shader and its defines to the text it
// answers gfx's PreprocessShader with, so it knows each module's shader
// whichever variants share their text; the recording adapter compiles the
// text without it.
package rendertest
