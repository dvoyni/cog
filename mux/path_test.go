package mux

import (
	"testing"

	"github.com/dvoyni/cog/kernel"
)

// gamePathSet holds paths over every kind of field and array step a path
// crosses.
type gamePathSet struct {
	turn     Path[game, int32]
	position Path[game, vec2]
	health   Path[game, int32]
	name     Path[game, string]
	cameraY  Path[game, float32]
	// memberPosition crosses two arrays, each at a hole.
	memberPosition Path[game, vec2]
	// lastMemberHealth crosses an array at a hole, then one at a literal
	// index, then a promoted field.
	lastMemberHealth Path[game, int32]
	// firstBanner and secondBanner cross an array at literal indices, so they
	// have no hole, and do not overlap.
	firstBanner  Path[game, float32]
	secondBanner Path[game, float32]
	// terrain crosses an array of arrays, at a hole each.
	terrain Path[game, float32]
}

// makeGamePathSet registers game and makes a gamePathSet over it.
func makeGamePathSet(configurator *kernel.Configurator) gamePathSet {
	RegisterModel[game, depth](configurator)
	return gamePathSet{
		turn:             MakePath[game, int32](configurator, "Turn"),
		position:         MakePath[game, vec2](configurator, "Hero.Position"),
		health:           MakePath[game, int32](configurator, "Hero.Health"),
		name:             MakePath[game, string](configurator, "Hero.Name"),
		cameraY:          MakePath[game, float32](configurator, "Camera.Y"),
		memberPosition:   MakePath[game, vec2](configurator, "Squads[].Members[].Position"),
		lastMemberHealth: MakePath[game, int32](configurator, "Squads[].Members[2].Health"),
		firstBanner:      MakePath[game, float32](configurator, "Squads[0].Banner"),
		secondBanner:     MakePath[game, float32](configurator, "Squads[1].Banner"),
		terrain:          MakePath[game, float32](configurator, "Terrain[][]"),
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
	expectGet(t, "Squads[1].Members[2].Position", paths.memberPosition, model, vec2{X: 3, Y: 4}, 1, 2)
	expectGet(t, "Squads[0].Members[2].Position", paths.memberPosition, model, vec2{}, 0, 2)
	expectGet(t, "Squads[1].Members[2].Health", paths.lastMemberHealth, model, 40, 1)
	expectGet(t, "Squads[0].Banner", paths.firstBanner, model, 0.25)
	expectGet(t, "Squads[1].Banner", paths.secondBanner, model, 0.5)
	expectGet(t, "Terrain[0][2]", paths.terrain, model, 3, 0, 2)
	expectGet(t, "Terrain[1][0]", paths.terrain, model, 4, 1, 0)
	copied := *model
	copied.Hero.Position.Y = 8
	copied.Terrain[1][0] = 9
	expectGet(t, "Hero.Position of a copy", paths.position, &copied, vec2{X: 1, Y: 8})
	expectGet(t, "Terrain[1][0] of a copy", paths.terrain, &copied, 9, 1, 0)
}

func TestPathGetAnswersFalseForAnIndexOutsideItsArray(t *testing.T) {
	var paths gamePathSet
	spawn(t, func(configurator *kernel.Configurator) { paths = makeGamePathSet(configurator) })
	model := newTestGame()
	for _, holes := range [][]int64{{2, 0}, {0, 3}, {-1, 0}, {0, -1}, {1 << 40, 0}, {2, 3}} {
		if got, ok := paths.terrain.Get(model, holes...); got != 0 || ok {
			t.Errorf("Terrain%v answered %v, %v, want 0, false", holes, got, ok)
		}
	}
	if got, ok := paths.lastMemberHealth.Get(model, 2); got != 0 || ok {
		t.Errorf("Squads[2].Members[2].Health answered %v, %v, want 0, false", got, ok)
	}
}

func TestPathGetPanicsOnAWrongNumberOfHoles(t *testing.T) {
	var paths gamePathSet
	spawn(t, func(configurator *kernel.Configurator) { paths = makeGamePathSet(configurator) })
	model := newTestGame()
	expectPanic(t, `path "Terrain[][]" has 2 holes, but is given 1`, func() { paths.terrain.Get(model, 1) })
	expectPanic(t, `path "Terrain[][]" has 2 holes, but is given 3`, func() { paths.terrain.Get(model, 1, 0, 0) })
	expectPanic(t, `path "Squads[].Members[2].Health" has 1 hole, but is given 0`, func() { paths.lastMemberHealth.Get(model) })
	expectPanic(t, `path "Squads[1].Banner" has 0 holes, but is given 1`, func() { paths.secondBanner.Get(model, 1) })
	expectPanic(t, `path "Turn" has 0 holes, but is given 2`, func() { paths.turn.Get(model, 0, 0) })
}

// positionSink, nameSink, memberPositionSink and terrainSink keep what
// TestPathGetAllocatesNothing reads, so the compiler cannot drop the reads it
// measures.
var (
	positionSink       vec2
	nameSink           string
	memberPositionSink vec2
	terrainSink        float32
)

func TestPathGetAllocatesNothing(t *testing.T) {
	var paths gamePathSet
	spawn(t, func(configurator *kernel.Configurator) { paths = makeGamePathSet(configurator) })
	model := newTestGame()
	allocations := testing.AllocsPerRun(100, func() {
		positionSink, _ = paths.position.Get(model)
		nameSink, _ = paths.name.Get(model)
		memberPositionSink, _ = paths.memberPosition.Get(model, 1, 2)
		terrainSink, _ = paths.terrain.Get(model, 2, 0)
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
		{"a hole over a literal index", `paths "Squads[0].Banner" and "Squads[].Banner" of model mux.game overlap`,
			func(configurator *kernel.Configurator) {
				MakePath[game, float32](configurator, "Squads[0].Banner")
				MakePath[game, float32](configurator, "Squads[].Banner")
			}},
		{"an element over a property within it", `paths "Terrain[][2]" and "Terrain[1]" of model mux.game overlap`,
			func(configurator *kernel.Configurator) {
				MakePath[game, float32](configurator, "Terrain[][2]")
				MakePath[game, [3]float32](configurator, "Terrain[1]")
			}},
		{"an index of a number", `path "Turn[]" of model mux.game reaches int32, which is no array to index`,
			func(configurator *kernel.Configurator) { MakePath[game, int32](configurator, "Turn[]") }},
		{"a literal index past the array's end", `path "Squads[2].Banner" of model mux.game indexes [2]mux.squad at 2, past its length 2`,
			func(configurator *kernel.Configurator) { MakePath[game, float32](configurator, "Squads[2].Banner") }},
		{"an index that is no number", `path "Squads[x].Banner" of model mux.game indexes [2]mux.squad with "x", which is no index`,
			func(configurator *kernel.Configurator) { MakePath[game, float32](configurator, "Squads[x].Banner") }},
		{"a negative index", `path "Squads[-1].Banner" of model mux.game indexes [2]mux.squad with "-1", which is no index`,
			func(configurator *kernel.Configurator) { MakePath[game, float32](configurator, "Squads[-1].Banner") }},
		{"an unclosed index", `path "Squads[" of model mux.game is malformed at "["`,
			func(configurator *kernel.Configurator) { MakePath[game, squad](configurator, "Squads[") }},
		{"text after an index", `path "Squads[]x" of model mux.game is malformed at "x"`,
			func(configurator *kernel.Configurator) { MakePath[game, squad](configurator, "Squads[]x") }},
		{"a pointer element", `path "Guards[].Name" of model mux.game crosses an element of [2]*mux.unit, a pointer *mux.unit`,
			func(configurator *kernel.Configurator) { MakePath[game, string](configurator, "Guards[].Name") }},
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
	t.Run("more holes than the depth", func(t *testing.T) {
		expectPanic(t, `path "Terrain[][]" of model mux.game has more holes, 2, than its depth [1]int64 holds`, func() {
			spawn(t, func(configurator *kernel.Configurator) {
				RegisterModel[game, [1]int64](configurator)
				MakePath[game, float32](configurator, "Squads[].Banner")
				MakePath[game, float32](configurator, "Terrain[][]")
			})
		})
	})
}

// expectGet fails t unless path answers want, and true, over model at the
// address holes fill.
func expectGet[T comparable](t *testing.T, name string, path Path[game, T], model *game, want T, holes ...int64) {
	t.Helper()
	if got, ok := path.Get(model, holes...); got != want || !ok {
		t.Fatalf("%s answered %v, %v, want %v", name, got, ok, want)
	}
}
