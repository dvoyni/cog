package kernel

import (
	"fmt"
	"reflect"
	"strings"
)

type ArchitectureDescription struct {
	Plugins       []PluginDescription
	Resources     []ResourceDescription
	Ports         []PortDescription
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

// PortDescription reports one plugin's declaration of a Port: the Port type and
// the type it is built on (an interface, or a value type), the plugin that declared it, whether it collects
// any number of Adapters or requires exactly one, and the Adapters bound to it,
// in plugin order. An Adapter for a Port nobody requires or collects binds to
// nothing and has no entry.
type PortDescription struct {
	Type      reflect.Type
	Interface reflect.Type
	Owner     PluginName
	Collects  bool
	Adapters  []AdapterDescription
}

// AdapterDescription names one contribution to a Port: the Adapter type it was
// provided as, and the plugin that provided it.
type AdapterDescription struct {
	Type   reflect.Type
	Plugin PluginName
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
	// SelfExclusive reports that the handler declared Exclusive: it never runs
	// concurrently with itself, and a second invocation queues behind the first.
	// It names no resource, so it appears here rather than among Writes, and it
	// raises no entry in the conflict report, which pairs distinct handlers only.
	SelfExclusive bool
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
	// SelfExclusive reports the same declaration CommandDescription does.
	SelfExclusive bool
}

// describeAccess renders one handler's finalized lock set. read and write are
// already the transitive closure — resolveUses folded every declared dispatch
// into them at composition — while uses holds the direct edges that explain how
// they got there. Each slice is a copy, so the description stays detached.
func describeAccess(access *ResourceAccess) (reads, writes, uses []reflect.Type, selfExclusive bool) {
	if access == nil {
		return nil, nil, nil, false
	}
	_, selfExclusive = access.write[access.self]
	// Only the write set can carry a key that names no resource: Exclusive adds
	// one, and absorb can fold a callee's in. Reads are resources by
	// construction, so they need no filtering.
	writes = make([]reflect.Type, 0, len(access.write))
	for _, id := range sortedTypes(access.write) {
		if access.isResource(id) {
			writes = append(writes, id)
		}
	}
	return sortedTypes(access.read), writes, sortedTypes(access.uses), selfExclusive
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

// compareTypes orders two types by TypeName. A nil type sorts first, which is how a
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
	return strings.Compare(TypeName(a), TypeName(b))
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
		fmt.Fprintf(&out, "  %s (%s)\n", TypeName(res.Type), res.Owner)
	}
	out.WriteString("ports:\n")
	for _, port := range description.Ports {
		verb := "requires"
		if port.Collects {
			verb = "collects"
		}
		fmt.Fprintf(&out, "  %s (%s) %s %s\n", TypeName(port.Type), port.Owner, verb, adapterList(port.Adapters))
	}
	out.WriteString("commands:\n")
	for _, cmd := range description.Commands {
		fmt.Fprintf(&out, "  %s (%s)%s\n", TypeName(cmd.Type), cmd.Owner, dumpAccess(cmd.Reads, cmd.Writes, cmd.Uses, cmd.SelfExclusive))
	}
	out.WriteString("subscriptions:\n")
	var event reflect.Type
	for _, sub := range description.Subscriptions {
		if sub.Event != event {
			event = sub.Event
			fmt.Fprintf(&out, "  %s\n", TypeName(event))
		}
		fmt.Fprintf(&out, "    %s (%s, %s)", TypeName(sub.Type), sub.Owner, sub.Phase)
		if len(sub.DependsOn) > 0 {
			fmt.Fprintf(&out, " after %v", typeNames(sub.DependsOn))
		}
		out.WriteString(dumpAccess(sub.Reads, sub.Writes, sub.Uses, sub.SelfExclusive))
		out.WriteString("\n")
	}
	dumpContention(&out, description.Contention)
	return out.String()
}

// dumpAccess renders a handler's resolved lock set as trailing columns, omitting
// the ones it has nothing in.
func dumpAccess(reads, writes, uses []reflect.Type, selfExclusive bool) string {
	var out strings.Builder
	if len(reads) > 0 {
		fmt.Fprintf(&out, " reads %v", typeNames(reads))
	}
	if len(writes) > 0 {
		fmt.Fprintf(&out, " writes %v", typeNames(writes))
	}
	if len(uses) > 0 {
		fmt.Fprintf(&out, " uses %v", typeNames(uses))
	}
	if selfExclusive {
		out.WriteString(" exclusive")
	}
	return out.String()
}

// typeNames renders each type with TypeName, for printing as a list.
func typeNames(types []reflect.Type) []string {
	names := make([]string, 0, len(types))
	for _, typ := range types {
		names = append(names, TypeName(typ))
	}
	return names
}
