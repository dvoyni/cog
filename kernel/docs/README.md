# kernel guide

How to use `github.com/dvoyni/cog/kernel`, with examples. For the reasons
behind each choice, see [design.md](design.md).

- [Concepts](#concepts)
- [Quick start](#quick-start)
- [Plugins](#plugins)
- [Resources](#resources)
- [Sockets](#sockets)
- [Tasks](#tasks)
- [Events and handlers](#events-and-handlers)
- [Scheduling with the Executor](#scheduling-with-the-executor)
- [The main thread](#the-main-thread)
- [Several kernels](#several-kernels)
- [Waiting from outside](#waiting-from-outside)
- [Errors and logging](#errors-and-logging)
- [Rules](#rules)

## Concepts

```
Supervisor ── lifetime, GetKernel and Broadcast, LogHandler
  ├─ Kernel 1  ── resources · tasks · handlers
  └─ Kernel 2  ── resources · tasks · handlers
```

| Type | What it is |
|---|---|
| `Supervisor` | Spawns and destroys kernels, runs main-thread jobs, ends the application. Owns the one `LogHandler`. |
| `Kernel` | Runtime handle passed to handlers and tasks: `ID`, `Publish`, `Notify`, `Logger`. |
| `Plugin` | Your code: `Init`, `Start`, `Stop`, `Destroy`. |
| `Configurator` | Handed to `Plugin.Init`: declares resources, sockets, tasks and subscriptions, and binds to other plugins' sockets. |
| `ExclusiveSocket[T]` / `Socket[T]` | The one handle to a socket a plugin declared: its binding, or its bindings, from `Start` on. |
| `Access` | Handed to a task's constructor: `Read[T]`, `Write[T]`, `Use(task)`. |
| `*Task[A, B]` | Registered code taking arg `A` and answering result `B`. |
| `Executor` | One publication's batch of work: `Schedule`, `ScheduleAfter`, `Join`, `Publish`. |
| `Job[T]` / `JobHandle` | One scheduled run of a task; the handle is what dependencies are declared with. |
| `Signal` | Lets code outside the kernel wait for a job. |

The flow is always the same: something publishes an event, the event's
handlers schedule jobs on an `Executor`, and the jobs run on the kernel's
workers once their dependencies have finished and their locks are free.

## Quick start

A complete program in two plugins. `clockPlugin` paces the kernel by
publishing a `Hit` on its own timer; `scorePlugin` adds each hit to a score,
logs the total, and ends the application after three rounds. `main` only
spawns and lends the main thread.

```go
package main

import (
	"fmt"
	"runtime"
	"time"

	"github.com/dvoyni/cog/kernel"
)

const (
	clockName kernel.PluginName = "clock"
	scoreName kernel.PluginName = "score"
)

// ClockConfig is clockPlugin's part of the kernel's config.
type ClockConfig struct {
	Period time.Duration
}

// ScoreConfig is scorePlugin's part of the kernel's config.
type ScoreConfig struct {
	Supervisor *kernel.Supervisor
	Rounds     int64
}

// Score is a resource: state the kernel owns and locks.
type Score struct{ Points int64 }

// Hit is an event. Events, task args and results are pointer-free.
type Hit struct{ Damage int32 }

// Total is a task result.
type Total struct{ Points int64 }

// clockPlugin paces the kernel: it publishes a Hit on every tick of its own
// timer.
type clockPlugin struct {
	config *ClockConfig
	stop   chan struct{}
}

func (p *clockPlugin) Name() kernel.PluginName { return clockName }

func (p *clockPlugin) Dependencies() []kernel.PluginName { return nil }

func (p *clockPlugin) Init(c *kernel.Configurator, config any) error {
	p.config = kernel.PluginConfigOrDefault[ClockConfig](config)
	return nil
}

func (p *clockPlugin) Start(k *kernel.Kernel) error {
	p.stop = make(chan struct{})
	go func() {
		ticker := time.NewTicker(p.config.Period)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				k.Publish(Hit{Damage: 5})
			case <-p.stop:
				return
			}
		}
	}()
	return nil
}

func (p *clockPlugin) Stop(k *kernel.Kernel) error {
	close(p.stop)
	return nil
}

func (p *clockPlugin) Destroy() error { return nil }

// scorePlugin adds up hits, logs each total, and ends the application after
// the configured number of rounds.
type scorePlugin struct {
	config *ScoreConfig
	add    *kernel.Task[Hit, Total]
	report *kernel.Task[Total, kernel.NoResult]
}

func (p *scorePlugin) Name() kernel.PluginName { return scoreName }

func (p *scorePlugin) Dependencies() []kernel.PluginName { return nil }

func (p *scorePlugin) Init(c *kernel.Configurator, config any) error {
	p.config = kernel.PluginConfigOrDefault[ScoreConfig](config)
	c.InitResource(Score{})

	// Requesting Write[Score] declares the lock and returns the handle.
	p.add = c.RegisterTask(func(access *kernel.Access) kernel.TaskBody[Hit, Total] {
		score := access.Write[Score]()
		return func(k *kernel.Kernel, hit Hit) Total {
			s := score.Get()
			s.Points += int64(hit.Damage)
			score.Set(s)
			return Total{Points: s.Points}
		}
	})
	// Locks nothing: it only reports the result it is fed.
	p.report = c.RegisterTask(func(access *kernel.Access) kernel.TaskBody[Total, kernel.NoResult] {
		return func(k *kernel.Kernel, t Total) kernel.NoResult {
			k.Logger().LogDebug("score", "points", t.Points)
			if t.Points >= 5*p.config.Rounds {
				p.config.Supervisor.Quit()
			}
			return kernel.NoResult{}
		}
	})

	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, hit Hit) (Hit, kernel.JobHandle) {
		added := ex.Schedule(p.add, hit, in)
		reported := ex.ScheduleAfter(p.report, added) // fed added's Total
		return hit, reported.Handle()
	})
	return nil
}

func (p *scorePlugin) Start(k *kernel.Kernel) error { return nil }
func (p *scorePlugin) Stop(k *kernel.Kernel) error  { return nil }
func (p *scorePlugin) Destroy() error               { return nil }

func init() { runtime.LockOSThread() } // keep main on the main thread

func main() {
	s := kernel.NewSupervisor(nil)
	config := map[kernel.PluginName]any{
		clockName: &ClockConfig{Period: 10 * time.Millisecond},
		scoreName: &ScoreConfig{Supervisor: s, Rounds: 3},
	}
	if _, err := s.Spawn(config, runtime.GOMAXPROCS(0), &clockPlugin{}, &scorePlugin{}); err != nil {
		panic(err)
	}
	for s.WaitMainThread() {
		s.TickMainThread()
	}
	fmt.Println("done")
}
```

Output:

```
[kernel 1] score points=5
[kernel 1] score points=10
[kernel 1] score points=15
done
```

## Plugins

```go
type Plugin interface {
	Name() PluginName
	Dependencies() []PluginName
	Init(configurator *Configurator, config any) error
	Start(kernel *Kernel) error
	Stop(kernel *Kernel) error
	Destroy() error
}
```

- **`Name`** is unique among one kernel's plugins; two plugins of one name
  panic in `Spawn`. It keys the plugin's part of the config.
- **Order** is the order of the plugins given to `Spawn`. `Init` and `Start`
  run in that order; `Stop` and `Destroy` run in reverse. A plugin that needs
  another's resource while registering is listed after it.
- **`Dependencies`** names the plugins whose resources `Init` needs; nil is
  none. `Spawn` checks, before any `Init`, that each is listed before the
  plugin, and panics naming both if one is later or missing, or if a plugin
  names itself or a name twice. It never reorders. A socket is no
  dependency: bindings resolve after every `Init`, whatever the order.
- **`Init`** declares everything: resources, sockets, tasks, subscriptions,
  and the bindings to other plugins' sockets. The `Configurator` is invalid
  once `Init` returns.
- **`Start`** runs once every plugin has initialized. The kernel's workers are
  already running, so `Start` may publish.
- **`Stop`** runs when the kernel is destroyed. Jobs its publications schedule
  still run.
- **`Destroy`** runs after the kernel's last job, for every plugin whose
  `Init` succeeded, and releases whatever `Init` acquired.
- **Config** is a `map[PluginName]any` given to `Spawn`. Each plugin's `Init`
  is handed only its own part, a pointer to its `Config` under its `Name`, or
  nil if there is none. `Init` reads it with `kernel.PluginConfigOrDefault`,
  which answers the pointer given, or the default `Init` passes, or a zero
  `Config`, and panics on any other type, a `Config` by value included. A
  plugin's constructor takes no parameters, `NewPlugin()`.

```go
config := map[kernel.PluginName]any{
	physics.PluginName: &physics.Config{Gravity: -9.8},
	render.PluginName:  &render.Config{VSync: true},
}
id, err := s.Spawn(config, runtime.GOMAXPROCS(0), physics.NewPlugin(), render.NewPlugin())

// in render.Plugin:
func (p *Plugin) Name() kernel.PluginName { return PluginName }

func (p *Plugin) Init(c *kernel.Configurator, config any) error {
	p.config = kernel.PluginConfigOrDefault(config, &Config{VSync: true}) // render's part only
	// ...
}
```

If any `Init` or `Start` fails, the plugins already started are stopped, the
plugins already initialized are destroyed, and `Spawn` returns the error.

Keep tasks and subscriptions in fields of the plugin, and pass a new plugin
value to each `Spawn`. Registration runs again for every kernel.

## Resources

A resource is a value of any type the kernel owns, one per type per kernel.
It is created in `Init` and never added or removed while the kernel runs.

```go
c.InitResource(Health{Points: 10})
c.InitResource(Position{})
```

What a plugin offers other plugins' jobs is a resource it installs, such as
platform's `*WindowManager`. A plugin itself is never a resource, so no job
reaches its lifecycle methods. An implementation one plugin needs from
another goes through a [socket](#sockets).

A resource with no value yet is created empty and filled later by a task
holding its `Write`:

```go
c.InitResource[*Level](nil)
```

A plugin that must register against something an earlier plugin installed
names that plugin among its `Dependencies`, is listed after it, and reads it
with `Resource`:

```go
func (p *game) Dependencies() []kernel.PluginName { return []kernel.PluginName{platform.PluginName} }

// in its Init, once Spawn has checked platform is listed before it:
tasks, _ := c.Resource[platform.Tasks]()
```

Initializing the same type twice panics.

## Sockets

A socket binds plugins to each other. The plugin that needs an
implementation declares the socket of its type in `Init`, and the plugins
that implement it bind to it in theirs:

```go
// platform, which needs the windows and the files:
windows := c.DeclareExclusiveSocket[platform.WindowsSocket](true) // optional, at most one
mounts := c.DeclareSocket[platform.ReadMountSocket](true)         // optional, any number

// gogpu, which implements the windows:
c.BindSocket[platform.WindowsSocket](implementation)
// a game, which ships its assets:
c.BindSocket(platform.ReadMountSocket{FS: assets})
```

- **The declaration's handle is the only way to the bindings.**
  `ExclusiveSocket.Get` answers the binding, or the zero `T` for an optional
  socket left unbound; `Socket.GetAll` answers the bindings in plugin order.
  A socket takes no lock and has no lookup, so no task reaches an
  implementation but through the resources the declaring plugin installs.
- **Bindings resolve after every `Init` and before any `Start`**, so the
  order of plugins in `Spawn` does not matter to them. `Get` and `GetAll`
  panic in `Init`: keep the handle, and read it from `Start` on, or when the
  resource is used.
- **A socket is its type**, named with the suffix `Socket`. Two sockets of
  one interface need two named types. Go infers `T` from the value, so a
  concrete value bound to an interface socket names `T`:
  `BindSocket[platform.WindowsSocket](implementation)`.
- **Composition errors panic** at `Spawn`, naming the socket and the
  plugins: declaring one twice, two bindings of an exclusive socket, none of
  one that is not optional, a binding nobody declares, and a nil binding.

## Tasks

A task is the only code that touches resources. `RegisterTask` runs its
constructor once; each handle it requests declares a lock, and the body
it returns closes over those handles.

A plain function whose parameters are handles registers without writing the
constructor: `RegisterTask0` … `RegisterTask8` declare each parameter.

```go
func heal(k *kernel.Kernel, amount int32, health kernel.Write[Health]) kernel.NoResult {
	h := health.Get()
	h.Points += amount
	health.Set(h)
	return kernel.NoResult{}
}

p.heal = c.RegisterTask1(heal)
```

A task that calls another directly with `Use` writes its constructor with
`RegisterTask`, since a parameter declared from its zero value cannot know
which task to use. Its handles are bound with the same `Declare` the
numbered forms call:

```go
p.healAll = c.RegisterTask(func(access *kernel.Access) kernel.TaskBody[int32, kernel.NoResult] {
	roster := kernel.Read[Roster]{}.Declare(access)
	heal := access.Use(p.heal)
	return func(k *kernel.Kernel, amount int32) kernel.NoResult {
		for range roster.Get().Count {
			heal(k, amount)
		}
		return kernel.NoResult{}
	}
})
```

```go
p.damage = c.RegisterTask(func(access *kernel.Access) kernel.TaskBody[int32, Damaged] {
	health := access.Write[Health]()
	return func(k *kernel.Kernel, amount int32) Damaged {
		h := health.Get()
		h.Points -= amount
		health.Set(h)
		return Damaged{Dead: h.Points <= 0}
	}
})
```

- `access.Read[T]()` returns a `Read[T]` with `Get`; `access.Write[T]()`
  returns a `Write[T]` with `Get` and `Set`. A write supersedes a read of the
  same resource.
- Jobs of a read-only task can run at once; a task that writes runs alone
  against whatever else locks the same resource.
- A task with no arg takes `kernel.NoArg`; one with no result answers
  `kernel.NoResult`.
- A body that panics is recovered and logged through `LogErr`. The job still
  finishes, with the zero result, so its dependents run.

### Calling a task inline

`access.Use(task)` folds the other task's locks into this one and returns a
direct call to its body. The call runs inside this task's job with the locks
already held: no scheduling, no allocation.

```go
p.hitTwice = c.RegisterTask(func(access *kernel.Access) kernel.TaskBody[int32, kernel.NoResult] {
	damage := access.Use(p.damage) // now also holds Write[Health]
	position := access.Read[Position]()
	return func(k *kernel.Kernel, amount int32) kernel.NoResult {
		_ = position.Get()
		damage(k, amount)
		damage(k, amount)
		return kernel.NoResult{}
	}
})
```

### Pinning to the main thread

Window and GPU calls that a platform allows on one thread only go in a
pinned task:

```go
p.draw = c.RegisterTask(func(access *kernel.Access) kernel.TaskBody[Frame, kernel.NoResult] {
	return func(k *kernel.Kernel, f Frame) kernel.NoResult { /* GPU calls */ return kernel.NoResult{} }
})
c.PinToMainThread(p.draw)
```

A pinned job waits for dependencies and locks like any other, and can feed
and be depended on. Only where it runs changes: on the main thread, when
`main` calls `TickMainThread`. A call through `Use` runs on its caller's
goroutine, pinned or not.

## Events and handlers

An event is any pointer-free type. A handler is subscribed in `Init`:

```go
type SubscriptionFunc[E any] func(k *Kernel, ex *Executor, in JobHandle, event E) (E, JobHandle)
```

- Handlers run one by one, synchronously, on the goroutine that called
  `Publish`. They hold no locks and get no resources; their work is to decide
  what to schedule.
- `in` is the previous handler's out dependency (zero for the first). A
  handler returns the event and its own out dependency: a job it scheduled,
  a `Join` of several, or `in` unchanged.
- A handler may change the event by returning a changed copy; the next
  handler receives that copy.
- `Publish` answers the last handler's event and out dependency, so the
  publisher can wait for everything the handlers started.

```go
_, done := k.Publish(Hit{Damage: 3})
```

An event nothing subscribes to is dropped.

### Ordering handlers

Handlers run in subscription order, which is plugin order. To change it,
keep the `Sub` that `Subscribe` returns and order two subscriptions of the
same event:

```go
p.simulate = c.Subscribe(p.onFrameSimulate)
p.render = c.Subscribe(p.onFrameRender)
c.OrderSubscriptions(p.simulate, p.render)
```

Any plugin holding both subscriptions may order them. A plugin that lets
others order around its handlers puts its `Sub`s in a resource. A cycle
panics at spawn.

## Scheduling with the Executor

An `Executor` is one publication's batch of work, handed to each of its
handlers. Nothing in it starts until the last handler returns.

```go
func (ex *Executor) Schedule[A, B any](task *Task[A, B], arg A, deps ...JobHandle) Job[B]
func (ex *Executor) ScheduleAfter[A, B any](task *Task[A, B], from Job[A], deps ...JobHandle) Job[B]
func (ex *Executor) Join(deps ...JobHandle) JobHandle
func (ex *Executor) Publish[E any](in JobHandle, event E) (E, JobHandle)
```

**Schedule** runs a task with an arg once `deps` have finished:

```go
moved := ex.Schedule(p.move, e.Delta, in)
```

**ScheduleAfter** runs a task with another job's result as its arg. The types
line up: a `Job[A]` can only feed a `Task[A, _]`. Every job fed by the same
`from` gets its own copy.

```go
hit := ex.Schedule(p.damage, e.Amount, in)        // Job[Damaged]
said := ex.ScheduleAfter(p.announce, hit)         // announce is *Task[Damaged, NoResult]
```

**Join** makes one dependency of several:

```go
a := ex.Schedule(p.hitTwice, 1, in)
b := ex.Schedule(p.damage, 2, in)
return e, ex.Join(a.Handle(), b.Handle())
```

**Publish** on the executor publishes a nested event into the same batch, so
its handlers' jobs can depend on and feed jobs already scheduled:

```go
c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, f Frame) (Frame, kernel.JobHandle) {
	_, presented := ex.Publish(in, Present{})
	return f, ex.Schedule(p.draw, f, presented).Handle()
})
```

Publishing on `k` from a handler instead starts a separate batch, which
cannot feed this one.

Notes:

- `Job[T].Handle()` turns a typed job into the `JobHandle` dependencies take.
- Jobs whose locks conflict run one at a time in no guaranteed order. Work
  that must happen in an order says so with a dependency.
- Scheduling only happens in a publication. A task or a plugin's `Start` that
  wants work done publishes an event.
- A task may publish; its handlers run inline in its job.

## The main thread

There is no `Run`. `main` spawns kernels and then lends the main thread to
pinned jobs until the application ends:

```go
func init() { runtime.LockOSThread() } // keep main on the main thread

func main() {
	s := kernel.NewSupervisor(nil)
	if _, err := s.Spawn(config, runtime.GOMAXPROCS(0), plugins...); err != nil {
		panic(err)
	}
	for s.WaitMainThread() {
		s.TickMainThread()
	}
}
```

- `TickMainThread` runs every pinned job pending so far and returns at once.
- `WaitMainThread` blocks until a pinned job may be pending, and answers
  false once `Quit` has finished.
- **Kernels pace themselves.** Nothing in `main` drives frames: a plugin with
  a timer runs its own goroutine and publishes its kernel's frame event on
  each tick.
- Keep ticking until `Done`. A kernel being destroyed waits for its pinned
  jobs, so stopping early leaves `Destroy` and `Quit` waiting for good.

## Several kernels

```go
gameID, _ := s.Spawn(gameConfig, runtime.GOMAXPROCS(0), gamePlugins...)
serverID, _ := s.Spawn(serverConfig, 1, serverPlugins...) // sequential
```

- **`workerCount`** is how many goroutines run the kernel's jobs.
  `runtime.GOMAXPROCS(0)` uses every CPU; `1` runs jobs one at a time, which
  suits a simulation that gets its parallelism from running many kernels.
- **Interop is publishing.** `s.GetKernel(id).Publish(event)` publishes into
  one kernel; `s.Broadcast(event)` into every live kernel in spawn order. The
  target's handlers run on the caller's goroutine and its jobs run in the
  target. There is no reply: a reply is another publish going back.
- **`Destroy(id)`** never waits, so a kernel can destroy itself from its own
  job. Its plugins stop, its remaining jobs finish, its plugins are destroyed.
- **`Quit()`** destroys every kernel, refuses further spawns, and closes
  `Done` once all are gone. Running out of kernels does not end the
  application.
- A `KernelID` is never reused, so an ID kept past its kernel names nothing:
  `GetKernel` answers nil for it. A `*Kernel` kept past its destroy stays
  safe to call, and publishes nothing.
- A plugin that needs to spawn or destroy kernels, or to reach another
  kernel, is given the `*Supervisor` or the `KernelID` in its part of the
  config.

```go
type NetConfig struct {
	Supervisor *kernel.Supervisor
	Server     kernel.KernelID
}

clientID, _ := s.Spawn(map[kernel.PluginName]any{
	"net": &NetConfig{Supervisor: s, Server: serverID},
}, runtime.GOMAXPROCS(0), &netPlugin{})

func (p *netPlugin) Init(c *kernel.Configurator, config any) error {
	p.config = kernel.PluginConfigOrDefault[NetConfig](config)
	// ...
}

// later, in a task:
p.config.Supervisor.GetKernel(p.config.Server).Publish(PlayerMoved{X: 1, Y: 2})
```

A `JobHandle` means something only in its own kernel; one carried in an event
into another kernel arrives meaningless.

## Waiting from outside

Code outside the kernel, such as a test or a tool, waits for a job with a
reusable `Signal`:

```go
game := s.GetKernel(gameID)
sig := kernel.NewSignal() // once; reuse it
_, done := game.Publish(Hit{Damage: 3})
game.Notify(sig, done) // signals once done finishes, or now if it has
<-sig.C()
```

Only outside code waits. A task or handler blocking on a `Signal` can
deadlock its kernel, and the main thread blocking on one can deadlock a
pinned job the work depends on.

## Errors and logging

There is one `LogHandler` per supervisor:

```go
type LogHandler interface {
	LogErr(source KernelID, err error)
	LogWarn(source KernelID, msg string, args ...any)
	LogDebug(source KernelID, msg string, args ...any)
}
```

`NewSupervisor(nil)` uses `StdLogger`: `LogErr` panics, `LogWarn` writes to
stderr and `LogDebug` to stdout, each line prefixed `[kernel N]`. Every error
is fatal under it, which suits development.

The kernel never judges an error; it forwards it and carries on. The
`LogHandler` decides what an error means. One that drops a failing kernel
instead of ending the process:

```go
type quitOnError struct {
	kernel.StdLogger
	supervisor *kernel.Supervisor
}

func (l *quitOnError) LogErr(source kernel.KernelID, err error) {
	l.StdLogger.LogWarn(source, "error", "err", err)
	l.supervisor.Destroy(source)
}

handler := &quitOnError{}
s := kernel.NewSupervisor(handler)
handler.supervisor = s
```

From inside a kernel, log through `k.Logger()`, which fills in the kernel's
ID. Args are slog-style key/value pairs:

```go
k.Logger().LogWarn("sprite missing", "id", spriteID)
```

For a condition that is true every frame, the `Once` forms log only the
first call under a key, until `ForgetLogOnce` clears it:

```go
log := k.Logger()
if !found {
	log.LogWarnOnce("sprite:missing", "sprite missing", "id", spriteID)
} else {
	log.ForgetLogOnce("sprite:missing")
}
```

Logging allocates (its `...any` args are boxed), so keep it off per-frame
paths except for the `Once` forms with constant keys.

## Rules

- **Pointer-free data.** Task args, task results and events contain no
  pointers, slices, maps, strings, interfaces, channels or funcs at any
  depth. Text and bulk data travel as an ID or hash, with the data itself in
  a resource. `RegisterTask` and `Subscribe` check this.
- **Composition errors panic.** Registering something invalid (a resource
  initialized twice, a non-pointer-free type, a nil body, a task of another
  kernel, an ordering cycle) or a bad `Spawn` argument panics, so the mistake
  surfaces at spawn. Runtime failures go to `LogErr`.
- **Alloc-free after spawn.** Publishing, scheduling and running jobs
  allocate nothing. Keep events small, since they are copied into and out of
  each handler. Spawn, destroy, plugin lifecycle and logging are exempt.
- **Jobs always complete.** There is no failed job; a panicking body finishes
  with the zero result.
