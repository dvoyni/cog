package internal

import (
	"reflect"
	"slices"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The load System's tests read its scratch, which is what it writes and what
// the recording System buckets Batches by, as a white-box extra. Each also
// checks that the frame still draws what it drew, so the scratch is never the
// only thing a test asserts.

// keysCmd snapshots the load System's scratch: every keyed Entity's handle and
// keys, and how many Entities its last run keyed.
type keysCmd kernel.Command[keysRequest, keysResponse]

type keysRequest struct{}

type keysResponse struct {
	models map[ecs.Entity]modelKeys
	meshes map[ecs.Entity]batchKey
	walked int
}

func keysCmdImpl() (kernel.Lock, kernel.Execute[keysRequest, keysResponse]) {
	var keys kernel.Read[*keyScratch]
	return func(access kernel.ResourceAccess) {
			keys = access.GetRead[*keyScratch]()
		}, func(kernel.Kernel, keysRequest) keysResponse {
			s := keys.Get()
			response := keysResponse{
				models: make(map[ecs.Entity]modelKeys, len(s.models)),
				meshes: make(map[ecs.Entity]batchKey, len(s.meshes)),
				walked: s.walked,
			}
			for e, entry := range s.models {
				entry.keys = slices.Clone(entry.keys)
				response.models[e] = entry
			}
			for e, key := range s.meshes {
				response.meshes[e] = key
			}
			return response
		}
}

// setParamsCmd replaces one Entity's Params, as a game System retinting it
// would.
type setParamsCmd kernel.Command[setParamsRequest, setParamsResponse]

type setParamsRequest struct {
	Entity ecs.Entity
	Params ecsscene.Params
}

type setParamsResponse struct{}

func setParamsCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[setParamsRequest, setParamsResponse]) {
	return ecs.ToExecute[setParamsRequest, setParamsResponse](registrar, func(
		request setParamsRequest, params *ecs.Set[ecsscene.Params],
	) {
		params.UpdateFor(request.Entity, request.Params)
	})
}

func (h *harness) keys(t testing.TB) keysResponse {
	t.Helper()
	return h.kernel.ExecuteCommand[keysCmd](keysRequest{})
}

func (h *harness) setParams(t testing.TB, e ecs.Entity, params ecsscene.Params) {
	t.Helper()
	h.kernel.ExecuteCommand[setParamsCmd](setParamsRequest{Entity: e, Params: params})
}

// tint is a Params holding one base colour, built afresh each call so no two
// Entities share a List's backing.
func tint(c m.Color) *ecsscene.Params {
	return &ecsscene.Params{Values: m.ListOf([]gfx.ParameterDescr{gfx.ColorParam("baseColorFactor", c)})}
}

var (
	red  = m.Color{R: 1, A: 1}
	blue = m.Color{B: 1, A: 1}
)

// keyedModels runs frames until every Entity named has a resident model keyed,
// and returns the snapshot that showed it.
func (h *harness) keyedModels(t testing.TB, entities ...ecs.Entity) keysResponse {
	t.Helper()
	var snapshot keysResponse
	h.frameUntil(t, "every model to be keyed", func() bool {
		snapshot = h.keys(t)
		for _, e := range entities {
			if entry, ok := snapshot.models[e]; !ok || entry.handle == 0 || len(entry.keys) == 0 {
				return false
			}
		}
		return true
	})
	return snapshot
}

// TestASteadyFrameTouchesNoEntityInTheLoadSystem is the per-frame budget: once
// every drawable is keyed, a frame in which nothing changed keys nothing - no
// Entity walked, so nothing resolved and nothing hashed - and the keys it left
// are the ones the change wrote. The frame still draws every crate.
func TestASteadyFrameTouchesNoEntityInTheLoadSystem(t *testing.T) {
	h := newDrawingHarness(t, 256)
	ref := h.bake(t)
	var entities []ecs.Entity
	for i := range 8 {
		entities = append(entities, h.spawn(t, spawnRequest{
			Place: m.At(float32(i)*2, 0, 0), Model: crateModelComponent(), Params: tint(red),
		}))
	}
	mesh := h.spawn(t, spawnRequest{Place: m.At(-2, 0, 0), Mesh: &ecsscene.Mesh{Ref: ref, NeverCull: true}})
	keyed := h.keyedModels(t, entities...)
	if _, ok := keyed.meshes[mesh]; !ok {
		t.Fatal("the Mesh Entity was never keyed")
	}

	for range 3 {
		h.frame(t)
		steady := h.keys(t)
		if steady.walked != 0 {
			t.Errorf("a steady frame keyed %d Entities, want none", steady.walked)
		}
		if !reflect.DeepEqual(steady.models, keyed.models) || !reflect.DeepEqual(steady.meshes, keyed.meshes) {
			t.Errorf("a steady frame changed the keys:\n%v\nwant\n%v", steady, keyed)
		}
	}
	if got := len(where(h.drawn(), ofTriangle)); got != 9 {
		t.Errorf("the steady frame drew %d triangles, want the eight crates and the mesh", got)
	}
	h.noErrors(t)
}

// TestAChangedParamsRewritesOnlyThatEntitysKey is keying on change: retinting
// one crate of three keys that one crate, once, and leaves the other two keys
// as they were. The retinted crate still draws.
func TestAChangedParamsRewritesOnlyThatEntitysKey(t *testing.T) {
	h := newDrawingHarness(t, 256)
	var entities []ecs.Entity
	for i := range 3 {
		entities = append(entities, h.spawn(t, spawnRequest{
			Place: m.At(float32(i)*2, 0, 0), Model: crateModelComponent(), Params: tint(red),
		}))
	}
	before := h.keyedModels(t, entities...)
	h.frame(t)

	retinted := entities[1]
	h.setParams(t, retinted, *tint(blue))
	h.frame(t)
	after := h.keys(t)

	if after.walked != 1 {
		t.Errorf("the frame after one retint keyed %d Entities, want 1", after.walked)
	}
	for _, e := range entities {
		was, now := before.models[e], after.models[e]
		if e == retinted {
			if now.handle != was.handle || len(now.keys) != len(was.keys) {
				t.Fatalf("the retinted crate's model changed: %v, was %v", now, was)
			}
			for j := range now.keys {
				if now.keys[j].params == was.keys[j].params {
					t.Errorf("the retinted crate's primitive %d kept its Params hash", j)
				}
				changed := now.keys[j]
				changed.params = was.keys[j].params
				if changed != was.keys[j] {
					t.Errorf("the retint rewrote more than the Params hash: %v, was %v", now.keys[j], was.keys[j])
				}
			}
			continue
		}
		if !reflect.DeepEqual(now, was) {
			t.Errorf("a crate nobody retinted was rekeyed: %v, was %v", now, was)
		}
	}
	if got := len(where(h.drawn(), at(m.Vec3{X: 2}))); got != 1 {
		t.Errorf("the retinted crate drew %d instances, want 1", got)
	}
	h.noErrors(t)
}

// TestEqualParamsGiveEqualKeys is what makes a tinted crowd batch: two crates
// tinted red in two separate Lists key the same, a blue one keys differently,
// and so do two Meshes. A crate with no Params keys apart from all three, and
// the same as one whose Params are empty, because the two draw the same.
func TestEqualParamsGiveEqualKeys(t *testing.T) {
	h := newDrawingHarness(t, 256)
	ref := h.bake(t)
	redA := h.spawn(t, spawnRequest{Place: m.At(0, 0, 0), Model: crateModelComponent(), Params: tint(red)})
	redB := h.spawn(t, spawnRequest{Place: m.At(2, 0, 0), Model: crateModelComponent(), Params: tint(red)})
	blueC := h.spawn(t, spawnRequest{Place: m.At(4, 0, 0), Model: crateModelComponent(), Params: tint(blue)})
	plain := h.spawn(t, spawnRequest{Place: m.At(6, 0, 0), Model: crateModelComponent()})
	empty := h.spawn(t, spawnRequest{Place: m.At(8, 0, 0), Model: crateModelComponent(), Params: &ecsscene.Params{}})
	meshRed := h.spawn(t, spawnRequest{Place: m.At(-2, 0, 0), Mesh: &ecsscene.Mesh{Ref: ref, NeverCull: true}, Params: tint(red)})
	meshRedToo := h.spawn(t, spawnRequest{Place: m.At(-4, 0, 0), Mesh: &ecsscene.Mesh{Ref: ref, NeverCull: true}, Params: tint(red)})
	meshBlue := h.spawn(t, spawnRequest{Place: m.At(-6, 0, 0), Mesh: &ecsscene.Mesh{Ref: ref, NeverCull: true}, Params: tint(blue)})
	keys := h.keyedModels(t, redA, redB, blueC, plain, empty)

	only := func(e ecs.Entity) batchKey {
		t.Helper()
		entry := keys.models[e]
		if len(entry.keys) != 1 {
			t.Fatalf("the crate %v keyed %d primitives, want its one", e, len(entry.keys))
		}
		return entry.keys[0]
	}
	if only(redA) != only(redB) {
		t.Errorf("two crates with equal Params keyed %v and %v", only(redA), only(redB))
	}
	if only(blueC) == only(redA) {
		t.Errorf("a blue crate keyed the same as a red one: %v", only(blueC))
	}
	if only(plain) == only(redA) || only(plain) == only(blueC) {
		t.Errorf("a crate with no Params keyed the same as a tinted one: %v", only(plain))
	}
	if only(plain) != only(empty) {
		t.Errorf("empty Params keyed %v, want what no Params keys, %v", only(empty), only(plain))
	}
	if keys.meshes[meshRed] != keys.meshes[meshRedToo] {
		t.Errorf("two Meshes with equal Params keyed %v and %v", keys.meshes[meshRed], keys.meshes[meshRedToo])
	}
	if keys.meshes[meshBlue] == keys.meshes[meshRed] {
		t.Errorf("a blue Mesh keyed the same as a red one: %v", keys.meshes[meshBlue])
	}
	h.frame(t)
	if got := len(where(h.drawn(), ofTriangle)); got != 8 {
		t.Errorf("the frame drew %d triangles, want every crate and mesh", got)
	}
	h.noErrors(t)
}

// TestTheMaterialKeyIsTheFilesOrTheOverrides is the other half of the key: a
// crate with no Material keys its file material through the key the load took,
// and a Material override keys by its own content, the same for two crates
// whose overrides are equal.
func TestTheMaterialKeyIsTheFilesOrTheOverrides(t *testing.T) {
	h := newDrawingHarness(t, 256)
	override := func() *ecsscene.Material {
		return &ecsscene.Material{Tags: m.ListOf([]ecsscene.MaterialTag{{
			Shader: gfx.ShaderWithResource("shaders/flat.wgsl"),
			Params: m.ListOf([]gfx.ParameterDescr{gfx.ColorParam("tint", red)}),
		}})}
	}
	file := h.spawn(t, spawnRequest{Place: m.At(0, 0, 0), Model: crateModelComponent()})
	first := h.spawn(t, spawnRequest{Place: m.At(2, 0, 0), Model: crateModelComponent(), Material: override()})
	second := h.spawn(t, spawnRequest{Place: m.At(4, 0, 0), Model: crateModelComponent(), Material: override()})
	keys := h.keyedModels(t, file, first, second)

	fileKey := keys.models[file].keys[0]
	if fileKey.material == 0 {
		t.Error("the file's own material keyed as zero, which is the bundled PBR's")
	}
	if a, b := keys.models[first].keys[0], keys.models[second].keys[0]; a != b || a.material == fileKey.material {
		t.Errorf("equal overrides keyed %v and %v, and the file material %v", a, b, fileKey)
	}
	if keys.models[file].handle != keys.models[first].handle {
		t.Error("two Entities naming one file resolved to two handles")
	}
}

// TestTheLoadSystemIsTheOnlyOneWritingTheLookup is the lock-set claim: of
// everything ecsscene subscribes or handles, the load System alone holds
// *model.Lookup for writing. It is ordered before the recording System, reads
// every Store it keys from, and writes none of them.
func TestTheLoadSystemIsTheOnlyOneWritingTheLookup(t *testing.T) {
	h := newHarness(t)
	description := h.engine.Describe()
	lookup := reflect.TypeFor[*model.Lookup]()
	var writers []reflect.Type
	var loader, recorder *kernel.SubscriptionDescription
	for i := range description.Subscriptions {
		sub := &description.Subscriptions[i]
		if sub.Owner != ecsscene.Name {
			continue
		}
		if containsType(sub.Writes, lookup) {
			writers = append(writers, sub.Type)
		}
		switch sub.Type {
		case reflect.TypeFor[ecsscene.LoadOnUpdate]():
			loader = sub
		case reflect.TypeFor[ecsscene.RecordOnUpdate]():
			recorder = sub
		}
	}
	for _, command := range description.Commands {
		if command.Owner == ecsscene.Name && containsType(command.Writes, lookup) {
			writers = append(writers, command.Type)
		}
	}
	if loader == nil || recorder == nil {
		t.Fatal("the description does not list both of ecsscene's Systems")
	}
	if len(writers) != 1 || writers[0] != loader.Type {
		t.Errorf("ecsscene's handlers writing *model.Lookup are %v, want the load System alone", writers)
	}
	if !containsType(recorder.DependsOn, loader.Type) {
		t.Errorf("the recording System does not wait for the load System; it depends on %v", recorder.DependsOn)
	}
	for _, component := range []reflect.Type{
		reflect.TypeFor[*ecs.Store[ecsscene.Model]](),
		reflect.TypeFor[*ecs.Store[ecsscene.Mesh]](),
		reflect.TypeFor[*ecs.Store[ecsscene.Material]](),
		reflect.TypeFor[*ecs.Store[ecsscene.Params]](),
	} {
		if !containsType(loader.Reads, component) {
			t.Errorf("the load System's read set %v does not name %v", loader.Reads, component)
		}
		if containsType(loader.Writes, component) {
			t.Errorf("the load System writes %v, which would make it that Component's writer", component)
		}
	}
	if !containsType(loader.Writes, reflect.TypeFor[*keyScratch]()) {
		t.Errorf("the load System's write set %v does not name its scratch", loader.Writes)
	}
	if containsType(loader.Writes, reflect.TypeFor[*ecs.Entities]()) {
		t.Error("the load System holds the authority for write: keying is not a structural change")
	}
}
