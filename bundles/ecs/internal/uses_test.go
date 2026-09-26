package internal

import (
	"errors"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// doubleCmd is the empty-lock Command a System dispatches through Uses: it
// touches nothing, so its lock is nil, which is CompileShaderCmd's shape.
type doubleCmd kernel.Command[int, int]

func double(_ kernel.Kernel, n int) int { return 2 * n }

func handleDouble(registrar *kernel.Registrar) {
	registrar.HandleCommand[doubleCmd](func() (kernel.Lock, kernel.Execute[int, int]) {
		return nil, double
	})
}

// usesLedger is the resource the locking Command writes, and the one a second
// System writes by naming it: the two must serialise.
type usesLedger struct{ n int }

// tallyCmd is the Command whose lock is not empty: it writes *usesLedger.
type tallyCmd kernel.Command[int, int]

// handleTally registers *usesLedger and tallyCmd, which calls enter before it
// writes the ledger.
func handleTally(registrar *kernel.Registrar, enter func()) {
	registrar.InitResource(&usesLedger{})
	registrar.HandleCommand[tallyCmd](func() (kernel.Lock, kernel.Execute[int, int]) {
		var ledger kernel.Write[*usesLedger]
		return func(access kernel.ResourceAccess) {
				ledger = access.GetWrite[*usesLedger]()
			}, func(_ kernel.Kernel, n int) int {
				enter()
				ledger.Get().n += n
				return ledger.Get().n
			}
	})
}

type (
	usesSystem   kernel.Subscription[app.UpdateEvent]
	plainSystem  kernel.Subscription[app.UpdateEvent]
	writerSystem kernel.Subscription[app.UpdateEvent]
	usesOuterCmd kernel.Command[int, int]
)

// describedSubscription is the description of the subscription whose identity
// type is S.
func describedSubscription[S any](t *testing.T, engine *kernel.Engine) kernel.SubscriptionDescription {
	t.Helper()
	for _, sub := range engine.Describe().Subscriptions {
		if sub.Type == reflect.TypeFor[S]() {
			return sub
		}
	}
	t.Fatalf("no %s subscription in the description", kernel.TypeName(reflect.TypeFor[S]()))
	return kernel.SubscriptionDescription{}
}

// TestASystemDispatchesAnEmptyLockCommandThroughUses is the parameter's
// purpose: the System names the Command in its signature, dispatches it from
// its body, and gets the Command's response. The Command's lock is empty, so
// the System's lock set is what it would be without the parameter, and the use
// shows in Describe as the edge that explains it.
func TestASystemDispatchesAnEmptyLockCommandThroughUses(t *testing.T) {
	var answers []int
	_, _, engine := newWorld(t, 8, func(registrar *kernel.Registrar) {
		handleDouble(registrar)
		registrar.Subscribe[usesSystem](ToHandler[app.UpdateEvent](registrar,
			func(k kernel.Kernel, q *Query[typesMoveQuery], twice *Uses[doubleCmd, int, int]) {
				answers = append(answers, twice.Execute(k, 21))
			}))
		registrar.Subscribe[plainSystem](ToHandler[app.UpdateEvent](registrar,
			func(k kernel.Kernel, q *Query[typesMoveQuery]) {}))
	})

	frame(t, engine, 1)

	if !slices.Equal(answers, []int{42}) {
		t.Fatalf("the System got %v from the Command over one frame, want [42]", answers)
	}
	using, plain := describedSubscription[usesSystem](t, engine), describedSubscription[plainSystem](t, engine)
	if !slices.Contains(using.Uses, reflect.TypeFor[doubleCmd]()) {
		t.Fatalf("the System's description uses %v, which does not include the Command", using.Uses)
	}
	if !slices.Equal(using.Reads, plain.Reads) || !slices.Equal(using.Writes, plain.Writes) {
		t.Fatalf("using an empty-lock Command widened the lock set: reads %v writes %v, against %v and %v without it",
			using.Reads, using.Writes, plain.Reads, plain.Writes)
	}
}

// TestUsingALockingCommandSerialisesAgainstItsWriter is the fold's half of the
// contract: a Command that writes a resource makes the System hold that write
// for its run, so it can never overlap a System that writes the same resource
// by naming it. The two Systems are unordered on one event, so only the lock
// set keeps them apart; the Command's body and the writer each note whether
// the other was inside.
//
// The race detector cannot build here, so this is run with -count=10.
func TestUsingALockingCommandSerialisesAgainstItsWriter(t *testing.T) {
	var inside, overlaps atomic.Int32
	enter := func() {
		if inside.Add(1) > 1 {
			overlaps.Add(1)
		}
		time.Sleep(2 * time.Millisecond)
		inside.Add(-1)
	}
	_, _, engine := newWorld(t, 8, func(registrar *kernel.Registrar) {
		handleTally(registrar, enter)
		registrar.Subscribe[usesSystem](ToHandler[app.UpdateEvent](registrar,
			func(k kernel.Kernel, tally *Uses[tallyCmd, int, int]) { tally.Execute(k, 1) }))
		registrar.Subscribe[writerSystem](ToHandler[app.UpdateEvent](registrar,
			func(ledger *Write[*usesLedger]) {
				enter()
				ledger.Get().n += 10
			}))
	})

	for range 10 {
		frame(t, engine, 1)
	}

	if n := overlaps.Load(); n != 0 {
		t.Fatalf("the Command and the writer of its resource overlapped %d times in 10 frames", n)
	}
	using := describedSubscription[usesSystem](t, engine)
	if !namesType(using.Writes, "usesLedger") {
		t.Fatalf("the System using the Command writes %v, which does not include the Command's resource", using.Writes)
	}
	if !slices.Contains(using.Uses, reflect.TypeFor[tallyCmd]()) {
		t.Fatalf("the System's description uses %v, which does not include the Command", using.Uses)
	}
}

// TestUsesOfAnUnregisteredCommandFailsComposition is the kernel's own refusal,
// reached through the signature: the declaration cannot be resolved, so the
// System's lock closure cannot be either.
func TestUsesOfAnUnregisteredCommandFailsComposition(t *testing.T) {
	var failure error
	kernel.New(nil).
		Handler(func(err error) error { failure = err; return err }).
		WithPlugins(
			authority{ids: 8},
			&componentsPlugin{ids: 8},
			&systemsPlugin{subscribe: func(registrar *kernel.Registrar) {
				registrar.Subscribe[usesSystem](ToHandler[app.UpdateEvent](registrar,
					func(k kernel.Kernel, twice *Uses[doubleCmd, int, int]) {}))
			}},
		)

	var unknown kernel.ErrUsingUnknownCommand
	if !errors.As(failure, &unknown) {
		t.Fatalf("composing a System using an unregistered Command reported %v, want ErrUsingUnknownCommand", failure)
	}
	if unknown.Command != reflect.TypeFor[doubleCmd]() || unknown.Declaring != reflect.TypeFor[usesSystem]() {
		t.Fatalf("the failure names %s using %s, want the System using the Command",
			kernel.TypeName(unknown.Declaring), kernel.TypeName(unknown.Command))
	}
}

// usesDispatches is how many dispatches one invocation of the allocation
// test's System makes, so a per-dispatch allocation cannot hide in rounding.
const usesDispatches = 100

// TestAUsesDispatchAllocatesNothing holds the dispatch to what the Command
// itself allocates, which for doubleCmd is nothing: a System dispatching it
// through Uses a hundred times an invocation allocates no more than one calling
// the handler directly as often.
func TestAUsesDispatchAllocatesNothing(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	measure := func(system any) float64 {
		_, _, engine := newWorld(t, 8, func(registrar *kernel.Registrar) {
			handleDouble(registrar)
			registrar.HandleCommand[usesOuterCmd](ToExecute[int, int](registrar, system))
		})
		executioner := engine.Executioner()
		return testing.AllocsPerRun(1000, func() {
			executioner.ExecuteCommand[usesOuterCmd](usesDispatches)
		})
	}
	direct := measure(func(k kernel.Kernel, n int, answer *Resp[int]) {
		total := 0
		for i := range n {
			total += double(k, i)
		}
		answer.Set(total)
	})
	dispatched := measure(func(k kernel.Kernel, n int, twice *Uses[doubleCmd, int, int], answer *Resp[int]) {
		total := 0
		for i := range n {
			total += twice.Execute(k, i)
		}
		answer.Set(total)
	})
	t.Logf("objects an invocation of %d calls: direct %.3f, through Uses %.3f", usesDispatches, direct, dispatched)
	if dispatched > direct {
		t.Fatalf("%d dispatches through Uses cost %.3f objects an invocation against %.3f for direct calls",
			usesDispatches, dispatched, direct)
	}
}

// benchmarkUses prices one call from inside a System: the System is invoked
// once, as a command, and makes b.N calls, so the command's own dispatch is
// amortised away and what is left is the call itself.
func benchmarkUses(b *testing.B, system any) {
	_, _, engine := newWorld(b, 8, func(registrar *kernel.Registrar) {
		handleDouble(registrar)
		handleTally(registrar, func() {})
		registrar.HandleCommand[usesOuterCmd](ToExecute[int, int](registrar, system))
	})
	executioner := engine.Executioner()

	b.ReportAllocs()
	b.ResetTimer()
	usesSink = executioner.ExecuteCommand[usesOuterCmd](b.N)
}

// usesSink keeps both arms' results observable.
var usesSink int

// BenchmarkUsesDispatch is a dispatch of an empty-lock Command through Uses:
// the kernel's pooled invocation, its empty-lock fast path, and the handler.
func BenchmarkUsesDispatch(b *testing.B) {
	benchmarkUses(b, func(k kernel.Kernel, n int, twice *Uses[doubleCmd, int, int], answer *Resp[int]) {
		total := 0
		for i := range n {
			total += twice.Execute(k, i)
		}
		answer.Set(total)
	})
}

// BenchmarkUsesDispatchLocking is a dispatch of a Command that writes a
// resource. It prices the same as the empty-lock one, because the fold granted
// the System the Command's locks at composition and the nested dispatch
// acquires nothing: the fast path is the declared dispatch's, whatever the
// Command locks.
func BenchmarkUsesDispatchLocking(b *testing.B) {
	benchmarkUses(b, func(k kernel.Kernel, n int, tally *Uses[tallyCmd, int, int], answer *Resp[int]) {
		total := 0
		for range n {
			total += tally.Execute(k, 1)
		}
		answer.Set(total)
	})
}

// BenchmarkUsesDirectCall is the same handler called directly, the control.
func BenchmarkUsesDirectCall(b *testing.B) {
	call := double
	benchmarkUses(b, func(k kernel.Kernel, n int, answer *Resp[int]) {
		total := 0
		for i := range n {
			total += call(k, i)
		}
		answer.Set(total)
	})
}
