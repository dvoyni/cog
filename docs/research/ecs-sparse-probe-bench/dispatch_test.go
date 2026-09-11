package sbench

import (
	"context"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

// cog#240 asks whether a Spawn should be a cog Command reached through Uses, or
// a plain method on a handle the System already holds. Both are sound — Uses
// folds the command's lock set statically, and a handle declared in the
// System's signature declares the same set directly. The difference is what a
// call costs.
//
// A declared nested dispatch requests no locks (kernel/command.go:108), but it
// still goes through engine.runTask -> scheduler.execute (kernel/command.go:134,
// kernel/scheduler.go:191), which is a round-trip to the coordinator goroutine.
// This measures that against the same work called directly. nox fires 16
// missiles per cast, so a per-spawn constant is on the hot path.

type counter struct{ n int }

type spawnCmd kernel.Command[int, int]

func spawnCmdImpl() (kernel.Lock, kernel.Execute[int, int]) {
	var c kernel.Write[*counter]
	return func(a kernel.ResourceAccess) {
			c = a.GetWrite[*counter]()
		}, func(_ kernel.Kernel, req int) (int, error) {
			c.Get().n += req
			return req, nil
		}
}

// tickEvent carries the loop count so one publication measures many calls and
// the publication's own cost amortises away.
type tickEvent struct {
	n      int
	direct bool
}

type tickSub kernel.Subscription[tickEvent]

func tickSubImpl() (kernel.Lock, kernel.Observe[tickEvent]) {
	var c kernel.Write[*counter]
	var dispatch func(kernel.Kernel, int) (int, error)
	return func(a kernel.ResourceAccess) {
			c = a.GetWrite[*counter]()
			dispatch = a.Uses[spawnCmd, int, int]()
		}, func(k kernel.Kernel, e tickEvent) error {
			if e.direct {
				// The shape a plain method on a promoted handle would have: the
				// System already holds the write lock, so the call is a call.
				for range e.n {
					c.Get().n++
				}
				return nil
			}
			for range e.n {
				if _, err := dispatch(k, 1); err != nil {
					return err
				}
			}
			return nil
		}
}

type benchPlugin struct{}

func (benchPlugin) Name() kernel.PluginName           { return "sbench" }
func (benchPlugin) Dependencies() []kernel.PluginName { return nil }

func (benchPlugin) Register(r *kernel.Registrar, _ any) error {
	r.InitResource[*counter](&counter{})
	r.HandleCommand[spawnCmd, int, int](spawnCmdImpl)
	r.Subscribe[tickSub, tickEvent](tickSubImpl)
	return nil
}

func startBenchEngine(b *testing.B) *kernel.Engine {
	b.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	b.Cleanup(cancel)
	e := kernel.New(nil).
		Handler(func(err error) bool { b.Fatalf("kernel error: %v", err); return true }).
		WithPlugins(benchPlugin{})
	go e.Run(ctx)
	<-e.Ready()
	return e
}

func benchDispatch(b *testing.B, direct bool) {
	e := startBenchEngine(b)
	// Warm the publication plan and the command's invocation pool.
	e.Executioner().PublishEvent(tickEvent{n: 1, direct: direct}).Wait()
	b.ReportAllocs()
	b.ResetTimer()
	e.Executioner().PublishEvent(tickEvent{n: b.N, direct: direct}).Wait()
}

// Through Uses: the folded-lock dispatch cog#240 would use if Spawn were a
// Command.
func BenchmarkDispatch_ViaUses(b *testing.B) { benchDispatch(b, false) }

// Direct: the same mutation performed by the handler that already holds the
// lock, which is what a method on a promoted handle compiles to.
func BenchmarkDispatch_Direct(b *testing.B) { benchDispatch(b, true) }
