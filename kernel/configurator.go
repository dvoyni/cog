package kernel

import (
	"fmt"
	"reflect"
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

// RegisterTask registers a task of this kernel and returns it for
// scheduling. declare runs once, here: it requests from its Access the
// handles the body uses, which declares the task's locks, and returns the
// body, which closes over those handles. A and B have no size limit.
//
// It is a composition error, and panics, for declare or the body it returns
// to be nil, for A or B not to be pointer-free, or for declare to break a
// rule of Access.
func (c *Configurator) RegisterTask[A, B any](declare func(access *Access) TaskBody[A, B]) *Task[A, B] {
	if declare == nil {
		panic("kernel: registering a task with a nil declare function")
	}
	for _, t := range [2]reflect.Type{reflect.TypeFor[A](), reflect.TypeFor[B]()} {
		if !isPointerFree(t) {
			panic(fmt.Sprintf("kernel: task arg or result %v is not pointer-free", t))
		}
	}
	access := &Access{kernel: c.kernel}
	body := declare(access)
	access.kernel = nil
	if body == nil {
		panic("kernel: a task's declare function returned a nil body")
	}
	return &Task[A, B]{kernel: c.kernel, reads: access.reads, writes: access.writes, run: body}
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
	task.pinnedToMainThread = true
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
	if !isPointerFree(t) {
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

// isPointerFree reports whether t holds no pointer at any depth: no pointer,
// unsafe pointer, slice, map, string, interface, channel or func. Numbers,
// bools, and arrays and structs of those are pointer-free.
func isPointerFree(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return true
	case reflect.Array:
		return isPointerFree(t.Elem())
	case reflect.Struct:
		for i := range t.NumField() {
			if !isPointerFree(t.Field(i).Type) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
