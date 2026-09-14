package internal

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/extensions/mcp"
)

// One tool acts and one looks, and the annotation is what makes them two: MCP
// annotates a tool rather than an argument, so looking cheaply and often needs
// a capability of its own.
func TestCapabilities_OneActsAndOneLooks(t *testing.T) {
	capabilities := (provider{}).Capabilities()
	if len(capabilities) != 2 {
		t.Fatalf("input offers %d capabilities, want 2", len(capabilities))
	}
	offered := map[string]mcp.Capability{}
	for _, one := range capabilities {
		if err := one.Err(); err != nil {
			t.Errorf("capability %q did not construct: %v", one.Name(), err)
		}
		offered[one.Name()] = one
	}

	send, offers := offered[sendName]
	if !offers {
		t.Fatalf("input offers no send")
	}
	if send.ReadOnly() {
		t.Error("pressing a key changes the game, so send is not read-only")
	}
	if send.RequestType() != reflect.TypeFor[input.SynthesizeRequest]() || send.ResponseType() != reflect.TypeFor[input.StateResponse]() {
		t.Errorf("send is %v -> %v", send.RequestType(), send.ResponseType())
	}

	state, offers := offered[stateName]
	if !offers {
		t.Fatalf("input offers no state")
	}
	if !state.ReadOnly() {
		t.Error("asking what is held changes nothing, so state is read-only")
	}
	if state.RequestType() != reflect.TypeFor[input.StateRequest]() || state.ResponseType() != reflect.TypeFor[input.StateResponse]() {
		t.Errorf("state is %v -> %v", state.RequestType(), state.ResponseType())
	}
}

// The description is prompt text, and the three things a caller most needs to
// be told are the batching rule, that coordinates are not image pixels, and
// that nothing releases a key for them.
func TestCapabilities_TheDescriptionsCarryWhatACallerGetsWrong(t *testing.T) {
	for _, one := range (provider{}).Capabilities() {
		if one.Description() == "" {
			t.Fatalf("capability %q has no description", one.Name())
		}
	}
	send := (provider{}).Capabilities()[0]
	for _, wanted := range []string{
		"same tick", "complete click", "drag", "window units", "mouse_left",
		"does **not** press keys", "stays down until something releases it", "paused",
	} {
		if !strings.Contains(send.Description(), wanted) {
			t.Errorf("the send description never says %q", wanted)
		}
	}
	state := (provider{}).Capabilities()[1]
	for _, wanted := range []string{"Changes nothing", "window units", "left down by an earlier call"} {
		if !strings.Contains(state.Description(), wanted) {
			t.Errorf("the state description never says %q", wanted)
		}
	}
}

// A refused sequence leaves the seam exactly as it was, asserted through the
// capability an agent would use to check rather than through internals.
func TestSend_ARefusedSequenceLeavesTheDownSetUnchanged(t *testing.T) {
	harness := newPlayHarness(t)
	capabilities := (provider{}).Capabilities()
	send, state := capabilities[0], capabilities[1]

	pressed, err := send.Invoke(harness.k, &input.SynthesizeRequest{Actions: []input.Action{
		{Do: input.ActionMove, X: 12, Y: 34},
		{Do: input.ActionKeyDown, Key: input.KeyLeftShift},
	}})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	before := pressed.(input.StateResponse)

	_, err = send.Invoke(harness.k, &input.SynthesizeRequest{Actions: []input.Action{
		{Do: input.ActionKeyDown, Key: input.KeyW},
		{Do: input.ActionDelay, Ms: 20000},
		{Do: input.ActionKeyUp, Key: input.KeyW},
	}})
	var reason mcp.Unavailable
	if !errors.As(err, &reason) {
		t.Fatalf("error %v is not an expected outcome an agent can read", err)
	}
	if !strings.Contains(reason.Reason, "up to 10s") {
		t.Errorf("reason %q does not name the cap", reason.Reason)
	}

	looked, err := state.Invoke(harness.k, &input.StateRequest{})
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	after := looked.(input.StateResponse)
	if !slices.Equal(after.Down, before.Down) || after.Pointer != before.Pointer {
		t.Errorf("the seam is %+v, want the %+v the refused sequence was supposed to leave",
			after, before)
	}
	if !slices.Equal(after.Down, []input.Key{input.KeyLeftShift}) {
		t.Errorf("the seam holds %v, want only what the accepted sequence pressed", after.Down)
	}
}

// The read-only capability answers the same question the acting one does,
// without pressing anything.
func TestState_AnswersTheSameQuestionWithoutPressingAnything(t *testing.T) {
	harness := newPlayHarness(t)
	capabilities := (provider{}).Capabilities()
	send, state := capabilities[0], capabilities[1]

	pressed, err := send.Invoke(harness.k, &input.SynthesizeRequest{Actions: []input.Action{
		{Do: input.ActionMove, X: 5, Y: 6},
		{Do: input.ActionKeyDown, Key: input.KeyMouseLeft},
	}})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	looked, err := state.Invoke(harness.k, &input.StateRequest{})
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if looked.(input.StateResponse).Pointer != pressed.(input.StateResponse).Pointer ||
		!slices.Equal(looked.(input.StateResponse).Down, pressed.(input.StateResponse).Down) {
		t.Errorf("state answered %+v where send answered %+v", looked, pressed)
	}
	again, err := state.Invoke(harness.k, &input.StateRequest{})
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if !slices.Equal(again.(input.StateResponse).Down, []input.Key{input.KeyMouseLeft}) {
		t.Errorf("looking changed the seam to %+v", again)
	}
}
