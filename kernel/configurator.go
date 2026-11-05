package kernel

import (
	"fmt"
	"reflect"

	"github.com/dvoyni/cog/kernel/container"
)

// SubscriptionFunc is a subscription's body: the event handler Subscribe
// adds. Handlers run synchronously and one by one, on the goroutine that
// published, and hold no locks: a handler that needs resources schedules a
// task on ex.
//
// in is the previous handler's out dependency, or the zero JobHandle for the
// first handler. A handler returns the event and its own out dependency: a
// job it scheduled, ex.Join of several, or in unchanged. That is how handlers
// later in the order wait for work earlier ones started.
//
// A handler may change the event by returning a changed copy, which is what
// the next handler receives; one that leaves it alone returns event as given.
// The event travels by value, never by pointer, because a pointer handed to a
// handler escapes to the heap. It is pointer-free, which Subscribe checks.
type SubscriptionFunc[E any] func(kernel *Kernel, ex *Executor, in JobHandle, event E) (E, JobHandle)

// Sub is one subscription of a handler to E, returned by Subscribe. It is
// what OrderSubscriptions orders, and its type keeps that to two
// subscriptions of the same event. The zero Sub is not a subscription.
type Sub[E any] struct {
	index int32
}

// Configurator is a kernel's registration surface, handed to each plugin's
// Init. It is invalid after Init returns.
type Configurator struct {
	kernel *Kernel
	// plugin is the plugin whose Init was handed it, which composition errors
	// name.
	plugin PluginName
}

// ID returns the ID of the kernel being configured.
func (c *Configurator) ID() KernelID { return c.kernel.id }

// InitResource initializes resource T. Initializing T twice is a composition
// error, and panics.
//
// value may be T's zero value, nil for an interface or pointer type:
// InitResource[gfx.Backend](nil) creates the resource with nothing in it yet.
// It exists from then on, so tasks may lock it, and a task holding its Write
// fills it later with Set.
func (c *Configurator) InitResource[T any](value T) {
	t := reflect.TypeFor[T]()
	if _, ok := c.kernel.resources[t]; ok {
		panic(fmt.Sprintf("kernel: resource %v is initialized twice", t))
	}
	c.kernel.resources[t] = &resource{index: len(c.kernel.resources), value: &value}
}

// Resource returns the value resource T was initialized with, for a plugin
// that must register against what an earlier plugin installed. It answers
// false if T is not initialized yet.
func (c *Configurator) Resource[T any]() (T, bool) {
	r, ok := c.kernel.resources[reflect.TypeFor[T]()]
	if !ok {
		var zero T
		return zero, false
	}
	return *r.value.(*T), true
}

// DeclareExclusiveSocket declares the socket of T, which takes one binding,
// and returns the only handle to it: the socket is no resource, so no lock
// and no lookup reach it, and others reach its binding only through what this
// plugin offers. The binding resolves once every plugin has initialized, so
// it may come from a plugin listed before this one or after. Declaring T
// twice, two bindings, or none when the socket is not optional, is a
// composition error, and Spawn panics.
func (c *Configurator) DeclareExclusiveSocket[T any](optional bool) ExclusiveSocket[T] {
	return c.declareSocket[T](true, optional)
}

// DeclareSocket declares the socket of T, which takes any number of
// bindings, and returns the only handle to it, as DeclareExclusiveSocket
// does. Declaring T twice, or no binding when the socket is not optional, is
// a composition error, and Spawn panics.
func (c *Configurator) DeclareSocket[T any](optional bool) Socket[T] {
	return c.declareSocket[T](false, optional)
}

// BindSocket binds value to the socket of T, which a plugin listed before
// this one or after declares. A nil value, or a socket no plugin declares, is
// a composition error, and panics; a typed nil, such as a nil pointer, is not
// nil. Go infers T from value, so a concrete value for an interface socket
// names T: BindSocket[platform.WindowsSocket](windows).
func (c *Configurator) BindSocket[T any](value T) {
	t := reflect.TypeFor[T]()
	if any(value) == nil {
		panic(fmt.Sprintf("kernel: plugin %q binds a nil %v", c.plugin, t))
	}
	c.kernel.socketBindings = append(c.kernel.socketBindings, socketBinding{socket: t, plugin: c.plugin, value: value})
}

//go:generate go run ./internal/generate

// RegisterTask registers a task of this kernel and returns it for
// scheduling. constructor runs once, here: it requests from its Access the
// handles the body uses, which declares the task's locks, and returns the
// body, which closes over those handles. A and B have no size limit.
//
// It is a composition error, and panics, for constructor or the body it
// returns to be nil, for A or B not to be pointer-free, or for constructor to
// break a rule of Access.
func (c *Configurator) RegisterTask[A, B any](constructor TaskConstructorFunc[A, B]) *Task[A, B] {
	if constructor == nil {
		panic("kernel: registering a task with a nil constructor")
	}
	for _, t := range [2]reflect.Type{reflect.TypeFor[A](), reflect.TypeFor[B]()} {
		if !container.IsPointerFree(t) {
			panic(fmt.Sprintf("kernel: task arg or result %v is not pointer-free", t))
		}
	}
	access := &Access{kernel: c.kernel}
	body := constructor(access)
	access.kernel = nil
	if body == nil {
		panic("kernel: a task's constructor returned a nil body")
	}
	task := &Task[A, B]{
		kernel: c.kernel,
		index:  uint32(len(c.kernel.tasks)),
		locks:  &taskLocks{reads: access.reads, writes: access.writes},
		run:    body,
	}
	c.kernel.tasks = append(c.kernel.tasks, task)
	c.kernel.taskLocks = append(c.kernel.taskLocks, task.locks)
	return task
}

// PinToMainThread makes every job of task run on the main thread instead of a
// worker. A pinned job still waits for its dependencies and locks like any
// other, and jobs may depend on it and take its result; only where it runs
// changes. It is for work a platform allows on one thread alone, such as
// window and GPU calls. Pinning governs scheduled jobs only: a call through
// Access.Use runs on its caller's goroutine. Pinning a nil task, or one
// registered in another kernel, is a composition error, and panics.
func (c *Configurator) PinToMainThread[A, B any](task *Task[A, B]) {
	if task == nil || task.kernel != c.kernel {
		panic("kernel: pinning a task that is not this kernel's")
	}
	task.locks.pinnedToMainThread = true
}

// Subscribe adds fn to E's handlers and returns its subscription, for
// OrderSubscriptions. Handlers run in subscription order, which is plugin
// order, except where OrderSubscriptions says otherwise.
//
// E must be pointer-free, as a task's arg must, and fn must not be nil;
// either is a composition error, and panics. That covers every event, because
// an event nothing subscribes to is never delivered.
func (c *Configurator) Subscribe[E any](fn SubscriptionFunc[E]) Sub[E] {
	t := reflect.TypeFor[E]()
	if !container.IsPointerFree(t) {
		panic(fmt.Sprintf("kernel: event %v is not pointer-free", t))
	}
	if fn == nil {
		panic(fmt.Sprintf("kernel: a nil handler subscribes to %v", t))
	}
	d := c.kernel.dispatcher[E]()
	d.handlers = append(d.handlers, fn)
	return Sub[E]{index: int32(len(d.handlers))}
}

// OrderSubscriptions declares that before's handler runs before after's in
// every publication of E. Any plugin holding both subscriptions may order
// them, whichever plugins made them; a plugin that lets others order against
// its handlers hands its subscriptions out in a resource.
//
// Order is settled once every plugin has initialized: each event's handlers
// are sorted to satisfy every declaration, keeping subscription order wherever
// no declaration decides. A cycle among declarations is a composition error,
// as is ordering a zero Sub or one of another kernel, and panics.
func (c *Configurator) OrderSubscriptions[E any](before, after Sub[E]) {
	d := c.kernel.dispatcher[E]()
	for _, sub := range [2]Sub[E]{before, after} {
		if sub.index < 1 || int(sub.index) > len(d.handlers) {
			panic(fmt.Sprintf("kernel: ordering a subscription to %v that is not this kernel's", reflect.TypeFor[E]()))
		}
	}
	d.orders = append(d.orders, [2]int32{before.index - 1, after.index - 1})
}

// declareSocket declares the socket of T, panicking if one is declared.
func (c *Configurator) declareSocket[T any](exclusive, optional bool) *socket[T] {
	t := reflect.TypeFor[T]()
	if existing, ok := c.kernel.sockets[t]; ok {
		panic(fmt.Sprintf("kernel: socket %v is declared twice, by plugins %q and %q", t, existing.declaredBy(), c.plugin))
	}
	declared := &socket[T]{plugin: c.plugin, exclusive: exclusive, optional: optional}
	c.kernel.sockets[t] = declared
	return declared
}
