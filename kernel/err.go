package kernel

import (
	"fmt"
	"reflect"
)

// ErrSchedulerStopped is returned when work is submitted after the scheduler's
// context has been canceled.
type ErrSchedulerStopped struct{}

func (ErrSchedulerStopped) Error() string {
	return "scheduler has stopped"
}

// ErrConflictingPluginName is returned by Run when two plugins share a name.
type ErrConflictingPluginName struct {
	PluginName PluginName
}

func (e ErrConflictingPluginName) Error() string {
	return fmt.Sprintf("trying to register already registered plugin: %s", e.PluginName)
}

// ErrMissingPluginDependency is reported when a registered plugin declares a
// dependency on a plugin that was not registered.
type ErrMissingPluginDependency struct {
	Plugin     PluginName
	Dependency PluginName
}

func (e ErrMissingPluginDependency) Error() string {
	return fmt.Sprintf("plugin %q depends on unregistered plugin %q", e.Plugin, e.Dependency)
}

// ErrPluginDependencyCycle is reported when plugin dependencies cannot be
// ordered, including when a plugin depends on itself.
type ErrPluginDependencyCycle struct {
	Plugins []PluginName
}

func (e ErrPluginDependencyCycle) Error() string {
	return fmt.Sprintf("cyclic plugin dependencies among %v", e.Plugins)
}

type ErrMultipleHosts struct {
	First  PluginName
	Second PluginName
}

func (e ErrMultipleHosts) Error() string {
	return fmt.Sprintf("plugins %q and %q both implement kernel.Host", e.First, e.Second)
}

type ErrDuplicateRegistration struct {
	Kind     string
	Type     reflect.Type
	Owner    PluginName
	Existing PluginName
}

func (e ErrDuplicateRegistration) Error() string {
	return fmt.Sprintf("plugin %q registered duplicate %s %v already owned by %q", e.Owner, e.Kind, e.Type, e.Existing)
}

type ErrMissingResource struct {
	Type reflect.Type
}

func (e ErrMissingResource) Error() string {
	return fmt.Sprintf("required resource %v has no initial value", e.Type)
}

type ErrPluginPanic struct {
	Plugin    PluginName
	Boundary  string
	Recovered any
	Stack     []byte
}

func (e ErrPluginPanic) Error() string {
	return fmt.Sprintf("plugin %q panicked in %s: %v", e.Plugin, e.Boundary, e.Recovered)
}

// ErrUsingUnknownCommand is reported when a handler declares Uses of a command
// no plugin registered, so its lock closure cannot be resolved.
type ErrUsingUnknownCommand struct {
	Declaring reflect.Type
	Command   reflect.Type
}

func (e ErrUsingUnknownCommand) Error() string {
	return fmt.Sprintf("handler %v declares use of unregistered command %v", e.Declaring, e.Command)
}

// ErrUsingCommandCycle is reported when Uses declarations form a cycle, whose
// lock closure is not computable.
type ErrUsingCommandCycle struct {
	Commands []reflect.Type
}

func (e ErrUsingCommandCycle) Error() string {
	return fmt.Sprintf("cyclic command use among %v", e.Commands)
}

// ErrUndeclaredDependency is reported when a plugin declares a lock on a
// resource owned by a plugin it did not declare a dependency on. Ownership is
// the coupling that matters, so Dependencies must name every plugin whose
// resources a handler binds.
type ErrUndeclaredDependency struct {
	Plugin   PluginName
	Owner    PluginName
	Resource reflect.Type
}

func (e ErrUndeclaredDependency) Error() string {
	return fmt.Sprintf("plugin %q locks resource %v owned by %q without declaring it as a dependency",
		e.Plugin, e.Resource, e.Owner)
}

// ErrSubscriptionCycle is returned when an event's subscriptions have an
// unsatisfiable before/after ordering.
type ErrSubscriptionCycle struct {
	EventType         reflect.Type
	SubscriptionTypes []reflect.Type
}

func (e ErrSubscriptionCycle) Error() string {
	return fmt.Sprintf("cyclic subscription dependencies for event %v among handler types %v", e.EventType, e.SubscriptionTypes)
}

// ErrExecutingUnknownCommand is returned when executing an unregistered command type.
type ErrExecutingUnknownCommand[TCommand any] struct {
}

func (e ErrExecutingUnknownCommand[TCommand]) Error() string {
	return fmt.Sprintf("trying to execute unknown command %v", reflect.TypeFor[TCommand]())
}
