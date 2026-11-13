package mux

import (
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
)

// timelineProbe is game registered with paths to tween and read, and handles
// on its model and timeline declared as a job's are: a test drives updates
// and views through them, and Step through the kernel.
type timelineProbe struct {
	harness  *harness
	model    kernel.Write[*game]
	timeline kernel.Write[Timeline[game, depth]]
	muxTasks Tasks[game, depth]
	cameraY  Path[game, float32]
	turn     Path[game, int32]
	name     Path[game, string]
	visible  Path[game, bool]
	terrain  Path[game, float32]
	// unitHealth crosses a slice at a hole.
	unitHealth Path[game, int32]
}

// spawnTimelineProbe spawns a kernel of mux's plugin and a timelineProbe,
// whose Init then runs init unless it is nil.
func spawnTimelineProbe(t *testing.T, init func(configurator *kernel.Configurator)) *timelineProbe {
	probe := &timelineProbe{}
	probe.harness = spawn(t, func(configurator *kernel.Configurator) {
		RegisterModel[game, depth](configurator)
		probe.cameraY = MakePath[game, float32](configurator, "Camera.Y")
		probe.turn = MakePath[game, int32](configurator, "Turn")
		probe.name = MakePath[game, string](configurator, "Hero.Name")
		probe.visible = MakePath[game, bool](configurator, "Hero.Visible")
		probe.terrain = MakePath[game, float32](configurator, "Terrain[][]")
		probe.unitHealth = MakePath[game, int32](configurator, "Units[].Health")
		probe.model = declare[kernel.Write[*game]](configurator)
		probe.timeline = declare[kernel.Write[Timeline[game, depth]]](configurator)
		probe.muxTasks, _ = configurator.Resource[Tasks[game, depth]]()
		if init != nil {
			init(configurator)
		}
	})
	return probe
}

// runStep runs Step with delta, as a job.
func (p *timelineProbe) runStep(delta time.Duration) {
	p.harness.run(func(executor *kernel.Executor) kernel.JobHandle {
		return executor.Schedule(p.muxTasks.Step, delta).Handle()
	})
}

// stepAndRead runs Step with delta, then reads the camera's Y as a view
// does, failing t unless Current answers true.
func (p *timelineProbe) stepAndRead(t *testing.T, delta time.Duration) float32 {
	t.Helper()
	p.runStep(delta)
	value, ok := p.timeline.Get().Current(p.cameraY, p.model.Get())
	if !ok {
		t.Fatalf("Current answered %v, false for the camera's Y at %v, want true", value, p.timeline.Get().Now())
	}
	return value
}

// expectTween tweens path from from to to over a second from Now, eased by
// easing, and fails t unless its current value is want once at has passed.
// It then steps on to the tween's end.
func (p *timelineProbe) expectTween[T comparable](t *testing.T, path Path[game, T], from, to T, easing Easing, at time.Duration, want T) {
	t.Helper()
	timeline := ElevateTimeline(p.timeline)
	timeline.Tween(path, timeline.Now(), from, to, time.Second, easing)
	p.runStep(at)
	if got, _ := p.timeline.Get().Current(path, p.model.Get()); got != want {
		t.Errorf("%q tweened from %v to %v by easing %d is current at %v after %v, want %v", path.compiled.text, from, to, easing, got, at, want)
	}
	p.runStep(time.Second - at)
}

// expectCurrent fails t unless path's current value at the address holes
// fill is want, and true.
func (p *timelineProbe) expectCurrent[T comparable](t *testing.T, path Path[game, T], want T, holes ...int64) {
	t.Helper()
	if got, ok := p.timeline.Get().Current(path, p.model.Get(), holes...); got != want || !ok {
		t.Errorf("%q%v is current at %v, %v at %v, want %v, true", path.compiled.text, holes, got, ok, p.timeline.Get().Now(), want)
	}
}

// expectSteps runs Step with each delta in turn, failing t unless the
// camera's Y is then current at the value wanted.
func (p *timelineProbe) expectSteps(t *testing.T, steps []timedValue) {
	t.Helper()
	for _, step := range steps {
		if got := p.stepAndRead(t, step.delta); got != step.want {
			t.Fatalf("the camera's Y is current at %v at %v, want %v", got, p.timeline.Get().Now(), step.want)
		}
	}
}

// timedValue is a Step's delta and the current value a view should read
// after it.
type timedValue struct {
	delta time.Duration
	want  float32
}

func TestTweenCatchesTheModelUpWithNoJump(t *testing.T) {
	probe := spawnTimelineProbe(t, nil)
	probe.model.Get().Camera.Y = 10
	probe.model.Get().Turn = 7
	ElevateTimeline(probe.timeline).Tween(probe.cameraY, 0, 0, 10, time.Second, EasingLinear)
	probe.expectSteps(t, []timedValue{
		{250 * time.Millisecond, 2.5},
		{250 * time.Millisecond, 5},
		{375 * time.Millisecond, 8.75},
		{125 * time.Millisecond, 10},
		{time.Second, 10},
	})
	if turn, ok := probe.timeline.Get().Current(probe.turn, probe.model.Get()); turn != 7 || !ok {
		t.Fatalf("Current answered %v, %v on a path never tweened, want the model's 7, true", turn, ok)
	}
}

func TestTweenShowsOnlyWithinItsSpan(t *testing.T) {
	probe := spawnTimelineProbe(t, nil)
	// The model holds neither end of the tween, so each read tells whose value
	// it is.
	probe.model.Get().Camera.Y = 10
	ElevateTimeline(probe.timeline).Tween(probe.cameraY, time.Second, 2, 6, 2*time.Second, EasingLinear)
	probe.expectSteps(t, []timedValue{
		{500 * time.Millisecond, 10},
		{500 * time.Millisecond, 2},
		{500 * time.Millisecond, 3},
		{time.Second, 5},
		{500 * time.Millisecond, 10},
		{time.Second, 10},
	})
}

func TestTweenStartedInThePastBeginsPartway(t *testing.T) {
	probe := spawnTimelineProbe(t, nil)
	probe.expectSteps(t, []timedValue{{time.Second, 0}})
	timeline := ElevateTimeline(probe.timeline)
	timeline.Tween(probe.cameraY, timeline.Now()-500*time.Millisecond, 0, 10, time.Second, EasingLinear)
	probe.expectSteps(t, []timedValue{{0, 5}, {250 * time.Millisecond, 7.5}})
}

func TestTweensOnElementsOfOnePathKeepSeparateCurrentValues(t *testing.T) {
	probe := spawnTimelineProbe(t, nil)
	probe.model.Get().Terrain = [2][3]float32{{1, 2, 3}, {4, 5, 6}}
	timeline := ElevateTimeline(probe.timeline)
	timeline.Tween(probe.terrain, 0, 0, 10, time.Second, EasingLinear, 0, 1)
	timeline.Tween(probe.terrain, 0, 20, 40, time.Second, EasingLinear, 1, 2)
	probe.runStep(500 * time.Millisecond)
	testCases := []struct {
		holes  []int64
		want   float32
		wantOK bool
	}{
		{[]int64{0, 1}, 5, true},
		{[]int64{1, 2}, 30, true},
		// No override is on this element, though others of its path have some.
		{[]int64{1, 1}, 5, true},
		{[]int64{0, 0}, 1, true},
		// Outside the arrays, with no override there.
		{[]int64{2, 1}, 0, false},
		{[]int64{0, 3}, 0, false},
		{[]int64{-1, 1}, 0, false},
	}
	for _, testCase := range testCases {
		if got, ok := probe.timeline.Get().Current(probe.terrain, probe.model.Get(), testCase.holes...); got != testCase.want || ok != testCase.wantOK {
			t.Errorf("Terrain%v is current at %v, %v, want %v, %v", testCase.holes, got, ok, testCase.want, testCase.wantOK)
		}
	}
}

func TestGetAndCurrentReadTheArrayASliceMovedTo(t *testing.T) {
	probe := spawnTimelineProbe(t, nil)
	model := probe.model.Get()
	model.Units = []unit{{vitality: vitality{Health: 10}}, {vitality: vitality{Health: 20}}}
	timeline := ElevateTimeline(probe.timeline)
	timeline.Tween(probe.unitHealth, 0, 0, 100, time.Second, EasingLinear, 0)
	probe.runStep(500 * time.Millisecond)
	// The slice is full, so appending moves it to a new array, and the
	// element changed there is not changed in the old one.
	oldArray := &model.Units[0]
	model.Units = append(model.Units, unit{vitality: vitality{Health: 30}})
	if &model.Units[0] == oldArray {
		t.Fatal("appending to a full slice kept its array, so nothing moved")
	}
	model.Units[1].Health = 25
	testCases := []struct {
		hole   int64
		want   int32
		wantOK bool
	}{
		{0, 50, true},
		{1, 25, true},
		{2, 30, true},
		{3, 0, false},
	}
	for _, testCase := range testCases {
		if got, ok := probe.timeline.Get().Current(probe.unitHealth, model, testCase.hole); got != testCase.want || ok != testCase.wantOK {
			t.Errorf("Units[%d].Health is current at %v, %v, want %v, %v", testCase.hole, got, ok, testCase.want, testCase.wantOK)
		}
	}
	for _, testCase := range testCases[1:] {
		if got, ok := probe.unitHealth.Get(model, testCase.hole); got != testCase.want || ok != testCase.wantOK {
			t.Errorf("Units[%d].Health answered %v, %v, want %v, %v", testCase.hole, got, ok, testCase.want, testCase.wantOK)
		}
	}
}

func TestTweenPanicsNamingWhatIsWrong(t *testing.T) {
	probe := spawnTimelineProbe(t, nil)
	timeline := ElevateTimeline(probe.timeline)
	expectPanic(t, `path "Camera.Y" is tweened over 0s, which is no positive length`, func() {
		timeline.Tween(probe.cameraY, 0, 0, 1, 0, EasingLinear)
	})
	expectPanic(t, `path "Camera.Y" is tweened over -1s, which is no positive length`, func() {
		timeline.Tween(probe.cameraY, 0, 0, 1, -time.Second, EasingLinear)
	})
	expectPanic(t, `path "Camera.Y" is tweened by easing 31, which is not registered`, func() {
		timeline.Tween(probe.cameraY, 0, 0, 1, time.Second, Easing(31))
	})
	expectPanic(t, `path "Hero.Name" is tweened, but no interpolator tweens its string`, func() {
		timeline.Tween(probe.name, 0, "ada", "bob", time.Second, EasingLinear)
	})
	expectPanic(t, `path "Camera.Y" has 0 holes, but is given 1`, func() {
		timeline.Tween(probe.cameraY, 0, 0, 1, time.Second, EasingLinear, 3)
	})
	expectPanic(t, `path "Camera.Y" has 0 holes, but is given 2`, func() {
		probe.timeline.Get().Current(probe.cameraY, probe.model.Get(), 3, 4)
	})
	expectPanic(t, `path "Terrain[][]" has 2 holes, but is given 1`, func() {
		timeline.Tween(probe.terrain, 0, 0, 1, time.Second, EasingLinear, 1)
	})
	expectPanic(t, `path "Terrain[][]" has 2 holes, but is given 0`, func() {
		probe.timeline.Get().Current(probe.terrain, probe.model.Get())
	})
	// A Current where a tween is active checks the count too.
	timeline.Tween(probe.terrain, 0, 0, 1, time.Second, EasingLinear, 1, 1)
	probe.runStep(time.Millisecond)
	expectPanic(t, `path "Terrain[][]" has 2 holes, but is given 3`, func() {
		probe.timeline.Get().Current(probe.terrain, probe.model.Get(), 1, 1, 0)
	})
}

// cameraYSink, headingSink, gaugeSink, turnSink, the terrain, unit and
// reserve sinks and bannerSink keep what
// TestTweenStepAndCurrentAllocateNothing reads, so the compiler cannot drop
// the reads it measures.
var (
	cameraYSink          float32
	headingSink          heading
	gaugeSink            int32
	turnSink             int32
	tweenedTerrainSink   float32
	untweenedTerrainSink float32
	bannerSink           float32
	tweenedUnitSink      int32
	untweenedUnitSink    int32
	reserveSink          int32
)

func TestTweenStepAndCurrentAllocateNothing(t *testing.T) {
	var headingPath Path[game, heading]
	var gaugePath Path[game, int32]
	var bannerPath Path[game, float32]
	var reservePath Path[game, int32]
	probe := spawnTimelineProbe(t, func(configurator *kernel.Configurator) {
		headingPath = MakePath[game, heading](configurator, "Gauge.Heading")
		gaugePath = MakePath[game, int32](configurator, "Gauge.Int32")
		bannerPath = MakePath[game, float32](configurator, "Squads[].Banner")
		reservePath = MakePath[game, int32](configurator, "Squads[].Reserves[].Health")
		RegisterInterpolator(configurator, turnShortestWay)
	})
	timeline := ElevateTimeline(probe.timeline)
	// Each round tweens over two Steps, so the round before's tweens end at
	// this round's Step and the timeline stays at the size the first rounds
	// grew it to. The easing is one of Penner's, so the sample eases by a
	// registered curve, and the paths' interpolators are float32's and int32's
	// built in and heading's registered. Current reads the active tweens, an
	// element of a tweened path with none, and paths with none, with and
	// without holes, through arrays and slices.
	probe.model.Get().Terrain[0][0] = 7
	probe.model.Get().Units = []unit{{vitality: vitality{Health: 6}}, {}}
	probe.model.Get().Squads[1].Reserves = []unit{{vitality: vitality{Health: 9}}}
	round := func() {
		timeline.Tween(probe.cameraY, timeline.Now(), 0, 10, 2*time.Millisecond, EasingQuadInOut)
		timeline.Tween(headingPath, timeline.Now(), 0, 10, 2*time.Millisecond, EasingQuadInOut)
		timeline.Tween(gaugePath, timeline.Now(), 0, 10, 2*time.Millisecond, EasingQuadInOut)
		timeline.Tween(probe.terrain, timeline.Now(), 0, 10, 2*time.Millisecond, EasingQuadInOut, 1, 2)
		timeline.Tween(probe.unitHealth, timeline.Now(), 0, 10, 2*time.Millisecond, EasingQuadInOut, 1)
		step(nil, time.Millisecond, probe.model, probe.timeline)
		cameraYSink, _ = probe.timeline.Get().Current(probe.cameraY, probe.model.Get())
		headingSink, _ = probe.timeline.Get().Current(headingPath, probe.model.Get())
		gaugeSink, _ = probe.timeline.Get().Current(gaugePath, probe.model.Get())
		turnSink, _ = probe.timeline.Get().Current(probe.turn, probe.model.Get())
		tweenedTerrainSink, _ = probe.timeline.Get().Current(probe.terrain, probe.model.Get(), 1, 2)
		untweenedTerrainSink, _ = probe.timeline.Get().Current(probe.terrain, probe.model.Get(), 0, 0)
		bannerSink, _ = probe.timeline.Get().Current(bannerPath, probe.model.Get(), 1)
		tweenedUnitSink, _ = probe.timeline.Get().Current(probe.unitHealth, probe.model.Get(), 1)
		untweenedUnitSink, _ = probe.timeline.Get().Current(probe.unitHealth, probe.model.Get(), 0)
		reserveSink, _ = probe.timeline.Get().Current(reservePath, probe.model.Get(), 1, 0)
	}
	round()
	round()
	if cameraYSink != 5 || headingSink != 5 || gaugeSink != 5 || tweenedTerrainSink != 5 || tweenedUnitSink != 5 {
		t.Fatalf("the tweens are current at %v, %v, %v, %v and %v, want 5, each InOut tween halfway", cameraYSink, headingSink, gaugeSink, tweenedTerrainSink, tweenedUnitSink)
	}
	if untweenedTerrainSink != 7 || untweenedUnitSink != 6 || reserveSink != 9 {
		t.Fatalf("Terrain[0][0], Units[0].Health and Squads[1].Reserves[0].Health are current at %v, %v and %v, want the model's 7, 6 and 9", untweenedTerrainSink, untweenedUnitSink, reserveSink)
	}
	if allocations := testing.AllocsPerRun(100, round); allocations != 0 {
		t.Fatalf("Tween, Step and Current allocated %v times per round", allocations)
	}
}

func TestInternGivesEachKeyOneIndexThatIndexFinds(t *testing.T) {
	_, timeline, writeTimeline := spawnGamePathSet(t)
	testCases := []struct {
		name string
		got  int64
		want int64
	}{
		{`Intern(unitName("bo"))`, writeTimeline.Intern(unitName("bo")), 0},
		{`Intern(unitName("cy"))`, writeTimeline.Intern(unitName("cy")), 1},
		{`Intern(unitName("bo")) again`, writeTimeline.Intern(unitName("bo")), 0},
		// Each key type numbers its own keys.
		{"Intern(cell{})", writeTimeline.Intern(cell{}), 0},
		{"Intern(chronicle{})", writeTimeline.Intern(chronicle{}), 0},
		{"Intern(cell{Row: 1})", writeTimeline.Intern(cell{Row: 1}), 1},
		{`Intern(unitName("di"))`, writeTimeline.Intern(unitName("di")), 2},
	}
	for _, testCase := range testCases {
		if testCase.got != testCase.want {
			t.Errorf("%s answered %d, want %d", testCase.name, testCase.got, testCase.want)
		}
	}
	expectIndex(t, timeline, unitName("cy"), 1, true)
	expectIndex(t, timeline, unitName("di"), 2, true)
	expectIndex(t, timeline, unitName("zed"), 0, false)
	expectIndex(t, timeline, cell{Row: 1}, 1, true)
	expectIndex(t, timeline, cell{Column: 1}, 0, false)
}

// expectIndex fails t unless timeline answers want and wantOK for key's
// index.
func expectIndex[K comparable](t *testing.T, timeline Timeline[game, depth], key K, want int64, wantOK bool) {
	t.Helper()
	if got, ok := timeline.Index(key); got != want || ok != wantOK {
		t.Errorf("Index(%#v) answered %d, %v, want %d, %v", key, got, ok, want, wantOK)
	}
}

func TestInternAndIndexPanicForAKeyTypeNoPathsMapInterns(t *testing.T) {
	_, timeline, writeTimeline := spawnGamePathSet(t)
	// game's maps key units by unitName, never by a plain string.
	expectPanic(t, "a string key is interned, but no path of model mux.game crosses a map interning string keys", func() {
		writeTimeline.Intern("bob")
	})
	expectPanic(t, "a string key is looked up, but no path of model mux.game crosses a map interning string keys", func() {
		timeline.Index("bob")
	})
	// An integer key is its own hole, so no map interns one.
	expectPanic(t, "a uint32 key is interned, but no path of model mux.game crosses a map interning uint32 keys", func() {
		writeTimeline.Intern(uint32(7))
	})
	// Only a map a path crosses interns keys.
	expectPanic(t, "a *mux.unit key is looked up, but no path of model mux.game crosses a map interning *mux.unit keys", func() {
		timeline.Index(&unit{})
	})
}

func TestTweenOnAMapElementShowsAtItsKeysIndex(t *testing.T) {
	var elevation Path[game, float32]
	var morale Path[game, float32]
	probe := spawnTimelineProbe(t, func(configurator *kernel.Configurator) {
		elevation = MakePath[game, float32](configurator, "Elevations[]")
		morale = MakePath[game, float32](configurator, "Morale[]")
	})
	model := probe.model.Get()
	model.Elevations = map[cell]float32{{Column: 1}: 3, {Column: 2}: 4}
	model.Morale = map[int8]float32{-1: 5}
	timeline := ElevateTimeline(probe.timeline)
	east := timeline.Intern(cell{Column: 1})
	west := timeline.Intern(cell{Column: 2})
	timeline.Tween(elevation, 0, 0, 10, time.Second, EasingLinear, east)
	timeline.Tween(morale, 0, 0, 10, time.Second, EasingLinear, -1)
	probe.runStep(250 * time.Millisecond)
	testCases := []struct {
		name   string
		path   Path[game, float32]
		hole   int64
		want   float32
		wantOK bool
	}{
		{"Elevations[{1 0}]", elevation, east, 2.5, true},
		{"Elevations[{2 0}]", elevation, west, 4, true},
		{"Elevations[] at a hole no key was interned for", elevation, west + 1, 0, false},
		{"Morale[-1]", morale, -1, 2.5, true},
		{"Morale[255], which no int8 holds", morale, 255, 0, false},
	}
	for _, testCase := range testCases {
		if got, ok := probe.timeline.Get().Current(testCase.path, model, testCase.hole); got != testCase.want || ok != testCase.wantOK {
			t.Errorf("%s is current at %v, %v, want %v, %v", testCase.name, got, ok, testCase.want, testCase.wantOK)
		}
	}
}

// elevationSink, indexSink and foundSink keep what
// TestMapStepsInternAndIndexAllocateNothing reads, so the compiler cannot
// drop the reads it measures.
var (
	elevationSink float32
	indexSink     int64
	foundSink     bool
)

func TestMapStepsInternAndIndexAllocateNothing(t *testing.T) {
	var elevation Path[game, float32]
	var unitPosition Path[game, vec2]
	probe := spawnTimelineProbe(t, func(configurator *kernel.Configurator) {
		elevation = MakePath[game, float32](configurator, "Elevations[]")
		unitPosition = MakePath[game, vec2](configurator, "UnitsByName[].Position")
	})
	model := probe.model.Get()
	model.Elevations = map[cell]float32{{Column: 1}: 3, {Column: 2}: 4}
	timeline := ElevateTimeline(probe.timeline)
	east := timeline.Intern(cell{Column: 1})
	west := timeline.Intern(cell{Column: 2})
	timeline.Intern(unitName("bo"))
	// Each round tweens over two Steps, so the round before's tween ends at
	// this round's Step and the timeline stays at the size the first rounds
	// grew it to. Current reads the active tween, an element of the tweened
	// path with none, and a key never interned; Intern and Index take keys
	// known, and Index one never interned.
	round := func() {
		timeline.Tween(elevation, timeline.Now(), 0, 10, 2*time.Millisecond, EasingQuadInOut, east)
		step(nil, time.Millisecond, probe.model, probe.timeline)
		elevationSink, _ = probe.timeline.Get().Current(elevation, model, east)
		elevationSink, _ = probe.timeline.Get().Current(elevation, model, west)
		elevationSink, _ = probe.timeline.Get().Current(elevation, model, west+1)
		indexSink = timeline.Intern(cell{Column: 2})
		indexSink = timeline.Intern(unitName("bo"))
		indexSink, foundSink = probe.timeline.Get().Index(unitName("bo"))
		indexSink, foundSink = probe.timeline.Get().Index(unitName("zed"))
		indexSink, foundSink = probe.timeline.Get().Index(cell{Column: 3})
		positionSink, _ = unitPosition.Get(model, 0)
	}
	round()
	round()
	if got, _ := probe.timeline.Get().Current(elevation, model, east); got != 5 {
		t.Fatalf("Elevations[{1 0}] is current at %v, want 5, the InOut tween halfway", got)
	}
	if allocations := testing.AllocsPerRun(100, round); allocations != 0 {
		t.Fatalf("Tween, Step, Current, Intern and Index allocated %v times per round", allocations)
	}
}

func TestInternOfANewKeyAllocatesOnlyWhileItsTablesGrow(t *testing.T) {
	_, _, timeline := spawnGamePathSet(t)
	const keyCount = 4096
	var nextColumn int16
	// AllocsPerRun interns keyCount new keys once before it counts, and
	// once while it counts, so the tables grow from keyCount keys to twice
	// that.
	allocations := testing.AllocsPerRun(1, func() {
		for range keyCount {
			timeline.Intern(cell{Column: nextColumn, Row: 1})
			nextColumn++
		}
	})
	if allocations >= keyCount/64 {
		t.Fatalf("interning %d new keys allocated %v times, want fewer than one for every 64 keys: only while the tables grow", keyCount, allocations)
	}
}

func TestHoldKeepsItsValueOverItsSpan(t *testing.T) {
	probe := spawnTimelineProbe(t, nil)
	probe.model.Get().Camera.Y = 10
	ElevateTimeline(probe.timeline).Hold(probe.cameraY, time.Second, 3, 2*time.Second)
	probe.expectSteps(t, []timedValue{
		{500 * time.Millisecond, 10},
		{500 * time.Millisecond, 3},
		{time.Second, 3},
		{999 * time.Millisecond, 3},
		{time.Millisecond, 10},
	})
}

func TestHoldWorksOnATypeNoInterpolatorTweens(t *testing.T) {
	var position Path[game, vec2]
	probe := spawnTimelineProbe(t, func(configurator *kernel.Configurator) {
		position = MakePath[game, vec2](configurator, "Hero.Position")
	})
	hero := &probe.model.Get().Hero
	hero.Visible = true
	hero.Name = "ada"
	hero.Position = vec2{X: 1, Y: 2}
	// A bool, a string and a struct, none of which an interpolator tweens,
	// are each held at a value the model does not hold.
	timeline := ElevateTimeline(probe.timeline)
	timeline.Hold(probe.visible, 0, false, time.Second)
	timeline.Hold(probe.name, 0, "bo", time.Second)
	timeline.Hold(position, 0, vec2{X: 3, Y: 4}, time.Second)
	probe.runStep(500 * time.Millisecond)
	probe.expectCurrent(t, probe.visible, false)
	probe.expectCurrent(t, probe.name, "bo")
	probe.expectCurrent(t, position, vec2{X: 3, Y: 4})
	probe.runStep(500 * time.Millisecond)
	probe.expectCurrent(t, probe.visible, true)
	probe.expectCurrent(t, probe.name, "ada")
	probe.expectCurrent(t, position, vec2{X: 1, Y: 2})
}

func TestHoldNeverCallsItsTypesInterpolator(t *testing.T) {
	var position Path[game, vec2]
	interpolatorCalls := 0
	probe := spawnTimelineProbe(t, func(configurator *kernel.Configurator) {
		position = MakePath[game, vec2](configurator, "Hero.Position")
		RegisterInterpolator(configurator, func(a, b vec2, t float32, ease func(a, b, t float32) float32) vec2 {
			interpolatorCalls++
			return vec2{X: ease(a.X, b.X, t), Y: ease(a.Y, b.Y, t)}
		})
	})
	timeline := ElevateTimeline(probe.timeline)
	timeline.Hold(position, 0, vec2{X: 3, Y: 4}, time.Second)
	probe.runStep(500 * time.Millisecond)
	probe.expectCurrent(t, position, vec2{X: 3, Y: 4})
	if interpolatorCalls != 0 {
		t.Fatalf("holding a vec2 called its interpolator %d times, want none", interpolatorCalls)
	}
	// A tween of the path calls the interpolator, so the hold had one to
	// call.
	timeline.Tween(position, timeline.Now(), vec2{}, vec2{X: 2}, time.Second, EasingLinear)
	probe.runStep(500 * time.Millisecond)
	probe.expectCurrent(t, position, vec2{X: 1})
	if interpolatorCalls == 0 {
		t.Fatal("tweening a vec2 never called its interpolator")
	}
}

func TestEndAnswersWhenAnAddressIsNextFree(t *testing.T) {
	probe := spawnTimelineProbe(t, nil)
	timeline := ElevateTimeline(probe.timeline)
	probe.runStep(time.Second)
	// Terrain[0][1]'s tween ends last, though the hold is scheduled after
	// it and starts sooner.
	timeline.Tween(probe.terrain, 2*time.Second, 0, 1, time.Second, EasingLinear, 0, 1)
	timeline.Hold(probe.terrain, time.Second, 5, 500*time.Millisecond, 0, 1)
	timeline.Tween(probe.terrain, 0, 0, 1, 5*time.Second, EasingLinear, 1, 2)
	timeline.Tween(probe.cameraY, 0, 0, 1, 9*time.Second, EasingLinear)
	// Terrain[1][1]'s hold has ended, though no Step has retired it yet.
	timeline.Hold(probe.terrain, 0, 5, 500*time.Millisecond, 1, 1)
	expectEnds := func(wantFirst time.Duration) {
		t.Helper()
		testCases := []struct {
			name string
			got  time.Duration
			want time.Duration
		}{
			{"Terrain[0][1]", timeline.End(probe.terrain, 0, 1), wantFirst},
			{"Terrain[1][2]", timeline.End(probe.terrain, 1, 2), 5 * time.Second},
			{"Terrain[1][1], whose last override has ended", timeline.End(probe.terrain, 1, 1), timeline.Now()},
			{"Terrain[0][0], which has no override", timeline.End(probe.terrain, 0, 0), timeline.Now()},
			{"Turn, never scheduled on", timeline.End(probe.turn), timeline.Now()},
			{"Camera.Y", timeline.End(probe.cameraY), 9 * time.Second},
		}
		for _, testCase := range testCases {
			if testCase.got != testCase.want {
				t.Errorf("End of %s answered %v at %v, want %v", testCase.name, testCase.got, timeline.Now(), testCase.want)
			}
		}
	}
	expectEnds(3 * time.Second)
	probe.runStep(time.Second)
	expectEnds(3 * time.Second)
	// Terrain[0][1]'s overrides have all ended, and Step retired them.
	probe.runStep(1500 * time.Millisecond)
	expectEnds(3500 * time.Millisecond)
}

func TestCancelRemovesEveryOverrideOnTheAddress(t *testing.T) {
	probe := spawnTimelineProbe(t, nil)
	probe.model.Get().Terrain = [2][3]float32{{1, 2, 3}, {4, 5, 6}}
	timeline := ElevateTimeline(probe.timeline)
	// Terrain[0][1] has a tween running and a hold and a tween yet to start;
	// Terrain[1][2] has a tween running.
	timeline.Tween(probe.terrain, 0, 0, 10, 2*time.Second, EasingLinear, 0, 1)
	timeline.Hold(probe.terrain, 2*time.Second, 20, time.Second, 0, 1)
	timeline.Tween(probe.terrain, 3*time.Second, 0, 10, time.Second, EasingLinear, 0, 1)
	timeline.Tween(probe.terrain, 0, 0, 40, 4*time.Second, EasingLinear, 1, 2)
	probe.runStep(time.Second)
	probe.expectCurrent(t, probe.terrain, 5, 0, 1)
	timeline.Cancel(probe.terrain, 0, 1)
	if end := timeline.End(probe.terrain, 0, 1); end != time.Second {
		t.Errorf("End of Terrain[0][1] answered %v after Cancel, want Now, 1s", end)
	}
	// Current reads the sample the last Step took, so the model's own value
	// shows from the next.
	probe.expectCurrent(t, probe.terrain, 5, 0, 1)
	probe.runStep(0)
	probe.expectCurrent(t, probe.terrain, 2, 0, 1)
	probe.expectCurrent(t, probe.terrain, 10, 1, 2)
	// Neither the hold nor the last tween starts.
	probe.runStep(1500 * time.Millisecond)
	probe.expectCurrent(t, probe.terrain, 2, 0, 1)
	probe.expectCurrent(t, probe.terrain, 25, 1, 2)
	probe.runStep(time.Second)
	probe.expectCurrent(t, probe.terrain, 2, 0, 1)
	probe.expectCurrent(t, probe.terrain, 35, 1, 2)
	// An address with no override, and a path never scheduled on, are left
	// as they are.
	timeline.Cancel(probe.terrain, 0, 0)
	timeline.Cancel(probe.turn)
	probe.expectCurrent(t, probe.terrain, 35, 1, 2)
	probe.expectCurrent(t, probe.terrain, 1, 0, 0)
}

func TestHoldEndAndCancelPanicNamingWhatIsWrong(t *testing.T) {
	probe := spawnTimelineProbe(t, nil)
	timeline := ElevateTimeline(probe.timeline)
	expectPanic(t, `path "Hero.Name" is held over 0s, which is no positive length`, func() {
		timeline.Hold(probe.name, 0, "bo", 0)
	})
	expectPanic(t, `path "Hero.Name" is held over -1s, which is no positive length`, func() {
		timeline.Hold(probe.name, 0, "bo", -time.Second)
	})
	expectPanic(t, `path "Terrain[][]" has 2 holes, but is given 1`, func() {
		timeline.Hold(probe.terrain, 0, 1, time.Second, 1)
	})
	expectPanic(t, `path "Terrain[][]" has 2 holes, but is given 3`, func() {
		timeline.End(probe.terrain, 1, 1, 1)
	})
	expectPanic(t, `path "Terrain[][]" has 2 holes, but is given 0`, func() {
		timeline.Cancel(probe.terrain)
	})
}

// visibleSink and endSink keep what TestHoldEndAndCancelAllocateNothing
// reads, beside nameSink and terrainSink, so the compiler cannot drop the
// reads it measures.
var (
	visibleSink bool
	endSink     time.Duration
)

func TestHoldEndAndCancelAllocateNothing(t *testing.T) {
	probe := spawnTimelineProbe(t, nil)
	timeline := ElevateTimeline(probe.timeline)
	// Each round holds and tweens over two Steps, so the round before's
	// overrides end at this round's Step and the timeline stays at the size
	// the first rounds grew it to. It holds a string, a bool and a float32,
	// cancels a tween yet to be sampled and one running, and asks End of an
	// address with overrides, one with none and a path never scheduled on.
	round := func() {
		timeline.Hold(probe.name, timeline.Now(), "bo", 2*time.Millisecond)
		timeline.Hold(probe.visible, timeline.Now(), true, 2*time.Millisecond)
		timeline.Hold(probe.terrain, timeline.Now(), 5, 2*time.Millisecond, 0, 1)
		timeline.Tween(probe.terrain, timeline.Now(), 0, 10, 2*time.Millisecond, EasingLinear, 0, 2)
		timeline.Tween(probe.terrain, timeline.Now(), 0, 10, 2*time.Millisecond, EasingLinear, 1, 2)
		timeline.Cancel(probe.terrain, 0, 2)
		endSink = timeline.End(probe.terrain, 0, 1)
		endSink = timeline.End(probe.terrain, 1, 1)
		endSink = timeline.End(probe.turn)
		step(nil, time.Millisecond, probe.model, probe.timeline)
		timeline.Cancel(probe.terrain, 1, 2)
		nameSink, _ = probe.timeline.Get().Current(probe.name, probe.model.Get())
		visibleSink, _ = probe.timeline.Get().Current(probe.visible, probe.model.Get())
		terrainSink, _ = probe.timeline.Get().Current(probe.terrain, probe.model.Get(), 0, 1)
	}
	round()
	round()
	if nameSink != "bo" || !visibleSink || terrainSink != 5 {
		t.Fatalf("the holds are current at %q, %v and %v, want bo, true and 5", nameSink, visibleSink, terrainSink)
	}
	if allocations := testing.AllocsPerRun(100, round); allocations != 0 {
		t.Fatalf("Hold, End, Cancel, Step and Current allocated %v times per round", allocations)
	}
}
