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
