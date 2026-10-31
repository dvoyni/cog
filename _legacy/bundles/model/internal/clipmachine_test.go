package internal

import (
	"errors"
	"testing"
)

// gaitClips is what Clips would report for a small rig: two loops and a
// one-shot.
var gaitClips = []ClipInfo{
	{Name: "walk", Duration: 1},
	{Name: "run", Duration: 0.5},
	{Name: "jump", Duration: 0.375},
	{Name: "idle", Duration: 2},
}

// gaitStates are the rig's four states. Walk leaves Rate at zero, which reads
// as 1; Run plays at double speed; Jump holds its last frame.
var gaitStates = []ClipState{
	{Name: "Walk", Clip: "walk", Loop: true},
	{Name: "Run", Clip: "run", Loop: true, Rate: 2},
	{Name: "Jump", Clip: "jump"},
	{Name: "Idle", Clip: "idle", Loop: true},
}

const (
	stateWalk = 0
	stateRun  = 1
	stateJump = 2
)

// gaitMachine builds the rig with its walk-to-run transition eased by ease.
func gaitMachine(t *testing.T, ease EaseKind) ClipMachine {
	t.Helper()
	machine, err := NewClipMachine(gaitClips, gaitStates, []ClipTransition{
		{From: "Walk", To: "Run", On: "run", Crossfade: 1, Ease: ease},
		{From: "Run", To: "Walk", On: "walk", Crossfade: 1},
		{From: "Run", To: "Jump", On: "jump", Crossfade: 1},
		{From: "Run", To: "Idle", On: "rest", Crossfade: 1},
		{From: "Walk", To: "Jump", On: "hop"},
		{From: "Jump", To: "Walk", OnFinish: true, Crossfade: 0.5},
	})
	if err != nil {
		t.Fatalf("NewClipMachine: %v", err)
	}
	return machine
}

// weights maps each live play's clip to its weight.
func weights(machine *ClipMachine) map[string]float32 {
	out := map[string]float32{}
	for _, play := range machine.Plays(nil) {
		out[play.Clip] += play.Weight
	}
	return out
}

// times maps each live play's clip to its time.
func times(machine *ClipMachine) map[string]float32 {
	out := map[string]float32{}
	for _, play := range machine.Plays(nil) {
		out[play.Clip] = play.Time
	}
	return out
}

func TestAMachineStartsInItsFirstStateAtTimeZero(t *testing.T) {
	machine := gaitMachine(t, EaseLinear)
	plays := machine.Plays(nil)
	if len(plays) != 1 {
		t.Fatalf("a fresh machine plays %d clips, want 1", len(plays))
	}
	if want := (ClipPlay{Clip: "walk", Loop: true, Weight: 1}); plays[0] != want {
		t.Errorf("a fresh machine plays %+v, want %+v", plays[0], want)
	}
}

// Each ease is sampled where its curve is known exactly: nothing at the
// start, the curve's own midpoint in the middle, and the incoming state alone
// at the end.
func TestACrossfadeEasesItsWeights(t *testing.T) {
	for _, tc := range []struct {
		ease EaseKind
		mid  float32
	}{
		{EaseLinear, 0.5},
		{EaseCubicIn, 0.125},
		{EaseCubicOut, 0.875},
		{EaseCubicInOut, 0.5},
	} {
		machine := gaitMachine(t, tc.ease)
		if !machine.Fire("run") {
			t.Fatalf("ease %d: Fire(run) found no transition out of Walk", tc.ease)
		}
		start := weights(&machine)
		if !near(start["walk"], 1) || !near(start["run"], 0) {
			t.Errorf("ease %d: at the start walk %v run %v, want 1 and 0", tc.ease, start["walk"], start["run"])
		}
		machine.Step(0.5, nil)
		mid := weights(&machine)
		if !near(mid["run"], tc.mid) || !near(mid["walk"], 1-tc.mid) {
			t.Errorf("ease %d: halfway walk %v run %v, want %v and %v",
				tc.ease, mid["walk"], mid["run"], 1-tc.mid, tc.mid)
		}
		machine.Step(0.5, nil)
		plays := machine.Plays(nil)
		if len(plays) != 1 || plays[0].Clip != "run" || plays[0].Weight != 1 {
			t.Errorf("ease %d: at the end the machine plays %+v, want run alone at 1", tc.ease, plays)
		}
	}
}

// The ease curves are anim's, re-implemented, so they are checked against the
// formulas anim uses at a point off every symmetry.
func TestTheEaseCurvesAreAnims(t *testing.T) {
	p := float32(0.3)
	inverse := 1 - p
	for _, tc := range []struct {
		ease EaseKind
		want float32
	}{
		{EaseLinear, p},
		{EaseCubicIn, p * p * p},
		{EaseCubicOut, 1 - inverse*inverse*inverse},
		{EaseCubicInOut, 4 * p * p * p},
	} {
		if got := tc.ease.apply(p); !near(got, tc.want) {
			t.Errorf("ease %d at %v = %v, want %v", tc.ease, p, got, tc.want)
		}
	}
	late := float32(0.8)
	tail := 2 - 2*late
	if got, want := EaseCubicInOut.apply(late), 1-tail*tail*tail/2; !near(got, want) {
		t.Errorf("EaseCubicInOut at %v = %v, want %v", late, got, want)
	}
}

// A Fire during a crossfade takes the mix as it stands, keeps its proportions,
// and fades the whole of it out while the new state fades in; the frozen plays'
// clocks keep running.
func TestFireMidCrossfadeFreezesTheMixAndFadesItAsOneGroup(t *testing.T) {
	machine := gaitMachine(t, EaseLinear)
	machine.Fire("run")
	machine.Step(0.5, nil)
	before := times(&machine)
	if !machine.Fire("rest") {
		t.Fatal("Fire(rest) found no transition out of Run")
	}
	frozen := weights(&machine)
	if !near(frozen["walk"], 0.5) || !near(frozen["run"], 0.5) || !near(frozen["idle"], 0) {
		t.Fatalf("at the interrupt walk %v run %v idle %v, want 0.5, 0.5 and 0",
			frozen["walk"], frozen["run"], frozen["idle"])
	}

	machine.Step(0.25, nil)
	quarter := weights(&machine)
	if !near(quarter["idle"], 0.25) || !near(quarter["walk"], 0.375) || !near(quarter["run"], 0.375) {
		t.Errorf("a quarter into the new fade walk %v run %v idle %v, want 0.375, 0.375 and 0.25",
			quarter["walk"], quarter["run"], quarter["idle"])
	}
	after := times(&machine)
	if !near(after["walk"], before["walk"]+0.25) {
		t.Errorf("the frozen walk moved from %v to %v, want +0.25", before["walk"], after["walk"])
	}
	if !near(after["run"], before["run"]+0.5) {
		t.Errorf("the frozen run moved from %v to %v, want +0.5 at Rate 2", before["run"], after["run"])
	}
	if !near(after["idle"], 0.25) {
		t.Errorf("the incoming idle is at %v, want 0.25 from zero", after["idle"])
	}

	machine.Step(0.75, nil)
	plays := machine.Plays(nil)
	if len(plays) != 1 || plays[0].Clip != "idle" {
		t.Errorf("after the fade the machine plays %+v, want idle alone", plays)
	}
}

// A ring of five states, each fading to the next on "next". Firing it faster
// than a fade lasts piles plays up; the pile never passes the cap, and what is
// shed is whichever play was lightest.
func TestAnInterruptChainDropsTheLightestPlay(t *testing.T) {
	names := []string{"A", "B", "C", "D", "E"}
	var clips []ClipInfo
	var states []ClipState
	var transitions []ClipTransition
	for i, name := range names {
		clips = append(clips, ClipInfo{Name: name, Duration: 1})
		states = append(states, ClipState{Name: name, Clip: name, Loop: true})
		transitions = append(transitions, ClipTransition{
			From: name, To: names[(i+1)%len(names)], On: "next", Crossfade: 1,
		})
	}
	machine, err := NewClipMachine(clips, states, transitions)
	if err != nil {
		t.Fatalf("NewClipMachine: %v", err)
	}
	var fixed [MaxClipPlays]ClipPlay
	for round := range 8 {
		machine.Step(0.1+0.05*float32(round), nil)
		before := weights(&machine)
		lightest, full := "", len(before) == MaxClipPlays
		for clip, weight := range before {
			if lightest == "" || weight < before[lightest] {
				lightest = clip
			}
		}
		machine.Fire("next")
		plays := machine.Plays(nil)
		if len(plays) > MaxClipPlays {
			t.Fatalf("round %d: %d live plays, want at most %d", round, len(plays), MaxClipPlays)
		}
		machine.PlaysInto(&fixed)
		if full {
			if _, kept := weights(&machine)[lightest]; kept {
				t.Errorf("round %d: the lightest play %q (%v) survived a full mix %v",
					round, lightest, before[lightest], before)
			}
			if len(plays) != MaxClipPlays {
				t.Errorf("round %d: a full mix left %d plays, want %d", round, len(plays), MaxClipPlays)
			}
		}
		var total float32
		for _, play := range plays {
			total += play.Weight
		}
		if !near(total, 1) {
			t.Errorf("round %d: the plays weigh %v in total, want 1", round, total)
		}
	}
}

// Jump is a one-shot whose OnFinish goes back to Walk. It finishes on the Step
// that carries its time to its duration, and says so once.
func TestAOneShotFinishesOnTheRightStepAndOnce(t *testing.T) {
	machine := gaitMachine(t, EaseLinear)
	machine.Fire("hop")
	machine.Step(0, nil) // collect the Fire's events

	events := machine.Step(0.25, nil)
	if len(events) != 0 {
		t.Fatalf("at 0.25 of a 0.375 clip the machine said %+v, want nothing", events)
	}
	events = machine.Step(0.125, nil)
	want := []ClipEvent{
		{Kind: ClipFinished, State: stateJump},
		{Kind: ClipExited, State: stateJump},
		{Kind: ClipEntered, State: stateWalk},
	}
	if !sameEvents(events, want) {
		t.Fatalf("the finishing Step said %+v, want %+v", events, want)
	}
	for i := range 10 {
		if events := machine.Step(0.1, nil); len(events) != 0 {
			t.Errorf("step %d after the finish said %+v, want nothing", i, events)
		}
	}
}

// A one-shot with nowhere to go holds its last frame, and finishes once.
func TestAOneShotWithNoExitHoldsAndFinishesOnce(t *testing.T) {
	machine, err := NewClipMachine(gaitClips, []ClipState{{Name: "Jump", Clip: "jump"}}, nil)
	if err != nil {
		t.Fatalf("NewClipMachine: %v", err)
	}
	finished := 0
	for range 20 {
		for _, event := range machine.Step(0.1, nil) {
			if event.Kind == ClipFinished {
				finished++
			}
		}
	}
	if finished != 1 {
		t.Errorf("a held one-shot finished %d times, want 1", finished)
	}
	if plays := machine.Plays(nil); len(plays) != 1 || plays[0].Loop {
		t.Errorf("the held one-shot plays %+v, want one play with Loop false", plays)
	}
}

// Fire's events arrive at the head of the next Step, exited before entered;
// a finish comes before the transition it takes; and Step appends rather than
// overwriting what the caller passed in.
func TestEventsArriveInTheDocumentedOrder(t *testing.T) {
	machine := gaitMachine(t, EaseLinear)
	machine.Fire("hop")
	carried := ClipEvent{Kind: ClipEntered, State: 99}
	events := machine.Step(0.375, []ClipEvent{carried})
	want := []ClipEvent{
		carried,
		{Kind: ClipExited, State: stateWalk},
		{Kind: ClipEntered, State: stateJump},
		{Kind: ClipFinished, State: stateJump},
		{Kind: ClipExited, State: stateJump},
		{Kind: ClipEntered, State: stateWalk},
	}
	if !sameEvents(events, want) {
		t.Errorf("the events were %+v, want %+v", events, want)
	}
	if name := machine.StateName(stateJump); name != "Jump" {
		t.Errorf("StateName(%d) = %q, want Jump", stateJump, name)
	}
}

func TestFireWithNoTransitionReturnsFalse(t *testing.T) {
	machine := gaitMachine(t, EaseLinear)
	if machine.Fire("walk") {
		t.Error("Fire(walk) out of Walk found a transition")
	}
	if machine.Fire("nonsense") {
		t.Error("Fire(nonsense) found a transition")
	}
	if events := machine.Step(0, nil); len(events) != 0 {
		t.Errorf("a refused Fire queued %+v", events)
	}
	var zero ClipMachine
	if zero.Fire("run") || len(zero.Step(1, nil)) != 0 || len(zero.Plays(nil)) != 0 {
		t.Error("the zero machine did something")
	}
}

// Rate scales the dt a state's clip advances by, and zero reads as 1.
func TestRateScalesClipTime(t *testing.T) {
	machine := gaitMachine(t, EaseLinear)
	machine.Step(0.25, nil)
	if got := times(&machine)["walk"]; !near(got, 0.25) {
		t.Errorf("Walk at Rate 0 advanced to %v, want 0.25", got)
	}
	machine.Fire("run")
	machine.Step(0.25, nil)
	if got := times(&machine)["run"]; !near(got, 0.5) {
		t.Errorf("Run at Rate 2 advanced to %v, want 0.5", got)
	}
}

func TestNewClipMachineRejectsWhatCannotRun(t *testing.T) {
	var missingClip ErrClipStateClipMissing
	_, err := NewClipMachine(gaitClips, []ClipState{{Name: "Swim", Clip: "swim"}}, nil)
	if !errors.As(err, &missingClip) || missingClip.State != "Swim" || missingClip.Clip != "swim" {
		t.Errorf("an unknown clip gave %v, want ErrClipStateClipMissing naming Swim and swim", err)
	}

	var missingState ErrClipTransitionStateMissing
	_, err = NewClipMachine(gaitClips, gaitStates, []ClipTransition{{From: "Walk", To: "Fly", On: "fly"}})
	if !errors.As(err, &missingState) || missingState.State != "Fly" {
		t.Errorf("an unknown To gave %v, want ErrClipTransitionStateMissing naming Fly", err)
	}
	_, err = NewClipMachine(gaitClips, gaitStates, []ClipTransition{{From: "Crawl", To: "Walk", On: "up"}})
	if !errors.As(err, &missingState) || missingState.State != "Crawl" {
		t.Errorf("an unknown From gave %v, want ErrClipTransitionStateMissing naming Crawl", err)
	}

	var trigger ErrClipTransitionTriggerInvalid
	for _, transition := range []ClipTransition{
		{From: "Walk", To: "Run", On: "run", OnFinish: true},
		{From: "Walk", To: "Run"},
	} {
		_, err = NewClipMachine(gaitClips, gaitStates, []ClipTransition{transition})
		if !errors.As(err, &trigger) || trigger.From != "Walk" || trigger.To != "Run" {
			t.Errorf("transition %+v gave %v, want ErrClipTransitionTriggerInvalid", transition, err)
		}
	}

	var empty ErrClipMachineEmpty
	if _, err = NewClipMachine(gaitClips, nil, nil); !errors.As(err, &empty) {
		t.Errorf("no states gave %v, want ErrClipMachineEmpty", err)
	}
}

// PlaysInto is Plays into a fixed array, with the unused slots emptied.
func TestPlaysAndPlaysIntoAgree(t *testing.T) {
	machine := gaitMachine(t, EaseCubicInOut)
	fixed := [MaxClipPlays]ClipPlay{{Clip: "stale"}, {Clip: "stale"}, {Clip: "stale"}, {Clip: "stale"}}
	for _, trigger := range []string{"run", "", "jump", "", "", "", "", "", "", ""} {
		if trigger != "" {
			machine.Fire(trigger)
		}
		machine.Step(0.2, nil)
		plays := machine.Plays(nil)
		machine.PlaysInto(&fixed)
		for i := range fixed {
			var want ClipPlay
			if i < len(plays) {
				want = plays[i]
			}
			if fixed[i] != want {
				t.Fatalf("after %q slot %d is %+v, Plays says %+v", trigger, i, fixed[i], want)
			}
		}
	}
}

func sameEvents(got, want []ClipEvent) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
