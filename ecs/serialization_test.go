package ecs

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// replicated is a marker a network layer could define.
type replicated struct{}

// transform is embedded in unit, whose fields it promotes.
type transform struct {
	Position position
	Angle    float32
	hidden   int32
}

// unit is a component with a marker, fields of every kind and an embedded
// struct.
type unit struct {
	replicated
	Health int32
	Target Entity
	Name   Ref[string]
	Mesh   Ref[mesh]
	Slots  [2]int16
	transform
	secret int32
}

// cache is a transient component.
type cache struct {
	Transient
	Value int32
}

// score is a component that is not a struct.
type score int32

// member is an element of squad's array: its marker and unexported field
// stay out of saves, and its json tag means nothing to them.
type member struct {
	replicated
	Unit   Entity
	Rank   int16 `json:"rank"`
	secret int32
}

// squad is a component holding an array of structs.
type squad struct{ Members [2]member }

// warnings is a LogHandler that keeps the warnings it is told of.
type warnings struct {
	mutex sync.Mutex
	lines []string
}

func (w *warnings) LogErr(source kernel.KernelID, err error) { panic(err) }

func (w *warnings) LogWarn(source kernel.KernelID, msg string, args ...any) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.lines = append(w.lines, strings.TrimSpace(fmt.Sprintln(append([]any{msg}, args...)...)))
}

func (w *warnings) LogDebug(source kernel.KernelID, msg string, args ...any) {}

// savedWorld is a world of every kind of component and refs a save holds.
type savedWorld struct {
	kernel    *kernel.Kernel
	world     World[game]
	units     Store[game, unit]
	positions Store[game, position]
	caches    Store[game, cache]
	frozens   Store[game, frozen]
	scores    Store[game, score]
	texts     Refs[game, string]
	meshes    KeyedRefs[game, string, mesh]
	scratches Refs[game, int]
}

func spawnSaved(t *testing.T, logHandler kernel.LogHandler) *savedWorld {
	o := &savedWorld{}
	h := spawnLogged(t, 4, logHandler, func(c *kernel.Configurator) {
		RegisterComponent[game, position](c, "position")
		RegisterComponent[game, unit](c, "unit")
		RegisterComponent[game, cache](c, "cache")
		RegisterComponent[game, frozen](c, "frozen")
		RegisterComponent[game, score](c, "score")
		RegisterRefs[game, string](c, "text", RefsOptions{})
		RegisterKeyedRefs[game, string, mesh](c, "mesh", RefsOptions{})
		RegisterRefs[game, int](c, "scratch", RefsOptions{Transient: true})
		o.world = declare[World[game]](c)
		o.units = declare[Store[game, unit]](c)
		o.positions = declare[Store[game, position]](c)
		o.caches = declare[Store[game, cache]](c)
		o.frozens = declare[Store[game, frozen]](c)
		o.scores = declare[Store[game, score]](c)
		o.texts = declare[Refs[game, string]](c)
		o.meshes = declare[KeyedRefs[game, string, mesh]](c)
		o.scratches = declare[Refs[game, int]](c)
	})
	o.kernel = h.kernel
	return o
}

// save saves the world, failing t on an error.
func (o *savedWorld) save(t *testing.T) string {
	t.Helper()
	var out bytes.Buffer
	if err := o.world.Save(&out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestSchema(t *testing.T) {
	o := spawnSaved(t, nil)
	schema := o.world.Schema()
	var u unit
	fieldsOf := func(info ComponentInfo) []string {
		var fields []string
		for _, f := range info.Fields {
			fields = append(fields, fmt.Sprintf("%s %v @%d", f.Path, f.Type, f.Offset))
		}
		return fields
	}
	want := []string{
		fmt.Sprintf("Health int32 @%d", unsafe.Offsetof(u.Health)),
		fmt.Sprintf("Target ecs.Entity @%d", unsafe.Offsetof(u.Target)),
		fmt.Sprintf("Name ecs.Ref[string] @%d", unsafe.Offsetof(u.Name)),
		fmt.Sprintf("Mesh ecs.Ref[github.com/dvoyni/cog/ecs.mesh] @%d", unsafe.Offsetof(u.Mesh)),
		fmt.Sprintf("Slots [2]int16 @%d", unsafe.Offsetof(u.Slots)),
		fmt.Sprintf("Position.X float32 @%d", unsafe.Offsetof(u.Position)+unsafe.Offsetof(u.Position.X)),
		fmt.Sprintf("Position.Y float32 @%d", unsafe.Offsetof(u.Position)+unsafe.Offsetof(u.Position.Y)),
		fmt.Sprintf("Angle float32 @%d", unsafe.Offsetof(u.Angle)),
	}
	units := schema.Components[o.world.ComponentID[unit]()]
	if got := fieldsOf(units); !slices.Equal(got, want) {
		t.Fatalf("unit's fields are\n%v\nwant\n%v", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if units.Name != "unit" || units.Size != unsafe.Sizeof(u) || !slices.Equal(units.Markers, []reflect.Type{reflect.TypeFor[replicated]()}) {
		t.Fatalf("unit is described as %+v", units)
	}
	if !HasMarker[replicated](units) || HasMarker[Transient](units) {
		t.Fatal("HasMarker misreads unit's markers")
	}
	caches := schema.Components[o.world.ComponentID[cache]()]
	if !HasMarker[Transient](caches) || !slices.Equal(fieldsOf(caches), []string{"Value int32 @0"}) {
		t.Fatalf("cache is described as %+v", caches)
	}
	if frozens := schema.Components[o.world.ComponentID[frozen]()]; len(frozens.Fields) != 0 || len(frozens.Markers) != 0 {
		t.Fatalf("frozen is described as %+v", frozens)
	}
	if scores := schema.Components[o.world.ComponentID[score]()]; !slices.Equal(fieldsOf(scores), []string{" ecs.score @0"}) {
		t.Fatalf("score is described as %+v", scores)
	}
	wantRefs := []RefInfo{
		{Name: "text", Type: reflect.TypeFor[string]()},
		{Name: "mesh", Type: reflect.TypeFor[mesh](), Keyed: true},
		{Name: "scratch", Type: reflect.TypeFor[int](), Transient: true},
	}
	if !slices.Equal(schema.Refs, wantRefs) {
		t.Fatalf("the refs are described as %+v, want %+v", schema.Refs, wantRefs)
	}
}

func TestSchemaCompositionErrors(t *testing.T) {
	type lateMarker struct {
		X int32
		replicated
	}
	type clash struct {
		transform
		Angle float32
	}
	expectPanic(t, "follows a field", func() {
		spawn(t, func(c *kernel.Configurator) { RegisterComponent[game, lateMarker](c, "late") })
	})
	expectPanic(t, "two fields at path \"Angle\"", func() {
		spawn(t, func(c *kernel.Configurator) { RegisterComponent[game, clash](c, "clash") })
	})
	expectPanic(t, "component name \"position\" is registered", func() {
		spawn(t, func(c *kernel.Configurator) {
			RegisterComponent[game, position](c, "position")
			RegisterComponent[game, velocity](c, "position")
		})
	})
	expectPanic(t, "refs name \"text\" is registered", func() {
		spawn(t, func(c *kernel.Configurator) {
			RegisterRefs[game, string](c, "text", RefsOptions{})
			RegisterKeyedRefs[game, string, mesh](c, "text", RefsOptions{})
		})
	})
}

func TestSaveFormat(t *testing.T) {
	o := spawnSaved(t, nil)
	text := o.texts.New("hello")
	e := o.world.Spawn()
	o.positions.Add(e, position{X: 1, Y: 2})
	o.frozens.Add(e, frozen{})
	o.units.Add(e, unit{Health: 3, Target: e, Name: text, Slots: [2]int16{4, 5}})
	o.caches.Add(e, cache{Value: 6})
	o.scores.Add(e, 7)
	o.scratches.New(8)
	want := fmt.Sprintf(`{
	"refs": {
		"text": [
			{
				"ref": %[2]d,
				"value": "hello"
			}
		],
		"mesh": []
	},
	"entities": [
		{
			"entity": %[1]d,
			"position": {
				"X": 1,
				"Y": 2
			},
			"unit": {
				"Health": 3,
				"Target": %[1]d,
				"Name": %[2]d,
				"Mesh": 0,
				"Slots": [
					4,
					5
				],
				"Position": {
					"X": 0,
					"Y": 0
				},
				"Angle": 0
			},
			"frozen": {},
			"score": 7
		}
	]
}
`, SerializeEntity(e), SerializeRef(text))
	if got := o.save(t); got != want {
		t.Fatalf("saved\n%s\nwant\n%s", got, want)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	from := spawnSaved(t, nil)
	text := from.texts.New("hello")
	cube := from.meshes.Put(mesh{Path: "cube", Vertices: []float32{1, 2}})
	sphere := from.meshes.Put(mesh{Path: "sphere", Vertices: []float32{3}})
	first := from.world.Spawn()
	second := from.world.Spawn()
	gone := from.world.Spawn()
	last := from.world.Spawn()
	from.world.Despawn(gone)
	saved := unit{Health: 5, Target: second, Name: text, Mesh: cube, Slots: [2]int16{3, 4}, secret: 9}
	saved.Position = position{X: 1, Y: 2}
	saved.Angle = 0.5
	from.units.Add(first, saved)
	from.units.Add(last, unit{Target: gone, Mesh: sphere})
	from.positions.Add(second, position{X: 7, Y: 8})
	from.caches.Add(second, cache{Value: 3})
	from.frozens.Add(second, frozen{})
	from.scratches.New(1)
	file := from.save(t)
	if strings.Contains(file, "cache") || strings.Contains(file, "scratch") {
		t.Fatalf("the save holds a transient component or refs:\n%s", file)
	}

	to := spawnSaved(t, nil)
	for range 3 {
		to.world.Spawn()
	}
	existingText := to.texts.New("existing")
	existingCube := to.meshes.Put(mesh{Path: "cube", Vertices: []float32{9}})
	if err := to.world.Load(to.kernel.Logger(), strings.NewReader(file)); err != nil {
		t.Fatal(err)
	}
	// The saved entities load one after another past the world's three,
	// the despawned one leaving no hole.
	moved := map[Entity]Entity{}
	for i, e := range []Entity{first, second, last} {
		moved[e] = makeEntity(3+uint32(i), e.generation())
	}
	got := to.units.Get(moved[first])
	if got == nil {
		t.Fatalf("%v did not load as %v", first, moved[first])
	}
	if got.Health != 5 || got.Target != moved[second] || got.Slots != saved.Slots || got.Position != saved.Position || got.Angle != 0.5 || got.secret != 0 {
		t.Fatalf("loaded %+v, saved %+v", *got, saved)
	}
	if got.Name == existingText || *to.texts.Get(got.Name) != "hello" {
		t.Fatalf("the text ref loaded as %v reading %v", got.Name, to.texts.Get(got.Name))
	}
	if got.Mesh != existingCube || to.meshes.Get(existingCube).Vertices[0] != 9 {
		t.Fatalf("the cube loaded as %v, not the world's %v keeping its value", got.Mesh, existingCube)
	}
	lastUnit := to.units.Get(moved[last])
	if lastUnit == nil || lastUnit.Target != NoEntity || to.meshes.Get(lastUnit.Mesh).Path != "sphere" {
		t.Fatalf("the last unit loaded as %+v", lastUnit)
	}
	if *to.positions.Get(moved[second]) != (position{X: 7, Y: 8}) || !to.frozens.Has(moved[second]) || to.caches.Has(moved[second]) {
		t.Fatal("the second entity did not load its position and frozen alone")
	}
	if fresh := to.world.Spawn(); fresh.index() != 6 {
		t.Fatalf("spawned %v, not the first index past the loaded ones", fresh)
	}
	if fresh := to.texts.New("next"); fresh.index != 2 {
		t.Fatalf("made %v, not the first index past the loaded refs", fresh)
	}
}

func TestLoadForgives(t *testing.T) {
	logged := &warnings{}
	o := spawnSaved(t, logged)
	file := `{
		"version": 3,
		"refs": {"sounds": [{"ref": 4294967296, "value": "boom"}]},
		"entities": [
			{"entity": 4294967296, "position": {"X": 1, "Z": 5}, "ghost": {}},
			{"ghost": 1, "entity": 4294967297}
		]
	}`
	if err := o.world.Load(o.kernel.Logger(), strings.NewReader(file)); err != nil {
		t.Fatal(err)
	}
	if p := o.positions.Get(makeEntity(0, 1)); p == nil || *p != (position{X: 1}) {
		t.Fatalf("loaded position %v", p)
	}
	if !o.world.Alive(makeEntity(1, 1)) {
		t.Fatal("the entity of an unknown component alone did not load")
	}
	want := []string{
		"ecs: load skips a refs the world does not have world ecs.game refs sounds",
		"ecs: load skips a component the world does not have world ecs.game component ghost",
	}
	if !slices.Equal(logged.lines, want) {
		t.Fatalf("warned\n%v\nwant\n%v", strings.Join(logged.lines, "\n"), strings.Join(want, "\n"))
	}
	if err := o.world.Load(nil, strings.NewReader(`{"entities": [{"entity": 4294967296, "ghost": {}}]}`)); err != nil {
		t.Fatalf("a load with no logger failed: %v", err)
	}
}

func TestSaveArraysOfStructsByTheirFields(t *testing.T) {
	var w World[game]
	var squads Store[game, squad]
	spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, squad](c, "squad")
		w = declare[World[game]](c)
		squads = declare[Store[game, squad]](c)
	})
	leader := w.Spawn()
	follower := w.Spawn()
	squads.Add(leader, squad{Members: [2]member{{Unit: follower, Rank: 1, secret: 5}, {Unit: leader, Rank: 2}}})
	var out bytes.Buffer
	if err := w.Save(&out); err != nil {
		t.Fatal(err)
	}
	compact := strings.Join(strings.Fields(out.String()), "")
	want := fmt.Sprintf(`"squad":{"Members":[{"Unit":%d,"Rank":1},{"Unit":%d,"Rank":2}]}`, SerializeEntity(follower), SerializeEntity(leader))
	if !strings.Contains(compact, want) {
		t.Fatalf("saved\n%s\nwant it to hold %s", compact, want)
	}
	file := fmt.Sprintf(`{"entities": [
		{"entity": %d, "squad": {"Members": [{"Unit": %d, "Rank": 1}, {}, {"Rank": 9}]}},
		{"entity": %d}
	]}`, SerializeEntity(leader), SerializeEntity(follower), SerializeEntity(follower))
	if err := w.Load(nil, strings.NewReader(file)); err != nil {
		t.Fatal(err)
	}
	moved := func(e Entity) Entity { return makeEntity(2+e.index(), e.generation()) }
	got := squads.Get(moved(leader))
	if got == nil || *got != (squad{Members: [2]member{{Unit: moved(follower), Rank: 1}}}) {
		t.Fatalf("loaded %+v, want the first member remapped, the second zero and the third skipped", got)
	}
}

func TestLoadOfNoRefsKeepsTheirAllocation(t *testing.T) {
	o := spawnSaved(t, nil)
	file := o.save(t)
	if !strings.Contains(file, `"text": []`) {
		t.Fatalf("the save holds no empty texts:\n%s", file)
	}
	kept := o.texts.New("kept")
	o.texts.New("other")
	if err := o.world.Load(nil, strings.NewReader(file)); err != nil {
		t.Fatal(err)
	}
	if fresh := o.texts.New("fresh"); fresh.index != 2 || *o.texts.Get(kept) != "kept" {
		t.Fatalf("made %v after loading no texts, and the first reads %q", fresh, *o.texts.Get(kept))
	}
}

func TestLoadKeyedRefs(t *testing.T) {
	o := spawnSaved(t, nil)
	cube := o.meshes.Put(mesh{Path: "cube", Vertices: []float32{9}})
	file := `{
		"refs": {"mesh": [
			{"ref": 4294967296, "value": {"Path": "cube", "Vertices": [1]}},
			{"ref": 4294967297, "value": {"Path": "cone", "Vertices": [2]}},
			{"ref": 4294967298, "value": {"Path": "cone", "Vertices": [3]}}
		]},
		"entities": [
			{"entity": 4294967296, "unit": {"Mesh": 4294967296}},
			{"entity": 4294967297, "unit": {"Mesh": 4294967297}},
			{"entity": 4294967298, "unit": {"Mesh": 4294967298}}
		]
	}`
	if err := o.world.Load(o.kernel.Logger(), strings.NewReader(file)); err != nil {
		t.Fatal(err)
	}
	meshOf := func(index uint32) Ref[mesh] { return o.units.Get(makeEntity(index, 1)).Mesh }
	if meshOf(0) != cube || o.meshes.Get(cube).Vertices[0] != 9 {
		t.Fatal("the saved cube did not take the world's, keeping its value")
	}
	cone, ok := o.meshes.Find("cone")
	if !ok || meshOf(1) != cone || meshOf(2) != cone || o.meshes.Get(cone).Vertices[0] != 2 {
		t.Fatalf("the saved cones loaded as %v and %v, the key finds %v", meshOf(1), meshOf(2), cone)
	}
	// Only the first cone took an index: the cube took the world's ref,
	// and the second cone the first one's.
	if fresh := o.meshes.Put(mesh{Path: "torus"}); fresh.index != 2 {
		t.Fatalf("a new mesh took %v, not the index past the first cone", fresh)
	}
}

func TestLoadFailsChangingNothing(t *testing.T) {
	o := spawnSaved(t, nil)
	e := o.world.Spawn()
	o.positions.Add(e, position{X: 1})
	o.texts.New("kept")
	before := o.save(t)
	files := map[string]string{
		"malformed":          `{"entities": [{"entity": 4294967296, "position": {"X": 1}`,
		"mistyped field":     `{"entities": [{"entity": 4294967296, "position": {"X": "one"}}]}`,
		"mistyped ref value": `{"refs": {"text": [{"ref": 4294967296, "value": 1}]}}`,
		"no handle":          `{"entities": [{"position": {}}]}`,
		"generation 0":       `{"entities": [{"entity": 1}]}`,
		"two at one index":   `{"entities": [{"entity": 4294967296}, {"entity": 8589934592}]}`,
		"not an object":      `[]`,
	}
	for name, file := range files {
		err := o.world.Load(o.kernel.Logger(), strings.NewReader(file))
		if err == nil {
			t.Fatalf("%s: loaded", name)
		}
		if name == "mistyped field" && !strings.Contains(err.Error(), `entity 4294967296: component "position"`) {
			t.Fatalf("%s: the error %q does not name the entity and its component", name, err)
		}
		if name == "mistyped ref value" && !strings.Contains(err.Error(), `refs "text": ref 4294967296`) {
			t.Fatalf("%s: the error %q does not name the ref", name, err)
		}
		if after := o.save(t); after != before {
			t.Fatalf("%s: a failed load changed the world from\n%s\nto\n%s", name, before, after)
		}
	}
}

func TestLoadPastTheRangeFails(t *testing.T) {
	type cramped [1]uint64
	var w World[cramped]
	var texts Refs[cramped, string]
	spawn(t, func(c *kernel.Configurator) {
		RegisterWorld[cramped](c, WorldOptions{LastIndex: 1})
		RegisterRefs[cramped, string](c, "text", RefsOptions{LastIndex: 1})
		w = declare[World[cramped]](c)
		texts = declare[Refs[cramped, string]](c)
	})
	w.Spawn()
	texts.New("kept")
	var before bytes.Buffer
	if err := w.Save(&before); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"entities": `{"entities": [{"entity": 4294967296}, {"entity": 4294967297}]}`,
		"refs":     `{"refs": {"text": [{"ref": 4294967296, "value": "a"}, {"ref": 4294967297, "value": "b"}]}}`,
	}
	for name, file := range files {
		err := w.Load(nil, strings.NewReader(file))
		if err == nil || !strings.Contains(err.Error(), "do not fit the allocation range past index 1") {
			t.Fatalf("%s: loading two into one free index answered %v", name, err)
		}
		var after bytes.Buffer
		if err := w.Save(&after); err != nil {
			t.Fatal(err)
		}
		if after.String() != before.String() {
			t.Fatalf("%s: a failed load changed the world from\n%s\nto\n%s", name, before.String(), after.String())
		}
	}
	if err := w.Load(nil, strings.NewReader(`{"refs": {"text": [{"ref": 4294967296, "value": "a"}]}, "entities": [{"entity": 4294967296}]}`)); err != nil {
		t.Fatalf("loading one into one free index failed: %v", err)
	}
}

func TestLoadObserved(t *testing.T) {
	var w World[game]
	var deltas DeltaQuery1[game, position]
	var refDeltas DeltaRefs[game, string]
	spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, position](c, "position")
		RegisterRefs[game, string](c, "text", RefsOptions{})
		w = declare[World[game]](c)
		deltas = declare[DeltaQuery1[game, position]](c)
		refDeltas = declare[DeltaRefs[game, string]](c)
	})
	ObservationResetSystem(nil, kernel.NoArg{}, w)
	file := `{"refs": {"text": [{"ref": 4294967296, "value": "hi"}]}, "entities": [{"entity": 4294967296, "position": {"X": 1}}]}`
	if err := w.Load(nil, strings.NewReader(file)); err != nil {
		t.Fatal(err)
	}
	var got []string
	for e, row := range deltas.All() {
		got = append(got, fmt.Sprintf("%v %v %v", e, row.Delta(), row.Get()))
	}
	for h, delta := range refDeltas.All() {
		got = append(got, fmt.Sprintf("%v %v %v", h, delta.Delta, *delta.Value))
	}
	want := []string{
		fmt.Sprintf("%v %v %v", makeEntity(0, 1), DeltaAdded, position{X: 1}),
		fmt.Sprintf("%v %v hi", Ref[string]{index: 0, generation: 1}, DeltaAdded),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the window after the load holds %v, want %v", got, want)
	}
}

func TestLoadPastReservations(t *testing.T) {
	var w World[game]
	var commands CommandBuffer[game, aiCommands]
	var commandRefs CommandRefs[game, aiCommands, string]
	var sink CommandSink[game, aiCommands]
	var refs Refs[game, string]
	spawn(t, func(c *kernel.Configurator) {
		RegisterRefs[game, string](c, "text", RefsOptions{})
		RegisterCommandBuffer[game, aiCommands](c)
		w = declare[World[game]](c)
		commands = declare[CommandBuffer[game, aiCommands]](c)
		commandRefs = declare[CommandRefs[game, aiCommands, string]](c)
		sink = declare[CommandSink[game, aiCommands]](c)
		refs = declare[Refs[game, string]](c)
	})
	reserved := commands.Reserve()
	commands.Spawn(reserved)
	pending := commandRefs.New("pending")
	file := `{"refs": {"text": [{"ref": 4294967296, "value": "loaded"}]}, "entities": [{"entity": 4294967296}]}`
	if err := w.Load(nil, strings.NewReader(file)); err != nil {
		t.Fatal(err)
	}
	sink.Commit()
	loaded := makeEntity(reserved.index()+1, 1)
	if !w.Alive(reserved) || !w.Alive(loaded) {
		t.Fatalf("the reserved entity %v or the loaded one past it %v is not alive", reserved, loaded)
	}
	if *refs.Get(pending) != "pending" || *refs.Get(Ref[string]{index: pending.index + 1, generation: 1}) != "loaded" {
		t.Fatal("the reserved ref or the loaded one past it does not read its value")
	}
}
