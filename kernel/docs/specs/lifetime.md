# cog kernel lifetime and errors — specification

`github.com/dvoyni/cog/kernel` decides three things this document specifies in
full: how a failure travels from the code that noticed it to the code that acts
on it, what may end a running engine, and what bounds the work a dispatch
starts. Today it answers each of those more than once. This specification
collapses each to a single answer.

The design is bound by five requirements, in this order, and every decision
below was taken against them:

1. **One way to report a failure.** `ReportError` and `ReportErrorOnce`, and
   nothing else. A handler does not also return an error, and the kernel does
   not also answer with one.
2. **Two termination points, and no third.** Initialization, and the error
   handler's verdict.
3. **No `context.Context` anywhere in the kernel.** cog is a standalone
   application. It is not closeable from outside, it carries no request-scoped
   values, and its one deadline belongs to the plugin that has one.
4. **`panic` is an abort, never a propagation channel.** Code that can return an
   error returns one. The engine never panics on a failure it can name.
5. **Nothing on the dispatch path that composition already settled.** A decision
   taken once at finalisation is not re-taken per invocation.

Two properties fall out of taking those in that order, and most of what follows
is a consequence of one or the other.

**The engine holds no liveness state.** There is no flag a dispatch consults to
learn whether the engine is still alive, because nothing is refused for being
late. `Run`'s return value *is* the outcome, and it is the only place the
outcome exists.

**Deadlock is prevented by construction rather than by discipline.** A `Kernel`
has no synchronous dispatch, so a handler holding locks cannot ask for more. The
only synchronous dispatch a handler has is a declared one, whose lock set was
folded into its own before the frame began.

**This specification is implemented**, except for the sections
[Out of scope](#out-of-scope) names. It was assembled from a design review of
`kernel` at `dc82bb9` and built on the branch `kernel-lifetime`; where the code
and this document disagree, the code is the defect unless a section below says
otherwise. [Required work](#required-work) is the checklist it was built from. Where a claim
rests on something unmeasured it is marked **Gap** and says what would settle
it; where putting these decisions next to each other settled something no
earlier document did, it is marked **Settled here**.

---

## Contents

- [Vocabulary](#vocabulary)
- [No context](#no-context) · [The one caller that keeps it](#the-one-caller-that-keeps-it)
- [Handler bodies stop returning errors](#handler-bodies-stop-returning-errors)
- [One report](#one-report) · [Code that cannot make one](#code-that-cannot-make-one)
- [The handler decides](#the-handler-decides)
- [Two termination points](#two-termination-points)
- [Panic is an abort](#panic-is-an-abort)
- [Lifecycle](#lifecycle)
- [Declared dispatch, and why it was priced out](#declared-dispatch-and-why-it-was-priced-out)
- [Shapes that were rejected](#shapes-that-were-rejected)
- [Required work](#required-work) · [Out of scope](#out-of-scope)

---

## Vocabulary

**Report**:
A failure handed to the centralized error handler through `Kernel.ReportError`
or `Kernel.ReportErrorOnce`. A report is not a return value and carries no
answer back: the reporter has finished with the failure.
_Avoid_: raise, throw, propagate

**Verdict**:
What the `ErrorHandler` answers to one report. `nil` continues; any error
terminates, and that error is the Cause.

**Cause**:
The error `Run` returns. It is the first non-nil Verdict of the engine's life,
or the initialization error that stopped startup. There is exactly one, and
whatever it knocked over afterwards is not it.

**Termination point**:
One of exactly two places an engine's life can end — initialization, or a
Verdict. Nothing else ends it, and neither one refuses work already in flight.

**Quit**:
The ordinary end of a run, with no Cause: the app asked the MainLoop to return.
Quit is not a Termination point, because nothing failed.

---

## No context

`context.Context` is doing four unrelated jobs in the kernel today, and cog
needs one of them.

**Cancellation from outside.** `Run(ctx)` (`engine.go:282`) lets whoever composed
the engine cancel it. Nothing in the repo does: a cog game ends because the app
asked it to, through the MainLoop. Deleted.

**Carrying the engine's lifetime into a dispatch.** `Kernel` holds `ctx`, `scope`
and `bounded` (`kernel.go:24`) so that a dispatch inherits engine cancellation,
and four separate sites splice engine cancellation onto a caller's context —
`PublishEvent` (`kernel.go:120`), `ExecuteCommandAsync` (`kernel.go:157`),
`ExecuteCommand` (`kernel.go:212`) and the `Uses` dispatcher (`command.go:89`).
Each is ten lines of `WithCancel` plus `AfterFunc` plus a hand-unwound
`stop`/`cancel`, carrying the same comment about why it is not a `defer`. With
no engine context there is nothing to splice. All four delete, and `bounded`
— a field that exists only to let those four sites skip the splice — deletes
with them.

**Aborting a wait.** `scheduler.execute` (`scheduler.go:191`) selects on
`ctx.Done()` to withdraw a lock request that has not been granted. The scheduler
already has its own stop signal for this, `s.done` (`scheduler.go:41`), and it
already selects on it. The context arms collapse into the channel that was
always there.

**Bounding a deadline.** This is the one real job, and it belongs to the plugin
that has a deadline. See below.

`task.locks(ctx)` (`scheduler.go:17`) takes a context neither implementation
reads; it loses the parameter. `commandContext` and `eventContext` stop
embedding `context.Context` and become plain carriers of the engine and the
payload. `Publication` loses the context it retained for `Wait`'s early-out.

### The one caller that keeps it

**mcp is a service and keeps its service concerns.** `bundles/mcp` runs an
`http.Server`, propagates request cancellation from the agent's client, and
wraps each capability call in `context.WithTimeout`. It is the only place an
external context enters a kernel value today, and after this change it stops
entering: mcp races its own timer against the dispatch and answers the agent
`Unavailable` on its own authority. A dispatch that outruns the agent's patience
finishes anyway, and the answer is discarded.

This is sound because a lock is held for the length of a handler body, and a
handler body is microseconds. A handler that genuinely hangs is a defect for
`ReportError` to name, not a state to unwind. **Gap:** nothing measures the
longest lock hold in a real frame. A histogram of grant-to-release under the
mcp providers would settle it; the claim rests on lock sets being small and
bodies being straight-line.

### A deadline is a request field

Two production waits are not lock waits at all. `slots/app` waits for a future
tick when stepping a paused engine, and `bundles/input` waits between synthesized
input batches. Both express that today as `context.WithTimeout` over
`k.Context()`, and both become a deadline field on the request — a value the
handler reads, not an ambient bound it inherits.

**Settled here.** A deadline that describes *the work* belongs in the request
that asks for the work. A deadline that describes *the caller's patience* belongs
to the caller and never enters the engine.

app spells the first as `TimeRequest.Wait`, answering `ErrStepNotPublished` when
it expires; mcp spells the second as a timer it races against the dispatch,
answering the agent `Unavailable` on its own authority while the body runs to
completion and its result is dropped.

---

## Handler bodies stop returning errors

A handler that reports and also returns says the same thing twice, and the
existing instructions already forbid doing both. Requirement 1 makes the
signature enforce it:

```go
type Execute[TRequest any, TResponse any] func(kernel Kernel, request TRequest) TResponse
type Observe[TEvent any] func(kernel Kernel, event TEvent)
```

**Expected outcomes were always response content.** A command that can fail in a
way its caller should act on says so in its response — a field, a status, a
`Maybe`. That rule does not change; what changes is that there is no longer a
second channel tempting a handler to use it instead.

Eight production bodies had one. Each grew an `Err` field on its response:
storage's three mount and values commands, gfx's two arms, canvas's and ui's
arms, and app's `TimeResponse`. They are outcomes the caller asked for and can
act on — a reserved mount id, an arm that found one already in flight, a step
whose wait expired — which is what makes them response content rather than
reports.

**`ExecuteCommand` answers with a response and nothing else.**

```go
func (e Executioner) ExecuteCommand[...](request TRequest) TResponse
```

A dispatch the kernel could not perform — an unregistered command, a body that
panicked — is reported, and the caller receives the zero response. **Settled
here:** the caller is not told, because there is nothing for it to do that
reporting has not already done.

**A stopped scheduler is the exception, and it is not reported at all.** A
dispatch arriving after the coordinator has gone is the engine ending, not a
failure in the thing that dispatched — Quit is an ordinary end, and reporting
would turn every in-flight call at shutdown into a fault every handler has to
recognise and forgive. `shutdownAside` drops `ErrSchedulerStopped` and passes
everything else through. **Settled here**, and found by building it: a test
harness whose handler failed the test began failing on its own shutdown. This deletes the classification mcp performs
today, in which a shutting-down engine and a failed tool were told apart by the
error's type.

**A `Publication` is a completion handle and nothing more.** `Wait()` returns
nothing: subscriber errors are reported by the subscribers, and there is no
context to cut the wait short.

**Settled here:** a panic is the only subscriber failure a publication can see,
so it is the only one that skips a dependent. A subscriber that merely reports
has still completed, and its dependents run. Reporting says what went wrong and
says nothing about whether the work downstream can go ahead — only the
subscriber knows that, and where the answer is no it belongs in state the
dependent reads. `Publication` keeps
`done` and loses `ctx`, `mu` and `err` — the mutex guarded a field written once
before a channel close and read only after it, which the close already ordered.

---

## One report

```go
func (k Kernel) ReportError(err error)
func (k Kernel) ReportErrorOnce[T comparable](key T, errs ...error)
func (k Kernel) ForgetReportedError[T comparable](key T)
func (k Kernel) ForgetReportedErrors[T comparable](match func(T) bool)
```

None returns anything. The `terminate bool` they return today is discarded at
every production call site, and under [the handler decides](#the-handler-decides)
there is nothing a reporter could do with it: in-flight work finishes either
way, and a handler body is too short to be worth interrupting.

### Code that cannot make one

**A `Kernel` is scoped to one dispatch, so code without one hands its error
back.** `extensions/gogpu`'s backend runs on the render thread inside a foreign
library's callback; it stashes its refusal and the plugin drains it where a
report can be made. That stays, and it is the sanctioned shape rather than an
exception apologised for.

**The engine recovers only the goroutines it spawned.** A plugin that spawns its
own goroutine owns its panics and its errors: it may recover and report through
the `Executioner` its lifecycle method received, hand the error back over a
channel, or anything else. The kernel makes no claim about code it did not
start. **Settled here** — the alternative, a long-lived reporter handle minted
by the engine, was rejected below.

---

## The handler decides

```go
type ErrorHandler func(err error) error
```

`nil` continues. Any error terminates, and that error is the Cause `Run`
returns. Wrapping is allowed and means nothing to the engine; returning the
error unchanged is the ordinary case.

### The default handler's opinion

```go
func defaultErrorHandler(err error) error {
	log.Printf("kernel: %v", err)
	var panicErr ErrPluginPanic
	if errors.As(err, &panicErr) {
		return err
	}
	return nil
}
```

**The panic rule becomes an opinion rather than engine policy.** `reportLocked`
today forces termination on a panic whatever the handler says — `terminate :=
errors.As(err, &panicErr) || handlerTerminate` (`engine.go:419`) — so a game
that installs a handler cannot decide the question. Moving the check into the
default handler is what makes the Verdict mean what it says.

**The default continues on everything else.** A missing texture reported every
frame should not kill a game; today only the report-once table stands between it
and exactly that. A game that wants startup to be strict, or wants any report at
all to be fatal, installs a handler that says so.

**Consequence, accepted with eyes open.** A failure reported during `Start`
under the default handler logs and the run continues. Startup is not implicitly
strict, and a plugin that needs a startup step to have succeeded checks its
response.

### First non-nil verdict wins

Handler calls are serialized, as they are today. The first non-nil Verdict is
the Cause; later reports still reach the handler, and their Verdicts are
recorded nowhere. Inside a `ReportErrorOnce` burst every error reaches the
handler, and the first non-nil Verdict of the burst is the Cause.

---

## Two termination points

**Initialization.** `Run` returns the error and the engine never starts. Every
composition fault, every `Register` that failed, every missing or duplicate
Adapter arrives this way.

**A Verdict.** The Cause is recorded, the MainLoop is asked to return, the Host
unwinds, `Stop` runs in reverse dependency order, and `Run` returns the Cause.

**Nothing else, and nothing in between.** In particular:

- **Nothing is refused mid-flight.** A dispatch that started finishes. There is
  no `refusal()` (`engine.go:382`), no `ErrEngineTerminated`, and therefore no
  atomic load on the dispatch path.
- **The `Start` pass is not special-cased.** A Verdict during startup is
  recorded like any other and acted on when the loop begins. A plugin that
  cannot start returns an error from `Start` instead, which is the first
  termination point, not the second.
- **The engine holds no liveness pair.** `terminated bool` (`engine.go:51`) and
  `failure atomic.Pointer[error]` (`engine.go:58`) both delete, along with
  `recordFailureLocked` and `Engine.Err()` (`engine.go:103`). The pair also
  carried a defect: `e.terminated` is read with no lock at `engine.go:400` and
  `engine.go:442` while written under `errorMu` at `engine.go:195` and
  `engine.go:421`, and both unlocked reads are reachable from concurrent
  handlers.

What survives is one mutex, serialising handler calls and guarding the
report-once table, and one field holding the Cause.

---

## Panic is an abort

**A plugin panic is recovered, reported, and judged.** `callPluginBoundary` and
the inlined recoveries in `commandContext.run` and `Ordering.run` keep working
exactly as they do; what changes is that the resulting `ErrPluginPanic` goes to
the handler like any other report and does not force termination by itself.

**Two panics survive, both because they must return a `T`:**
`RequiredAdapter.Get()` and `CollectedAdapters.Get()` read before composition
bound them. Both are unreachable in correct code — a failed composition means
`Start` never runs — and neither has an error channel, because both are called
at runtime rather than during registration. The alternative is returning a zero
interface and letting the caller dereference it three frames later.

**`Kernel.bound()`'s zero-kernel panic deletes** (`kernel.go:27`). A `Kernel` is
only ever constructed by the engine and handed to a handler; there is no route
by which a caller supplies one.

**`Registrar.Dependency` returns an error** (`registrar.go:48`):

```go
func (r *Registrar) Dependency[T any]() (T, error)
```

It cannot collect-and-continue like the other registration faults: `bundles/ecs`
reads the world through it and would go on to call methods on a zero value, so
the reported cause would be whatever the plugin tripped over rather than the
declaration that was wrong. `Register` already returns an error, which is the
channel.

**`portInterface`'s non-interface panic becomes a collected error**
(`adapter.go:155`). It returns no value to its caller's caller, so it has no
reason to panic while `ProvideAdapter`'s nil check, in the same file, collects.

**Registration fails fast.** The first fault stops finalisation and is what
`Run` returns. `registry.errs` becomes one error rather than a slice, and the
sort-by-message-text in `finalize` (`registrar.go:141`) deletes with it — it
existed only to make a joined list of map-ordered errors read the same way twice.

---

## Lifecycle

**`Run()` takes no argument and returns an error.** It blocks: it starts
plugins in dependency order, runs the Host, and stops the plugins that started,
in reverse. It returns the Cause, or nil on a Quit.

**`Engine.Quit()` asks a running engine to stop**, and a Verdict does the same.
A Run without a Host returns once every started plugin has stopped. A Run with
one cannot: the Host owns a blocking loop and only the Host can leave it, so
`PluginHost` carries a `Quit()` that asks it to. The engine calls that from a
watcher beside the Host rather than at the point of the Verdict, because the
Verdict is reached with the report lock held and plugin code must never be
entered under it. app implements it by asking its MainLoop to quit, which is
what `QuitCmd` already does, and gogpu's `quitOnCancellation` deletes.

**`Executioner.Quitting()` is a channel closed when the engine is asked to
stop.** It is for a plugin that owns something outside the engine — mcp's
listening socket — and must stop accepting before the engine tears down. It says
nothing about dispatch: in-flight work finishes, and a dispatch after it still
runs until the scheduler stops. Only lifecycle methods hold an Executioner,
which is where such a thing is started, so a per-dispatch handler cannot reach
it.

**`Ready()` stays.** It is closed once startup has been attempted, and it is how
a test knows a blocking `Run` has got that far.

**The scheduler is live from `New` to shutdown.** Starting the coordinator in
`New` rather than in `Run` removes `runTask`'s "there is no coordinator yet"
branch (`engine.go:370`) rather than re-keying it on something that is not the
engine context. There is always a coordinator, unconditionally. The cost is a
goroutine per constructed engine, which outlives only an engine that is built
and never run.

**What makes `Run` return** is the scheduler stopping and the MainLoop Port
being asked to return. Quitting is already an app concern with an Adapter behind
it, so the engine asks that Port rather than growing a quit concept of its own.

---

## Declared dispatch, and why it was priced out

### The deadlock `Uses` prevents

A handler is granted its declared lock set for the length of its body. If it
could dispatch a command needing a lock it does not hold, the request would go
to the coordinator, find the resource held by the caller's own outstanding
request, and sit in `pending` forever — while the caller blocks on the grant and
therefore never releases what it holds.

`Uses` resolves this at composition. `resolveUses` (`registrar.go:188`) walks the
declared edges depth-first and folds each callee's closure into the caller's set,
so the caller is granted the union before its body starts. The dispatcher then
calls with `noLocks, noLocks` (`command.go:108`) and acquires nothing.

**This is why `Kernel` has no `ExecuteCommand`.** The deadlock is unrepresentable
rather than forbidden: only `Executioner`, which by definition holds no locks,
can dispatch synchronously without declaring.

### The fold is static, so the round-trip is ceremony

Because the fold happens at finalisation, a declared dispatch acquires an empty
lock set — and an empty lock set can never conflict with anything. Yet
`scheduler.execute` still allocates a `lockRequest`, posts it to the coordinator,
waits on `granted` and posts a release. `bundles/ecs/internal/types/spawn.go:36`
measured what that costs and refused the mechanism over it:

> A `Uses` dispatch measures 1113 ns against 0.49 ns for the direct call — a
> factor of 2270, all of it the scheduler round-trip to the coordinator — and it
> would buy nothing, because a dispatch runs on the calling goroutine with no
> locks of its own and the `Uses` fold is static at finalisation.

`runTask` short-circuits an empty lock set to a direct call, guarded by the
scheduler's own stop signal:

```go
func (e *Engine) runTask(t task) error {
	read, write := t.locks()
	if len(read) == 0 && len(write) == 0 {
		select {
		case <-e.scheduler.done:
			return ErrSchedulerStopped{}
		default:
		}
		return t.run()
	}
	return e.scheduler.execute(t, read, write)
}
```

This changes no lock decision. It declines to pay for a decision composition
already took. It also catches commands that declare no locks at all, such as
app's step command, which is correct for the same reason.

**Measured.** `BenchmarkUsesNestedDispatch` against `BenchmarkExecuteCommand`
is the instrument: the gap between them is what the nested dispatch itself
costs. Two test binaries, three alternating pairs, 2000 iterations each:

| | `ExecuteCommand` | `UsesNestedDispatch` | nested dispatch |
|---|---|---|---|
| before | 1596 ns | 3048 ns | **~1452 ns** |
| after | 1614 ns | 1546 ns | **~0** |

A declared dispatch now costs what the plain dispatch it wraps costs, and the
reason ECS wrote it off is gone. The locked path is unchanged — 1596 against
1614 ns, inside a run-to-run spread of 1501–1762 — which is the number that
mattered, because the short-circuit reads `locks()` on every dispatch and not
only the ones it skips the coordinator for. `execute` takes the lock set as
arguments rather than asking the task a second time, so nothing is read twice.

These are this machine's figures. The 1113 ns in `spawn.go` was measured
elsewhere and is not comparable in absolute terms; the ratio is.

### What this does not do

It does not make `Uses` mandatory, and it does not condemn what production does
instead. A handler that binds another plugin's resource and calls a scoped
facade is choosing narrower locks over a single coupling edge, and the facade
splits in canvas and scene exist precisely to keep a lock set small. `Uses`
widens all-or-nothing: the callee's whole closure, for the caller's whole body.
Where a caller wants to hold A for one phase and A∪B for the next, `Uses` cannot
say it. Removing the round-trip makes the trade a real choice instead of a
foregone one.

---

## Shapes that were rejected

Recorded so they are not reinvented, each with the reason that actually killed
it rather than the first objection raised.

**Keeping `context.Context` for engine cancellation.** Rejected because nothing
cancels a cog engine from outside and nothing ever has. What the context was
really buying — engine cancellation reaching a dispatch — cost four copies of a
cancellation splice, a `bounded` flag in the `Kernel` interface to let those
four sites skip it, and a context parameter on `task.locks` that neither
implementation reads. The scheduler's own `done` channel already expressed the
only part that was load-bearing.

**Keeping a context on `Executioner` only, not on `Kernel`.** Rejected as a
half-measure. It leaves the splice, the flag and the plumbing in place to serve
one plugin, and that plugin — mcp — is better served owning its own deadline
outright, because the deadline it has is the agent's patience rather than the
engine's.

**A kernel-level deadline value in place of a context.** Rejected. It keeps the
withdraw-a-pending-request machinery for a case that does not arise: lock holds
are microseconds, so a pending request is never pending long enough for a
deadline to be the right answer. A handler that hangs is a defect, not a timeout.

**`ErrorHandler` returning `bool`.** Rejected in favour of `func(error) error`.
The `bool` says whether to terminate but not with what, which is why the engine
had to record the Cause separately and keep it in step with the flag. Returning
the error answers both questions in one value, and `Run` has something to return.

**The `terminated` / `failure` pair.** Rejected as one fact recorded twice, kept
in step by hand — the field comment says as much — and read without a lock at two
of its four sites. The pair existed to serve mid-flight refusal, which is itself
rejected below, so nothing needed it.

**`ErrEngineTerminated` and refusing a dispatch mid-flight.** Rejected. It puts
an atomic load on every command and every subscriber run to serve a window
measured in microseconds, and it buys a distinction no caller outside the kernel
ever made: `Engine.Err()` and `ErrEngineTerminated` have no callers outside the
package at all, while the five production sites that classify "the game is
shutting down" do it through `ErrSchedulerStopped`. Letting in-flight work
finish needs no state whatsoever.

**The engine panicking on a failed composition.** Rejected. The engine is best
placed to *name* the cause and worst placed to *decide* what a failure means; a
library that panics takes that decision away from the composition root. `Run`
returns the error and the caller chooses.

**Collecting every registration error and joining them.** Rejected in favour of
failing at the first. The joined list needed a total order to read the same way
twice, which it got by sorting on message text — a workaround for map iteration
masquerading as a feature. One error, at the point it was found, needs no order.

**Splitting `Start`/`Stop` out of `Run` so `Ready` could be private.** Rejected.
It buys a smaller public surface at the cost of a second lifecycle shape to
document and keep consistent, and `Ready` is a legitimate answer to "a blocking
`Run` has got as far as trying".

**A long-lived `Reporter` handle minted by the engine.** Rejected. It would make
"only `ReportError`" literally true for every goroutine, including ones the
engine never started, and that is the problem with it: the engine would be
claiming responsibility for code whose lifetime it does not know. A plugin that
spawns a goroutine owns it.

**Deleting `Uses` because production has no caller for it.** Rejected. Zero
adoption sizes uptake; it does not weigh a mechanism. The reason production
routed around `Uses` is the round-trip and the all-or-nothing fold, and the first
of those is now fixed. If it is still unused after that, it can be judged on its
merits.

**Removing `ExecuteCommand` and leaving only `Uses` and
`ExecuteCommandAsync`.** Rejected: they are not alternatives. `ExecuteCommand`
acquires the command's own lock set and is sound because its caller holds none;
`Uses` acquires nothing and is sound because the caller already holds the union.
Removing the first leaves nothing able to enter the engine from `Start`, from a
host callback, or from mcp — which is every production dispatch there is.

**A "the engine is stopping" check in `runTask`.** Rejected as a concept the
code already has. `scheduler.done` is closed when the coordinator returns, and
`execute` already selects on it so a caller never blocks on a dead coordinator.
The short-circuit respects the same channel; nothing new is introduced.

---

## Required work

In dependency order. Each item is a change to `kernel` unless it says otherwise.

**Remove context**

- `Kernel`: delete `ctx`, `scope`, `bounded`, `Context()`, `WithContext`;
  delete `Executioner.WithContext`.
- Delete the four cancellation splices in `PublishEvent`,
  `ExecuteCommandAsync`, `ExecuteCommand` and the `Uses` dispatcher.
- `Engine`: delete `ctx`, `cancel`, `schedulerDone`; `Run()` takes no argument.
- `scheduler.run()` and `scheduler.execute(t, read, write)` lose their context;
  the `ctx.Done()` arms collapse into `s.done`.
- `task.locks()` loses its unused parameter; `commandContext` and
  `eventContext` stop embedding `context.Context`; delete the
  `invocationContext` alias.
- `Publication`: delete `ctx`, `mu`, `err`.

**Collapse error propagation**

- `Execute` and `Observe` lose their `error` results; `ExecuteCommand` and the
  `Uses` dispatcher return a response only; `Publication.Wait()` returns nothing.
- `ReportError`, `ReportErrorOnce`, `ForgetReportedError`,
  `ForgetReportedErrors` return nothing.
- `ErrorHandler` becomes `func(error) error`; `defaultErrorHandler` logs, and
  returns the error for `ErrPluginPanic` only.
- Delete `terminated`, `failure`, `recordFailureLocked`, `refusal()`,
  `Engine.Err()`, `ErrEngineTerminated`, and the `errors.As(err, &panicErr) ||`
  override in `reportLocked`. Record the Cause once, first non-nil wins.

**Registration and panics**

- `Registrar.Dependency[T]() (T, error)`. Its ECS callers — `RegisterComponent`
  and `prepareSystem` — panic on the error, which is the fault channel those
  registration helpers already document.
- `portInterface` collects instead of panicking.
- `finalize` stops at the first fault; `registry.errs` becomes one `err` behind
  a `fail` that keeps the first; delete the message-text sort. The walks
  `finalize` performs are ordered by type instead, so the fault a broken
  composition stops at is the same one on every run — which is what sorting the
  joined messages was really for.
- Delete `Kernel.bound()`'s zero-kernel panic. Keep the two `Get()` panics and
  say in their doc comment why they are the only ones.

**Lifecycle and scheduling**

- Start the scheduler in `New`; delete `runTask`'s no-coordinator branch.
- `Run()` returns the Cause; the MainLoop Port is asked to return on a Verdict.
- `runTask` short-circuits an empty lock set, guarded by `s.done`.

**Free deletions that ride along**

- `sortTopologically` and the `ordered[ID]` plumbing it alone uses
  (`kernel/utils.go`): 126 lines with no caller, duplicating the topological
  sort `buildPublicationPlan` performs inline.
- `scheduler.schedule`: no production caller; kept alive by two of its own tests.

**Callers**

- 35 production handler bodies lose their `error` result; 8 of them carry real
  error logic and report instead.
- 22 production context sites across 8 packages. mcp keeps its own context and
  answers the agent on its own timer; `slots/app` and `bundles/input` take a
  deadline as a request field; `extensions/gogpu`'s `quitOnCancellation` is
  replaced by the MainLoop quit path.
- One `Publication.Wait()` result is inspected, at `slots/app/internal/loop.go`.

**Tests**

- ~155 handler bodies, 62 of them with error paths.
- 111 `ErrorHandler` literals: 76 return `true` unconditionally, 34 return
  `false`, and one is conditional — `bundles/input/internal/play_test.go`, which
  branches on `context.Canceled` and stops needing to.
- The negative composition tests keep asserting on an error, because `Run`
  returns one.

**Documentation**

- `kernel/docs/README.md`: the signatures above, the two termination points, and
  the deadline rule. It documents the current API, so it changes with the code
  and not before.
- `.github/instructions/kernel.instructions.md`: § The Kernel Value loses
  `WithContext`; § Commands And Dispatch loses the returned `error` and gains
  what a handler does instead; a new rule states that a plugin owns the
  goroutines it spawns. § Errors gains the one-report rule.
- `CONTEXT.md`: **Report**, **Verdict**, **Cause** and **Termination point** are
  new terms; **Kernel** stops being "a value carrying its engine and invocation
  context".

**Verification**

- `BenchmarkUsesNestedDispatch` before and after the short-circuit, as
  alternating runs of two binaries rather than two runs of one.
- The report-once tests keep their meaning with the `bool` gone: assert on what
  the handler saw, not on what the reporter was told.

---

## Out of scope

Three findings from the same review are independent of this one and are not
specified here:

- **Moving the contention report behind the `Describe` seam.** `contention.go`
  computes from registry internals what `ArchitectureDescription` already
  states, and `Dump` renders inside the runtime module. `bundles/mcp` is a
  second renderer over the same description, so the seam is real.
- **`Exclusive` no longer masquerading as a resource.** It writes the handler's
  own identity type into the write lock set, so three other modules must filter
  a key that names no resource.
- **A typed report-once handle** bound in `Lock`, so the key namespace comes
  from the handler rather than from caller discipline and five hand-written
  string prefixes.

`kernel.TypeName` stays where it is: `bundles/ecs` imports it for diagnostics,
so it is not report-only surface.
