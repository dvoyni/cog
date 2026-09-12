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
	// Contention is the conflict report derived from the lock sets above: which
	// handlers can never overlap, which resources they serialise on, and which
	// phases that leaves effectively single-threaded.
	Contention ContentionDescription
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

// CommandDescription reports one registered command and the lock set its
// handler ends up holding. Reads and Writes are the resolved, transitive sets:
// the resources the handler declared itself, plus everything folded in from the
// commands it declares in Uses. They are the one fact in a description that no
// source file states, because a handler deliberately never names the resources
// behind a command it dispatches.
type CommandDescription struct {
	Type   reflect.Type
	Owner  PluginName
	Reads  []reflect.Type
	Writes []reflect.Type
	Uses   []reflect.Type
}

// SubscriptionDescription reports one registered subscription, its position in
// its event's dependency graph, and — like CommandDescription — the resolved
// read and write sets its handler holds once declared dispatches are folded in.
type SubscriptionDescription struct {
	Event     reflect.Type
	Type      reflect.Type
	Owner     PluginName
	Phase     string
	DependsOn []reflect.Type
	Reads     []reflect.Type
	Writes    []reflect.Type
	Uses      []reflect.Type
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
		reads, writes, uses := describeAccess(command.resources)
		description.Commands = append(description.Commands, CommandDescription{
			Type: commandType, Owner: command.owner, Reads: reads, Writes: writes, Uses: uses,
		})
	}
	for eventType, plan := range e.registry.publications {
		for i, node := range plan.nodes {
			dependencyTypes := make([]reflect.Type, 0, node.dependsOn)
			for dependencyIndex, dependency := range plan.nodes {
				if slices.Contains(dependency.dependents, i) {
					dependencyTypes = append(dependencyTypes, plan.nodes[dependencyIndex].task.orderID())
				}
			}
			owner, access := node.task.coupling()
			reads, writes, uses := describeAccess(access)
			description.Subscriptions = append(description.Subscriptions, SubscriptionDescription{
				Event: eventType, Type: node.task.orderID(), Owner: owner,
				Phase: subscriptionPhase(node.task), DependsOn: dependencyTypes,
				Reads: reads, Writes: writes, Uses: uses,
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
	description.Contention = e.registry.describeContention()
	return description
}

// describeAccess renders one handler's finalized lock set. read and write are
// already the transitive closure — resolveUses folded every declared dispatch
// into them at composition — while uses holds the direct edges that explain how
// they got there. Each slice is a copy, so the description stays detached.
func describeAccess(access *ResourceAccess) (reads, writes, uses []reflect.Type) {
	if access == nil {
		return nil, nil, nil
	}
	return sortedTypes(access.read), sortedTypes(access.write), sortedTypes(access.uses)
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

// compareTypes orders two types by name. A nil type sorts first, which is how a
// command — which names no event — precedes every subscription in the conflict
// report.
func compareTypes(a, b reflect.Type) int {
	if a == nil || b == nil {
		if a == b {
			return 0
		}
		if a == nil {
			return -1
		}
		return 1
	}
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
		fmt.Fprintf(&out, "  %v (%s)%s\n", cmd.Type, cmd.Owner, dumpAccess(cmd.Reads, cmd.Writes, cmd.Uses))
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
		out.WriteString(dumpAccess(sub.Reads, sub.Writes, sub.Uses))
		out.WriteString("\n")
	}
	dumpContention(&out, description.Contention)
	return out.String()
}

// dumpAccess renders a handler's resolved lock set as trailing columns, omitting
// the ones it has nothing in.
func dumpAccess(reads, writes, uses []reflect.Type) string {
	var out strings.Builder
	if len(reads) > 0 {
		fmt.Fprintf(&out, " reads %v", reads)
	}
	if len(writes) > 0 {
		fmt.Fprintf(&out, " writes %v", writes)
	}
	if len(uses) > 0 {
		fmt.Fprintf(&out, " uses %v", uses)
	}
	return out.String()
}
