package mux

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

type depth [4]int64

type vec2 struct{ X, Y float32 }

type vitality struct{ Health int32 }

type unit struct {
	vitality
	Position vec2
	Name     string
	// Visible is whether the unit is drawn, which a hold keeps false until a
	// new unit's entrance.
	Visible bool
}

// escort reaches its vitality through a pointer, which no path may cross.
type escort struct{ *vitality }

// distance is a named number type of a float kind.
type distance float64

// level is a named number type of an integer kind.
type level uint8

// heading is a named number type that turns the shortest way round, by the
// interpolator turnShortestWay, which a test registers.
type heading float32

// turnShortestWay is heading's interpolator: it turns the shortest way
// round, so 350° eases to 10° through 360°, not back through 180°.
func turnShortestWay(a, b heading, t float32, ease func(a, b, t float32) float32) heading {
	turn := heading(math.Mod(float64(b-a)+540, 360)) - 180
	return heading(ease(float32(a), float32(a+turn), t))
}

// gauge holds a property of every float and integer kind, and of named
// number types.
type gauge struct {
	Int      int
	Int8     int8
	Int16    int16
	Int32    int32
	Int64    int64
	Uint     uint
	Uint8    uint8
	Uint16   uint16
	Uint32   uint32
	Uint64   uint64
	Uintptr  uintptr
	Float32  float32
	Float64  float64
	Distance distance
	Level    level
	Heading  heading
}

// squad is a fixed band of units under a banner, with reserves of any
// number.
type squad struct {
	Members  [3]unit
	Reserves []unit
	Banner   float32
}

// platoon is a band of any number of units, under two flags, drilled in
// formations numbered by a platform-wide int.
type platoon struct {
	Members []unit
	Flags   [2]float32
	Drills  map[int]float32
}

// unitName is a unit's name, a key every map keyed by it interns.
type unitName string

// cell is a tile of the battlefield, a struct key every map keyed by it
// interns.
type cell struct{ Column, Row int16 }

// chronicle is a struct key larger than a map keeps in its slots, so a map
// keyed by it holds each key through a pointer.
type chronicle struct {
	Era   int64
	Notes [16]int64
}

// annal is an element larger than a map keeps in its slots, so a map of them
// holds each through a pointer.
type annal [36]float32

type game struct {
	Turn    int32
	Hero    unit
	Camera  vec2
	Leader  *unit
	Escort  escort
	Plan    any
	Gauge   gauge
	Squads  [2]squad
	Terrain [2][3]float32
	// Guards holds pointers, which no path may cross.
	Guards   [2]*unit
	Units    []unit
	Platoons []platoon
	Heights  [][]float32
	// Scouts holds pointers, which no path may cross.
	Scouts []*unit
	// UnitsByName keys units by their names, which are interned.
	UnitsByName map[unitName]unit
	// UnitsByID keys units by their ids, each its own hole.
	UnitsByID map[uint32]unit
	// Elevations keys each cell's height by the cell, which is interned.
	Elevations map[cell]float32
	// Morale is keyed by a signed integer narrower than a hole.
	Morale map[int8]float32
	// Bounties is keyed by an integer as wide as a hole, but unsigned.
	Bounties map[uint64]float32
	// Grades, Medals and Ranks are keyed by integers of each width narrower
	// than a hole, each element lying right after its key in the map.
	Grades map[int8]int8
	Medals map[int16]int16
	Ranks  map[uint32]int32
	// Garrisons keys bands of any number of units by name: a map of slices.
	Garrisons map[unitName][]unit
	// Regions keys each region's heights by its number: a map of maps.
	Regions map[int16]map[cell]float32
	// Chronicles holds keys and elements a map keeps through pointers.
	Chronicles map[chronicle]annal
	// Wards is keyed by pointers, and Rumours by interfaces, which no path
	// may cross.
	Wards   map[*unit]float32
	Rumours map[any]float32
	// Captains holds pointers, which no path may cross.
	Captains map[unitName]*unit
}

// newTestGame returns a game whose fields hold values no zero game does.
func newTestGame() *game {
	return &game{
		Turn:   7,
		Hero:   unit{vitality: vitality{Health: 30}, Position: vec2{X: 1, Y: 2}, Name: "ada"},
		Camera: vec2{X: 5, Y: 6},
		Squads: [2]squad{{Banner: 0.25}, {
			Members:  [3]unit{2: {vitality: vitality{Health: 40}, Position: vec2{X: 3, Y: 4}}},
			Reserves: []unit{{vitality: vitality{Health: 60}}, {vitality: vitality{Health: 70}}},
			Banner:   0.5,
		}},
		Terrain: [2][3]float32{{1, 2, 3}, {4, 5, 6}},
		Units:   []unit{{Position: vec2{X: 7, Y: 8}, Name: "bo"}, {Position: vec2{X: 9, Y: 10}, Name: "cy"}},
		Platoons: []platoon{
			{
				Members: []unit{{vitality: vitality{Health: 80}, Position: vec2{X: 11, Y: 12}}},
				Flags:   [2]float32{0.75, 1},
				Drills:  map[int]float32{-5: 0.125},
			},
			// This platoon has no members and no drills: its slice and its
			// map are nil.
			{Flags: [2]float32{1.25, 1.5}},
		},
		Heights:     [][]float32{{1, 2}, {3, 4, 5}},
		UnitsByName: map[unitName]unit{"bo": {Position: vec2{X: 13, Y: 14}}, "cy": {vitality: vitality{Health: 90}}},
		UnitsByID:   map[uint32]unit{7: {Name: "di", Position: vec2{X: 15, Y: 16}}, 1<<32 - 1: {Name: "ed"}},
		Elevations:  map[cell]float32{{Column: 1, Row: 2}: 0.5, {Column: -1}: 1.5},
		Morale:      map[int8]float32{-128: 0.25, 3: 0.75},
		Bounties:    map[uint64]float32{1<<64 - 1: 2.5, 4: 3.5},
		Garrisons:   map[unitName][]unit{"north": {{vitality: vitality{Health: 15}}, {vitality: vitality{Health: 25}}}},
		Regions:     map[int16]map[cell]float32{2: {{Column: 3, Row: 4}: 6.5}},
		Chronicles:  map[chronicle]annal{{Era: 1, Notes: [16]int64{15: 9}}: {35: 7.5}},
	}
}

type menu struct{ Opacity float32 }

// battle is a model whose fighters die: a dead fighter stays in it, marked,
// while it fades out, and a callback purges it at the fade's end.
type battle struct {
	Fighters []fighter
	// FreeSlots holds the slots of Fighters purged, which a new fighter
	// takes.
	FreeSlots []int64
	// Reserves keys fighters by their names, which are interned.
	Reserves map[unitName]fighter
}

// fighter is a battle's unit, whose opacity fades when it dies.
type fighter struct {
	Opacity float32
	Dead    bool
}

// testPlugin runs init as its Init.
type testPlugin struct {
	init func(configurator *kernel.Configurator)
}

func (testPlugin) Name() kernel.PluginName { return "test" }

func (testPlugin) Dependencies() []kernel.PluginName { return nil }

func (p testPlugin) Init(configurator *kernel.Configurator, config any) error {
	p.init(configurator)
	return nil
}

func (testPlugin) Start(kernel *kernel.Kernel) error { return nil }

func (testPlugin) Stop(kernel *kernel.Kernel) error { return nil }

func (testPlugin) Destroy() error { return nil }

// runEvent is what harness.run publishes: its one handler schedules
// whatever the test asks.
type runEvent struct{}

// harness is a kernel of mux's plugin and a test plugin.
type harness struct {
	kernel   *kernel.Kernel
	schedule func(executor *kernel.Executor) kernel.JobHandle
}

// spawn spawns a kernel of mux's plugin and a test plugin whose Init runs
// init.
func spawn(t testing.TB, init func(configurator *kernel.Configurator)) *harness {
	spawned := &harness{}
	spawned.kernel = spawnPlugins(t, NewPlugin(), testPlugin{init: func(configurator *kernel.Configurator) {
		configurator.Subscribe(func(_ *kernel.Kernel, executor *kernel.Executor, _ kernel.JobHandle, event runEvent) (runEvent, kernel.JobHandle) {
			return event, spawned.schedule(executor)
		})
		init(configurator)
	}})
	return spawned
}

// run schedules with schedule and waits for the job it returns.
func (h *harness) run(schedule func(executor *kernel.Executor) kernel.JobHandle) {
	h.schedule = schedule
	_, job := h.kernel.Publish(runEvent{})
	signal := kernel.NewSignal()
	h.kernel.Notify(signal, job)
	<-signal.C()
}

// spawnPlugins spawns a kernel of plugins and answers it.
func spawnPlugins(t testing.TB, plugins ...kernel.Plugin) *kernel.Kernel {
	supervisor := kernel.NewSupervisor(nil)
	t.Cleanup(supervisor.Quit)
	id, err := supervisor.Spawn(nil, 4, plugins...)
	if err != nil {
		t.Fatal(err)
	}
	return supervisor.GetKernel(id)
}

// declare declares P through a task registered for it, as a job's handles
// are declared, and answers it.
func declare[P kernel.Parameter[P]](configurator *kernel.Configurator) P {
	var parameter P
	configurator.RegisterTask(func(access *kernel.Access) kernel.TaskBody[kernel.NoArg, kernel.NoResult] {
		parameter = parameter.Declare(access)
		return func(_ *kernel.Kernel, _ kernel.NoArg) kernel.NoResult { return kernel.NoResult{} }
	})
	return parameter
}

func expectPanic(t *testing.T, want string, function func()) {
	t.Helper()
	defer func() {
		t.Helper()
		got := fmt.Sprint(recover())
		if !strings.Contains(got, want) {
			t.Fatalf("panicked with %q, want one mentioning %q", got, want)
		}
	}()
	function()
}
