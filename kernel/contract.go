package kernel

// Named identifier types for the kernel's registered entities. Distinct types
// (rather than bare strings) keep the registration and dispatch APIs type-safe.
type PluginName string

// ErrorHandler handles a kernel error and reports whether the engine should
// terminate. Returning false keeps the engine running where recovery is
// possible. Errors may originate from different goroutines, but the engine
// serializes handler calls.
type ErrorHandler func(err error) (terminate bool)

// Plugin is a statically linked unit of functionality selected before startup
// and fixed for the engine lifetime. Registration follows dependency order.
type Plugin interface {
	Name() PluginName
	Dependencies() []PluginName
	Register(registrar *Registrar, config any) error
}

// PluginStarter optionally participates in startup after registration finalizes.
type PluginStarter interface {
	Plugin
	Start(kernel Executioner) error
}

// PluginStopper optionally participates in reverse dependency-order shutdown.
// Its Kernel carries the shutdown context, which outlives engine cancellation.
type PluginStopper interface {
	Plugin
	Stop(kernel Executioner) error
}

// PluginHost is the optional single plugin that owns the blocking application
// loop. An engine may have zero or one PluginHost.
type PluginHost interface {
	Plugin
	Run(kernel Executioner) error
}

// Lock binds a handler's resource handles. Requesting a handle is what declares
// the corresponding lock, so binding and declaring cannot drift apart. It runs
// once, during registration; a nil Lock declares no resources.
type Lock func(access ResourceAccess)

// Execute is a command's body. It runs once per invocation with the locks its
// Lock declared already held.
type Execute[TRequest any, TResponse any] func(kernel Kernel, request TRequest) (TResponse, error)

// Observe is a subscription's body, run once per matching publication.
type Observe[TEvent any] func(kernel Kernel, event TEvent) error

// Command is the shape of a command factory. A plugin names its command by
// defining a type from it, and that defined type is the command's identity:
//
//	type LoadCmd kernel.Command[LoadRequest, LoadResponse]
//	type LoadRequest struct{ ... }
//	type LoadResponse struct{ ... }
//
// The declaration carries the name; the factory that implements it is always
// private and named for the command with an Impl suffix, so the two never
// collide even when the command itself is package-private:
//
//	func loadCmdImpl() (kernel.Lock, kernel.Execute[LoadRequest, LoadResponse])
type Command[TRequest any, TResponse any] = func() (Lock, Execute[TRequest, TResponse])

// Subscription is the shape of a subscription factory, named the same way:
//
//	type UpdateEventHandler kernel.Subscription[app.UpdateEvent]
type Subscription[TEvent any] = func() (Lock, Observe[TEvent])

// CommandConstraint identifies a command by its distinct defined factory type.
// The factory is called once at registration to produce the command's Lock and
// Execute; both are cached for the engine lifetime.
type CommandConstraint[TRequest any, TResponse any] interface {
	~Command[TRequest, TResponse]
}

// SubscriptionConstraint identifies a subscription by its distinct defined
// factory type. Each logical subscription must use its own defined type.
type SubscriptionConstraint[TEvent any] interface {
	~Subscription[TEvent]
}
