package kernel

import (
	"context"
	"testing"
)

type benchRequest struct{ a, b, c int }
type benchResponse struct{ sum int }

type benchCmd Command[benchRequest, benchResponse]
type benchOuterCmd Command[benchRequest, benchResponse]

type benchEvent struct{ tick int }

type benchSoleHandler Subscription[benchEvent]
type benchFirstHandler Subscription[benchEvent]
type benchSecondHandler Subscription[benchEvent]
type benchThirdHandler Subscription[benchEvent]

type benchCounter int

type benchPlugin struct {
	name     PluginName
	register func(*Registrar) error
}

func (p benchPlugin) Name() PluginName         { return p.name }
func (benchPlugin) Dependencies() []PluginName { return nil }
func (p benchPlugin) Register(r *Registrar, _ any) error {
	return p.register(r)
}

func benchEngine(b *testing.B, register func(*Registrar) error) *Engine {
	b.Helper()
	engine := New(nil).
		Handler(func(err error) bool { b.Fatalf("unexpected kernel error: %v", err); return true }).
		WithPlugins(benchPlugin{name: "bench", register: register})
	ctx, cancel := context.WithCancel(context.Background())
	b.Cleanup(cancel)
	go engine.Run(ctx)
	<-engine.Ready()
	return engine
}

func registerBenchCmd(r *Registrar) {
	r.HandleCommand[benchCmd](benchCmdImpl)
}

func benchCmdImpl() (Lock, Execute[benchRequest, benchResponse]) {
	var counter Write[benchCounter]
	return func(access ResourceAccess) {
			counter = access.GetWrite[benchCounter]()
		}, func(_ Kernel, request benchRequest) (benchResponse, error) {
			counter.Set(counter.Get() + 1)
			return benchResponse{sum: request.a + request.b + request.c}, nil
		}
}

func benchOuterCmdImpl() (Lock, Execute[benchRequest, benchResponse]) {
	var inner func(Kernel, benchRequest) (benchResponse, error)
	return func(access ResourceAccess) {
			inner = access.Uses[benchCmd]()
		}, func(k Kernel, request benchRequest) (benchResponse, error) {
			return inner(k, request)
		}
}

// BenchmarkExecuteCommand measures one synchronous command dispatch holding a
// single write lock, the shape gameplay uses every frame.
func BenchmarkExecuteCommand(b *testing.B) {
	engine := benchEngine(b, func(r *Registrar) error {
		r.InitResource(benchCounter(0))
		registerBenchCmd(r)
		return nil
	})
	k := engine.Executioner()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if _, err := k.ExecuteCommand[benchCmd](benchRequest{a: i, b: 1, c: 2}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkUsesNestedDispatch measures the same command reached through a
// declared Uses dispatcher inside an outer command, so the gap from
// BenchmarkExecuteCommand is what the nested dispatch itself costs.
func BenchmarkUsesNestedDispatch(b *testing.B) {
	engine := benchEngine(b, func(r *Registrar) error {
		r.InitResource(benchCounter(0))
		registerBenchCmd(r)
		r.HandleCommand[benchOuterCmd](benchOuterCmdImpl)
		return nil
	})
	k := engine.Executioner()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if _, err := k.ExecuteCommand[benchOuterCmd](benchRequest{a: i, b: 1, c: 2}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPublishEventSingleSubscriber measures the dominant publication shape:
// one subscriber, waited on, as the driver does for every fixed update step.
func BenchmarkPublishEventSingleSubscriber(b *testing.B) {
	engine := benchEngine(b, func(r *Registrar) error {
		r.InitResource(benchCounter(0))
		r.Subscribe[benchSoleHandler](func() (Lock, Observe[benchEvent]) {
			var counter Write[benchCounter]
			return func(access ResourceAccess) {
					counter = access.GetWrite[benchCounter]()
				}, func(_ Kernel, event benchEvent) error {
					counter.Set(benchCounter(event.tick))
					return nil
				}
		})
		return nil
	})
	k := engine.Executioner()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if err := k.PublishEvent(benchEvent{tick: i}).Wait(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPublishEventThreeSubscribers measures a fan-out publication with an
// ordering constraint, the shape of app.UpdateEvent.
func BenchmarkPublishEventThreeSubscribers(b *testing.B) {
	engine := benchEngine(b, func(r *Registrar) error {
		r.InitResource(benchCounter(0))
		observe := func() (Lock, Observe[benchEvent]) {
			return nil, func(Kernel, benchEvent) error { return nil }
		}
		r.Subscribe[benchFirstHandler](observe).First()
		r.Subscribe[benchSecondHandler](observe)
		r.Subscribe[benchThirdHandler](observe).Last()
		return nil
	})
	k := engine.Executioner()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if err := k.PublishEvent(benchEvent{tick: i}).Wait(); err != nil {
			b.Fatal(err)
		}
	}
}
