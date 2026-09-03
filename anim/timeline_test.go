package anim

import (
	"slices"
	"testing"
)

type floatSeq struct{ Lerp[float32] }

type otherSeq struct{ Lerp[float32] }

func near(a, b float32) bool {
	const epsilon = 1e-5
	return a-b < epsilon && b-a < epsilon
}

func queryFloat(t *testing.T, tl *Timeline, id any) (float32, State) {
	t.Helper()
	sequence, progress, state := tl.Query[floatSeq](id)
	if !state.Found() {
		return 0, state
	}
	return sequence.At(progress), state
}

func TestAddChainsAtChainPoint(t *testing.T) {
	tl := &Timeline{}
	tl.Add("a", floatSeq{LerpFloat(0, 1)}, Over(1))
	tl.Add("b", floatSeq{LerpFloat(0, 1)}, Over(1))

	if _, state := queryFloat(t, tl, "b"); state != StatePending {
		t.Fatalf("b before its start: state = %v, want pending", state)
	}
	tl.advance(1)
	if value, state := queryFloat(t, tl, "b"); state != StateActive || !near(value, 0) {
		t.Fatalf("b at its start: value = %v state = %v, want 0 active", value, state)
	}
	tl.advance(0.5)
	if value, _ := queryFloat(t, tl, "b"); !near(value, 0.5) {
		t.Fatalf("b halfway: value = %v, want 0.5", value)
	}
}

func TestAddImmediateStartsNow(t *testing.T) {
	tl := &Timeline{}
	tl.Add("a", floatSeq{LerpFloat(0, 1)}, Over(1))
	tl.Add("b", floatSeq{LerpFloat(0, 1)}, Over(1).WithImmediate())

	if _, state := queryFloat(t, tl, "b"); state != StateActive {
		t.Fatalf("immediate b: state = %v, want active", state)
	}
	if !near(tl.chainEnd, 1) {
		t.Fatalf("chain point = %v, want 1 (immediate add still sets it)", tl.chainEnd)
	}
}

func TestRewindOverlaps(t *testing.T) {
	tl := &Timeline{}
	tl.Add("a", floatSeq{LerpFloat(0, 1)}, Over(1))
	tl.Rewind(-1)
	tl.Add("b", floatSeq{LerpFloat(0, 1)}, Over(1))

	tl.advance(0.5)
	a, _ := queryFloat(t, tl, "a")
	b, _ := queryFloat(t, tl, "b")
	if !near(a, 0.5) || !near(b, 0.5) {
		t.Fatalf("a = %v b = %v, want both 0.5", a, b)
	}
}

func TestLoopLeavesChainPoint(t *testing.T) {
	tl := &Timeline{}
	tl.Add("spin", floatSeq{LerpFloat(0, 360)}, Over(2).WithLoop())
	if tl.chainEnd != 0 {
		t.Fatalf("chain point = %v, want 0", tl.chainEnd)
	}
	tl.advance(3)
	if value, state := queryFloat(t, tl, "spin"); state != StateActive || !near(value, 180) {
		t.Fatalf("loop after 3s: value = %v state = %v, want 180 active", value, state)
	}
	if !tl.Idle() {
		t.Fatal("a timeline holding only loops should be idle")
	}
}

func TestQueryPrefersNewestActiveThenEarliestPending(t *testing.T) {
	tl := &Timeline{}
	tl.Add("a", floatSeq{LerpFloat(0, 1)}, Over(2))
	tl.Add("a", floatSeq{LerpFloat(5, 6)}, Over(1).WithImmediate())
	tl.Wait(3)
	tl.Add("a", floatSeq{LerpFloat(9, 9)}, Over(1))

	if value, _ := queryFloat(t, tl, "a"); !near(value, 5) {
		t.Fatalf("overlapping: value = %v, want the newest active (5)", value)
	}
	// The immediate track finishes first; the longer one underneath it takes over.
	tl.advance(1.5)
	if value, state := queryFloat(t, tl, "a"); state != StateActive || !near(value, 0.75) {
		t.Fatalf("after the overlap: value = %v state = %v, want 0.75 active", value, state)
	}

	// Both one-shot tracks gone, only the waiting one is left.
	tl.advance(1)
	value, state := queryFloat(t, tl, "a")
	if state != StatePending || !near(value, 9) {
		t.Fatalf("after both finish: value = %v state = %v, want 9 pending", value, state)
	}
}

func TestQueryFallsBackToEarliestPending(t *testing.T) {
	tl := &Timeline{}
	tl.Wait(2)
	tl.Add("a", floatSeq{LerpFloat(7, 8)}, Over(1))
	tl.Wait(5)
	tl.Add("a", floatSeq{LerpFloat(1, 2)}, Over(1))

	value, state := queryFloat(t, tl, "a")
	if state != StatePending || !near(value, 7) {
		t.Fatalf("value = %v state = %v, want 7 pending", value, state)
	}
}

func TestProgressUsesEasing(t *testing.T) {
	tl := &Timeline{}
	tl.Add("a", floatSeq{LerpFloat(0, 1)}, Over(1).WithEasing(EaseCubicOut))
	tl.advance(0.5)
	if value, _ := queryFloat(t, tl, "a"); !near(value, EaseCubicOut(0.5)) {
		t.Fatalf("value = %v, want %v", value, EaseCubicOut(0.5))
	}
}

func TestFinishedTrackDroppedAfterEnd(t *testing.T) {
	tl := &Timeline{}
	tl.Add("a", floatSeq{LerpFloat(0, 1)}, Over(1))
	tl.advance(1)
	if value, state := queryFloat(t, tl, "a"); state != StateActive || !near(value, 1) {
		t.Fatalf("at end: value = %v state = %v, want 1 active", value, state)
	}
	if tl.Idle() {
		t.Fatal("still active at its end: should not be idle")
	}
	tl.advance(0.001)
	if _, state := queryFloat(t, tl, "a"); state != StateNotFound {
		t.Fatalf("past end: state = %v, want not found", state)
	}
	if !tl.Idle() {
		t.Fatal("dropped: should be idle")
	}
}

func TestSlotsAreTypedById(t *testing.T) {
	tl := &Timeline{}
	tl.Add("a", floatSeq{LerpFloat(1, 1)}, Over(1))
	tl.Add("a", otherSeq{LerpFloat(2, 2)}, Over(1).WithImmediate())
	if value := tl.Value[floatSeq]("a", -1); !near(value, 1) {
		t.Fatalf("floatSeq value = %v, want 1", value)
	}
	if value := tl.Value[otherSeq]("a", -1); !near(value, 2) {
		t.Fatalf("otherSeq value = %v, want 2", value)
	}
}

func TestNilTimelineIsNoOp(t *testing.T) {
	var tl *Timeline
	tl.Add("a", floatSeq{LerpFloat(0, 1)}, Over(1))
	tl.Cue("x")
	tl.Rewind(-1)
	tl.Wait(1)
	tl.Reset()
	if value := tl.Value[floatSeq]("a", 42); value != 42 {
		t.Fatalf("value = %v, want fallback 42", value)
	}
	if _, _, state := tl.Query[floatSeq]("a"); state != StateNotFound {
		t.Fatalf("state = %v, want not found", state)
	}
	if !tl.Idle() || tl.Time() != 0 || tl.FiredCues() != nil {
		t.Fatal("nil timeline should be idle at time 0 with no cues")
	}
	for range tl.Fired[string]() {
		t.Fatal("nil timeline yielded a cue")
	}
}

func TestCueFiresOnNextAdvanceForOneTick(t *testing.T) {
	tl := &Timeline{}
	tl.Cue("hello")
	if fired := slices.Collect(tl.Fired[string]()); len(fired) != 0 {
		t.Fatalf("fired before advance: %v", fired)
	}
	if tl.Idle() {
		t.Fatal("pending cue: should not be idle")
	}
	tl.advance(0)
	if fired := slices.Collect(tl.Fired[string]()); !slices.Equal(fired, []string{"hello"}) {
		t.Fatalf("fired after advance: %v, want [hello]", fired)
	}
	if !tl.Idle() {
		t.Fatal("fired cue: should be idle")
	}
	tl.advance(0)
	if fired := slices.Collect(tl.Fired[string]()); len(fired) != 0 {
		t.Fatalf("fired two advances later: %v", fired)
	}
}

func TestCueWaitsForChainPoint(t *testing.T) {
	tl := &Timeline{}
	tl.Add("a", floatSeq{LerpFloat(0, 1)}, Over(1))
	tl.Cue("after a")
	tl.Wait(0.5)
	tl.Cue("after wait")

	tl.advance(0.5)
	if fired := slices.Collect(tl.Fired[string]()); len(fired) != 0 {
		t.Fatalf("fired mid-track: %v", fired)
	}
	tl.advance(0.5)
	if fired := slices.Collect(tl.Fired[string]()); !slices.Equal(fired, []string{"after a"}) {
		t.Fatalf("fired at track end: %v, want [after a]", fired)
	}
	tl.advance(0.5)
	if fired := slices.Collect(tl.Fired[string]()); !slices.Equal(fired, []string{"after wait"}) {
		t.Fatalf("fired after wait: %v, want [after wait]", fired)
	}
}

func TestCuesKeepQueueOrderAndFilterByType(t *testing.T) {
	tl := &Timeline{}
	tl.Cue("first")
	tl.Cue(7)
	tl.Cue("second")
	tl.advance(0)
	if fired := slices.Collect(tl.Fired[string]()); !slices.Equal(fired, []string{"first", "second"}) {
		t.Fatalf("strings = %v", fired)
	}
	if fired := slices.Collect(tl.Fired[int]()); !slices.Equal(fired, []int{7}) {
		t.Fatalf("ints = %v", fired)
	}
	if all := tl.FiredCues(); len(all) != 3 {
		t.Fatalf("FiredCues = %v", all)
	}
}

func TestIdle(t *testing.T) {
	tl := &Timeline{}
	if !tl.Idle() {
		t.Fatal("empty timeline should be idle")
	}
	tl.Wait(1)
	if tl.Idle() {
		t.Fatal("waiting: should not be idle")
	}
	tl.advance(1)
	if !tl.Idle() {
		t.Fatal("wait passed: should be idle")
	}
	tl.Add("a", floatSeq{LerpFloat(0, 1)}, Over(1))
	if tl.Idle() {
		t.Fatal("track queued: should not be idle")
	}
	tl.Reset()
	if !tl.Idle() || tl.Time() != 0 {
		t.Fatal("reset: should be idle at time 0")
	}
}

func TestTimelinesGetResetDeleteAdvance(t *testing.T) {
	timelines := newTimelines()
	if timelines.Lookup("a") != nil {
		t.Fatal("Lookup should not create")
	}
	a := timelines.Get("a")
	if timelines.Get("a") != a || timelines.Lookup("a") != a {
		t.Fatal("Get should return one timeline per key")
	}
	b := timelines.Get("b")
	a.Add("x", floatSeq{LerpFloat(0, 1)}, Over(1))

	timelines.advance(0.5)
	if !near(a.Time(), 0.5) || !near(b.Time(), 0.5) {
		t.Fatalf("times = %v %v, want both 0.5", a.Time(), b.Time())
	}

	timelines.Reset("a")
	if timelines.Get("a") != a || a.Time() != 0 || !a.Idle() {
		t.Fatal("Reset should clear in place and keep the pointer")
	}

	timelines.Delete("b")
	timelines.advance(1)
	if !near(b.Time(), 0.5) {
		t.Fatalf("deleted timeline advanced to %v", b.Time())
	}
	if timelines.Get("b") == b {
		t.Fatal("Get after Delete should create a fresh timeline")
	}
}

func TestEasings(t *testing.T) {
	for _, easing := range []Easing{Linear, EaseCubicIn, EaseCubicOut, EaseCubicInOut, Hold(0.5, nil), Reverse(EaseCubicOut)} {
		if !near(easing(0), 0) || !near(easing(1), 1) {
			t.Fatalf("easing endpoints = %v %v, want 0 1", easing(0), easing(1))
		}
	}
	hold := Hold(0.5, Linear)
	if hold(0.25) != 0 || !near(hold(0.75), 0.5) {
		t.Fatalf("Hold(0.5) = %v %v, want 0 0.5", hold(0.25), hold(0.75))
	}
	if !near(Reverse(EaseCubicOut)(0.5), EaseCubicIn(0.5)) {
		t.Fatal("Reverse(EaseCubicOut) should match EaseCubicIn")
	}
}
