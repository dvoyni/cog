package kernel

// Configurator is a kernel's registration surface, handed to each plugin's
// Register. It is invalid after Register returns.
type Configurator struct{}

// ID returns the ID of the kernel being configured.
func (c *Configurator) ID() KernelID { return 0 }

// InitResource initializes resource T. Initializing T twice is a registration
// failure.
//
// value may be T's zero value, nil for an interface or pointer type:
// InitResource[gfx.Backend](nil) creates the resource with nothing in it yet.
// It exists from then on, so tasks may lock it, and a task holding its Write
// fills it later with Set.
func (c *Configurator) InitResource[T any](value T) {}

// Resource returns the value resource T was initialized with, for a plugin
// that must register against what an earlier plugin installed. It fails if T
// is not initialized yet.
func (c *Configurator) Resource[T any]() (T, error) {
	var zero T
	return zero, nil
}

// RegisterTask registers fn as a task of this kernel and returns it for
// scheduling. R is reflected here, once: its handles are bound and its lock
// set compiled, so running the task allocates nothing. It is a registration
// failure for R to hold anything but Read and Write fields, to name a
// resource no plugin initializes, or for A or B not to be plain old data;
// RegisterTask then returns nil. A and B have no size limit.
func (c *Configurator) RegisterTask[R, A, B any](fn TaskFunc[R, A, B]) *Task[A, B] {
	return nil
}

// PinToMainThread makes every job of task run on the main thread instead of a
// pool worker. A pinned job still waits for its dependencies and locks like any
// other, and jobs may depend on it and take its result; only where it runs
// changes. It is for work a platform allows on one thread alone, such as
// window and GPU calls. Pinning a nil task, or one registered in another
// kernel, is a registration failure.
func (c *Configurator) PinToMainThread[A, B any](task *Task[A, B]) {}

// Subscribe adds fn to E's handlers and returns its subscription, for
// OrderSubscriptions. Handlers run in subscription order, which is plugin
// order, except where OrderSubscriptions says otherwise.
//
// E must be plain old data, as a task's arg must, and it is a registration
// failure otherwise. That covers every event, published or sent, because an
// event nothing subscribes to is never delivered.
func (c *Configurator) Subscribe[E any](fn SubscriptionFunc[E]) Sub[E] {
	return Sub[E]{}
}

// OrderSubscriptions declares that before's handler runs before after's in
// every publication of E. Any plugin holding both subscriptions may order
// them, whichever plugins made them; a plugin that lets others order against
// its handlers hands its subscriptions out in a resource.
//
// Order is settled once every plugin has registered: each event's handlers
// are sorted to satisfy every declaration, keeping subscription order wherever
// no declaration decides. A cycle among declarations is a registration
// failure, as is ordering a zero Sub.
func (c *Configurator) OrderSubscriptions[E any](before, after Sub[E]) {}
