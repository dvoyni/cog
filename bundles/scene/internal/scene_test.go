package internal

import (
	"io/fs"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/model/modelplugin"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// testBackend is a Backend that mints ids and records the passes it was asked
// to encode. Everything scene decides is decided before this is reached, which
// is what makes the whole ticket assertable with no GPU.
type testBackend struct {
	// shaderLayouts is each compiled shader's narrowed stand-in layout, kept
	// per id because the scene variants declare different bindings.
	shaderLayouts map[gfx.ShaderID]gfx.ShaderLayout
	nextTexture   gfx.TextureID
	nextBuffer    gfx.BufferID
	nextID        uint32
	passes        []gfx.PassDesc
	presents      int
	// draws, bindings and bakes are what the frame actually asked the GPU to
	// do, which is where the pass-relative instance slices and the one upload
	// per arena become assertable.
	draws []drawCall
	// indexBinds is the width each of the frame's index buffers was bound at.
	// A draw call carries a count and not a format, so this is the only place
	// the width scene derived is observable from outside the package.
	indexBinds []gfx.IndexWidth
	bindings   []bufferBinding
	textures   []textureBinding
	samplers   []samplerBinding
	bakes      int
	// baked keeps every uploaded buffer's bytes, so a test can read back the
	// records scene packed rather than only their offsets.
	baked map[gfx.BufferID][]byte
	// bakedTextures and releasedTextures are what the frame asked the GPU to do
	// with durable textures. They are the only place a test can see how many
	// times one image reached the GPU, which is what makes the texture cache's
	// dedup and its unload assertable with no GPU.
	bakedTextures    []textureBake
	releasedTextures []gfx.TextureID
}

// textureBake is one durable texture upload, kept whole so a test can read the
// placeholder's own pixels back rather than only its id.
type textureBake struct {
	id            gfx.TextureID
	width, height int
	format        gfx.TextureFormat
	mipmaps       bool
	pixels        []byte
}

// drawCall is one recorded draw, so a test can assert the arguments that reach
// the backend rather than an op encoding.
type drawCall struct {
	first, count, instances, firstInstance int
	indexed                                bool
}

// bufferBinding is one storage range bound into a bind group.
type bufferBinding struct {
	group, binding int
	buffer         gfx.BufferID
	offset, size   int
}

// textureBinding and samplerBinding are the material-group resources one draw
// bound, which is what makes "all five slots, always" assertable with no GPU.
type textureBinding struct {
	group, binding int
	texture        gfx.TextureID
}

type samplerBinding struct {
	group, binding int
	sampler        gfx.SamplerID
}

// testShaderLayout is what reflection reports for the bundled scene shader. The
// real source is reflected and asserted in the gogpu package, the only tree
// with a WGSL front end; here it stands in so that scene's bindings reach the
// backend and can be read back by name.
var testShaderLayout = gfx.ShaderLayout{Resources: []gfx.ShaderResource{
	{Name: "sceneFrame", StorageBuffer: true, Group: 0, Binding: 0},
	{Name: "sceneInstances", StorageBuffer: true, Group: 0, Binding: 1},
	{Name: "sceneAnim", StorageBuffer: true, Group: 0, Binding: 2},
	{Name: "sceneMeshes", StorageBuffer: true, Group: 0, Binding: 3},
	{Name: "scenePbrMaterial", StorageBuffer: true, Group: 1, Binding: 0},
	{Name: "baseColorTexture", Group: 1, Binding: 1},
	{Name: "baseColorSampler", Sampler: true, Group: 1, Binding: 2},
	{Name: "metallicRoughnessTexture", Group: 1, Binding: 3},
	{Name: "metallicRoughnessSampler", Sampler: true, Group: 1, Binding: 4},
	{Name: "normalTexture", Group: 1, Binding: 5},
	{Name: "normalSampler", Sampler: true, Group: 1, Binding: 6},
	{Name: "occlusionTexture", Group: 1, Binding: 7},
	{Name: "occlusionSampler", Sampler: true, Group: 1, Binding: 8},
	{Name: "emissiveTexture", Group: 1, Binding: 9},
	{Name: "emissiveSampler", Sampler: true, Group: 1, Binding: 10},
	{Name: "scenePoses", StorageBuffer: true, Group: 2, Binding: 0},
	{Name: "sceneSkinJoints", StorageBuffer: true, Group: 2, Binding: 1},
	{Name: "sceneMorphDeltas", StorageBuffer: true, Group: 2, Binding: 2},
}}

// texturesBoundTo reports the texture bound to one reflected binding on each
// draw, in the order the frame bound them. A slot nobody bound reports nothing,
// which is the failure that takes the whole frame's command buffer down.
// bakeOf reports the durable upload one texture id received, which is how a
// test reads a placeholder's own pixels back rather than only its id.
func (b *testBackend) bakeOf(id gfx.TextureID) (textureBake, bool) {
	for i := len(b.bakedTextures) - 1; i >= 0; i-- {
		if b.bakedTextures[i].id == id {
			return b.bakedTextures[i], true
		}
	}
	return textureBake{}, false
}

func (b *testBackend) texturesBoundTo(name string) []gfx.TextureID {
	group, binding := bindingOf(name)
	var found []gfx.TextureID
	for _, bound := range b.textures {
		if bound.group == group && bound.binding == binding {
			found = append(found, bound.texture)
		}
	}
	return found
}

// samplersBoundTo reports the sampler bound to one reflected binding per draw.
func (b *testBackend) samplersBoundTo(name string) []gfx.SamplerID {
	group, binding := bindingOf(name)
	var found []gfx.SamplerID
	for _, bound := range b.samplers {
		if bound.group == group && bound.binding == binding {
			found = append(found, bound.sampler)
		}
	}
	return found
}

func bindingOf(name string) (group, binding int) {
	for _, resource := range testShaderLayout.Resources {
		if resource.Name == name {
			return resource.Group, resource.Binding
		}
	}
	return -1, -1
}

// buffersBoundTo reports every range bound to one reflected binding, in the
// order the frame bound them.
func (b *testBackend) buffersBoundTo(name string) []bufferBinding {
	group, binding := -1, -1
	for _, resource := range testShaderLayout.Resources {
		if resource.Name == name {
			group, binding = resource.Group, resource.Binding
		}
	}
	var found []bufferBinding
	for _, bound := range b.bindings {
		if bound.group == group && bound.binding == binding {
			found = append(found, bound)
		}
	}
	return found
}

func (b *testBackend) Ready() bool { return true }

func (b *testBackend) NewTexture() gfx.TextureID { b.nextTexture++; return b.nextTexture }
func (b *testBackend) NewBuffer() gfx.BufferID   { b.nextBuffer++; return b.nextBuffer }
func (b *testBackend) NewSampler(gfx.SamplerDesc) (gfx.SamplerID, error) {
	b.nextID++
	return gfx.SamplerID(b.nextID), nil
}
func (b *testBackend) FreeSampler(gfx.SamplerID) {}
func (b *testBackend) NewShader(desc gfx.ShaderDesc) (gfx.ShaderID, error) {
	b.nextID++
	id := gfx.ShaderID(b.nextID)
	if b.shaderLayouts == nil {
		b.shaderLayouts = map[gfx.ShaderID]gfx.ShaderLayout{}
	}
	b.shaderLayouts[id] = layoutOf(desc)
	return id, nil
}
func (b *testBackend) FreeShader(gfx.ShaderID) {}
func (b *testBackend) ShaderLayout(id gfx.ShaderID) gfx.ShaderLayout {
	if layout, ok := b.shaderLayouts[id]; ok {
		return layout
	}
	return testShaderLayout
}

// layoutOf narrows the stand-in layout to the bindings this variant's flattened
// source actually declares, which is what reflection would report. It matters
// because the variants differ in exactly that: scene.wgsl declares the skinning
// and morph bindings only under SCENE_SKIN and SCENE_MORPH, and binds group 2
// only on the draws whose variant has it. A stand-in that declared all three on
// every shader would have every unskinned draw dropped for an unfilled storage
// binding, which is what gfx now reports rather than swallows.
func layoutOf(desc gfx.ShaderDesc) gfx.ShaderLayout {
	if len(desc.Code) == 0 {
		return testShaderLayout
	}
	code := string(desc.Code)
	layout := testShaderLayout
	layout.Resources = nil
	for _, resource := range testShaderLayout.Resources {
		if strings.Contains(code, resource.Name) {
			layout.Resources = append(layout.Resources, resource)
		}
	}
	return layout
}
func (b *testBackend) NewPipeline(gfx.PipelineDesc) (gfx.PipelineID, error) {
	b.nextID++
	return gfx.PipelineID(b.nextID), nil
}
func (b *testBackend) FreePipeline(gfx.PipelineID) {}
func (b *testBackend) ScreenFramebuffer() (gfx.TextureViewID, int, int) {
	return 1, 1600, 1200
}
func (b *testBackend) Limits() gfx.Limits { return gfx.DefaultLimits() }

// TextureFormat answers for no texture: this double keeps no descriptors, and
// gfx falls back to the frame buffer's format for a target it cannot place -
// which is what every pipeline in this fixture was keyed to anyway.
func (b *testBackend) TextureFormat(gfx.TextureID) (gfx.TextureFormat, bool) {
	return 0, false
}

func (b *testBackend) TextureView(gfx.TextureID, int, int) gfx.TextureViewID {
	b.nextID++
	return gfx.TextureViewID(b.nextID)
}
func (b *testBackend) Execute(queue *gfx.Queue) {
	queue.ReplayBakes(b)
	queue.ReplayPasses(b)
	queue.ReplayReleases(b)
}
func (b *testBackend) BeginPass(desc gfx.PassDesc) gfx.RenderPass {
	b.passes = append(b.passes, desc)
	return b
}
func (b *testBackend) EndPass(gfx.RenderPass) {}
func (b *testBackend) Present()               { b.presents++ }

// Capture is the readback seam; nothing here reads a frame back.
func (b *testBackend) Capture(gfx.CaptureDesc) {}

func (b *testBackend) TakeCapture() (gfx.Capture, bool) { return gfx.Capture{}, false }

func (b *testBackend) TransitionTextures([]gfx.TextureTransition) {}

// BakeBuffer keeps the bytes as well as counting the upload, because the
// records scene packs are only readable here: everything downstream of the
// arena is an offset and a size, and a flag written into the wrong instance is
// exactly the failure that reads as a plausible wrong picture.
func (b *testBackend) BakeBuffer(id gfx.BufferID, _ gfx.BufferKind, _ int, data []byte) {
	b.bakes++
	if b.baked == nil {
		b.baked = map[gfx.BufferID][]byte{}
	}
	b.baked[id] = append([]byte(nil), data...)
}
func (b *testBackend) BakeTexture(
	id gfx.TextureID, width, height int, format gfx.TextureFormat, pixels []byte, mipmaps bool,
) {
	b.bakedTextures = append(b.bakedTextures, textureBake{
		id: id, width: width, height: height, format: format, mipmaps: mipmaps,
		pixels: append([]byte(nil), pixels...),
	})
}
func (b *testBackend) AllocateTexture(gfx.TextureID, gfx.TextureDesc)       {}
func (b *testBackend) UpdateTexture(gfx.TextureID, int, gfx.Region, []byte) {}
func (b *testBackend) SetPipeline(gfx.PipelineID)                           {}
func (b *testBackend) SetParams([]byte)                                     {}
func (b *testBackend) SetTexture(texture gfx.TextureID, group, binding int) {
	b.textures = append(b.textures, textureBinding{group: group, binding: binding, texture: texture})
}

func (b *testBackend) SetSampler(sampler gfx.SamplerID, group, binding int) {
	b.samplers = append(b.samplers, samplerBinding{group: group, binding: binding, sampler: sampler})
}
func (b *testBackend) SetVertexBuffer(gfx.BufferID, int) {}
func (b *testBackend) SetIndexBuffer(_ gfx.BufferID, _ int, width gfx.IndexWidth) {
	b.indexBinds = append(b.indexBinds, width)
}
func (b *testBackend) SetBuffer(group, binding int, buffer gfx.BufferID, offset, size int) {
	b.bindings = append(b.bindings, bufferBinding{
		group: group, binding: binding, buffer: buffer, offset: offset, size: size,
	})
}

func (b *testBackend) Draw(first, count, instances, firstInstance int, indexed bool) {
	b.draws = append(b.draws, drawCall{
		first: first, count: count, instances: instances, firstInstance: firstInstance, indexed: indexed,
	})
}
func (b *testBackend) ReleaseBuffer(gfx.BufferID) {}
func (b *testBackend) ReleaseTexture(id gfx.TextureID) {
	b.releasedTextures = append(b.releasedTextures, id)
}

// recordPlugin is the gameplay side of the harness: a separate plugin that
// locks scene's OpQueue, exactly as a real recorder does. It holds the gfx
// queue as well, ordered ahead of scene's flush, so a recorder that allocates a
// temporary target allocates it in the frame the pass using it is emitted into.
type recordPlugin struct {
	record func(*scene.OpQueue, *gfx.OpQueue)
}
type recordHandler kernel.Subscription[app.UpdateEvent]

// inspectCmd runs a callback inside a handler holding scene's OpQueue, so a
// test reads Ops and Passes the way a real caller would.
type inspectCmd kernel.Command[inspectRequest, inspectResponse]
type inspectRequest struct{ run func(*scene.OpQueue) }
type inspectResponse struct{}

// lookupProbeCmd runs a callback with a valid scoped facade, either half.
type lookupProbeCmd kernel.Command[lookupProbeRequest, lookupProbeResponse]
type lookupProbeRequest struct {
	run func(model.LookupAccess)
	// device is the loading half. Preload, State and every query moved onto it
	// when the load moved inside the call that asks for it, so a test that
	// drives one asks for this facade rather than the other.
	device func(model.LookupDeviceAccess)
	// files is the separate probe readFile needs. Neither facade hands its
	// filesystem back, so a test that wants to read a mounted file asks for one
	// here.
	files func(storage.FileSystem)
	// lookup hands over the resource itself, which is how a test asserts on
	// state no facade exposes.
	lookup func(*model.Lookup)
	// model hands over the resource together with what it takes to reach one
	// loaded model, which is how a test asserts that a draw record points at
	// the cache's own value rather than at a copy of it. Nothing outside this
	// package can ask that question, and it is the whole of "binds the file's
	// records directly".
	model func(*model.Lookup, kernel.Kernel, fs.FS, *gfx.ResourceQueue)
}
type lookupProbeResponse struct{}

func (p recordPlugin) Name() kernel.PluginName { return "scene-test-recorder" }
func (p recordPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{scene.Name, storage.Name}
}

func (p recordPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[recordHandler](func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
		var queue kernel.Write[*scene.OpQueue]
		var gfxQueue kernel.Write[*gfx.OpQueue]
		return func(access kernel.ResourceAccess) {
				queue = access.GetWrite[*scene.OpQueue]()
				gfxQueue = access.GetWrite[*gfx.OpQueue]()
			}, func(kernel.Kernel, app.UpdateEvent) {
				p.record(queue.Get(), gfxQueue.Get())
			}
	}).Before[scene.FlushOnUpdate]()
	registrar.HandleCommand[inspectCmd](inspectCmdImpl)
	registrar.HandleCommand[lookupProbeCmd](lookupProbeCmdImpl)
	return nil
}

func inspectCmdImpl() (kernel.Lock, kernel.Execute[inspectRequest, inspectResponse]) {
	var queue kernel.Write[*scene.OpQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*scene.OpQueue]()
		}, func(_ kernel.Kernel, req inspectRequest) inspectResponse {
			req.run(queue.Get())
			return inspectResponse{}
		}
}

func lookupProbeCmdImpl() (kernel.Lock, kernel.Execute[lookupProbeRequest, lookupProbeResponse]) {
	var lookup kernel.Write[*model.Lookup]
	var filesystem kernel.Read[storage.FileSystem]
	var resources kernel.Write[*gfx.ResourceQueue]
	return func(access kernel.ResourceAccess) {
			lookup = access.GetWrite[*model.Lookup]()
			filesystem = access.GetRead[storage.FileSystem]()
			resources = access.GetWrite[*gfx.ResourceQueue]()
		}, func(k kernel.Kernel, req lookupProbeRequest) lookupProbeResponse {
			if req.files != nil {
				req.files(filesystem.Get())
			}
			if req.run != nil {
				req.run(model.NewLookupAccess(k, lookup.Get()))
			}
			if req.device != nil {
				req.device(model.NewLookupDeviceAccess(
					k, lookup.Get(), fs.FS(filesystem.Get()), resources.Get()))
			}
			if req.lookup != nil {
				req.lookup(lookup.Get())
			}
			if req.model != nil {
				req.model(lookup.Get(), k, fs.FS(filesystem.Get()), resources.Get())
			}
			return lookupProbeResponse{}
		}
}

// errorSink collects reports under a mutex. A model load reports from its own
// goroutine rather than from the flush the test drives, which is the whole
// point of "an error can outlive the draw call that caused it" - so the sink
// that reads them has to be safe to read from a different one.
type errorSink struct {
	mu   sync.Mutex
	errs []error
}

func (s *errorSink) add(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, err)
}

func (s *errorSink) snapshot() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]error(nil), s.errs...)
}

type harness struct {
	kernel   kernel.Executioner
	backend  *testBackend
	reported *[]error
	sink     *errorSink
}

// errors reports what the engine has reported so far, safely to read while a
// load goroutine may still be running.
func (h *harness) errors() []error { return h.sink.snapshot() }

func newHarness(t testing.TB, record func(*scene.OpQueue)) *harness {
	t.Helper()
	var reported []error
	return newHarnessWithErrors(t, record, &reported)
}

// newHarnessWithGfx is newHarness for a recorder that also allocates from the
// gfx queue, the way an app rendering a camera into a temporary target does.
func newHarnessWithGfx(t testing.TB, record func(*scene.OpQueue, *gfx.OpQueue)) *harness {
	t.Helper()
	var reported []error
	return newHarnessRecording(t, record, &reported)
}

func newHarnessWithErrors(t testing.TB, record func(*scene.OpQueue), reported *[]error) *harness {
	t.Helper()
	return newHarnessRecording(t, func(q *scene.OpQueue, _ *gfx.OpQueue) { record(q) }, reported)
}

// newHarnessWithFiles is newHarness over a filesystem holding the given
// files, which is how a model test hands scene a glTF file to load without
// this package growing a testdata directory.
func newHarnessWithFiles(t testing.TB, files fstest.MapFS, record func(*scene.OpQueue)) *harness {
	t.Helper()
	return newHarnessWithFS(t, files, record)
}

// newHarnessWithFS is newHarnessWithFiles over any filesystem, so a test that
// wants to watch the reads themselves can wrap the map in one of its own.
func newHarnessWithFS(t testing.TB, files fs.FS, record func(*scene.OpQueue)) *harness {
	t.Helper()
	var reported []error
	return newHarnessOver(t, files, func(q *scene.OpQueue, _ *gfx.OpQueue) { record(q) }, &reported)
}

func newHarnessRecording(t testing.TB, record func(*scene.OpQueue, *gfx.OpQueue), reported *[]error) *harness {
	return newHarnessOver(t, fstest.MapFS{}, record, reported)
}

func newHarnessOver(
	t testing.TB, files fs.FS,
	record func(*scene.OpQueue, *gfx.OpQueue), reported *[]error,
) *harness {
	t.Helper()
	backend := &testBackend{}
	sink := &errorSink{}
	configs := map[kernel.PluginName]any{
		model.Name: model.Config{},
	}
	engine := kernel.New(configs).
		Handler(func(err error) error { sink.add(err); *reported = append(*reported, err); return nil }).
		WithPlugins(storageplugin.New(), permanentAdapter{}, readMountAdapter{storage.ReadMount{Id: "test", Priority: 10, FS: files}}, appplugin.New(), mainLoopAdapter{}, gfxplugin.New(), backendAdapter{backend}, modelplugin.New(), New(), recordPlugin{record: record})
	go engine.Run()
	<-engine.Ready()
	k := engine.Executioner()
	k.PublishEvent(app.InitEvent{}).Wait()
	k.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{
		Width: 800, Height: 600, FramebufferWidth: 1600, FramebufferHeight: 1200,
	})
	return &harness{kernel: k, backend: backend, reported: reported, sink: sink}
}

func (h *harness) frame() {
	h.kernel.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()
	h.kernel.PublishEvent(app.RenderEvent{}).Wait()
}

// frameUntil runs frames until ready. A load is synchronous now, so a model
// draw is resident in the frame that named it; what still takes frames is
// everything that lands at the frame boundary - the deferred bakes and the
// buffer releases - and the very first frames, before the viewport and the
// backend are up.
func (h *harness) frameUntil(t testing.TB, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.frame()
		if ready() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// inspect runs fn inside a handler holding scene's OpQueue write lock.
func (h *harness) inspect(fn func(*scene.OpQueue)) {
	h.kernel.ExecuteCommand[inspectCmd](inspectRequest{run: fn})
}

// passes reads the flush result back out of scene's queue.
func (h *harness) passes() []scene.PassView {
	var out []scene.PassView
	h.inspect(func(q *scene.OpQueue) { out = q.Passes(nil) })
	return out
}

func (h *harness) ops() []scene.Op {
	var out []scene.Op
	h.inspect(func(q *scene.OpQueue) { out = q.Ops(nil) })
	return out
}

// readFile reads one path out of the engine's storage, which is how a test
// checks that a plugin's builtin mount is in place.
func (h *harness) readFile(t testing.TB, path string) []byte {
	t.Helper()
	var data []byte
	var err error
	h.kernel.ExecuteCommand[lookupProbeCmd](lookupProbeRequest{files: func(files storage.FileSystem) {
		data, err = fs.ReadFile(files, path)
	}})
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return data
}

// bundledMaterials wraps the bundled PBR's four forward materials as the scene
// materials the flush interns them as.
func bundledMaterials(defaults model.PbrDefaults) [model.VariantCount]scene.Material {
	var wrapped [model.VariantCount]scene.Material
	for variant, descr := range model.BundledPbr(defaults) {
		wrapped[variant] = scene.Material{{Tag: scene.TagForward, Descr: descr}}
	}
	return wrapped
}

// lookupDefaults reads the two default textures the first frame baked, off the
// bundled PBR they are bound into. It is called after a frame, so the bake it
// hands EnsureBundled is never reached.
func lookupDefaults(lookup *model.Lookup) model.PbrDefaults {
	bundled := lookup.EnsureBundled(func(int, int, gfx.TextureFormat, []byte) gfx.TextureDescr {
		panic("the defaults were not baked by the first frame")
	})
	var defaults model.PbrDefaults
	for _, param := range bundled[model.VariantStatic].Params() {
		texture, ok := param.TextureValue()
		switch {
		case !ok:
		case param.Name() == model.PbrSlots[0].Texture:
			defaults.White = texture
		case param.Name() == model.PbrSlots[model.NormalSlot].Texture:
			defaults.FlatNormal = texture
		}
	}
	return defaults
}

// durableMeshes lists the Lookup's resident meshes in table order. A test that
// releases nothing leaves every slot at its first generation, so the walk asks
// for each id at generation 1 and stops at the first that does not resolve.
func durableMeshes(lookup *model.Lookup) []model.MeshRecord {
	var meshes []model.MeshRecord
	for id := uint32(1); ; id++ {
		mesh, ok := lookup.Mesh(model.NewMeshRef(model.MeshDurable, id, 1))
		if !ok {
			return meshes
		}
		meshes = append(meshes, mesh)
	}
}

// wrapsForward reports whether material is the one-entry forward material the
// flush wraps a model's forward gfx material in, sharing that material's
// parameters rather than a copy of them.
func wrapsForward(material scene.Material, forward gfx.MaterialDescr) bool {
	if len(material) != 1 || material[0].Tag != scene.TagForward {
		return false
	}
	got, want := material[0].Descr.Params(), forward.Params()
	return len(got) == len(want) && len(got) > 0 && &got[0] == &want[0] &&
		material[0].Descr.Fingerprint() == forward.Fingerprint()
}
