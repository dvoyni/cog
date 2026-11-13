package mux

import (
	"strconv"
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
	// unitPosition crosses a slice at a hole.
	unitPosition Path[game, vec2]
	// firstUnitName and secondUnitName cross a slice at literal indices, so
	// they have no hole, and do not overlap.
	firstUnitName  Path[game, string]
	secondUnitName Path[game, string]
	// platoonMemberPosition crosses two slices, each at a hole.
	platoonMemberPosition Path[game, vec2]
	// platoonLeaderHealth crosses a slice at a hole, then one at a literal
	// index, then a promoted field.
	platoonLeaderHealth Path[game, int32]
	// platoonFlag crosses a slice, then an array, each at a hole.
	platoonFlag Path[game, float32]
	// reserveHealth crosses an array, then a slice, each at a hole.
	reserveHealth Path[game, int32]
	// secondRowHeight crosses a slice of slices at a literal index, then at
	// a hole.
	secondRowHeight Path[game, float32]
	// unitByNamePosition crosses a map whose keys are interned, at a hole.
	unitByNamePosition Path[game, vec2]
	// unitByIDPosition crosses a map of integer keys at a hole.
	unitByIDPosition Path[game, vec2]
	// seventhUnitName crosses a map of integer keys at a literal key, so it
	// has no hole, and does not overlap unitByIDPosition.
	seventhUnitName Path[game, string]
	// elevation crosses a map of struct keys, which are interned.
	elevation Path[game, float32]
	// morale crosses a map keyed by an integer narrower than a hole.
	morale Path[game, float32]
	// bounty crosses a map keyed by an unsigned integer as wide as a hole.
	bounty Path[game, float32]
	// garrisonHealth crosses a map, then a slice, then a promoted field.
	garrisonHealth Path[game, int32]
	// regionElevation crosses a map, then a map.
	regionElevation Path[game, float32]
	// chronicleEntry crosses a map that keeps its keys and elements through
	// pointers, then an array.
	chronicleEntry Path[game, float32]
	// platoonDrill crosses a slice, then a map keyed by an int.
	platoonDrill Path[game, float32]
}

// makeGamePathSet registers game and makes a gamePathSet over it.
func makeGamePathSet(configurator *kernel.Configurator) gamePathSet {
	RegisterModel[game, depth](configurator)
	return gamePathSet{
		turn:                  MakePath[game, int32](configurator, "Turn"),
		position:              MakePath[game, vec2](configurator, "Hero.Position"),
		health:                MakePath[game, int32](configurator, "Hero.Health"),
		name:                  MakePath[game, string](configurator, "Hero.Name"),
		cameraY:               MakePath[game, float32](configurator, "Camera.Y"),
		memberPosition:        MakePath[game, vec2](configurator, "Squads[].Members[].Position"),
		lastMemberHealth:      MakePath[game, int32](configurator, "Squads[].Members[2].Health"),
		firstBanner:           MakePath[game, float32](configurator, "Squads[0].Banner"),
		secondBanner:          MakePath[game, float32](configurator, "Squads[1].Banner"),
		terrain:               MakePath[game, float32](configurator, "Terrain[][]"),
		unitPosition:          MakePath[game, vec2](configurator, "Units[].Position"),
		firstUnitName:         MakePath[game, string](configurator, "Units[0].Name"),
		secondUnitName:        MakePath[game, string](configurator, "Units[1].Name"),
		platoonMemberPosition: MakePath[game, vec2](configurator, "Platoons[].Members[].Position"),
		platoonLeaderHealth:   MakePath[game, int32](configurator, "Platoons[].Members[0].Health"),
		platoonFlag:           MakePath[game, float32](configurator, "Platoons[].Flags[]"),
		reserveHealth:         MakePath[game, int32](configurator, "Squads[].Reserves[].Health"),
		secondRowHeight:       MakePath[game, float32](configurator, "Heights[1][]"),
		unitByNamePosition:    MakePath[game, vec2](configurator, "UnitsByName[].Position"),
		unitByIDPosition:      MakePath[game, vec2](configurator, "UnitsByID[].Position"),
		seventhUnitName:       MakePath[game, string](configurator, "UnitsByID[7].Name"),
		elevation:             MakePath[game, float32](configurator, "Elevations[]"),
		morale:                MakePath[game, float32](configurator, "Morale[]"),
		bounty:                MakePath[game, float32](configurator, "Bounties[]"),
		garrisonHealth:        MakePath[game, int32](configurator, "Garrisons[][].Health"),
		regionElevation:       MakePath[game, float32](configurator, "Regions[][]"),
		chronicleEntry:        MakePath[game, float32](configurator, "Chronicles[][]"),
		platoonDrill:          MakePath[game, float32](configurator, "Platoons[].Drills[]"),
	}
}

// spawnGamePathSet spawns a kernel of mux's plugin and a test plugin making
// a gamePathSet, and answers it with both sides of game's timeline: the
// reading side, which looks keys up, and the writing side, which interns
// them.
func spawnGamePathSet(t *testing.T) (gamePathSet, Timeline[game, depth], WriteTimeline[game, depth]) {
	var paths gamePathSet
	var timeline kernel.Write[Timeline[game, depth]]
	spawn(t, func(configurator *kernel.Configurator) {
		paths = makeGamePathSet(configurator)
		timeline = declare[kernel.Write[Timeline[game, depth]]](configurator)
	})
	return paths, timeline.Get(), ElevateTimeline(timeline)
}

func TestPathGetAnswersTheModelsOwnValue(t *testing.T) {
	paths, _, _ := spawnGamePathSet(t)
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
	expectGet(t, "Units[1].Position", paths.unitPosition, model, vec2{X: 9, Y: 10}, 1)
	expectGet(t, "Units[0].Name", paths.firstUnitName, model, "bo")
	expectGet(t, "Units[1].Name", paths.secondUnitName, model, "cy")
	expectGet(t, "Platoons[0].Members[0].Position", paths.platoonMemberPosition, model, vec2{X: 11, Y: 12}, 0, 0)
	expectGet(t, "Platoons[0].Members[0].Health", paths.platoonLeaderHealth, model, 80, 0)
	expectGet(t, "Platoons[0].Flags[0]", paths.platoonFlag, model, 0.75, 0, 0)
	expectGet(t, "Platoons[1].Flags[1]", paths.platoonFlag, model, 1.5, 1, 1)
	expectGet(t, "Squads[1].Reserves[1].Health", paths.reserveHealth, model, 70, 1, 1)
	expectGet(t, "Heights[1][2]", paths.secondRowHeight, model, 5, 2)
	copied := *model
	copied.Hero.Position.Y = 8
	copied.Terrain[1][0] = 9
	copied.Units = []unit{{Name: "di"}}
	expectGet(t, "Hero.Position of a copy", paths.position, &copied, vec2{X: 1, Y: 8})
	expectGet(t, "Terrain[1][0] of a copy", paths.terrain, &copied, 9, 1, 0)
	expectGet(t, "Units[0].Name of a copy given its own slice", paths.firstUnitName, &copied, "di")
	expectGet(t, "Units[0].Name of the model the copy was made from", paths.firstUnitName, model, "bo")
}

func TestPathGetAnswersFalseForAnIndexOutsideItsArrayOrSlice(t *testing.T) {
	paths, _, _ := spawnGamePathSet(t)
	model := newTestGame()
	for _, holes := range [][]int64{{2, 0}, {0, 3}, {-1, 0}, {0, -1}, {1 << 40, 0}, {2, 3}} {
		expectMiss(t, "Terrain[][]", paths.terrain, model, holes...)
	}
	expectMiss(t, "Squads[].Members[2].Health", paths.lastMemberHealth, model, 2)
	for _, holes := range [][]int64{{2}, {-1}, {1 << 40}, {-1 << 63}} {
		expectMiss(t, "Units[].Position", paths.unitPosition, model, holes...)
	}
	// The second platoon's members are a nil slice.
	for _, holes := range [][]int64{{1, 0}, {0, 1}, {2, 0}, {0, -1}} {
		expectMiss(t, "Platoons[].Members[].Position", paths.platoonMemberPosition, model, holes...)
	}
	expectMiss(t, "Platoons[].Members[0].Health, past the end of a nil slice,", paths.platoonLeaderHealth, model, 1)
	for _, holes := range [][]int64{{0, 2}, {2, 0}} {
		expectMiss(t, "Platoons[].Flags[]", paths.platoonFlag, model, holes...)
	}
	expectMiss(t, "Squads[].Reserves[].Health, in a nil slice,", paths.reserveHealth, model, 0, 0)
	expectMiss(t, "Heights[1][]", paths.secondRowHeight, model, 3)
	expectMiss(t, "Heights[1][], past the end of one row,", paths.secondRowHeight, &game{Heights: [][]float32{{1}}}, 0)
	expectMiss(t, "Units[0].Name of a game with no units", paths.firstUnitName, &game{})
}

func TestPathGetReadsThroughMaps(t *testing.T) {
	paths, _, timeline := spawnGamePathSet(t)
	model := newTestGame()
	bo := timeline.Intern(unitName("bo"))
	north := timeline.Intern(unitName("north"))
	expectGet(t, `UnitsByName["bo"].Position`, paths.unitByNamePosition, model, vec2{X: 13, Y: 14}, bo)
	expectGet(t, "UnitsByID[7].Position", paths.unitByIDPosition, model, vec2{X: 15, Y: 16}, 7)
	expectGet(t, "UnitsByID[4294967295].Position", paths.unitByIDPosition, model, vec2{}, 1<<32-1)
	expectGet(t, "UnitsByID[7].Name", paths.seventhUnitName, model, "di")
	expectGet(t, "Elevations[{1 2}]", paths.elevation, model, 0.5, timeline.Intern(cell{Column: 1, Row: 2}))
	expectGet(t, "Elevations[{-1 0}]", paths.elevation, model, 1.5, timeline.Intern(cell{Column: -1}))
	expectGet(t, "Morale[-128]", paths.morale, model, 0.25, -128)
	expectGet(t, "Morale[3]", paths.morale, model, 0.75, 3)
	// A uint64 key past the largest int64 is the hole its bits make.
	expectGet(t, "Bounties[18446744073709551615], at the hole -1,", paths.bounty, model, 2.5, -1)
	expectGet(t, "Bounties[4]", paths.bounty, model, 3.5, 4)
	expectGet(t, `Garrisons["north"][1].Health`, paths.garrisonHealth, model, 25, north, 1)
	expectGet(t, "Regions[2][{3 4}]", paths.regionElevation, model, 6.5, 2, timeline.Intern(cell{Column: 3, Row: 4}))
	expectGet(t, "Chronicles[{1 [... 9]}][35]", paths.chronicleEntry, model, 7.5, timeline.Intern(chronicle{Era: 1, Notes: [16]int64{15: 9}}), 35)
	expectGet(t, "Platoons[0].Drills[-5]", paths.platoonDrill, model, 0.125, 0, -5)
	copied := *model
	copied.UnitsByName = map[unitName]unit{"bo": {Position: vec2{X: 17}}}
	expectGet(t, `UnitsByName["bo"].Position of a copy given its own map`, paths.unitByNamePosition, &copied, vec2{X: 17}, bo)
	expectGet(t, `UnitsByName["bo"].Position of the model the copy was made from`, paths.unitByNamePosition, model, vec2{X: 13, Y: 14}, bo)
}

func TestPathGetAnswersFalseForAKeyItsMapLacks(t *testing.T) {
	paths, _, timeline := spawnGamePathSet(t)
	model := newTestGame()
	bo := timeline.Intern(unitName("bo"))
	cy := timeline.Intern(unitName("cy"))
	north := timeline.Intern(unitName("north"))
	zed := timeline.Intern(unitName("zed"))
	origin := timeline.Intern(cell{})
	plateau := timeline.Intern(cell{Column: 3, Row: 4})
	expectMiss(t, `UnitsByName["zed"].Position, of a key interned but absent,`, paths.unitByNamePosition, model, zed)
	delete(model.UnitsByName, "cy")
	expectMiss(t, `UnitsByName["cy"].Position, of a key deleted,`, paths.unitByNamePosition, model, cy)
	// No key was interned at these holes.
	for _, hole := range []int64{zed + 1, -1, 1 << 40, -1 << 63} {
		expectMiss(t, "UnitsByName[].Position", paths.unitByNamePosition, model, hole)
	}
	expectMiss(t, "UnitsByID[].Position", paths.unitByIDPosition, model, 8)
	// No int8 holds these holes, so none reads the key it narrows to.
	for _, hole := range []int64{128, -129, 3 + 1<<8, 3 - 1<<32, -128 + 1<<62} {
		expectMiss(t, "Morale[]", paths.morale, model, hole)
	}
	expectMiss(t, "Bounties[5]", paths.bounty, model, 5)
	expectMiss(t, `Garrisons["north"][2].Health`, paths.garrisonHealth, model, north, 2)
	expectMiss(t, `Garrisons["bo"][0].Health`, paths.garrisonHealth, model, bo, 0)
	expectMiss(t, "Regions[2][{0 0}]", paths.regionElevation, model, 2, origin)
	expectMiss(t, "Regions[3][{3 4}]", paths.regionElevation, model, 3, plateau)
	expectMiss(t, "Platoons[0].Drills[5]", paths.platoonDrill, model, 0, 5)
	expectMiss(t, "Platoons[1].Drills[-5], in a nil map,", paths.platoonDrill, model, 1, -5)
	delete(model.UnitsByID, 7)
	expectMiss(t, "UnitsByID[7].Name, of a key deleted,", paths.seventhUnitName, model)
	// Every map of an empty game is nil.
	empty := &game{}
	expectMiss(t, `UnitsByName["bo"].Position, in a nil map,`, paths.unitByNamePosition, empty, bo)
	expectMiss(t, "UnitsByID[7].Position, in a nil map,", paths.unitByIDPosition, empty, 7)
	expectMiss(t, "UnitsByID[7].Name, in a nil map,", paths.seventhUnitName, empty)
	expectMiss(t, "Elevations[{0 0}], in a nil map,", paths.elevation, empty, origin)
	expectMiss(t, "Morale[3], in a nil map,", paths.morale, empty, 3)
	expectMiss(t, "Bounties[4], in a nil map,", paths.bounty, empty, 4)
	expectMiss(t, `Garrisons["north"][0].Health, in a nil map,`, paths.garrisonHealth, empty, north, 0)
	expectMiss(t, "Regions[2][{3 4}], in a nil map,", paths.regionElevation, empty, 2, plateau)
}

// TestMapStepsAnswerAsATypedLookupDoes holds every map step to a typed
// lookup over the same map, for string, integer and struct keys, each
// present, deleted or never there. A map step reaches the runtime's own map
// access, outside Go's compatibility promise, so a change in it fails here.
func TestMapStepsAnswerAsATypedLookupDoes(t *testing.T) {
	paths, _, timeline := spawnGamePathSet(t)
	model := &game{
		UnitsByName: map[unitName]unit{},
		UnitsByID:   map[uint32]unit{},
		Elevations:  map[cell]float32{},
		Morale:      map[int8]float32{},
		Bounties:    map[uint64]float32{},
		Chronicles:  map[chronicle]annal{},
	}
	// Enough keys that each map spreads over several tables; every third is
	// deleted again, and keys from keyCount on were never there.
	const keyCount = 3000
	makeID := func(index int) uint32 { return uint32(index) * 2654435761 }
	makeBounty := func(index int) uint64 { return uint64(index) * 0x9E3779B97F4A7C15 }
	makeCell := func(index int) cell { return cell{Column: int16(index), Row: int16(-index)} }
	makeChronicle := func(index int) chronicle { return chronicle{Era: int64(index), Notes: [16]int64{15: int64(-index)}} }
	for index := range keyCount {
		model.UnitsByName[unitName(strconv.Itoa(index))] = unit{Position: vec2{X: float32(index)}}
		model.UnitsByID[makeID(index)] = unit{Position: vec2{Y: float32(index)}}
		model.Elevations[makeCell(index)] = float32(index)
		model.Bounties[makeBounty(index)] = float32(index)
		model.Chronicles[makeChronicle(index)] = annal{35: float32(index)}
	}
	for index := 0; index < keyCount; index += 3 {
		delete(model.UnitsByName, unitName(strconv.Itoa(index)))
		delete(model.UnitsByID, makeID(index))
		delete(model.Elevations, makeCell(index))
		delete(model.Bounties, makeBounty(index))
		delete(model.Chronicles, makeChronicle(index))
	}
	for key := range 256 {
		model.Morale[int8(key)] = float32(key)
	}
	for key := 0; key < 256; key += 3 {
		delete(model.Morale, int8(key))
	}
	for index := range keyCount + keyCount/2 {
		name := unitName(strconv.Itoa(index))
		byName, byNameOK := model.UnitsByName[name]
		expectLookup(t, "UnitsByName", name, paths.unitByNamePosition, model, byName.Position, byNameOK, timeline.Intern(name))
		byID, byIDOK := model.UnitsByID[makeID(index)]
		expectLookup(t, "UnitsByID", makeID(index), paths.unitByIDPosition, model, byID.Position, byIDOK, int64(makeID(index)))
		elevation, elevationOK := model.Elevations[makeCell(index)]
		expectLookup(t, "Elevations", makeCell(index), paths.elevation, model, elevation, elevationOK, timeline.Intern(makeCell(index)))
		bounty, bountyOK := model.Bounties[makeBounty(index)]
		expectLookup(t, "Bounties", makeBounty(index), paths.bounty, model, bounty, bountyOK, int64(makeBounty(index)))
		entries, entriesOK := model.Chronicles[makeChronicle(index)]
		expectLookup(t, "Chronicles", makeChronicle(index), paths.chronicleEntry, model, entries[35], entriesOK, timeline.Intern(makeChronicle(index)), 35)
	}
	for hole := int64(-300); hole <= 300; hole++ {
		morale, moraleOK := model.Morale[int8(hole)]
		if int64(int8(hole)) != hole {
			morale, moraleOK = 0, false
		}
		expectLookup(t, "Morale", hole, paths.morale, model, morale, moraleOK, hole)
	}
}

func TestPathGetPanicsOnAWrongNumberOfHoles(t *testing.T) {
	paths, _, _ := spawnGamePathSet(t)
	model := newTestGame()
	expectPanic(t, `path "Terrain[][]" has 2 holes, but is given 1`, func() { paths.terrain.Get(model, 1) })
	expectPanic(t, `path "Terrain[][]" has 2 holes, but is given 3`, func() { paths.terrain.Get(model, 1, 0, 0) })
	expectPanic(t, `path "Squads[].Members[2].Health" has 1 hole, but is given 0`, func() { paths.lastMemberHealth.Get(model) })
	expectPanic(t, `path "Squads[1].Banner" has 0 holes, but is given 1`, func() { paths.secondBanner.Get(model, 1) })
	expectPanic(t, `path "Turn" has 0 holes, but is given 2`, func() { paths.turn.Get(model, 0, 0) })
	expectPanic(t, `path "Units[0].Name" has 0 holes, but is given 1`, func() { paths.firstUnitName.Get(model, 0) })
	expectPanic(t, `path "Platoons[].Members[0].Health" has 1 hole, but is given 2`, func() { paths.platoonLeaderHealth.Get(model, 0, 0) })
	expectPanic(t, `path "Heights[1][]" has 1 hole, but is given 0`, func() { paths.secondRowHeight.Get(model) })
}

// positionSink, nameSink, memberPositionSink, terrainSink and healthSink
// keep what TestPathGetAllocatesNothing reads, so the compiler cannot drop
// the reads it measures.
var (
	positionSink       vec2
	nameSink           string
	memberPositionSink vec2
	terrainSink        float32
	healthSink         int32
)

func TestPathGetAllocatesNothing(t *testing.T) {
	paths, _, timeline := spawnGamePathSet(t)
	model := newTestGame()
	bo := timeline.Intern(unitName("bo"))
	zed := timeline.Intern(unitName("zed"))
	north := timeline.Intern(unitName("north"))
	plateau := timeline.Intern(cell{Column: 3, Row: 4})
	chronicleIndex := timeline.Intern(chronicle{Era: 1, Notes: [16]int64{15: 9}})
	allocations := testing.AllocsPerRun(100, func() {
		positionSink, _ = paths.unitByNamePosition.Get(model, bo)
		positionSink, _ = paths.unitByNamePosition.Get(model, zed)
		positionSink, _ = paths.unitByIDPosition.Get(model, 7)
		nameSink, _ = paths.seventhUnitName.Get(model)
		terrainSink, _ = paths.morale.Get(model, -128)
		terrainSink, _ = paths.morale.Get(model, 300)
		terrainSink, _ = paths.regionElevation.Get(model, 2, plateau)
		terrainSink, _ = paths.chronicleEntry.Get(model, chronicleIndex, 35)
		healthSink, _ = paths.garrisonHealth.Get(model, north, 1)
		positionSink, _ = paths.position.Get(model)
		nameSink, _ = paths.name.Get(model)
		memberPositionSink, _ = paths.memberPosition.Get(model, 1, 2)
		terrainSink, _ = paths.terrain.Get(model, 2, 0)
		positionSink, _ = paths.unitPosition.Get(model, 1)
		nameSink, _ = paths.firstUnitName.Get(model)
		memberPositionSink, _ = paths.platoonMemberPosition.Get(model, 0, 0)
		terrainSink, _ = paths.secondRowHeight.Get(model, 2)
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
		{"a hole over a literal slice index", `paths "Units[0].Name" and "Units[].Name" of model mux.game overlap`,
			func(configurator *kernel.Configurator) {
				MakePath[game, string](configurator, "Units[0].Name")
				MakePath[game, string](configurator, "Units[].Name")
			}},
		{"a literal key over a hole", `paths "UnitsByID[7].Position" and "UnitsByID[].Position" of model mux.game overlap`,
			func(configurator *kernel.Configurator) {
				MakePath[game, vec2](configurator, "UnitsByID[7].Position")
				MakePath[game, vec2](configurator, "UnitsByID[].Position")
			}},
		{"a literal index of a map whose keys are interned", `path "UnitsByName[0].Name" of model mux.game indexes map[mux.unitName]mux.unit at 0, but a map whose keys are interned takes only a hole`,
			func(configurator *kernel.Configurator) { MakePath[game, string](configurator, "UnitsByName[0].Name") }},
		{"a literal key no key of the map holds", `path "Morale[128]" of model mux.game indexes map[int8]float32 at 128, which no int8 holds`,
			func(configurator *kernel.Configurator) { MakePath[game, float32](configurator, "Morale[128]") }},
		{"a pointer key", `path "Wards[]" of model mux.game crosses a key of map[*mux.unit]float32, a pointer *mux.unit`,
			func(configurator *kernel.Configurator) { MakePath[game, float32](configurator, "Wards[]") }},
		{"an interface key", `path "Rumours[]" of model mux.game crosses a key of map[interface {}]float32, an interface interface {}`,
			func(configurator *kernel.Configurator) { MakePath[game, float32](configurator, "Rumours[]") }},
		{"a pointer element of a map", `path "Captains[].Name" of model mux.game crosses an element of map[mux.unitName]*mux.unit, a pointer *mux.unit`,
			func(configurator *kernel.Configurator) { MakePath[game, string](configurator, "Captains[].Name") }},
		{"an index of a number", `path "Turn[]" of model mux.game reaches int32, which is no array, slice or map to index`,
			func(configurator *kernel.Configurator) { MakePath[game, int32](configurator, "Turn[]") }},
		{"a literal slice index past any int", `path "Units[9223372036854775808].Name" of model mux.game indexes []mux.unit with "9223372036854775808", which is no index`,
			func(configurator *kernel.Configurator) {
				MakePath[game, string](configurator, "Units[9223372036854775808].Name")
			}},
		{"a pointer element of a slice", `path "Scouts[].Name" of model mux.game crosses an element of []*mux.unit, a pointer *mux.unit`,
			func(configurator *kernel.Configurator) { MakePath[game, string](configurator, "Scouts[].Name") }},
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
				// A literal slice index takes a step, but no hole.
				MakePath[game, float32](configurator, "Heights[1][]")
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

// expectLookup fails t unless path answers, over model at the address holes
// fill, what a typed lookup of key in the map mapName answered: want and
// wantOK.
func expectLookup[T comparable](t *testing.T, mapName string, key any, path Path[game, T], model *game, want T, wantOK bool, holes ...int64) {
	t.Helper()
	if got, ok := path.Get(model, holes...); got != want || ok != wantOK {
		t.Fatalf("%s[%v] through %q answered %v, %v, but a typed lookup answers %v, %v", mapName, key, path.compiled.text, got, ok, want, wantOK)
	}
}

// expectMiss fails t unless path answers T's zero value, and false, over
// model at the address holes fill.
func expectMiss[T comparable](t *testing.T, name string, path Path[game, T], model *game, holes ...int64) {
	t.Helper()
	var zero T
	if got, ok := path.Get(model, holes...); got != zero || ok {
		t.Errorf("%s at %v answered %v, %v, want %v, false", name, holes, got, ok, zero)
	}
}
