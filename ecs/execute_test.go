package ecs

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// nudgeCmd is a System invoked as a command. The declaration carries the name;
// ToExecute is the factory that implements it, exactly as loadCmdImpl would be
// for a hand-written one.
type nudgeCmd kernel.Command[nudgeRequest, nudgeResponse]

type nudgeRequest struct{ By float32 }

type nudgeResponse struct{}

// TestASystemIsInvocableAsACommand is ToExecute's whole job: the same func, the
// same classification and the same lock set, registered with HandleCommand
// instead of Subscribe. The request reaches it through a Feed, so the System is
// no more welded to the request than it is to an event.
func TestASystemIsInvocableAsACommand(t *testing.T) {
	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar, world *Entities) {
		registrar.HandleCommand[nudgeCmd](ToExecute[nudgeRequest, nudgeResponse](world,
			func(q *Query[moveQuery], by *In[float32]) {
				amount := by.Get()
				for _, it := range q.All() {
					it.Body.X += amount
				}
			},
			Feed(func(r nudgeRequest) float32 { return r.By })))
	})

	e := entities.alloc()
	components.bodies.Set(e, body{X: 1})
	components.velocities.Set(e, velocity{})

	if _, err := engine.Executioner().ExecuteCommand[nudgeCmd](nudgeRequest{By: 4}); err != nil {
		t.Fatalf("executing the System as a command: %v", err)
	}

	if value, _ := components.bodies.Get(e); value.X != 5 {
		t.Fatalf("body is %v after a nudge of 4 from 1, want {5 0}", value)
	}

	// The lock set is the union of what the parameters declare, the same way a
	// subscription's is: a command is a different registration, not a different
	// classification.
	for _, cmd := range engine.Describe().Commands {
		if cmd.Type != reflect.TypeFor[nudgeCmd]() {
			continue
		}
		if !namesType(cmd.Reads, "*ecs.Entities") || !namesType(cmd.Writes, "body]") {
			t.Fatalf("the command reads %v and writes %v", cmd.Reads, cmd.Writes)
		}
		return
	}
	t.Fatalf("no nudgeCmd in the description")
}

// TestASystemInvokedAsACommandMayNameItsRequest is the request half of "naming
// the value is legal and is not the default shape". The diagnostic says
// "request" rather than "event", because that is what the author wrote.
func TestASystemInvokedAsACommandMayNameItsRequest(t *testing.T) {
	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar, world *Entities) {
		registrar.HandleCommand[nudgeCmd](ToExecute[nudgeRequest, nudgeResponse](world,
			func(request nudgeRequest, q *Query[moveQuery]) {
				for _, it := range q.All() {
					it.Body.X += request.By
				}
			}))
	})

	e := entities.alloc()
	components.bodies.Set(e, body{X: 2})
	components.velocities.Set(e, velocity{})

	if _, err := engine.Executioner().ExecuteCommand[nudgeCmd](nudgeRequest{By: 3}); err != nil {
		t.Fatalf("executing the System as a command: %v", err)
	}
	if value, _ := components.bodies.Get(e); value.X != 5 {
		t.Fatalf("body is %v after a nudge of 3 from 2, want {5 0}", value)
	}
}

// TestACommandSystemRefusesAnUnknownParameterAsARequest shows the refusal says
// "request value" where a subscription's says "event value": the classification
// is one contract, and the sentence names the shape the author is actually in.
func TestACommandSystemRefusesAnUnknownParameterAsARequest(t *testing.T) {
	entities := NewEntities(8)
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("a command System taking an unknown parameter was accepted")
		}
		message, _ := recovered.(string)
		for _, want := range []string{"request value", "ecs.nudgeRequest", "*ecs.Store["} {
			if !strings.Contains(message, want) {
				t.Fatalf("panic %v does not name %q", recovered, want)
			}
		}
	}()
	ToExecute[nudgeRequest, nudgeResponse](entities, func(s *Store[body]) {})
}

// TestTheSameSystemIsBothACommandAndASubscription is the payoff of one
// classification serving both builders: the func names neither the event nor the
// request, so the adapter is free to be either.
func TestTheSameSystemIsBothACommandAndASubscription(t *testing.T) {
	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar, world *Entities) {
		registrar.HandleCommand[nudgeCmd](ToExecute[nudgeRequest, nudgeResponse](world, advance,
			Feed(func(r nudgeRequest) float64 { return float64(r.By) })))
		registrar.Subscribe[advanceOnUpdate](ToHandler[app.UpdateEvent](world, advance,
			Feed(func(e app.UpdateEvent) float64 { return e.Dt })))
	})

	e := entities.alloc()
	components.bodies.Set(e, body{})
	components.velocities.Set(e, velocity{X: 10})

	frame(t, engine, 0.5)
	if _, err := engine.Executioner().ExecuteCommand[nudgeCmd](nudgeRequest{By: 0.25}); err != nil {
		t.Fatalf("executing the System as a command: %v", err)
	}

	if value, _ := components.bodies.Get(e); value.X != 7.5 {
		t.Fatalf("body is %v after a tick of 0.5 and a command of 0.25 at velocity 10, want {7.5 0}", value)
	}
}
