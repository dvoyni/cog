package kernel

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"
)

type testHandlerA[T any] func() (Lock, Observe[T])
type testHandlerB[T any] func() (Lock, Observe[T])
type testHandlerC[T any] func() (Lock, Observe[T])
type testHandlerDone[T any] func() (Lock, Observe[T])
type testHandlerWriter[T any] func() (Lock, Observe[T])
type testHandlerReader[T any] func() (Lock, Observe[T])
type testHandlerFail[T any] func() (Lock, Observe[T])
type testHandlerContinue[T any] func() (Lock, Observe[T])
type testHandlerFirst[T any] func() (Lock, Observe[T])
type testHandlerLast[T any] func() (Lock, Observe[T])
type testHandlerInt[T any] func() (Lock, Observe[T])
type testHandlerString[T any] func() (Lock, Observe[T])
type testHandlerReplace[T any] func() (Lock, Observe[T])
type testDoubleCmd Command[int, int]
type testOtherDoubleCmd Command[int, int]
type testFailCmd Command[struct{}, int]
type testPanicCmd Command[struct{}, int]
type testMissingCmd Command[struct{}, int]
type testCounterResource int
type testLateResource int

type invocationContextKey struct{}

// observing and executing wrap a bare body as a factory that declares no locks.
func observing[TEvent any](body func(Kernel, TEvent) error) func() (Lock, Observe[TEvent]) {
	return func() (Lock, Observe[TEvent]) { return nil, body }
}

func executing[TRequest any, TResponse any](
	body func(Kernel, TRequest) (TResponse, error),
) Command[TRequest, TResponse] {
	return func() (Lock, Execute[TRequest, TResponse]) { return nil, body }
}

func orderTestSubscription[TEvent any](subscription *Ordering[TEvent], before, after []string) {
	for _, other := range before {
		switch other {
		case "a":
			subscription.Before[testHandlerA[TEvent]]()
		case "b":
			subscription.Before[testHandlerB[TEvent]]()
		case "fail":
			subscription.Before[testHandlerFail[TEvent]]()
		case "writer":
			subscription.Before[testHandlerWriter[TEvent]]()
		case "reader":
			subscription.Before[testHandlerReader[TEvent]]()
		}
	}
	for _, other := range after {
		switch other {
		case "a":
			subscription.After[testHandlerA[TEvent]]()
		case "b":
			subscription.After[testHandlerB[TEvent]]()
		case "fail":
			subscription.After[testHandlerFail[TEvent]]()
		case "writer":
			subscription.After[testHandlerWriter[TEvent]]()
		case "reader":
			subscription.After[testHandlerReader[TEvent]]()
		}
	}
}

func subscribeForTest[TEvent any](
	registry *Registrar, name string,
	lock Lock,
	before, after []string,
	body func(Kernel, TEvent) error,
) {
	factory := func() (Lock, Observe[TEvent]) { return lock, body }
	var subscription *Ordering[TEvent]
	switch name {
	case "a":
		subscription = registry.Subscribe[testHandlerA[TEvent]](factory)
	case "b":
		subscription = registry.Subscribe[testHandlerB[TEvent]](factory)
	case "c":
		subscription = registry.Subscribe[testHandlerC[TEvent]](factory)
	case "writer":
		subscription = registry.Subscribe[testHandlerWriter[TEvent]](factory)
	case "reader":
		subscription = registry.Subscribe[testHandlerReader[TEvent]](factory)
	default:
		panic("unknown test subscription " + name)
	}
	orderTestSubscription(subscription, before, after)
}

type testPlugin struct {
	name     PluginName
	deps     []PluginName
	register func(registry *Registrar) error
	start    func(Executioner) error
	stop     func(Executioner) error
}

func (p testPlugin) Name() PluginName           { return p.name }
func (p testPlugin) Dependencies() []PluginName { return p.deps }
func (p testPlugin) Register(registrar *Registrar, _ any) error {
	if p.register == nil {
		return nil
	}
	return p.register(registrar)
}
func (p testPlugin) Start(kernel Executioner) error {
	if p.start == nil {
		return nil
	}
	return p.start(kernel)
}
func (p testPlugin) Stop(kernel Executioner) error {
	if p.stop == nil {
		return nil
	}
	return p.stop(kernel)
}

// testHostPlugin is a Plugin that also implements Host.
type testHostPlugin struct {
	name PluginName
	run  func() error
}

func (p *testHostPlugin) Name() PluginName               { return p.name }
func (p *testHostPlugin) Dependencies() []PluginName     { return nil }
func (p *testHostPlugin) Register(*Registrar, any) error { return nil }
func (p *testHostPlugin) Run(Executioner) error          { return p.run() }

type publishingHostPlugin struct{}

func (p *publishingHostPlugin) Name() PluginName               { return "publishing-system" }
func (p *publishingHostPlugin) Dependencies() []PluginName     { return nil }
func (p *publishingHostPlugin) Register(*Registrar, any) error { return nil }
func (p *publishingHostPlugin) Run(kernel Executioner) error {
	kernel.PublishEvent(1)
	<-kernel.Context().Done()
	return nil
}

// configPlugin captures the config it is handed at Init, for config-passing tests.
type configPlugin struct {
	name PluginName
	got  *any
}

func (p configPlugin) Name() PluginName           { return p.name }
func (p configPlugin) Dependencies() []PluginName { return nil }
func (p configPlugin) Register(_ *Registrar, config any) error {
	*p.got = config
	return nil
}

type startOnlyPlugin struct {
	name    PluginName
	started *bool
}

func (p startOnlyPlugin) Name() PluginName               { return p.name }
func (p startOnlyPlugin) Dependencies() []PluginName     { return nil }
func (p startOnlyPlugin) Register(*Registrar, any) error { return nil }
func (p startOnlyPlugin) Start(Executioner) error {
	*p.started = true
	return nil
}

type stopOnlyPlugin struct {
	name    PluginName
	stopped *bool
}

func (p stopOnlyPlugin) Name() PluginName               { return p.name }
func (p stopOnlyPlugin) Dependencies() []PluginName     { return nil }
func (p stopOnlyPlugin) Register(*Registrar, any) error { return nil }
func (p stopOnlyPlugin) Stop(Executioner) error {
	*p.stopped = true
	return nil
}

// startEngine builds an engine bound to a test-scoped context.
func startEngine(t *testing.T, plugins ...Plugin) *Engine {
	t.Helper()
	return startEngineWithHandler(t, func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}, plugins...)
}

func startEngineWithHandler(t *testing.T, handler ErrorHandler, plugins ...Plugin) *Engine {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e := New(nil).Handler(handler).WithPlugins(plugins...)
	go e.Run(ctx)
	<-e.Ready()
	return e
}

// publishForTest publishes under a bounded context, so a stuck subscriber fails
// the publication instead of hanging the test.
func publishForTest[TEvent any](t *testing.T, engine *Engine, event TEvent) *Publication {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return engine.Executioner().WithContext(ctx).PublishEvent(event)
}

// waitFor blocks until the async event batch signals completion (or times out).
// Publishing is fire-and-forget, so tests register a last-in-order subscription
// that signals done and wait on it before asserting side effects.
func waitFor(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event subscribers")
	}
}

// Subscriptions run in topological order derived from their before/after edges.
func TestKernel_SubscriptionsRunInTopologicalOrder(t *testing.T) {
	var order []string
	record := func(name string) func(Kernel, int) error {
		return func(Kernel, int) error {
			order = append(order, name)
			return nil
		}
	}

	done := make(chan struct{})
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		// Registered out of order on purpose; edges define the order a<b<c.
		subscribeForTest[int](registry, "c", nil, nil, []string{"b"}, record("c"))
		subscribeForTest[int](registry, "a", nil, nil, nil, record("a"))
		subscribeForTest[int](registry, "b", nil, nil, []string{"a"}, record("b"))
		registry.Subscribe[testHandlerDone[int]](observing(func(Kernel, int) error {
			close(done)
			return nil
		})).Last()
		return nil
	}}

	e := startEngine(t, p)
	publishForTest(t, e, 1).Wait()

	want := []string{"a", "b", "c"}
	if !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestKernel_IndependentSubscribersRunConcurrently(t *testing.T) {
	startedA := make(chan struct{})
	startedB := make(chan struct{})
	release := make(chan struct{})
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerA[int]](observing(func(Kernel, int) error {
			close(startedA)
			<-release
			return nil
		}))
		registry.Subscribe[testHandlerB[int]](observing(func(Kernel, int) error {
			close(startedB)
			<-release
			return nil
		}))
		return nil
	}}
	e := startEngine(t, p)
	publication := publishForTest(t, e, 1)
	waitStarted := func(started <-chan struct{}) {
		t.Helper()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("independent subscriber did not start concurrently")
		}
	}
	waitStarted(startedA)
	waitStarted(startedB)
	close(release)
	if err := publication.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestKernel_SubscriberReceivesPublicationContext(t *testing.T) {
	got := false
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerA[int]](observing(func(k Kernel, _ int) error {
			got = k.Context() != nil
			return nil
		}))
		return nil
	}}
	e := startEngine(t, p)
	if err := publishForTest(t, e, 1).Wait(); err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("subscriber received nil context")
	}
}

func TestKernel_PublicationContextCancelsSubscriber(t *testing.T) {
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerA[int]](observing(func(k Kernel, _ int) error {
			<-k.Context().Done()
			return k.Context().Err()
		}))
		return nil
	}}
	e := startEngineWithHandler(t, func(error) bool { return false }, p)
	ctx, cancel := context.WithCancel(context.Background())
	publication := e.Executioner().WithContext(ctx).PublishEvent(1)
	cancel()
	if err := publication.Wait(); !errors.Is(err, context.Canceled) {
		t.Fatalf("publication error = %v, want context.Canceled", err)
	}
}

func TestKernel_SubscriptionsSortAfterAllPluginsInit(t *testing.T) {
	var order []string
	first := testPlugin{name: "first", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerFirst[int]](observing(func(Kernel, int) error {
			order = append(order, "first")
			return nil
		})).After[testHandlerLast[int]]()
		return nil
	}}
	later := testPlugin{name: "later", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerLast[int]](observing(func(Kernel, int) error {
			order = append(order, "later")
			return nil
		}))
		return nil
	}}

	e := startEngine(t, first, later)
	publishForTest(t, e, 1).Wait()
	if len(order) != 2 || order[0] != "later" || order[1] != "first" {
		t.Fatalf("order = %v, want [later first]", order)
	}
}

func TestKernel_DescribeReportsOwnersAndSubscriptionDependencies(t *testing.T) {
	provider := testPlugin{name: "provider", register: func(registry *Registrar) error {
		registry.InitResource(testCounterResource(1))
		registry.HandleCommand[testDoubleCmd](executing(func(Kernel, int) (int, error) { return 0, nil }))
		registry.Subscribe[testHandlerA[int]](observing(func(Kernel, int) error { return nil }))
		return nil
	}}
	consumer := testPlugin{name: "consumer", deps: []PluginName{"provider"}, register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerB[int]](observing(func(Kernel, int) error { return nil })).
			After[testHandlerA[int]]()
		return nil
	}}
	e := New(nil).WithPlugins(consumer, provider)
	description := e.Describe()

	if len(description.Plugins) != 2 || description.Plugins[0].Name != "provider" || description.Plugins[1].Name != "consumer" {
		t.Fatalf("plugins = %+v", description.Plugins)
	}
	if len(description.Resources) != 1 || description.Resources[0].Owner != "provider" {
		t.Fatalf("resources = %+v", description.Resources)
	}
	if len(description.Commands) != 1 || description.Commands[0].Owner != "provider" {
		t.Fatalf("commands = %+v", description.Commands)
	}
	if Dump(e) == "" {
		t.Fatal("Dump returned an empty architecture table")
	}
	for _, subscription := range description.Subscriptions {
		if subscription.Type == reflect.TypeFor[testHandlerB[int]]() {
			if subscription.Owner != "consumer" || !slices.Contains(subscription.DependsOn, reflect.TypeFor[testHandlerA[int]]()) {
				t.Fatalf("consumer subscription = %+v", subscription)
			}
			return
		}
	}
	t.Fatal("consumer subscription missing from description")
}

// A handle bound before its resource is initialized still observes the value,
// because registration creates the cell once and never replaces it. Binding and
// initialization happen in one plugin so the ordering under test is registration
// order within Register, not plugin order.
func TestKernel_ResourceHandleBoundBeforeInitializationObservesValue(t *testing.T) {
	var got int
	var late Read[testLateResource]
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerReader[int]](func() (Lock, Observe[int]) {
			return func(access ResourceAccess) {
					late = access.GetRead[testLateResource]()
				}, func(Kernel, int) error {
					got = int(late.Get())
					return nil
				}
		})
		registry.InitResource(testLateResource(42))
		return nil
	}}

	e := startEngine(t, p)
	publishForTest(t, e, 1).Wait()
	if got != 42 {
		t.Fatalf("resource value = %d, want 42", got)
	}
}

// Reading another plugin's resource is allowed once the dependency is declared,
// regardless of the order the plugins were handed to WithPlugins.
func TestKernel_DeclaredDependencyMayLockAnotherPluginsResource(t *testing.T) {
	var got int
	var late Read[testLateResource]
	subscriber := testPlugin{name: "subscriber", deps: []PluginName{"resource-owner"}, register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerReader[int]](func() (Lock, Observe[int]) {
			return func(access ResourceAccess) {
					late = access.GetRead[testLateResource]()
				}, func(Kernel, int) error {
					got = int(late.Get())
					return nil
				}
		})
		return nil
	}}
	resourceOwner := testPlugin{name: "resource-owner", register: func(registry *Registrar) error {
		registry.InitResource(testLateResource(42))
		return nil
	}}

	e := startEngine(t, subscriber, resourceOwner)
	publishForTest(t, e, 1).Wait()
	if got != 42 {
		t.Fatalf("resource value = %d, want 42", got)
	}
}

func TestKernel_MissingDeclaredResourceFailsFinalization(t *testing.T) {
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerReader[int]](func() (Lock, Observe[int]) {
			return func(access ResourceAccess) {
				access.GetRead[testLateResource]()
			}, func(Kernel, int) error { return nil }
		})
		return nil
	}}
	var handled error
	New(nil).Handler(func(err error) bool { handled = err; return true }).WithPlugins(p)
	var missing ErrMissingResource
	if !errors.As(handled, &missing) || missing.Type != reflect.TypeFor[testLateResource]() {
		t.Fatalf("handled = %v, want missing testLateResource", handled)
	}
}

// A writer subscription's value is visible to a later reader subscription through
// the shared resource store.
func TestKernel_ResourceReadWrite(t *testing.T) {
	var got int
	done := make(chan struct{})
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.InitResource(testCounterResource(0))
		var writable Write[testCounterResource]
		subscribeForTest(registry, "writer",
			func(access ResourceAccess) { writable = access.GetWrite[testCounterResource]() },
			nil, nil,
			func(_ Kernel, event int) error {
				writable.Set(testCounterResource(event * 2))
				return nil
			})
		var readable Read[testCounterResource]
		subscribeForTest(registry, "reader",
			func(access ResourceAccess) { readable = access.GetRead[testCounterResource]() },
			nil, []string{"writer"},
			func(Kernel, int) error {
				got = int(readable.Get())
				return nil
			})
		registry.Subscribe[testHandlerDone[int]](observing(func(Kernel, int) error {
			close(done)
			return nil
		})).Last()
		return nil
	}}

	e := startEngine(t, p)
	e.Executioner().PublishEvent(21)
	waitFor(t, done)
	if got != 42 {
		t.Fatalf("got = %d, want 42", got)
	}
}

// A resource's initial value is available before any write.
func TestKernel_ResourceInitialValue(t *testing.T) {
	var got int
	done := make(chan struct{})
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.InitResource(testCounterResource(42))
		var readable Read[testCounterResource]
		subscribeForTest(registry, "reader",
			func(access ResourceAccess) { readable = access.GetRead[testCounterResource]() },
			nil, nil,
			func(Kernel, int) error {
				got = int(readable.Get())
				return nil
			})
		registry.Subscribe[testHandlerDone[int]](observing(func(Kernel, int) error {
			close(done)
			return nil
		})).Last()
		return nil
	}}

	e := startEngine(t, p)
	e.Executioner().PublishEvent(1)
	waitFor(t, done)
	if got != 42 {
		t.Fatalf("counter = %d, want initial value 42 (read before any write)", got)
	}
}

func TestKernel_RegistrationFailureStopsInitialization(t *testing.T) {
	boom := errors.New("init boom")
	initialized := false
	failing := testPlugin{name: "failing", register: func(*Registrar) error { return boom }}
	following := testPlugin{name: "following", register: func(*Registrar) error {
		initialized = true
		return nil
	}}
	var handled error
	New(nil).Handler(func(err error) bool { handled = err; return false }).WithPlugins(failing, following)
	if !errors.Is(handled, boom) || initialized {
		t.Fatalf("handled error = %v, following initialized = %v", handled, initialized)
	}
}

func TestKernel_PluginStartExecutesCommandsAfterRegistration(t *testing.T) {
	got := 0
	provider := testPlugin{name: "provider", register: func(registry *Registrar) error {
		registry.HandleCommand[testDoubleCmd](executing(func(Kernel, int) (int, error) {
			return 42, nil
		}))
		return nil
	}}
	consumer := testPlugin{name: "consumer", deps: []PluginName{"provider"}, start: func(k Executioner) error {
		var err error
		got, err = k.ExecuteCommand[testDoubleCmd](0)
		return err
	}}

	startEngine(t, consumer, provider)
	if got != 42 {
		t.Fatalf("startup command response = %d, want 42", got)
	}
}

func TestKernel_DuplicateResourceRegistrationFails(t *testing.T) {
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.InitResource(testCounterResource(1))
		registry.InitResource(testCounterResource(2))
		return nil
	}}
	var handled error
	New(nil).Handler(func(err error) bool { handled = err; return true }).WithPlugins(p)
	var duplicate ErrDuplicateRegistration
	if !errors.As(handled, &duplicate) || duplicate.Kind != "resource" {
		t.Fatalf("handled error = %v, want duplicate resource registration", handled)
	}
}

func TestResourceAccess_WriteDeclarationSupersedesRead(t *testing.T) {
	access := newResourceAccess(map[reflect.Type]*resource{})
	access.GetRead[testCounterResource]()
	access.GetWrite[testCounterResource]()
	id := reflect.TypeFor[testCounterResource]()
	if _, ok := access.read[id]; ok {
		t.Fatal("resource remained in read set after write declaration")
	}
	if _, ok := access.write[id]; !ok {
		t.Fatal("resource missing from write set")
	}
}

func TestResourceAccess_BindingCreatesCellAndSetInitializes(t *testing.T) {
	access := newResourceAccess(map[reflect.Type]*resource{})
	counter := access.GetWrite[testCounterResource]()
	if got := counter.Get(); got != 0 {
		t.Fatalf("uninitialized resource = %d, want zero value", got)
	}
	counter.Set(1)
	if got := counter.Get(); got != 1 {
		t.Fatalf("resource = %d, want 1", got)
	}
}

// A command's write declaration grants both reads and writes on the handle.
func TestKernel_ExecuteCommand(t *testing.T) {
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.InitResource(testCounterResource(0))
		var counter Write[testCounterResource]
		registry.HandleCommand[testDoubleCmd](func() (Lock, Execute[int, int]) {
			return func(access ResourceAccess) {
					counter = access.GetWrite[testCounterResource]()
				}, func(_ Kernel, request int) (int, error) {
					counter.Set(testCounterResource(request))
					return int(counter.Get()) * 2, nil
				}
		})
		return nil
	}}

	e := startEngine(t, p)
	got, err := e.Executioner().ExecuteCommand[testDoubleCmd](21)
	if err != nil || got != 42 {
		t.Fatalf("got = %d, %v; want 42, nil", got, err)
	}
}

// A Uses declaration is what grants a nested dispatch: the declaring handler
// ends up holding the used command's locks even though it names none itself.
func TestKernel_UsesGrantsNestedDispatch(t *testing.T) {
	var got int
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.InitResource(testCounterResource(7))
		var counter Write[testCounterResource]
		registry.HandleCommand[testDoubleCmd](func() (Lock, Execute[int, int]) {
			return func(access ResourceAccess) {
				counter = access.GetWrite[testCounterResource]()
			}, func(Kernel, int) (int, error) { return int(counter.Get()), nil }
		})
		var double func(Kernel, int) (int, error)
		registry.Subscribe[testHandlerA[int]](func() (Lock, Observe[int]) {
			return func(access ResourceAccess) {
					double = access.Uses[testDoubleCmd]()
				}, func(k Kernel, _ int) error {
					value, err := double(k, 0)
					got = value
					return err
				}
		})
		return nil
	}}
	e := startEngine(t, p)
	if err := publishForTest(t, e, 1).Wait(); err != nil {
		t.Fatal(err)
	}
	if got != 7 {
		t.Fatalf("command response = %d, want 7", got)
	}
	subscriber := e.registry.subscriptions[reflect.TypeFor[int]()][0]
	_, access := subscriber.coupling()
	if _, held := access.write[reflect.TypeFor[testCounterResource]()]; !held {
		t.Fatal("Uses did not fold the command's write lock into the subscriber")
	}
}

// Uses is transitive: a handler holds the locks of everything reachable through
// the commands it declares, not just the ones it names directly.
func TestKernel_UsesWidensLockSetTransitively(t *testing.T) {
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.InitResource(testCounterResource(0))
		registry.InitResource(testLateResource(0))
		registry.HandleCommand[testFailCmd](func() (Lock, Execute[struct{}, int]) {
			return func(access ResourceAccess) {
				access.GetWrite[testLateResource]()
			}, func(Kernel, struct{}) (int, error) { return 0, nil }
		})
		registry.HandleCommand[testOtherDoubleCmd](func() (Lock, Execute[int, int]) {
			return func(access ResourceAccess) {
				access.GetRead[testCounterResource]()
				access.Uses[testFailCmd]()
			}, func(Kernel, int) (int, error) { return 0, nil }
		})
		registry.HandleCommand[testDoubleCmd](func() (Lock, Execute[int, int]) {
			return func(access ResourceAccess) {
				access.GetWrite[testCounterResource]()
				access.Uses[testOtherDoubleCmd]()
			}, func(Kernel, int) (int, error) { return 0, nil }
		})
		return nil
	}}
	e := startEngine(t, p)

	outer := e.registry.commands[reflect.TypeFor[testDoubleCmd]()].resources
	if _, held := outer.write[reflect.TypeFor[testLateResource]()]; !held {
		t.Fatal("transitive Uses did not reach the innermost command's lock")
	}
	// The outer command writes the counter, so absorbing the middle command's read
	// must not demote it.
	if _, demoted := outer.read[reflect.TypeFor[testCounterResource]()]; demoted {
		t.Fatal("absorbed read superseded the declaring handler's write")
	}
}

func TestKernel_UsesCycleFailsComposition(t *testing.T) {
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.HandleCommand[testDoubleCmd](func() (Lock, Execute[int, int]) {
			return func(access ResourceAccess) { access.Uses[testOtherDoubleCmd]() },
				func(Kernel, int) (int, error) { return 0, nil }
		})
		registry.HandleCommand[testOtherDoubleCmd](func() (Lock, Execute[int, int]) {
			return func(access ResourceAccess) { access.Uses[testDoubleCmd]() },
				func(Kernel, int) (int, error) { return 0, nil }
		})
		return nil
	}}
	var handled error
	New(nil).Handler(func(err error) bool { handled = err; return true }).WithPlugins(p)
	var cycle ErrUsingCommandCycle
	if !errors.As(handled, &cycle) || len(cycle.Commands) < 2 {
		t.Fatalf("handled error = %v, want ErrUsingCommandCycle", handled)
	}
}

func TestKernel_UsesUnknownCommandFailsComposition(t *testing.T) {
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.HandleCommand[testDoubleCmd](func() (Lock, Execute[int, int]) {
			return func(access ResourceAccess) { access.Uses[testMissingCmd]() },
				func(Kernel, int) (int, error) { return 0, nil }
		})
		return nil
	}}
	var handled error
	New(nil).Handler(func(err error) bool { handled = err; return true }).WithPlugins(p)
	var unknown ErrUsingUnknownCommand
	if !errors.As(handled, &unknown) || unknown.Command != reflect.TypeFor[testMissingCmd]() {
		t.Fatalf("handled error = %v, want ErrUsingUnknownCommand for testMissingCmd", handled)
	}
}

// A Uses declaration binds after every plugin has registered, so the command may
// be registered by a plugin that comes later.
func TestKernel_UsesResolvesRegardlessOfRegistrationOrder(t *testing.T) {
	var got int
	user := testPlugin{name: "user", deps: []PluginName{"provider"}, register: func(registry *Registrar) error {
		var double func(Kernel, int) (int, error)
		registry.HandleCommand[testOtherDoubleCmd](func() (Lock, Execute[int, int]) {
			return func(access ResourceAccess) {
					double = access.Uses[testDoubleCmd]()
				}, func(k Kernel, request int) (int, error) {
					return double(k, request)
				}
		})
		return nil
	}}
	provider := testPlugin{name: "provider", register: func(registry *Registrar) error {
		registry.InitResource(testCounterResource(21))
		var counter Write[testCounterResource]
		registry.HandleCommand[testDoubleCmd](func() (Lock, Execute[int, int]) {
			return func(access ResourceAccess) {
				counter = access.GetWrite[testCounterResource]()
			}, func(Kernel, int) (int, error) { return int(counter.Get()) * 2, nil }
		})
		return nil
	}}

	e := startEngine(t, user, provider)
	got, err := e.Executioner().ExecuteCommand[testOtherDoubleCmd](0)
	if err != nil || got != 42 {
		t.Fatalf("response = %d, %v; want 42, nil", got, err)
	}
}

// ExecuteCommandAsync runs a top-level task with its own locks, so it works from
// a handler that holds none of them.
func TestKernel_ExecuteCommandAsyncAcquiresOwnLocks(t *testing.T) {
	applied := make(chan int, 1)
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.InitResource(testCounterResource(7))
		var counter Write[testCounterResource]
		registry.HandleCommand[testDoubleCmd](func() (Lock, Execute[int, int]) {
			return func(access ResourceAccess) {
					counter = access.GetWrite[testCounterResource]()
				}, func(Kernel, int) (int, error) {
					applied <- int(counter.Get())
					return 0, nil
				}
		})
		registry.Subscribe[testHandlerA[int]](observing(func(k Kernel, _ int) error {
			k.ExecuteCommandAsync[testDoubleCmd](0)
			return nil
		}))
		return nil
	}}
	e := startEngine(t, p)
	if err := publishForTest(t, e, 1).Wait(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-applied:
		if got != 7 {
			t.Fatalf("counter = %d, want 7", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for asynchronous command")
	}
}

func TestKernel_ExecuteCommandAsyncReportsErrors(t *testing.T) {
	boom := errors.New("async boom")
	errCh := make(chan error, 1)
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.HandleCommand[testFailCmd](executing(func(Kernel, struct{}) (int, error) { return 0, boom }))
		registry.Subscribe[testHandlerA[int]](observing(func(k Kernel, _ int) error {
			k.ExecuteCommandAsync[testFailCmd](struct{}{})
			return nil
		}))
		return nil
	}}
	e := startEngineWithHandler(t, func(err error) bool { errCh <- err; return false }, p)
	if err := publishForTest(t, e, 1).Wait(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-errCh:
		if !errors.Is(got, boom) {
			t.Fatalf("handled error = %v, want %v", got, boom)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for asynchronous command error")
	}
}

func TestKernel_ZeroKernelPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("zero Kernel did not panic")
		}
	}()
	var zero Kernel
	zero.ReportError(errors.New("boom"))
}

func TestKernel_CommandTypesWithSameSignatureAreIndependent(t *testing.T) {
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.HandleCommand[testDoubleCmd](executing(func(Kernel, int) (int, error) { return 1, nil }))
		registry.HandleCommand[testOtherDoubleCmd](executing(func(Kernel, int) (int, error) { return 2, nil }))
		return nil
	}}
	e := startEngine(t, p)
	first, firstErr := e.Executioner().ExecuteCommand[testDoubleCmd](0)
	second, secondErr := e.Executioner().ExecuteCommand[testOtherDoubleCmd](0)
	if firstErr != nil || secondErr != nil || first != 1 || second != 2 {
		t.Fatalf("responses = %d/%v, %d/%v; want 1/nil, 2/nil", first, firstErr, second, secondErr)
	}
}

func TestKernel_DuplicateCommandRegistrationFails(t *testing.T) {
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.HandleCommand[testDoubleCmd](executing(func(Kernel, int) (int, error) { return 1, nil }))
		registry.HandleCommand[testDoubleCmd](executing(func(Kernel, int) (int, error) { return 2, nil }))
		return nil
	}}
	var handled error
	New(nil).Handler(func(err error) bool { handled = err; return true }).WithPlugins(p)
	var duplicate ErrDuplicateRegistration
	if !errors.As(handled, &duplicate) || duplicate.Kind != "command" {
		t.Fatalf("handled error = %v, want duplicate command registration", handled)
	}
}

func TestKernel_ExecuteCommandReturnsSystemError(t *testing.T) {
	boom := errors.New("command boom")
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.HandleCommand[testFailCmd](executing(func(Kernel, struct{}) (int, error) { return 99, boom }))
		return nil
	}}
	var handled error
	e := startEngineWithHandler(t, func(err error) bool { handled = err; return false }, p)

	got, err := e.Executioner().ExecuteCommand[testFailCmd](struct{}{})
	if got != 0 {
		t.Fatalf("response = %d, want zero", got)
	}
	if !errors.Is(err, boom) || handled != nil {
		t.Fatalf("returned error = %v, handled error = %v; want boom returned only", err, handled)
	}
}

func TestKernel_CommandPanicReturnsTypedFault(t *testing.T) {
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.HandleCommand[testPanicCmd](executing(func(Kernel, struct{}) (int, error) {
			panic("boom")
		}))
		return nil
	}}
	e := startEngine(t, p)

	_, err := e.Executioner().ExecuteCommand[testPanicCmd](struct{}{})
	var panicErr ErrPluginPanic
	if !errors.As(err, &panicErr) || panicErr.Plugin != "p" || len(panicErr.Stack) == 0 {
		t.Fatalf("error = %#v, want typed plugin panic with stack", err)
	}
}

func TestKernel_CommandReceivesInvocationContext(t *testing.T) {
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.HandleCommand[testDoubleCmd](executing(func(k Kernel, _ int) (int, error) {
			return k.Context().Value(invocationContextKey{}).(int), nil
		}))
		return nil
	}}
	e := startEngine(t, p)
	ctx := context.WithValue(context.Background(), invocationContextKey{}, 42)
	got, err := e.Executioner().WithContext(ctx).ExecuteCommand[testDoubleCmd](0)
	if err != nil || got != 42 {
		t.Fatalf("response = %d, %v; want 42, nil", got, err)
	}
}

func TestKernel_ExecuteUnknownCommandReturnsError(t *testing.T) {
	var handled error
	e := startEngineWithHandler(t, func(err error) bool { handled = err; return false })

	got, err := e.Executioner().ExecuteCommand[testMissingCmd](struct{}{})
	if got != 0 {
		t.Fatalf("response = %d, want zero", got)
	}
	var unknown ErrExecutingUnknownCommand[testMissingCmd]
	if !errors.As(err, &unknown) || handled != nil {
		t.Fatalf("returned error = %v, handled error = %v; want unknown command returned only", err, handled)
	}
}

// WithPlugins reports ErrMissingPluginDependency when a plugin declares a
// dependency on a plugin that was not registered.
func TestKernel_MissingPluginDependencyIsReported(t *testing.T) {
	var handled error
	dependent := testPlugin{name: "dependent", deps: []PluginName{"absent"}, register: func(*Registrar) error { return nil }}
	New(nil).Handler(func(err error) bool { handled = err; return true }).WithPlugins(dependent)

	var missing ErrMissingPluginDependency
	if !errors.As(handled, &missing) {
		t.Fatalf("handled error = %v, want ErrMissingPluginDependency", handled)
	}
	if missing.Plugin != "dependent" || missing.Dependency != "absent" {
		t.Fatalf("missing = %+v, want {Plugin:dependent Dependency:absent}", missing)
	}
}

func TestKernel_CompositionFailureClosesReadyEvenWhenHandlerContinues(t *testing.T) {
	dependent := testPlugin{name: "dependent", deps: []PluginName{"absent"}}
	e := New(nil).Handler(func(error) bool { return false }).WithPlugins(dependent)
	select {
	case <-e.Ready():
	default:
		t.Fatal("Ready remained open after composition failure")
	}
}

// Dependencies determine initialization order while unrelated plugins retain
// their caller order.
func TestKernel_PluginsInitializeInDependencyOrder(t *testing.T) {
	var order []PluginName
	record := func(name PluginName) func(*Registrar) error {
		return func(*Registrar) error {
			order = append(order, name)
			return nil
		}
	}
	dependent := testPlugin{name: "dependent", deps: []PluginName{"provider"}, register: record("dependent")}
	unrelated := testPlugin{name: "unrelated", register: record("unrelated")}
	provider := testPlugin{name: "provider", register: record("provider")}

	New(nil).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(dependent, unrelated, provider)

	want := []PluginName{"unrelated", "provider", "dependent"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("initialization order = %v, want %v", order, want)
	}
}

func TestKernel_PluginDependencyCycleIsReportedBeforeInitialization(t *testing.T) {
	initialized := false
	first := testPlugin{name: "first", deps: []PluginName{"second"}, register: func(*Registrar) error {
		initialized = true
		return nil
	}}
	second := testPlugin{name: "second", deps: []PluginName{"first"}, register: func(*Registrar) error {
		initialized = true
		return nil
	}}

	var handled error
	New(nil).Handler(func(err error) bool { handled = err; return true }).WithPlugins(first, second)
	var cycle ErrPluginDependencyCycle
	if !errors.As(handled, &cycle) {
		t.Fatalf("handled error = %v, want ErrPluginDependencyCycle", handled)
	}
	if initialized {
		t.Fatal("plugin initialized before dependency graph validation")
	}
}

func TestKernel_StartsInDependencyOrderAndStopsInReverse(t *testing.T) {
	var order []string
	first := testPlugin{name: "first", start: func(Executioner) error {
		order = append(order, "start-first")
		return nil
	}, stop: func(Executioner) error {
		order = append(order, "stop-first")
		return nil
	}}
	second := testPlugin{name: "second", deps: []PluginName{"first"}, start: func(Executioner) error {
		order = append(order, "start-second")
		return nil
	}, stop: func(Executioner) error {
		order = append(order, "stop-second")
		return nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	e := New(nil).WithPlugins(second, first)
	done := make(chan struct{})
	go func() { e.Run(ctx); close(done) }()
	<-e.Ready()
	cancel()
	<-done

	want := []string{"start-first", "start-second", "stop-second", "stop-first"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("lifecycle order = %v, want %v", order, want)
	}
}

// Stop's Kernel outlives engine cancellation, so shutdown work still has one.
func TestKernel_StopReceivesLiveShutdownContext(t *testing.T) {
	var stopErr error
	p := testPlugin{name: "p", stop: func(k Executioner) error {
		stopErr = k.Context().Err()
		return nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	e := New(nil).WithPlugins(p)
	done := make(chan struct{})
	go func() { e.Run(ctx); close(done) }()
	<-e.Ready()
	cancel()
	<-done
	if stopErr != nil {
		t.Fatalf("shutdown context error = %v, want nil", stopErr)
	}
}

func TestKernel_OptionalLifecycleCapabilitiesAreIndependent(t *testing.T) {
	started := false
	stopped := false
	ctx, cancel := context.WithCancel(context.Background())
	e := New(nil).WithPlugins(
		startOnlyPlugin{name: "starter", started: &started},
		stopOnlyPlugin{name: "stopper", stopped: &stopped},
	)
	done := make(chan struct{})
	go func() { e.Run(ctx); close(done) }()
	<-e.Ready()
	if !started {
		t.Fatal("PluginStarter.Start was not called")
	}
	cancel()
	<-done
	if !stopped {
		t.Fatal("PluginStopper.Stop was not called")
	}
}

func TestKernel_StartFailureStopsOnlyStartedPlugins(t *testing.T) {
	boom := errors.New("start boom")
	var order []string
	first := testPlugin{name: "first", stop: func(Executioner) error {
		order = append(order, "stop-first")
		return nil
	}}
	second := testPlugin{name: "second", deps: []PluginName{"first"}, start: func(Executioner) error {
		return boom
	}, stop: func(Executioner) error {
		order = append(order, "stop-second")
		return nil
	}}
	third := testPlugin{name: "third", deps: []PluginName{"second"}, start: func(Executioner) error {
		order = append(order, "start-third")
		return nil
	}}
	var handled error
	New(nil).Handler(func(err error) bool { handled = err; return false }).WithPlugins(third, second, first).Run(context.Background())
	if !errors.Is(handled, boom) || !reflect.DeepEqual(order, []string{"stop-first"}) {
		t.Fatalf("handled = %v, lifecycle order = %v", handled, order)
	}
}

func TestKernel_ShutdownAttemptsAllPluginsAndAggregatesErrors(t *testing.T) {
	firstErr := errors.New("first stop")
	secondErr := errors.New("second stop")
	var stopped []string
	first := testPlugin{name: "first", stop: func(Executioner) error {
		stopped = append(stopped, "first")
		return firstErr
	}}
	second := testPlugin{name: "second", deps: []PluginName{"first"}, stop: func(Executioner) error {
		stopped = append(stopped, "second")
		return secondErr
	}}
	ctx, cancel := context.WithCancel(context.Background())
	var handled error
	e := New(nil).Handler(func(err error) bool { handled = err; return false }).WithPlugins(second, first)
	done := make(chan struct{})
	go func() { e.Run(ctx); close(done) }()
	<-e.Ready()
	cancel()
	<-done

	if !reflect.DeepEqual(stopped, []string{"second", "first"}) {
		t.Fatalf("stopped = %v", stopped)
	}
	if !errors.Is(handled, firstErr) || !errors.Is(handled, secondErr) {
		t.Fatalf("handled = %v, want both shutdown errors", handled)
	}
}

func TestKernel_MultipleHostsFailComposition(t *testing.T) {
	first := &testHostPlugin{name: "first", run: func() error { return nil }}
	second := &testHostPlugin{name: "second", run: func() error { return nil }}
	var handled error
	New(nil).Handler(func(err error) bool { handled = err; return true }).WithPlugins(first, second)
	var multiple ErrMultipleHosts
	if !errors.As(handled, &multiple) {
		t.Fatalf("handled = %v, want ErrMultipleHosts", handled)
	}
}

// A host plugin's Run is invoked after init, and the engine cancels its context
// once the host returns.
func TestKernel_HostRunsThenShutsDown(t *testing.T) {
	ran := false
	sp := &testHostPlugin{name: "sys", run: func() error { ran = true; return nil }}

	e := New(nil).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(sp).Run(context.Background())
	if !ran {
		t.Fatal("host plugin Run was not called")
	}

	// Run blocked until the host returned, then canceled the engine ctx.
	select {
	case <-e.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("engine context not canceled after host returned")
	}
}

// A host plugin's Run error is sent to the central handler and terminates.
func TestKernel_HostRunError(t *testing.T) {
	sentinel := errors.New("system boom")
	sp := &testHostPlugin{name: "sys", run: func() error { return sentinel }}

	var got error
	New(nil).Handler(func(err error) bool { got = err; return true }).WithPlugins(sp).Run(context.Background())
	if !errors.Is(got, sentinel) {
		t.Fatalf("handled error = %v, want %v", got, sentinel)
	}
}

// An asynchronous subscriber failure reaches the central handler, whose true
// result cancels the engine and stops the host.
func TestKernel_AsyncSubscriberErrorTerminatesRun(t *testing.T) {
	boom := errors.New("async boom")
	subscriber := testPlugin{name: "subscriber", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerFail[int]](observing(func(Kernel, int) error { return boom }))
		return nil
	}}

	var got error
	New(nil).Handler(func(err error) bool { got = err; return true }).
		WithPlugins(subscriber, &publishingHostPlugin{}).
		Run(context.Background())
	if !errors.Is(got, boom) {
		t.Fatalf("handled error = %v, want %v", got, boom)
	}
}

// Without a Host, asynchronous failures still reach the central handler.
func TestKernel_AsyncSubscriberErrorHandledWithoutHost(t *testing.T) {
	boom := errors.New("async boom")
	subscriber := testPlugin{name: "subscriber", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerFail[int]](observing(func(Kernel, int) error { return boom }))
		return nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	e := New(nil).Handler(func(err error) bool { errCh <- err; return true }).WithPlugins(subscriber)
	go e.Run(ctx)
	<-e.Ready()

	e.Executioner().PublishEvent(1)
	select {
	case got := <-errCh:
		if !errors.Is(got, boom) {
			t.Fatalf("handled error = %v, want %v", got, boom)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for async failure")
	}
}

// A failed subscriber blocks its dependency descendants even when the central
// handler elects to keep the engine running.
func TestKernel_EventFailureSkipsDependents(t *testing.T) {
	boom := errors.New("handled boom")
	ran := false
	subscriber := testPlugin{name: "subscriber", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerFail[int]](observing(func(Kernel, int) error { return boom }))
		registry.Subscribe[testHandlerContinue[int]](observing(func(Kernel, int) error {
			ran = true
			return nil
		})).After[testHandlerFail[int]]()
		return nil
	}}
	var got error
	e := startEngineWithHandler(t, func(err error) bool { got = err; return false }, subscriber)
	err := publishForTest(t, e, 1).Wait()
	if !errors.Is(got, boom) || !errors.Is(err, boom) || ran {
		t.Fatalf("handled error = %v, publication error = %v, dependent ran = %v", got, err, ran)
	}
}

func TestKernel_ErrorHandlerCanContinueAsyncEventBatch(t *testing.T) {
	boom := errors.New("handled boom")
	ran := false
	subscriber := testPlugin{name: "subscriber", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerFail[int]](observing(func(Kernel, int) error { return boom }))
		registry.Subscribe[testHandlerContinue[int]](observing(func(Kernel, int) error {
			ran = true
			return nil
		})).After[testHandlerFail[int]]()
		return nil
	}}
	errCh := make(chan error, 1)
	e := startEngineWithHandler(t, func(err error) bool { errCh <- err; return false }, subscriber)

	publication := publishForTest(t, e, 1)
	select {
	case got := <-errCh:
		if !errors.Is(got, boom) {
			t.Fatalf("handled error = %v, want %v", got, boom)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for handled error")
	}
	if err := publication.Wait(); !errors.Is(err, boom) {
		t.Fatalf("publication error = %v, want %v", err, boom)
	}
	if ran {
		t.Fatal("dependent subscriber ran after predecessor failure")
	}
}

// A cyclic before/after constraint is rejected at construction time.
func TestKernel_SubscriptionCycleDetected(t *testing.T) {
	noop := observing(func(Kernel, int) error { return nil })
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerA[int]](noop).After[testHandlerB[int]]()
		registry.Subscribe[testHandlerB[int]](noop).After[testHandlerA[int]]()
		return nil
	}}

	var err error
	New(nil).Handler(func(got error) bool { err = got; return true }).WithPlugins(p)
	var cycleErr ErrSubscriptionCycle
	if !errors.As(err, &cycleErr) {
		t.Fatalf("err = %v, want ErrSubscriptionCycle", err)
	}
}

func TestKernel_DuplicateSubscriptionRegistrationFails(t *testing.T) {
	oldRan := false
	newRan := false
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerReplace[int]](observing(func(Kernel, int) error { oldRan = true; return nil }))
		registry.Subscribe[testHandlerReplace[int]](observing(func(Kernel, int) error { newRan = true; return nil }))
		return nil
	}}
	var handled error
	New(nil).Handler(func(err error) bool { handled = err; return true }).WithPlugins(p)
	var duplicate ErrDuplicateRegistration
	if !errors.As(handled, &duplicate) || duplicate.Kind != "subscription" {
		t.Fatalf("handled error = %v, want duplicate subscription registration", handled)
	}
	if oldRan || newRan {
		t.Fatal("subscription ran during failed registration")
	}
}

// A wildcard in After/Before pins a subscription last/first regardless of the
// others.
func TestKernel_WildcardOrdering(t *testing.T) {
	var order []string
	var orderMu sync.Mutex
	record := func(name string) func(Kernel, int) error {
		return func(Kernel, int) error {
			orderMu.Lock()
			defer orderMu.Unlock()
			order = append(order, name)
			return nil
		}
	}

	done := make(chan struct{})
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerLast[int]](observing(record("last"))).Last()
		registry.Subscribe[testHandlerA[int]](observing(record("a")))
		registry.Subscribe[testHandlerFirst[int]](observing(record("first"))).First()
		registry.Subscribe[testHandlerB[int]](observing(record("b")))
		registry.Subscribe[testHandlerDone[int]](observing(func(Kernel, int) error {
			close(done)
			return nil
		})).Last()
		return nil
	}}

	e := startEngine(t, p)
	publishForTest(t, e, 1).Wait()

	if len(order) != 4 {
		t.Fatalf("order = %v, want 4 handlers", order)
	}
	if order[0] != "first" {
		t.Fatalf("order = %v, want 'first' to run first", order)
	}
	if order[len(order)-1] != "last" {
		t.Fatalf("order = %v, want 'last' to run last", order)
	}
}

func TestKernel_FirstAndLastPhasesAllowInternalDependencies(t *testing.T) {
	var order []string
	record := func(name string) func(Kernel, int) error {
		return func(Kernel, int) error {
			order = append(order, name)
			return nil
		}
	}
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerA[int]](observing(record("first-a"))).First()
		registry.Subscribe[testHandlerB[int]](observing(record("first-b"))).First().After[testHandlerA[int]]()
		registry.Subscribe[testHandlerC[int]](observing(record("ordinary")))
		registry.Subscribe[testHandlerLast[int]](observing(record("last-a"))).Last()
		registry.Subscribe[testHandlerDone[int]](observing(record("last-b"))).Last().After[testHandlerLast[int]]()
		return nil
	}}
	e := startEngine(t, p)
	if err := publishForTest(t, e, 1).Wait(); err != nil {
		t.Fatal(err)
	}
	want := []string{"first-a", "first-b", "ordinary", "last-a", "last-b"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

// Publishing an event type with no subscribers is a harmless no-op.
func TestKernel_PublishEventWithoutSubscribers(t *testing.T) {
	e := startEngine(t, testPlugin{name: "p", register: func(*Registrar) error { return nil }})
	e.Executioner().PublishEvent(1)
}

// Event identity is the concrete event type, so different types dispatch to
// independent subscriber sets.
func TestKernel_EventTypesDispatchIndependently(t *testing.T) {
	var gotInt int
	var gotString string
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerInt[int]](observing(func(_ Kernel, event int) error {
			gotInt = event
			return nil
		}))
		registry.Subscribe[testHandlerString[string]](observing(func(_ Kernel, event string) error {
			gotString = event
			return nil
		}))
		return nil
	}}

	e := startEngine(t, p)
	publishForTest(t, e, 42).Wait()
	publishForTest(t, e, "answer").Wait()
	if gotInt != 42 || gotString != "answer" {
		t.Fatalf("got int=%d string=%q, want 42 and answer", gotInt, gotString)
	}
}

func TestKernel_PublicationWaitBlocksUntilDone(t *testing.T) {
	var ran int
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerA[int]](observing(func(Kernel, int) error { ran++; return nil }))
		registry.Subscribe[testHandlerB[int]](observing(func(Kernel, int) error { ran++; return nil })).
			After[testHandlerA[int]]()
		return nil
	}}

	e := startEngine(t, p)
	publishForTest(t, e, 1).Wait()
	if ran != 2 {
		t.Fatalf("ran = %d, want 2 after publication completion", ran)
	}
}

func TestKernel_PublicationHandlesSubscriberError(t *testing.T) {
	boom := errors.New("boom")
	p := testPlugin{name: "p", register: func(registry *Registrar) error {
		registry.Subscribe[testHandlerA[int]](observing(func(Kernel, int) error { return boom }))
		return nil
	}}

	var got error
	e := startEngineWithHandler(t, func(err error) bool { got = err; return false }, p)
	err := publishForTest(t, e, 1).Wait()
	if !errors.Is(got, boom) {
		t.Fatalf("handled error = %v, want boom", got)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("publication error = %v, want boom", err)
	}
}

func TestKernel_PublicationWithoutSubscribersCompletes(t *testing.T) {
	e := startEngine(t, testPlugin{name: "p", register: func(*Registrar) error { return nil }})
	if err := publishForTest(t, e, 1).Wait(); err != nil {
		t.Fatal(err)
	}
}

// Configure gives each plugin its own entry from the map, keyed by plugin name.
func TestKernel_PluginReceivesConfig(t *testing.T) {
	var got any
	p := configPlugin{name: "p", got: &got}
	New(map[PluginName]any{"p": 42}).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(p)
	if got != 42 {
		t.Fatalf("config = %v, want 42", got)
	}
}
