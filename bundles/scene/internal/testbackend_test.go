package internal

import (
	"encoding/binary"
	"math"
	"strings"
	"sync"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// testBackend is a gfx.Backend that mints ids and records what the last frame
// asked the GPU to do. It is the oracle every drawing test here asserts
// against: the binding is judged by what reached gfx, not by anything scene
// would have to publish for inspection.
//
// It replays gfx.Queue through the public sinks and keeps, per pass, every
// draw with the state bound when it was issued: the pipeline, the SetParams
// bytes, the vertex and index buffers and every storage range. Uploaded bytes
// are kept per buffer, so a test decodes the records scene packed rather than
// only their offsets.
//
// Every shader it reflects declares scenePbrMaterial whole and one uniform of
// its own for each name in testBindings, so the values a draw's set and its
// frame's version supply reach the backend and read back by name.
type testBackend struct {
	mu          sync.Mutex
	nextTexture gfx.TextureID
	nextBuffer  gfx.BufferID
	nextID      uint32
	// shaders is each compiled module's source, which is how a test tells a
	// custom material's shader from the bundled one: gfx.ShaderWithText hands
	// its text through untouched.
	shaders   map[gfx.ShaderID]string
	pipelines map[gfx.PipelineID]gfx.PipelineDesc
	baked     map[gfx.BufferID][]byte
	uniforms  []byte
	// last is the most recent frame's passes, and building the one being
	// replayed. A test reads last between frames.
	last, building []recordedPass
	current        *recordedPass
	state          recordedDraw
}

// recordedPass is one render pass as the backend received it.
type recordedPass struct {
	desc  gfx.PassDescr
	draws []recordedDraw
}

// recordedDraw is one draw call and the state it was issued under.
type recordedDraw struct {
	pipeline gfx.PipelineID
	// params is every uniform block bound when the draw was issued, by group
	// and binding.
	params   map[[2]int][]byte
	vertices gfx.BufferID
	index    gfx.BufferID
	// buffers is every storage range bound when the draw was issued, keyed by
	// the reflected name scene binds it through.
	buffers                                map[string]bufferRange
	first, count, instances, firstInstance int
	indexed                                bool
}

// bufferRange is one storage binding: the buffer and the bound window of it.
// A size of zero binds the buffer whole.
type bufferRange struct {
	buffer       gfx.BufferID
	offset, size int
}

// testUniforms is the bundled material's scenePbrMaterial block at the offsets
// gogpu reflects, which is where a test reads a draw's material numbers back
// the way the shader would. The block is set whole by a draw's set and its
// frame's version. The names after it are a uniform block's members only for
// the old path's shaders, NewShader's; a reflected shader declares them as
// testBindings instead.
var testUniforms = []gfx.StorageMember{
	{Name: "baseColorFactor", Offset: 0}, {Name: "emissiveFactor", Offset: 16},
	{Name: "baseColorTransform", Offset: 32}, {Name: "metallicRoughnessTransform", Offset: 48},
	{Name: "normalTransform", Offset: 64}, {Name: "occlusionTransform", Offset: 80},
	{Name: "emissiveTransform", Offset: 96},
	{Name: "baseColorRotation", Offset: 112}, {Name: "metallicRoughnessRotation", Offset: 116},
	{Name: "normalRotation", Offset: 120}, {Name: "occlusionRotation", Offset: 124},
	{Name: "emissiveRotation", Offset: 128},
	{Name: "metallicFactor", Offset: 132}, {Name: "roughnessFactor", Offset: 136},
	{Name: "normalScale", Offset: 140}, {Name: "occlusionStrength", Offset: 144},
	{Name: "alphaCutoff", Offset: 148}, {Name: "uvSets", Offset: 152},
	{Name: "fade", Offset: 160}, {Name: "a", Offset: 176}, {Name: "b", Offset: 192},
	{Name: "c", Offset: 208}, {Name: "d", Offset: 224},
}

// testUniformSize is testUniforms' span, which fits gfx's 256-byte cap.
const testUniformSize = 240

// sceneResources is what reflection reports for scene's bindings. The real
// source is reflected in the gogpu package; here it stands in so that scene's
// ranges reach the backend and can be read back by name. The bundled module
// is narrowed to the names its variant's source declares, and a custom
// material's module declares group 0 and the material record, which is what a
// shader written against scene's contract does.
var sceneResources = []gfx.ShaderResource{
	{Name: "sceneFrame", Kind: gfx.ResourceStorageBuffer, Group: 0, Binding: 0},
	{Name: "sceneInstances", Kind: gfx.ResourceStorageBuffer, Group: 0, Binding: 1},
	{Name: "sceneAnim", Kind: gfx.ResourceStorageBuffer, Group: 0, Binding: 2},
	{Name: "sceneMeshes", Kind: gfx.ResourceStorageBuffer, Group: 0, Binding: 3},
	{Name: "baseColorTexture", Group: 1, Binding: 1},
	{Name: "baseColorSampler", Kind: gfx.ResourceSampler, Group: 1, Binding: 2},
	{Name: "metallicRoughnessTexture", Group: 1, Binding: 3},
	{Name: "metallicRoughnessSampler", Kind: gfx.ResourceSampler, Group: 1, Binding: 4},
	{Name: "normalTexture", Group: 1, Binding: 5},
	{Name: "normalSampler", Kind: gfx.ResourceSampler, Group: 1, Binding: 6},
	{Name: "occlusionTexture", Group: 1, Binding: 7},
	{Name: "occlusionSampler", Kind: gfx.ResourceSampler, Group: 1, Binding: 8},
	{Name: "emissiveTexture", Group: 1, Binding: 9},
	{Name: "emissiveSampler", Kind: gfx.ResourceSampler, Group: 1, Binding: 10},
	{Name: "scenePoses", Kind: gfx.ResourceStorageBuffer, Group: 2, Binding: 0},
	{Name: "sceneSkinJoints", Kind: gfx.ResourceStorageBuffer, Group: 2, Binding: 1},
	{Name: "sceneMorphDeltas", Kind: gfx.ResourceStorageBuffer, Group: 2, Binding: 2},
}

// customResources are the bindings a custom material's module declares.
var customResources = map[string]bool{
	"sceneFrame": true, "sceneInstances": true, "sceneAnim": true, "sceneMeshes": true,
}

// layoutOf is the stand-in reflection for one module. The bundled source
// names every binding it declares, so it is narrowed to those - the variants
// differ in exactly which of group 2 they declare, and declaring one a draw
// does not fill drops the draw. Any other source is a custom material's.
func layoutOf(code string) gfx.ShaderLayout {
	layout := gfx.ShaderLayout{
		Resources: []gfx.ShaderResource{{Name: "params", Kind: gfx.ResourceUniformBuffer, Group: 1, Binding: 0, Size: testUniformSize, Members: testUniforms}},
	}
	bundled := strings.Contains(code, "sceneInstances")
	for _, resource := range sceneResources {
		if (bundled && strings.Contains(code, resource.Name)) || (!bundled && customResources[resource.Name]) {
			layout.Resources = append(layout.Resources, resource)
		}
	}
	return layout
}

func (b *testBackend) Ready() bool { return true }

func (b *testBackend) NewTexture() gfx.TextureID {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextTexture++
	return b.nextTexture
}

func (b *testBackend) NewBuffer() gfx.BufferID {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextBuffer++
	return b.nextBuffer
}

func (b *testBackend) next() uint32 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	return b.nextID
}

func (b *testBackend) NewSampler(gfx.SamplerDesc) (gfx.SamplerID, error) {
	return gfx.SamplerID(b.next()), nil
}

func (b *testBackend) FreeSampler(gfx.SamplerID) {}

func (b *testBackend) FreeShader(gfx.ShaderID)     {}
func (b *testBackend) ReserveShader() gfx.ShaderID { return gfx.ShaderID(b.next()) }

// CreateShader keeps a reserved shader's source, which is how a test tells the
// shader a draw's set was built on.
func (b *testBackend) CreateShader(id gfx.ShaderID, desc gfx.ShaderDesc) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.shaders == nil {
		b.shaders = map[gfx.ShaderID]string{}
	}
	b.shaders[id] = string(desc.Code)
	return nil
}

// ReflectShader is the reflection a compile goes through, which is how model
// builds its draw params at load: layoutOf's narrowing, with the material
// block as the shader declares it - scenePbrMaterial, whole, at its real size.
// model's own tests hold that size to the WGSL through naga.
func (b *testBackend) ReflectShader(code []byte) (gfx.ShaderLayout, error) {
	layout := layoutOf(string(code))
	layout.Resources[0] = gfx.ShaderResource{
		Name: "scenePbrMaterial", Kind: gfx.ResourceUniformBuffer, Group: 1, Binding: 0, Size: scenePbrMaterialSize,
	}
	layout.Resources = append(layout.Resources, testBindings...)
	return layout, nil
}

// testBindings are the uniforms of their own every reflected shader here
// declares, in group 3, for the names the tests bind: whole bindings, as a
// shader declaring a per-draw number of its own does.
var testBindings = []gfx.ShaderResource{
	{Name: "fade", Kind: gfx.ResourceUniformBuffer, Group: 3, Binding: 0, Size: 4},
	{Name: "a", Kind: gfx.ResourceUniformBuffer, Group: 3, Binding: 1, Size: 4},
	{Name: "b", Kind: gfx.ResourceUniformBuffer, Group: 3, Binding: 2, Size: 4},
	{Name: "c", Kind: gfx.ResourceUniformBuffer, Group: 3, Binding: 3, Size: 4},
	{Name: "d", Kind: gfx.ResourceUniformBuffer, Group: 3, Binding: 4, Size: 4},
	{Name: "bias", Kind: gfx.ResourceUniformBuffer, Group: 3, Binding: 5, Size: 4},
	{Name: "tint", Kind: gfx.ResourceUniformBuffer, Group: 3, Binding: 6, Size: 16},
}

// scenePbrMaterialSize is ScenePbrMaterial's size: seven vec4s and eleven
// scalars, rounded up to the struct's 16-byte alignment.
const scenePbrMaterialSize = 160

func (b *testBackend) NewPipeline(desc gfx.PipelineDesc) (gfx.PipelineID, error) {
	id := gfx.PipelineID(b.next())
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pipelines == nil {
		b.pipelines = map[gfx.PipelineID]gfx.PipelineDesc{}
	}
	b.pipelines[id] = desc
	return id, nil
}

func (b *testBackend) FreePipeline(gfx.PipelineID) {}

func (b *testBackend) ScreenFramebuffer() (gfx.TextureViewID, int, int) {
	return gfx.TextureViewID(1), 1600, 1200
}

// TextureFormat answers for no texture: this double keeps no descriptors, and
// gfx falls back to the frame buffer's format for a target it cannot place.
func (b *testBackend) TextureFormat(gfx.TextureID) (gfx.TextureFormat, bool) {
	return 0, false
}

func (b *testBackend) Limits() gfx.PipelineLimits { return gfx.DefaultLimits() }

func (b *testBackend) TakeCapture() (gfx.Capture, bool) { return gfx.Capture{}, false }

// Execute replays one frame and keeps it as the last one.
func (b *testBackend) Execute(queue *gfx.Queue) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.building = nil
	queue.ReplayBakes(b)
	queue.ReplayPasses(b)
	queue.ReplayReleases(b)
	b.last = b.building
}

func (b *testBackend) BeginPass(desc gfx.PassDescr) gfx.RenderPass {
	b.building = append(b.building, recordedPass{desc: desc})
	b.current = &b.building[len(b.building)-1]
	b.state = recordedDraw{buffers: map[string]bufferRange{}, params: map[[2]int][]byte{}}
	return b
}

func (b *testBackend) EndPass(gfx.RenderPass)                     {}
func (b *testBackend) Present()                                   {}
func (b *testBackend) Capture(gfx.CaptureDesc)                    {}
func (b *testBackend) TransitionTextures([]gfx.TextureTransition) {}

func (b *testBackend) BakeBuffer(id gfx.BufferID, _ gfx.BufferKind, _ int, data []byte) {
	if b.baked == nil {
		b.baked = map[gfx.BufferID][]byte{}
	}
	b.baked[id] = append([]byte(nil), data...)
}

func (b *testBackend) BakeTexture(gfx.TextureID, int, int, gfx.TextureFormat, []byte, bool) {}
func (b *testBackend) AllocateTexture(gfx.TextureID, gfx.TextureDesc)                       {}
func (b *testBackend) UpdateTexture(gfx.TextureID, int, gfx.Region, []byte)                 {}

func (b *testBackend) SetPipeline(id gfx.PipelineID) { b.state.pipeline = id }

func (b *testBackend) BakeUniforms(arena []byte) { b.uniforms = arena }

func (b *testBackend) SetUniformBlock(group, binding, offset, size int) {
	b.state.params[[2]int{group, binding}] = append([]byte(nil), b.uniforms[offset:offset+size]...)
}

func (b *testBackend) SetTexture(gfx.TextureID, int, int) {}
func (b *testBackend) SetSampler(gfx.SamplerID, int, int) {}

func (b *testBackend) SetVertexBuffer(id gfx.BufferID, _ int) { b.state.vertices = id }

func (b *testBackend) SetIndexBuffer(id gfx.BufferID, _ int, _ gfx.IndexWidth) { b.state.index = id }

func (b *testBackend) SetBuffer(group, binding int, buffer gfx.BufferID, offset, size int) {
	for _, resource := range sceneResources {
		if resource.Group == group && resource.Binding == binding {
			b.state.buffers[resource.Name] = bufferRange{buffer: buffer, offset: offset, size: size}
		}
	}
}

func (b *testBackend) Draw(first, count, instances, firstInstance int, indexed bool) {
	draw := b.state
	draw.buffers = make(map[string]bufferRange, len(b.state.buffers))
	for name, bound := range b.state.buffers {
		draw.buffers[name] = bound
	}
	draw.params = make(map[[2]int][]byte, len(b.state.params))
	for at, block := range b.state.params {
		draw.params[at] = block
	}
	draw.first, draw.count, draw.instances, draw.firstInstance, draw.indexed =
		first, count, instances, firstInstance, indexed
	if !indexed {
		draw.index = 0
	}
	b.current.draws = append(b.current.draws, draw)
}

func (b *testBackend) ReleaseBuffer(gfx.BufferID)   {}
func (b *testBackend) ReleaseTexture(gfx.TextureID) {}

// The record layouts the bundled shader reads. They mirror the records model packs -
// model.Instance, model.FrameBlock, model.Light and the
// sceneAnim block - field for field, and are spelled out here rather than read
// from model because the bytes are the contract a shader reads: a decode
// through the writer's own types would agree with itself whatever they are.
const (
	instanceRecordSize = 64
	lightRecordSize    = 48
	frameLightsAt      = 304
	frameLightCountAt  = 288
	animHeaderSize     = 32
	playRecordSize     = 16
	// sceneNoAnim is the animOffset of an instance that animates nothing.
	sceneNoAnim = ^uint32(0)
)

// drawnInstance is one instance a frame drew: the pass it was in, the
// pipeline and parameters it drew with, and the records packed for it.
type drawnInstance struct {
	pass     gfx.PassDescr
	shader   string
	state    gfx.DrawState
	params   map[[2]int][]byte
	vertices gfx.BufferID
	count    int
	indexed  bool
	// world is the instance record's world matrix, read back from its three
	// packed rows, and animOffset the vec4 its sceneAnim block starts at.
	world      m.Mat4
	animOffset uint32
	// buffers is every storage binding the draw's shader declared and gfx
	// bound, by name: which of group 2 is here is the variant it drew with.
	buffers map[string]bufferRange
	// anim is the instance's sceneAnim block when it has one.
	anim []byte
	// frame is the pass's sceneFrame block.
	frame []byte
}

// position is where the instance stands: the translation of its world matrix.
func (d drawnInstance) position() m.Vec3 {
	return m.Vec3{X: d.world[12], Y: d.world[13], Z: d.world[14]}
}

// param reads what a draw bound under one name, the way the shader would: a
// member of the scenePbrMaterial block at its offset, or one of testBindings
// whole, padded out to a vec4. A binding the draw did not bind reads as zero.
func (d drawnInstance) param(name string) m.Vec4 {
	for _, member := range testUniforms {
		if block := d.params[[2]int{1, 0}]; member.Name == name && len(block) > member.Offset {
			var padded [16]byte
			copy(padded[:], block[member.Offset:])
			return vec4At(padded[:], 0)
		}
	}
	for _, binding := range testBindings {
		if binding.Name == name {
			var padded [16]byte
			copy(padded[:], d.params[[2]int{binding.Group, binding.Binding}])
			return vec4At(padded[:], 0)
		}
	}
	return m.Vec4{}
}

// passes returns the last frame's passes.
func (b *testBackend) passes() []recordedPass {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.last
}

// instances flattens the last frame into every instance it drew, in the order
// the backend received them.
func (b *testBackend) instances() []drawnInstance {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []drawnInstance
	for _, pass := range b.last {
		for _, draw := range pass.draws {
			pipeline := b.pipelines[draw.pipeline]
			base := drawnInstance{
				pass: pass.desc, shader: b.shaders[pipeline.Shader], state: pipeline.State,
				params: draw.params, vertices: draw.vertices, count: draw.count, indexed: draw.indexed,
				buffers: draw.buffers,
				frame:   b.rangeBytes(draw.buffers["sceneFrame"]),
			}
			records := b.rangeBytes(draw.buffers["sceneInstances"])
			anims := b.rangeBytes(draw.buffers["sceneAnim"])
			for i := range draw.instances {
				at := (draw.firstInstance + i) * instanceRecordSize
				if at+instanceRecordSize > len(records) {
					continue
				}
				instance := base
				record := records[at : at+instanceRecordSize]
				for row := range 3 {
					v := vec4At(record, 16*row)
					instance.world[row], instance.world[row+4], instance.world[row+8], instance.world[row+12] = v.X, v.Y, v.Z, v.W
				}
				instance.world[15] = 1
				instance.animOffset = binary.LittleEndian.Uint32(record[48:])
				if instance.animOffset != sceneNoAnim && int(instance.animOffset)*16 < len(anims) {
					instance.anim = anims[instance.animOffset*16:]
				}
				out = append(out, instance)
			}
		}
	}
	return out
}

// rangeBytes is the bytes one binding covered, or nil for no binding.
func (b *testBackend) rangeBytes(bound bufferRange) []byte {
	data, ok := b.baked[bound.buffer]
	if !ok || bound.offset > len(data) {
		return nil
	}
	end := len(data)
	if bound.size > 0 && bound.offset+bound.size <= len(data) {
		end = bound.offset + bound.size
	}
	return data[bound.offset:end]
}

func floatAt(data []byte, at int) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(data[at:]))
}

func vec4At(data []byte, at int) m.Vec4 {
	return m.Vec4{X: floatAt(data, at), Y: floatAt(data, at+4), Z: floatAt(data, at+8), W: floatAt(data, at+12)}
}

func mat4At(data []byte, at int) m.Mat4 {
	var out m.Mat4
	for i := range out {
		out[i] = floatAt(data, at+4*i)
	}
	return out
}
