package wgpu

import (
	"errors"
	"strings"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/mcp"
)

// unavailable reports the expected outcome an agent reads, or fails the test
// when the call was a system fault or no refusal at all.
func unavailable(t *testing.T, err error) mcp.Unavailable {
	t.Helper()
	var reason mcp.Unavailable
	if !errors.As(err, &reason) {
		t.Fatalf("error %v is not an expected outcome an agent can read", err)
	}
	return reason
}

// The driver offers exactly one capability, and it is the tick source.
func TestTimeCapability_IsTheDriversOneCapability(t *testing.T) {
	capabilities := New().Capabilities()

	if len(capabilities) != 1 {
		t.Fatalf("wgpu offers %d capabilities, want 1", len(capabilities))
	}
	one := capabilities[0]
	if one.Name() != "time" {
		t.Errorf("capability name = %q, want time", one.Name())
	}
	if err := one.Err(); err != nil {
		t.Errorf("capability did not construct: %v", err)
	}
	if one.ReadOnly() {
		t.Error("pause, resume and step change the game, so the capability is not read-only")
	}
	for _, wanted := range []string{"pause", "resume", "step", "status", "hold", "release"} {
		if !strings.Contains(one.Description(), wanted) {
			t.Errorf("the description never mentions %q", wanted)
		}
	}
}

// status reports without changing anything, and every call answers with the
// resulting state.
func TestTimeCapability_StatusReports(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig())

	response, err := timeControl(harness.k, TimeRequest{Action: "status"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if response.Paused {
		t.Error("a running engine reported itself paused")
	}

	harness.control(app.TimeRequest{Action: app.TimePause})
	response, err = timeControl(harness.k, TimeRequest{Action: "status"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !response.Paused {
		t.Error("a paused engine reported itself running")
	}
}

// Pausing an already-paused engine reads back as an ordinary answer, not an
// error, and the words carry the state the struct cannot.
func TestTimeCapability_PausingTwiceIsAnOrdinaryAnswer(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig())

	if _, err := timeControl(harness.k, TimeRequest{Action: "pause"}); err != nil {
		t.Fatalf("pause: %v", err)
	}
	_, err := timeControl(harness.k, TimeRequest{Action: "pause"})
	reason := unavailable(t, err)
	if !strings.Contains(reason.Reason, "already paused") {
		t.Errorf("reason %q does not say the engine is already paused", reason.Reason)
	}
	if !strings.Contains(reason.Reason, "resume") {
		t.Errorf("reason %q does not name what to do instead", reason.Reason)
	}
}

// A step advances the game and implies pause; the answer reports both.
func TestTimeCapability_StepAdvancesAndImpliesPause(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig())

	done := make(chan TimeResponse, 1)
	go func() {
		response, err := timeControl(harness.k, TimeRequest{Action: "step", Steps: 3})
		if err != nil {
			t.Errorf("step: %v", err)
		}
		done <- response
	}()
	harness.waitPending(3)
	harness.frame(0.010)

	response := <-done
	if !response.Paused {
		t.Error("a step did not imply pause")
	}
	if response.Stepped != 3 || response.Advanced != 3 {
		t.Errorf("step 3 reported %+v, want three ticks", response)
	}
	if got := len(harness.recorded()); got != 3 {
		t.Errorf("step 3 published %d ticks, want 3", got)
	}
}

// A request that cannot be served is refused in words, before anything is
// armed, so a bad request costs no frames.
func TestTimeCapability_RefusesBadRequestsInWords(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig())

	for _, one := range []struct {
		name    string
		request TimeRequest
		says    string
	}{
		{"unknown action", TimeRequest{Action: "freeze"}, "not an action"},
		{"no action", TimeRequest{}, "not an action"},
		{"too many steps", TimeRequest{Action: "step", Steps: maxTimeSteps + 1}, "ten seconds"},
		{"negative steps", TimeRequest{Action: "step", Steps: -1}, "ask for between"},
		{"steps without step", TimeRequest{Action: "pause", Steps: 2}, "applies to step"},
	} {
		t.Run(one.name, func(t *testing.T) {
			_, err := timeControl(harness.k, one.request)
			reason := unavailable(t, err)
			if !strings.Contains(reason.Reason, one.says) {
				t.Errorf("reason %q does not say %q", reason.Reason, one.says)
			}
		})
	}

	if response := harness.control(app.TimeRequest{Action: app.TimeStatus}); response.Paused {
		t.Error("a refused request paused the engine anyway")
	}
	if got := len(harness.recorded()); got != 0 {
		t.Errorf("refused requests cost %d ticks, want none", got)
	}
}

// hold and release are two more arguments to the one tool, and they read
// back the same way pause and resume do: the state the call left, and a
// refusal in words when the call asked for a state the engine is in.
func TestTimeCapability_HoldAndRelease(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig())

	held, err := timeControl(harness.k, TimeRequest{Action: "hold"})
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	if !held.Held || !held.Paused {
		t.Fatalf("hold answered %+v, want a paused engine with the window held", held)
	}
	if held.HoldMs <= 0 || held.HoldMs > int(maxHoldDuration.Milliseconds()) {
		t.Errorf("hold answered %d ms left, want the default within the cap", held.HoldMs)
	}

	again := unavailable(t, mustFail(t, harness, TimeRequest{Action: "hold"}))
	if !strings.Contains(again.Reason, "already open") {
		t.Errorf("a second hold said %q, want it to name the standing one", again.Reason)
	}

	released, err := timeControl(harness.k, TimeRequest{Action: "release"})
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if released.Held {
		t.Errorf("release answered %+v, want the hold gone", released)
	}
	spare := unavailable(t, mustFail(t, harness, TimeRequest{Action: "release"}))
	if !strings.Contains(spare.Reason, "no hold is open") {
		t.Errorf("releasing nothing said %q, want it to say there is nothing held", spare.Reason)
	}
}

// An over-long hold is refused in words before anything is held: the typed
// refusal the tick source raises reaches the agent as prose, like every other
// expected outcome in this family.
func TestTimeCapability_RefusesAHoldLongerThanTheCap(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig())

	for _, one := range []struct {
		name    string
		request TimeRequest
		says    string
	}{
		{"too long", TimeRequest{Action: "hold", Ms: int(maxHoldDuration.Milliseconds()) + 1}, "hold for between"},
		{"negative", TimeRequest{Action: "hold", Ms: -1}, "hold for between"},
		{"ms without hold", TimeRequest{Action: "pause", Ms: 500}, "applies to hold"},
	} {
		t.Run(one.name, func(t *testing.T) {
			reason := unavailable(t, mustFail(t, harness, one.request))
			if !strings.Contains(reason.Reason, one.says) {
				t.Errorf("reason %q does not say %q", reason.Reason, one.says)
			}
		})
	}
	if status, _ := timeControl(harness.k, TimeRequest{Action: "status"}); status.Held {
		t.Error("a refused hold was taken out anyway")
	}
}

// Every answer names the tick the engine is on, which is the number a
// snapshot of that tick reports, so an agent can line the two up without
// counting steps.
func TestTimeCapability_EveryAnswerNamesTheTick(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig())
	harness.frame(0.010)

	status, err := timeControl(harness.k, TimeRequest{Action: "status"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Tick != 1 {
		t.Errorf("status named tick %d, want the one tick published so far", status.Tick)
	}
}

// mustFail runs a request that is expected to be refused and returns the
// refusal, failing the test when the call succeeded instead.
func mustFail(t *testing.T, harness *tickHarness, request TimeRequest) error {
	t.Helper()
	response, err := timeControl(harness.k, request)
	if err == nil {
		t.Fatalf("%s answered %+v, want a refusal", request.Action, response)
	}
	return err
}
