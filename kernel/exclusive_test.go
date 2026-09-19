package kernel

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// The commands below are dispatched concurrently and coordinate through gate,
// which is what makes exclusion observable without a race detector: an
// invocation announces that it has entered and then waits to be released, so a
// test can assert what did *not* happen while the first one is still inside.
type exclusiveCmd Command[int, int]
type otherExclusiveCmd Command[int, int]
type openCmd Command[int, int]
type exclusiveInnerCmd Command[int, int]
type usesExclusiveCmd Command[int, int]

type exclusiveSub Subscription[int]

// gate records every entry into a handler body and holds each one until the
// test releases it.
type gate struct {
	entered chan int
	release chan struct{}
}

func newGate() *gate {
	return &gate{entered: make(chan int, 8), release: make(chan struct{})}
}

// enter announces this invocation and blocks until releaseAll. A body that is
// never admitted simply never announces, which is the assertion below.
func (g *gate) enter(value int) {
	g.entered <- value
	<-g.release
}

func (g *gate) releaseAll() { close(g.release) }

// awaitEntries reports how many invocations announced within the window. It
// always waits the whole window when it wants to prove a *negative*, so a slow
// machine cannot turn "not admitted yet" into a pass by accident.
func (g *gate) awaitEntries(t *testing.T, want int, within time.Duration) int {
	t.Helper()
	deadline := time.After(within)
	seen := 0
	for seen < want {
		select {
		case <-g.entered:
			seen++
		case <-deadline:
			return seen
		}
	}
	return seen
}

// gatedCommand is a command body that passes through the gate. lock is the
// handler's Lock, so each test supplies exactly the declaration it is pinning.
func gatedCommand(g *gate, lock Lock) Command[int, int] {
	return func() (Lock, Execute[int, int]) {
		return lock, func(_ Kernel, request int) int {
			g.enter(request)
			return request
		}
	}
}

// dispatchConcurrently fires n invocations of TCommand, each on its own
// goroutine, and returns a channel carrying every response.
func dispatchConcurrently[TCommand CommandConstraint[int, int]](
	t *testing.T, engine *Engine, requests ...int,
) <-chan int {
	t.Helper()
	responses := make(chan int, len(requests))
	for _, request := range requests {
		go func() {
			response := engine.Executioner().ExecuteCommand[TCommand, int, int](request)
			responses <- response
		}()
	}
	return responses
}

// A handler that declares Exclusive never runs concurrently with itself: the
// second invocation waits for the first to finish rather than joining it.
func TestKernel_ExclusiveHandlerDoesNotRunConcurrentlyWithItself(t *testing.T) {
	g := newGate()
	engine := startEngine(t, testPlugin{
		name: "exclusive",
		register: func(r *Registrar) error {
			r.HandleCommand[exclusiveCmd](gatedCommand(g, func(access ResourceAccess) {
				access.Exclusive()
			}))
			return nil
		},
	})

	dispatchConcurrently[exclusiveCmd](t, engine, 1, 2)

	if entered := g.awaitEntries(t, 2, 200*time.Millisecond); entered != 1 {
		t.Fatalf("entered %d invocations while the first was inside, want 1", entered)
	}
	g.releaseAll()
	if entered := g.awaitEntries(t, 1, time.Second); entered != 1 {
		t.Fatalf("the queued invocation never entered after the first was released")
	}
}

// The mirror of the test above, and the reason it is here: without the
// declaration the same handler *does* run concurrently with itself. It is what
// stops an implementation whose Exclusive writes into a map nobody reads from
// passing, and it pins that a handler declaring nothing keeps today's behaviour.
func TestKernel_AHandlerWithoutExclusiveRunsConcurrentlyWithItself(t *testing.T) {
	g := newGate()
	engine := startEngine(t, testPlugin{
		name: "open",
		register: func(r *Registrar) error {
			r.HandleCommand[openCmd](gatedCommand(g, nil))
			return nil
		},
	})

	dispatchConcurrently[openCmd](t, engine, 1, 2)

	if entered := g.awaitEntries(t, 2, time.Second); entered != 2 {
		t.Fatalf("entered %d invocations concurrently, want 2", entered)
	}
	g.releaseAll()
}

// Exclusion is against the handler itself and nothing else: two handlers that
// both declare it still run at the same time, so self-exclusion never costs
// unrelated parallelism.
func TestKernel_TwoExclusiveHandlersStillRunConcurrently(t *testing.T) {
	g := newGate()
	engine := startEngine(t, testPlugin{
		name: "two-exclusive",
		register: func(r *Registrar) error {
			exclusive := func(access ResourceAccess) { access.Exclusive() }
			r.HandleCommand[exclusiveCmd](gatedCommand(g, exclusive))
			r.HandleCommand[otherExclusiveCmd](gatedCommand(g, exclusive))
			return nil
		},
	})

	dispatchConcurrently[exclusiveCmd](t, engine, 1)
	dispatchConcurrently[otherExclusiveCmd](t, engine, 2)

	if entered := g.awaitEntries(t, 2, time.Second); entered != 2 {
		t.Fatalf("entered %d invocations concurrently, want 2", entered)
	}
	g.releaseAll()
}

// A subscription may declare it too, and two publications of one event then
// reach the subscriber one at a time.
func TestKernel_AnExclusiveSubscriptionDoesNotOverlapItself(t *testing.T) {
	g := newGate()
	engine := startEngine(t, testPlugin{
		name: "exclusive-sub",
		register: func(r *Registrar) error {
			r.Subscribe[exclusiveSub](func() (Lock, Observe[int]) {
				return func(access ResourceAccess) { access.Exclusive() },
					func(_ Kernel, event int) {
						g.enter(event)
					}
			})
			return nil
		},
	})

	executioner := engine.Executioner()
	executioner.PublishEvent(1)
	executioner.PublishEvent(2)

	if entered := g.awaitEntries(t, 2, 200*time.Millisecond); entered != 1 {
		t.Fatalf("entered %d invocations while the first was inside, want 1", entered)
	}
	g.releaseAll()
	if entered := g.awaitEntries(t, 1, time.Second); entered != 1 {
		t.Fatalf("the queued publication never reached the subscriber")
	}
}

// A handler that dispatches an exclusive command through Uses absorbs its
// exclusion, the way it absorbs every other lock the callee declares, so the
// caller does not run concurrently with itself either.
func TestKernel_UsesAbsorbsTheCalleesExclusion(t *testing.T) {
	g := newGate()
	engine := startEngine(t, testPlugin{
		name: "uses-exclusive",
		register: func(r *Registrar) error {
			r.HandleCommand[exclusiveInnerCmd](func() (Lock, Execute[int, int]) {
				return func(access ResourceAccess) { access.Exclusive() },
					func(_ Kernel, request int) int { return request }
			})
			r.HandleCommand[usesExclusiveCmd](func() (Lock, Execute[int, int]) {
				var inner func(Kernel, int) int
				return func(access ResourceAccess) {
						inner = access.Uses[exclusiveInnerCmd, int, int]()
					}, func(k Kernel, request int) int {
						g.enter(request)
						return inner(k, request)
					}
			})
			return nil
		},
	})

	dispatchConcurrently[usesExclusiveCmd](t, engine, 1, 2)

	if entered := g.awaitEntries(t, 2, 200*time.Millisecond); entered != 1 {
		t.Fatalf("entered %d invocations while the first was inside, want 1", entered)
	}
	g.releaseAll()
}

// Describe reports the declaration as its own flag, and never as a resource:
// a handler's identity type is not something anybody can contend for, and an
// agent reading the architecture must not see one listed among real resources.
func TestKernel_DescribeReportsExclusiveWithoutNamingItAResource(t *testing.T) {
	engine := New(nil).
		Handler(func(err error) error { t.Errorf("unexpected kernel error: %v", err); return err }).
		WithPlugins(testPlugin{
			name: "described",
			register: func(r *Registrar) error {
				r.HandleCommand[exclusiveCmd](gatedCommand(newGate(), func(access ResourceAccess) {
					access.Exclusive()
				}))
				r.HandleCommand[openCmd](gatedCommand(newGate(), nil))
				r.HandleCommand[exclusiveInnerCmd](func() (Lock, Execute[int, int]) {
					return func(access ResourceAccess) { access.Exclusive() },
						func(_ Kernel, request int) int { return request }
				})
				r.HandleCommand[usesExclusiveCmd](func() (Lock, Execute[int, int]) {
					var inner func(Kernel, int) int
					return func(access ResourceAccess) {
						inner = access.Uses[exclusiveInnerCmd, int, int]()
					}, func(k Kernel, request int) int { return inner(k, request) }
				})
				return nil
			},
		})
	go engine.Run()
	t.Cleanup(engine.Quit)
	<-engine.Ready()

	commands := map[reflect.Type]CommandDescription{}
	for _, command := range engine.Describe().Commands {
		commands[command.Type] = command
	}

	exclusive := commands[reflect.TypeFor[exclusiveCmd]()]
	if !exclusive.SelfExclusive {
		t.Error("the declaring command is not reported as self-exclusive")
	}
	if len(exclusive.Writes) != 0 {
		t.Errorf("the declaring command writes %v, want nothing", typeNames(exclusive.Writes))
	}

	if commands[reflect.TypeFor[openCmd]()].SelfExclusive {
		t.Error("a command declaring nothing is reported as self-exclusive")
	}

	// The caller absorbed the callee's key. It is not the caller's own
	// declaration, so the flag stays false, and it is not a resource either, so
	// it must not surface in Writes.
	caller := commands[reflect.TypeFor[usesExclusiveCmd]()]
	if caller.SelfExclusive {
		t.Error("a Uses caller is reported as declaring self-exclusion itself")
	}
	if len(caller.Writes) != 0 {
		t.Errorf("a Uses caller writes %v, want nothing", typeNames(caller.Writes))
	}
}

// Every response reaches the caller that asked for it. This is the shape of the
// ECS failure the concept exists for, expressed in the kernel: a body whose
// per-invocation state is shared across invocations answers the wrong caller
// once two of them overlap.
func TestKernel_AnExclusiveCommandAnswersEachCallerItsOwnRequest(t *testing.T) {
	// answer is the shared cell, standing in for ecs.ToExecute's single
	// Resp[Res]: written on the way in and read back on the way out.
	answer := new(int)
	engine := startEngine(t, testPlugin{
		name: "shared-cell",
		register: func(r *Registrar) error {
			r.HandleCommand[exclusiveCmd](func() (Lock, Execute[int, int]) {
				return func(access ResourceAccess) { access.Exclusive() },
					func(_ Kernel, request int) int {
						*answer = request
						time.Sleep(time.Microsecond)
						return *answer
					}
			})
			return nil
		},
	})

	const callers = 8
	var group sync.WaitGroup
	wrong := make(chan [2]int, callers)
	for request := range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			response := engine.Executioner().ExecuteCommand[exclusiveCmd, int, int](request)
			if response != request {
				wrong <- [2]int{request, response}
			}
		}()
	}
	group.Wait()
	close(wrong)
	for pair := range wrong {
		t.Errorf("caller %d received %d", pair[0], pair[1])
	}
}

// Dump names the declaration, and only on the handler that made it. It is the
// human-readable half of the same fact mcpserver_architecture reports, and the
// only place a reader sees an exclusion at all: the conflict report pairs
// distinct handlers, so it never mentions one.
func TestKernel_DumpNamesTheExclusiveHandler(t *testing.T) {
	engine := New(nil).
		Handler(func(err error) error { t.Errorf("unexpected kernel error: %v", err); return err }).
		WithPlugins(testPlugin{
			name: "dumped",
			register: func(r *Registrar) error {
				r.HandleCommand[exclusiveCmd](gatedCommand(newGate(), func(access ResourceAccess) {
					access.Exclusive()
				}))
				r.HandleCommand[openCmd](gatedCommand(newGate(), nil))
				return nil
			},
		})
	go engine.Run()
	t.Cleanup(engine.Quit)
	<-engine.Ready()

	for _, line := range strings.Split(Dump(engine), "\n") {
		switch {
		case strings.Contains(line, TypeName(reflect.TypeFor[exclusiveCmd]())):
			if !strings.Contains(line, "exclusive") {
				t.Errorf("the declaring command dumps as %q, want it named exclusive", line)
			}
		case strings.Contains(line, TypeName(reflect.TypeFor[openCmd]())):
			if strings.Contains(line, "exclusive") {
				t.Errorf("a command declaring nothing dumps as %q", line)
			}
		}
	}
}
