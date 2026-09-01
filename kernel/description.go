package kernel

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

type ArchitectureDescription struct {
	Plugins       []PluginDescription
	Resources     []ResourceDescription
	Commands      []CommandDescription
	Subscriptions []SubscriptionDescription
}

type PluginDescription struct {
	Name         PluginName
	Dependencies []PluginName
	Host         bool
	Starts       bool
	Stops        bool
}

type ResourceDescription struct {
	Type  reflect.Type
	Owner PluginName
}

type CommandDescription struct {
	Type  reflect.Type
	Owner PluginName
}

type SubscriptionDescription struct {
	Event     reflect.Type
	Type      reflect.Type
	Owner     PluginName
	Phase     string
	DependsOn []reflect.Type
}

// Describe returns a detached description of the finalized engine architecture.
func (e *Engine) Describe() ArchitectureDescription {
	description := ArchitectureDescription{
		Plugins:       make([]PluginDescription, 0, len(e.plugins)),
		Resources:     make([]ResourceDescription, 0, len(e.registry.resources)),
		Commands:      make([]CommandDescription, 0, len(e.registry.commands)),
		Subscriptions: make([]SubscriptionDescription, 0),
	}
	for _, plugin := range e.plugins {
		_, host := plugin.(PluginHost)
		_, starts := plugin.(PluginStarter)
		_, stops := plugin.(PluginStopper)
		description.Plugins = append(description.Plugins, PluginDescription{
			Name: plugin.Name(), Dependencies: slices.Clone(plugin.Dependencies()),
			Host: host, Starts: starts, Stops: stops,
		})
	}
	for resourceType, resource := range e.registry.resources {
		description.Resources = append(description.Resources, ResourceDescription{Type: resourceType, Owner: resource.owner})
	}
	for commandType, command := range e.registry.commands {
		description.Commands = append(description.Commands, CommandDescription{Type: commandType, Owner: command.owner})
	}
	for eventType, plan := range e.registry.publications {
		for i, node := range plan.nodes {
			dependencyTypes := make([]reflect.Type, 0, node.dependsOn)
			for dependencyIndex, dependency := range plan.nodes {
				if slices.Contains(dependency.dependents, i) {
					dependencyTypes = append(dependencyTypes, plan.nodes[dependencyIndex].task.orderID())
				}
			}
			description.Subscriptions = append(description.Subscriptions, SubscriptionDescription{
				Event: eventType, Type: node.task.orderID(), Owner: subscriptionOwner(node.task),
				Phase: subscriptionPhase(node.task), DependsOn: dependencyTypes,
			})
		}
	}
	slices.SortFunc(description.Resources, func(a, b ResourceDescription) int { return compareTypes(a.Type, b.Type) })
	slices.SortFunc(description.Commands, func(a, b CommandDescription) int { return compareTypes(a.Type, b.Type) })
	slices.SortFunc(description.Subscriptions, func(a, b SubscriptionDescription) int {
		if eventOrder := compareTypes(a.Event, b.Event); eventOrder != 0 {
			return eventOrder
		}
		return compareTypes(a.Type, b.Type)
	})
	return description
}

func subscriptionOwner(value subscription) PluginName {
	switch task := value.(type) {
	case interface{ pluginOwner() PluginName }:
		return task.pluginOwner()
	default:
		return ""
	}
}

func subscriptionPhase(value subscription) string {
	for _, id := range value.orderBefore() {
		if id == nil {
			return "first"
		}
	}
	for _, id := range value.orderAfter() {
		if id == nil {
			return "last"
		}
	}
	return "ordinary"
}

func compareTypes(a, b reflect.Type) int {
	if a.String() < b.String() {
		return -1
	}
	if a.String() > b.String() {
		return 1
	}
	return 0
}

// Dump renders Describe as a readable architecture table. Resource locks are
// declared inside each handler's Lock, so this is the only place the whole
// coupling map can be seen at once.
func Dump(engine *Engine) string {
	description := engine.Describe()
	var out strings.Builder
	out.WriteString("plugins:\n")
	for _, plugin := range description.Plugins {
		var roles []string
		if plugin.Host {
			roles = append(roles, "host")
		}
		if plugin.Starts {
			roles = append(roles, "starts")
		}
		if plugin.Stops {
			roles = append(roles, "stops")
		}
		fmt.Fprintf(&out, "  %s %v %v\n", plugin.Name, plugin.Dependencies, roles)
	}
	out.WriteString("resources:\n")
	for _, res := range description.Resources {
		fmt.Fprintf(&out, "  %v (%s)\n", res.Type, res.Owner)
	}
	out.WriteString("commands:\n")
	for _, cmd := range description.Commands {
		fmt.Fprintf(&out, "  %v (%s)\n", cmd.Type, cmd.Owner)
	}
	out.WriteString("subscriptions:\n")
	var event reflect.Type
	for _, sub := range description.Subscriptions {
		if sub.Event != event {
			event = sub.Event
			fmt.Fprintf(&out, "  %v\n", event)
		}
		fmt.Fprintf(&out, "    %v (%s, %s)", sub.Type, sub.Owner, sub.Phase)
		if len(sub.DependsOn) > 0 {
			fmt.Fprintf(&out, " after %v", sub.DependsOn)
		}
		out.WriteString("\n")
	}
	return out.String()
}
