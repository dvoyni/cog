package mux

import (
	"testing"

	"github.com/dvoyni/cog/kernel"
)

// gamePathSet holds paths over every kind of field a path of fields crosses.
type gamePathSet struct {
	turn     Path[game, int32]
	position Path[game, vec2]
	health   Path[game, int32]
	name     Path[game, string]
	cameraY  Path[game, float32]
}

// makeGamePathSet registers game and makes a gamePathSet over it.
func makeGamePathSet(configurator *kernel.Configurator) gamePathSet {
	RegisterModel[game, depth](configurator)
	return gamePathSet{
		turn:     MakePath[game, int32](configurator, "Turn"),
		position: MakePath[game, vec2](configurator, "Hero.Position"),
		health:   MakePath[game, int32](configurator, "Hero.Health"),
		name:     MakePath[game, string](configurator, "Hero.Name"),
		cameraY:  MakePath[game, float32](configurator, "Camera.Y"),
	}
}

func TestPathGetAnswersTheModelsOwnValue(t *testing.T) {
	var paths gamePathSet
	spawn(t, func(configurator *kernel.Configurator) { paths = makeGamePathSet(configurator) })
	model := newTestGame()
	expectGet(t, "Turn", paths.turn, model, 7)
	expectGet(t, "Hero.Position", paths.position, model, vec2{X: 1, Y: 2})
	expectGet(t, "Hero.Health, a promoted field,", paths.health, model, 30)
	expectGet(t, "Hero.Name", paths.name, model, "ada")
	expectGet(t, "Camera.Y", paths.cameraY, model, 6)
	copied := *model
	copied.Hero.Position.Y = 8
	expectGet(t, "Hero.Position of a copy", paths.position, &copied, vec2{X: 1, Y: 8})
}

// positionSink and nameSink keep what TestPathGetAllocatesNothing reads, so
// the compiler cannot drop the reads it measures.
var (
	positionSink vec2
	nameSink     string
)

func TestPathGetAllocatesNothing(t *testing.T) {
	var paths gamePathSet
	spawn(t, func(configurator *kernel.Configurator) { paths = makeGamePathSet(configurator) })
	model := newTestGame()
	allocations := testing.AllocsPerRun(100, func() {
		positionSink, _ = paths.position.Get(model)
		nameSink, _ = paths.name.Get(model)
	})
	if allocations != 0 {
		t.Fatalf("Get allocated %v times per run", allocations)
	}
}

func TestMakePathPanicsNamingWhatIsWrong(t *testing.T) {
	expectPanic(t, `path "Turn" of model mux.game is made before the mux plugin`, func() {
		spawnPlugins(t, testPlugin{init: func(configurator *kernel.Configurator) { MakePath[game, int32](configurator, "Turn") }})
	})
	testCases := []struct {
		name string
		want string
		make func(configurator *kernel.Configurator)
	}{
		{"an unregistered model", `path "Opacity" is made for mux.menu, which is not a registered model`,
			func(configurator *kernel.Configurator) { MakePath[menu, float32](configurator, "Opacity") }},
		{"a missing field", `path "Hero.Speed" of model mux.game reaches mux.unit, which has no field "Speed"`,
			func(configurator *kernel.Configurator) { MakePath[game, float32](configurator, "Hero.Speed") }},
		{"an empty path", `path "" of model mux.game reaches mux.game, which has no field ""`,
			func(configurator *kernel.Configurator) { MakePath[game, game](configurator, "") }},
		{"a field of a number", `path "Turn.X" of model mux.game reaches int32, which is no struct to hold field "X"`,
			func(configurator *kernel.Configurator) { MakePath[game, float32](configurator, "Turn.X") }},
		{"a pointer along the path", `path "Leader.Name" of model mux.game crosses field Leader, a pointer *mux.unit`,
			func(configurator *kernel.Configurator) { MakePath[game, string](configurator, "Leader.Name") }},
		{"a pointer property", `path "Leader" of model mux.game crosses field Leader, a pointer *mux.unit`,
			func(configurator *kernel.Configurator) { MakePath[game, *unit](configurator, "Leader") }},
		{"an embedded pointer", `path "Escort.Health" of model mux.game crosses field vitality, a pointer *mux.vitality`,
			func(configurator *kernel.Configurator) { MakePath[game, int32](configurator, "Escort.Health") }},
		{"an interface", `path "Plan" of model mux.game crosses field Plan, an interface interface {}`,
			func(configurator *kernel.Configurator) { MakePath[game, any](configurator, "Plan") }},
		{"a T not the property's type", `path "Turn" of model mux.game reaches int32, not float64`,
			func(configurator *kernel.Configurator) { MakePath[game, float64](configurator, "Turn") }},
		{"a path twice", `paths "Turn" and "Turn" of model mux.game overlap`,
			func(configurator *kernel.Configurator) {
				MakePath[game, int32](configurator, "Turn")
				MakePath[game, int32](configurator, "Turn")
			}},
		{"a prefix made first", `paths "Hero" and "Hero.Position" of model mux.game overlap`,
			func(configurator *kernel.Configurator) {
				MakePath[game, unit](configurator, "Hero")
				MakePath[game, vec2](configurator, "Hero.Position")
			}},
		{"a prefix made last", `paths "Hero.Position.X" and "Hero.Position" of model mux.game overlap`,
			func(configurator *kernel.Configurator) {
				MakePath[game, float32](configurator, "Hero.Position.X")
				MakePath[game, vec2](configurator, "Hero.Position")
			}},
		{"two spellings of one field", `paths "Hero.Health" and "Hero.vitality.Health" of model mux.game overlap`,
			func(configurator *kernel.Configurator) {
				MakePath[game, int32](configurator, "Hero.Health")
				MakePath[game, int32](configurator, "Hero.vitality.Health")
			}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			expectPanic(t, testCase.want, func() {
				spawn(t, func(configurator *kernel.Configurator) {
					RegisterModel[game, depth](configurator)
					testCase.make(configurator)
				})
			})
		})
	}
}

// expectGet fails t unless path answers want, and true, over model.
func expectGet[T comparable](t *testing.T, name string, path Path[game, T], model *game, want T) {
	t.Helper()
	if got, ok := path.Get(model); got != want || !ok {
		t.Fatalf("%s answered %v, %v, want %v", name, got, ok, want)
	}
}
