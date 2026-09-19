package kernel

// Named identifier types for the kernel's registered entities. Distinct types
// (rather than bare strings) keep the registration and dispatch APIs type-safe.
type PluginName string

// ErrorHandler handles a kernel error and decides what it means. Returning nil
// keeps the engine running; returning an error terminates it, and that error is
// what Run returns. Wrapping is allowed and means nothing to the engine.
//
// It is the only place termination is decided. The engine has no opinion of its
// own about any error, including a plugin panic: that opinion lives in
// defaultErrorHandler, where a game can replace it.
//
// Errors may originate from different goroutines, but the engine serializes
// handler calls, and the first non-nil answer is the one Run returns.
type ErrorHandler func(err error) error

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
// Its Executioner still dispatches: the scheduler stops after every Stop has
// run, so shutdown work is talking to a live engine.
type PluginStopper interface {
	Plugin
	Stop(kernel Executioner) error
}

// PluginHost is the optional single plugin that owns the blocking application
// loop. An engine may have zero or one PluginHost.
//
// Run blocks for the life of the application. Quit asks it to return, and is
// the whole of how a running engine is stopped: the Host owns the loop, so only
// the Host can leave it. The engine calls Quit when it is asked to stop and when
// a report terminates the run, from a goroutine that is not the one inside Run,
// and it may call it more than once or before Run has been entered.
type PluginHost interface {
	Plugin
	Run(kernel Executioner) error
	Quit()
}

// Lock binds a handler's resource handles. Requesting a handle is what declares
// the corresponding lock, so binding and declaring cannot drift apart. It runs
// once, during registration; a nil Lock declares no resources.
type Lock func(access ResourceAccess)

// Execute is a command's body. It runs once per invocation with the locks its
// Lock declared already held.
//
// It returns a response and nothing else. A failure the caller should act on is
// part of that response; a failure nobody can act on goes to ReportError. There
// is no third channel, which is what stops a body reporting and returning the
// same fact twice.
type Execute[TRequest any, TResponse any] func(kernel Kernel, request TRequest) TResponse

// Observe is a subscription's body, run once per matching publication. It
// returns nothing: a subscriber has no caller to answer, so what goes wrong in
// one goes to ReportError.
type Observe[TEvent any] func(kernel Kernel, event TEvent)

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

// Subscription is the shape of a subscription factory, declared the same way and
// named verb plus event, for what the handler does on which event:
//
//	type FlushOnUpdate kernel.Subscription[app.UpdateEvent]
type Subscription[TEvent any] = func() (Lock, Observe[TEvent])

// RequiredPort is the shape of a Port that needs exactly one Adapter. A plugin
// names its Port by defining a type from it, with the interface its Adapter
// implements as the type argument, and that defined type is the Port's identity:
//
//	type BackendPort kernel.RequiredPort[Backend]
//
// The shape is never called; it exists so that the Port type carries its
// interface and its kind, and the compiler can read both back.
type RequiredPort[I any] = func(requiredPort) I

// CollectedPort is the shape of a Port that takes any number of Adapters, zero
// included, declared the same way:
//
//	type ProviderPort kernel.CollectedPort[Provider]
type CollectedPort[I any] = func(collectedPort) I

// Adapter is the shape of an Adapter identity. The plugin that fills a Port
// defines a type from it with that Port as the type argument:
//
//	type GfxBackend kernel.Adapter[gfx.BackendPort]
type Adapter[P any] = func(adapterOf) P

// requiredPort, collectedPort and adapterOf mark the three shapes apart, so a
// required Port cannot be collected, a collected one cannot be required, and a
// Port cannot be provided in place of an Adapter.
type (
	requiredPort  struct{}
	collectedPort struct{}
	adapterOf     struct{}
)

// portKind is either Port kind, for the declarations that accept both.
type portKind interface{ requiredPort | collectedPort }

// RequiredPortConstraint identifies a required Port by its defined type.
type RequiredPortConstraint[I any] interface {
	~RequiredPort[I]
}

// CollectedPortConstraint identifies a collected Port by its defined type.
type CollectedPortConstraint[I any] interface {
	~CollectedPort[I]
}

// AdapterConstraint identifies an Adapter by its defined type.
type AdapterConstraint[P any] interface {
	~Adapter[P]
}

// portConstraint identifies a Port of either kind.
type portConstraint[K portKind, I any] interface {
	~func(K) I
}

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
