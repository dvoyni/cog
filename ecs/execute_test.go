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

// nudgeResponse is an answer with something in it, because the interesting case
// is a command that is a question rather than an instruction.
type nudgeResponse struct {
	Moved int
	Total float32
}

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

// TestASystemAnswersThroughItsResponseWrapper is what makes ToExecute a command
// rather than only an instruction: the System still returns nothing, and the
// answer leaves through a parameter the builder recognises by type.
func TestASystemAnswersThroughItsResponseWrapper(t *testing.T) {
	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar, world *Entities) {
		registrar.HandleCommand[nudgeCmd](ToExecute[nudgeRequest, nudgeResponse](world,
			func(request nudgeRequest, q *Query[moveQuery], answer *Resp[nudgeResponse]) {
				reply := nudgeResponse{}
				for _, it := range q.All() {
					it.Body.X += request.By
					reply.Moved++
					reply.Total += it.Body.X
				}
				answer.Set(reply)
			}))
	})

	for i := range 3 {
		e := entities.alloc()
		components.bodies.Set(e, body{X: float32(i)})
		components.velocities.Set(e, velocity{})
	}

	response, err := engine.Executioner().ExecuteCommand[nudgeCmd](nudgeRequest{By: 10})
	if err != nil {
		t.Fatalf("executing the System as a command: %v", err)
	}
	// Bodies at 0, 1 and 2, each nudged by 10: 10 + 11 + 12.
	if response.Moved != 3 || response.Total != 33 {
		t.Fatalf("the command answered %+v, want {Moved:3 Total:33}", response)
	}
}

// TestAResponseIsClearedBetweenInvocations holds the half of the wrapper that is
// easy to get wrong: the cell is allocated once at registration and reused, so an
// invocation that writes nothing must answer the zero value rather than whatever
// the invocation before it left there.
func TestAResponseIsClearedBetweenInvocations(t *testing.T) {
	_, _, engine := newWorld(t, 8, func(registrar *kernel.Registrar, world *Entities) {
		registrar.HandleCommand[nudgeCmd](ToExecute[nudgeRequest, nudgeResponse](world,
			func(request nudgeRequest, answer *Resp[nudgeResponse]) {
				if request.By == 0 {
					return // answers nothing at all
				}
				answer.Set(nudgeResponse{Moved: 1, Total: request.By})
			}))
	})

	executioner := engine.Executioner()
	first, err := executioner.ExecuteCommand[nudgeCmd](nudgeRequest{By: 7})
	if err != nil {
		t.Fatalf("executing the System as a command: %v", err)
	}
	if first.Moved != 1 || first.Total != 7 {
		t.Fatalf("the first invocation answered %+v, want {Moved:1 Total:7}", first)
	}
	second, err := executioner.ExecuteCommand[nudgeCmd](nudgeRequest{By: 0})
	if err != nil {
		t.Fatalf("executing the System as a command: %v", err)
	}
	if second != (nudgeResponse{}) {
		t.Fatalf("an invocation that answered nothing returned %+v, want the zero response", second)
	}
}

// TestASystemNamingNoResponseStillAnswersTheZero keeps the instruction shape
// working: naming the wrapper is optional, and a command that is an order rather
// than a question needs no answer.
func TestASystemNamingNoResponseStillAnswersTheZero(t *testing.T) {
	_, _, engine := newWorld(t, 8, func(registrar *kernel.Registrar, world *Entities) {
		registrar.HandleCommand[nudgeCmd](ToExecute[nudgeRequest, nudgeResponse](world,
			func(request nudgeRequest) {}))
	})
	response, err := engine.Executioner().ExecuteCommand[nudgeCmd](nudgeRequest{By: 1})
	if err != nil {
		t.Fatalf("executing the System as a command: %v", err)
	}
	if response != (nudgeResponse{}) {
		t.Fatalf("a System naming no response answered %+v, want the zero response", response)
	}
}

// TestASubscriptionMayNotNameAResponse is the refusal that keeps the two
// builders' contracts distinct: an event has no response to write into, and a
// System written for one and registered as the other should say so rather than
// writing into a cell nobody reads.
func TestASubscriptionMayNotNameAResponse(t *testing.T) {
	message := composeAndFail(t, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[guardSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[moveQuery], answer *Resp[nudgeResponse]) {}))
	})
	for _, want := range []string{"ecs.Resp[", "ToExecute"} {
		if !strings.Contains(message, want) {
			t.Fatalf("composition failure %q does not name %q", message, want)
		}
	}
}

// TestAResponseOfTheWrongTypeIsRejected names both types, because the mistake is
// always a copied registration line and the useful sentence is which two answers
// were confused.
func TestAResponseOfTheWrongTypeIsRejected(t *testing.T) {
	entities := NewEntities(8)
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("a System answering with the wrong type was accepted")
		}
		message, _ := recovered.(string)
		for _, want := range []string{"ecs.Resp[github.com/dvoyni/cog/ecs.body]", "ecs.nudgeResponse"} {
			if !strings.Contains(message, want) {
				t.Fatalf("panic %v does not name %q", recovered, want)
			}
		}
	}()
	ToExecute[nudgeRequest, nudgeResponse](entities, func(answer *Resp[body]) {})
}

// TestASystemNamingTheResponseTwiceIsRejected is the "at most once" rule the
// event and the request already carry: two parameters would be one cell, so the
// second is never a second answer.
func TestASystemNamingTheResponseTwiceIsRejected(t *testing.T) {
	entities := NewEntities(8)
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("a System naming the response twice was accepted")
		}
		message, _ := recovered.(string)
		for _, want := range []string{"ecs.Resp[github.com/dvoyni/cog/ecs.nudgeResponse]", "more than once"} {
			if !strings.Contains(message, want) {
				t.Fatalf("panic %v does not name %q", recovered, want)
			}
		}
	}()
	ToExecute[nudgeRequest, nudgeResponse](entities, func(a, b *Resp[nudgeResponse]) {})
}

// TestAnsweringThroughTheWrapperAllocatesNothing is where an allocation would
// appear if the response left the System by any route but the cell: the wrapper
// is allocated once at registration and the value is copied out under the lock.
func TestAnsweringThroughTheWrapperAllocatesNothing(t *testing.T) {
	measure := func(system any) float64 {
		_, _, engine := newWorld(t, 8, func(registrar *kernel.Registrar, world *Entities) {
			registrar.HandleCommand[nudgeCmd](ToExecute[nudgeRequest, nudgeResponse](world, system))
		})
		executioner := engine.Executioner()
		return testing.AllocsPerRun(1000, func() {
			if _, err := executioner.ExecuteCommand[nudgeCmd](nudgeRequest{By: 1}); err != nil {
				t.Fatalf("executing the System as a command: %v", err)
			}
		})
	}
	silent := measure(func(request nudgeRequest) {})
	answering := measure(func(request nudgeRequest, answer *Resp[nudgeResponse]) {
		answer.Set(nudgeResponse{Moved: 1, Total: request.By})
	})
	t.Logf("objects an invocation: a System answering nothing %.3f, a System answering through the wrapper %.3f",
		silent, answering)
	if answering > silent {
		t.Fatalf("answering through the wrapper costs %.3f objects an invocation against %.3f for answering nothing",
			answering, silent)
	}
}
