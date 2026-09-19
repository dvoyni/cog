package kernel

import (
	"reflect"
	"slices"
	"strings"
)

// Describe returns a detached description of the finalized engine architecture.
func (e *Engine) Describe() ArchitectureDescription {
	description := ArchitectureDescription{
		Plugins:       make([]PluginDescription, 0, len(e.plugins)),
		Resources:     make([]ResourceDescription, 0, len(e.registry.resources)),
		Ports:         make([]PortDescription, 0, len(e.registry.adapterDeclarations)),
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
	for _, declaration := range e.registry.adapterDeclarations {
		description.Ports = append(description.Ports, PortDescription{
			Type: declaration.port, Interface: declaration.iface, Owner: declaration.owner,
			Collects: declaration.collects,
			Adapters: describeAdapters(e.registry.adapterContributions[declaration.port]),
		})
	}
	for commandType, command := range e.registry.commands {
		reads, writes, uses, selfExclusive := describeAccess(command.resources)
		description.Commands = append(description.Commands, CommandDescription{
			Type: commandType, Owner: command.owner, Reads: reads, Writes: writes, Uses: uses,
			SelfExclusive: selfExclusive,
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
			reads, writes, uses, selfExclusive := describeAccess(access)
			description.Subscriptions = append(description.Subscriptions, SubscriptionDescription{
				Event: eventType, Type: node.task.orderID(), Owner: owner,
				Phase: subscriptionPhase(node.task), DependsOn: dependencyTypes,
				Reads: reads, Writes: writes, Uses: uses, SelfExclusive: selfExclusive,
			})
		}
	}
	slices.SortFunc(description.Resources, func(a, b ResourceDescription) int { return compareTypes(a.Type, b.Type) })
	slices.SortStableFunc(description.Ports, func(a, b PortDescription) int {
		if portOrder := compareTypes(a.Type, b.Type); portOrder != 0 {
			return portOrder
		}
		return strings.Compare(string(a.Owner), string(b.Owner))
	})
	slices.SortFunc(description.Commands, func(a, b CommandDescription) int { return compareTypes(a.Type, b.Type) })
	slices.SortFunc(description.Subscriptions, func(a, b SubscriptionDescription) int {
		if eventOrder := compareTypes(a.Event, b.Event); eventOrder != 0 {
			return eventOrder
		}
		return compareTypes(a.Type, b.Type)
	})
	description.Contention = describeContention(e.registry)
	return description
}
