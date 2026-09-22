package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
)

// architectureName is the broker's one capability, rendered as the tool
// mcpserver_architecture: the prefix names who offers it, the name says what it
// is about.
const architectureName = "architecture"

// architectureDescription is prompt text, and it is reproduced in
// bundles/mcp/docs/specs/broker.md so it is reviewed as prompt text rather than
// buried as a string literal.
const architectureDescription = "What this engine is actually composed of: the plugins in start " +
	"order, who owns which command, event and resource, which Adapters are bound to each Port, the " +
	"subscription dependency graph, and — " +
	"the part you cannot get by reading source — the full set of resources each handler ends up " +
	"locking once the commands it declares are folded in. Use it when you need to know what a " +
	"dispatch really touches, or why one handler waits for another. Commands are listed so you can " +
	"see who owns behaviour; you cannot call them from here, and there is no tool that takes a " +
	"command name. `contention` answers why a frame is not parallel: it lists the handlers that can " +
	"never run at the same time because one writes a resource the other holds. That is how shared " +
	"state works and is not a defect; read it to find what serialises, not to report a bug. " +
	"`contended` is false when nothing does. `resources` are ranked by how many handler pairs each " +
	"serialises, with the handlers that write it and a count of those that read it. A phase " +
	"appears in `phases` only if some pair of its members conflicts, so a phase missing there runs " +
	"its members in parallel; `singleThreaded` means its members run one at a time, and " +
	"`widestLocks` names the members that conflict with every other, which are what makes it " +
	"serialise. Only the ten `handlerPairs` sharing the most resources are listed, and " +
	"`handlerPairsOmitted` counts the rest. A handler that uses a command is reported as " +
	"conflicting with that command, because it absorbs the command's locks; if the command " +
	"declares `Exclusive`, the handler absorbs that too, and the pair's resources may then name the " +
	"command itself. A handler marked `selfExclusive` never overlaps itself, so two simultaneous " +
	"calls to it queue; that raises no contention pair, so read it on the command or subscription " +
	"entry. Pass `path` to write the JSON to a file instead of returning it inline."

// architectureHandlerPairs bounds how many handler pairs the answer lists. It
// matches kernel.Dump's handler-pair cap, which is unexported, so the value is
// duplicated here on purpose: the pairwise set is quadratic in the number of
// handlers, and one resource every handler touches makes nearly all of it.
const architectureHandlerPairs = 10

// architectureRequest asks for the finalized architecture.
type architectureRequest struct {
	// Path, when given, receives the JSON instead of the agent receiving it
	// inline. It is absolute because a relative path would silently resolve
	// against the game's working directory, which need not be the agent's.
	Path string `json:"path,omitempty" jsonschema:"absolute path of a .json file to write the description to instead of returning it inline; parent directories are created and an existing file is overwritten"`
}

// architectureResponse is the finalized architecture as five flat arrays and
// the contention report, or just the path when one was given. There is no
// index: the type string is the address, and uses, dependsOn, reads, writes and
// every handler the contention report names are all joins on it.
type architectureResponse struct {
	// Path is set, and the rest empty, when the description was written to a
	// file instead.
	Path          string                     `json:"path,omitempty"`
	Plugins       []architecturePlugin       `json:"plugins,omitempty"`
	Resources     []architectureResource     `json:"resources,omitempty"`
	Ports         []architecturePort         `json:"ports,omitempty"`
	Commands      []architectureCommand      `json:"commands,omitempty"`
	Subscriptions []architectureSubscription `json:"subscriptions,omitempty"`
	// Contention is always set by describe and nil on the path-only reply. A
	// value would put a contended false on every path reply, which is a false
	// statement about an engine that may well contend.
	Contention *architectureContention `json:"contention,omitempty"`
}

// architectureContention is the kernel's conflict report in the order the
// kernel ranks it, with the handler pairs capped as kernel.Dump caps them.
type architectureContention struct {
	// Contended is always present, so an engine without contention says so
	// rather than leaving an Agent to infer it from missing arrays.
	Contended           bool                             `json:"contended"`
	Resources           []architectureResourceContention `json:"resources,omitempty"`
	Phases              []architecturePhaseContention    `json:"phases,omitempty"`
	HandlerPairs        []architectureHandlerPair        `json:"handlerPairs,omitempty"`
	HandlerPairsOmitted int                              `json:"handlerPairsOmitted,omitempty"`
}

// architectureResourceContention is one resource handler pairs serialise on.
// Readers is a count, because a resource every System reads would otherwise
// list hundreds of names.
type architectureResourceContention struct {
	Type      string   `json:"type"`
	Owner     string   `json:"owner"`
	Conflicts int      `json:"conflicts"`
	Writers   []string `json:"writers,omitempty"`
	Readers   int      `json:"readers"`
}

// architecturePhaseContention is one event's phase in which some member pair
// serialises. A phase absent here has none, and runs its members in parallel.
type architecturePhaseContention struct {
	Event          string   `json:"event"`
	Phase          string   `json:"phase"`
	Members        int      `json:"members"`
	Conflicts      int      `json:"conflicts"`
	SingleThreaded bool     `json:"singleThreaded"`
	WidestLocks    []string `json:"widestLocks,omitempty"`
}

// architectureHandlerPair is two handlers that can never overlap and the keys
// they serialise on, as the kernel computes them: an Exclusive key absorbed
// through Uses is among them, named by the command's identity type.
type architectureHandlerPair struct {
	A         string   `json:"a"`
	B         string   `json:"b"`
	Resources []string `json:"resources,omitempty"`
}

// architecturePlugin is one plugin in start order.
type architecturePlugin struct {
	Name         string   `json:"name"`
	Dependencies []string `json:"dependencies,omitempty"`
	Host         bool     `json:"host,omitempty"`
	Starts       bool     `json:"starts,omitempty"`
	Stops        bool     `json:"stops,omitempty"`
}

// architectureResource is one resource and the plugin that owns it.
type architectureResource struct {
	Type  string `json:"type"`
	Owner string `json:"owner"`
}

// architecturePort is one plugin's declaration of a Port: the interface it is
// built on, the Port type, whether it collects any number of Adapters or
// requires exactly one, and the Adapter types bound to it, in plugin order.
type architecturePort struct {
	Interface    string   `json:"interface"`
	Port         string   `json:"port"`
	Collects     bool     `json:"collects,omitempty"`
	Contributors []string `json:"contributors,omitempty"`
}

// architectureCommand is one command, its owner, and the resolved lock set its
// handler ends up holding once the commands it uses are folded in.
type architectureCommand struct {
	Type   string   `json:"type"`
	Owner  string   `json:"owner"`
	Reads  []string `json:"reads,omitempty"`
	Writes []string `json:"writes,omitempty"`
	Uses   []string `json:"uses,omitempty"`
	// SelfExclusive says the handler never runs concurrently with itself, so two
	// simultaneous calls that reach it queue rather than overlap. It names no
	// resource and so raises no pair in the contention report, which is why an
	// Agent wondering why its parallel calls serialised has to read it here.
	SelfExclusive bool `json:"selfExclusive,omitempty"`
}

// architectureSubscription is one subscription, its place in its event's
// dependency graph, and the same resolved lock set.
type architectureSubscription struct {
	Event     string   `json:"event"`
	Type      string   `json:"type"`
	Owner     string   `json:"owner"`
	Phase     string   `json:"phase"`
	DependsOn []string `json:"dependsOn,omitempty"`
	Reads     []string `json:"reads,omitempty"`
	Writes    []string `json:"writes,omitempty"`
	Uses      []string `json:"uses,omitempty"`
	// SelfExclusive says the same thing architectureCommand's does.
	SelfExclusive bool `json:"selfExclusive,omitempty"`
}

// architecture is the one capability body that dispatches nothing. It reads
// registry state that is immutable after finalization and returns a detached
// value, which is the narrow exception the capability-body rule names: no
// handle, no lock, no tick and no scheduler.
func architecture(k kernel.Executioner, request architectureRequest) (architectureResponse, error) {
	document := describe(k.Describe())
	if request.Path == "" {
		return document, nil
	}
	if err := checkPath(request.Path); err != nil {
		return architectureResponse{}, err
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return architectureResponse{}, err
	}
	if err := os.MkdirAll(filepath.Dir(request.Path), 0o755); err != nil {
		return architectureResponse{}, mcp.Unavailable{
			Reason: fmt.Sprintf("cannot create the directory for %s: %v", request.Path, err),
		}
	}
	if err := os.WriteFile(request.Path, encoded, 0o644); err != nil {
		return architectureResponse{}, mcp.Unavailable{
			Reason: fmt.Sprintf("cannot write %s: %v", request.Path, err),
		}
	}
	return architectureResponse{Path: request.Path}, nil
}

// checkPath applies the delivery contract every capability that produces a
// document follows. The checks happen before anything is written, so a typo
// costs microseconds.
func checkPath(path string) error {
	if !filepath.IsAbs(path) {
		return mcp.Unavailable{Reason: fmt.Sprintf(
			"the path %s is relative — pass an absolute one, because a relative path resolves "+
				"against the game's working directory rather than yours", path)}
	}
	if !strings.EqualFold(filepath.Ext(path), ".json") {
		return mcp.Unavailable{Reason: fmt.Sprintf(
			"the path %s does not end in .json, and this tool writes JSON", path)}
	}
	return nil
}

// describe flattens the kernel's description. A reflect.Type renders as
// kernel.TypeName renders it — gfx.RenderEvent, package-qualified by short name,
// and canvas.OpQueue rather than types.OpQueue for a type declared in
// canvas's internal/types package — because that is the form appearing in the source
// the agent greps next. The fully-qualified spelling is unambiguous but
// unsearchable.
func describe(description kernel.ArchitectureDescription) architectureResponse {
	document := architectureResponse{
		Plugins:       make([]architecturePlugin, 0, len(description.Plugins)),
		Resources:     make([]architectureResource, 0, len(description.Resources)),
		Ports:         make([]architecturePort, 0, len(description.Ports)),
		Commands:      make([]architectureCommand, 0, len(description.Commands)),
		Subscriptions: make([]architectureSubscription, 0, len(description.Subscriptions)),
	}
	for _, plugin := range description.Plugins {
		dependencies := make([]string, 0, len(plugin.Dependencies))
		for _, dependency := range plugin.Dependencies {
			dependencies = append(dependencies, string(dependency))
		}
		document.Plugins = append(document.Plugins, architecturePlugin{
			Name: string(plugin.Name), Dependencies: dependencies,
			Host: plugin.Host, Starts: plugin.Starts, Stops: plugin.Stops,
		})
	}
	for _, resource := range description.Resources {
		document.Resources = append(document.Resources, architectureResource{
			Type: kernel.TypeName(resource.Type), Owner: string(resource.Owner),
		})
	}
	for _, port := range description.Ports {
		contributors := make([]string, 0, len(port.Adapters))
		for _, adapter := range port.Adapters {
			contributors = append(contributors, kernel.TypeName(adapter.Type))
		}
		document.Ports = append(document.Ports, architecturePort{
			Interface: kernel.TypeName(port.Interface), Port: kernel.TypeName(port.Type),
			Collects: port.Collects, Contributors: contributors,
		})
	}
	for _, command := range description.Commands {
		document.Commands = append(document.Commands, architectureCommand{
			Type: kernel.TypeName(command.Type), Owner: string(command.Owner),
			Reads: typeNames(command.Reads), Writes: typeNames(command.Writes),
			Uses: typeNames(command.Uses), SelfExclusive: command.SelfExclusive,
		})
	}
	for _, subscription := range description.Subscriptions {
		document.Subscriptions = append(document.Subscriptions, architectureSubscription{
			Event: kernel.TypeName(subscription.Event), Type: kernel.TypeName(subscription.Type),
			Owner: string(subscription.Owner), Phase: subscription.Phase,
			DependsOn: typeNames(subscription.DependsOn),
			Reads:     typeNames(subscription.Reads), Writes: typeNames(subscription.Writes),
			Uses: typeNames(subscription.Uses), SelfExclusive: subscription.SelfExclusive,
		})
	}
	document.Contention = describeContention(description.Contention)
	return document
}

// describeContention carries the kernel's report across as it ranks it: no
// re-sort and no filter. Resources and phases go whole; handler pairs are cut
// where kernel.Dump cuts them and the rest counted. A handler renders by its
// identity type alone, which already joins to commands and subscriptions, where
// its kind, owner and event are listed.
func describeContention(contention kernel.ContentionDescription) *architectureContention {
	report := &architectureContention{
		// The test kernel.Dump uses to print "none".
		Contended: len(contention.Resources) > 0 || len(contention.Phases) > 0,
	}
	for _, resource := range contention.Resources {
		report.Resources = append(report.Resources, architectureResourceContention{
			Type: kernel.TypeName(resource.Type), Owner: string(resource.Owner),
			Conflicts: resource.Conflicts, Writers: handlerNames(resource.Writers),
			Readers: len(resource.Readers),
		})
	}
	for _, phase := range contention.Phases {
		report.Phases = append(report.Phases, architecturePhaseContention{
			Event: kernel.TypeName(phase.Event), Phase: phase.Phase,
			Members: phase.Members, Conflicts: phase.Conflicts,
			SingleThreaded: phase.SingleThreaded, WidestLocks: handlerNames(phase.WidestLocks),
		})
	}
	listed := min(len(contention.Handlers), architectureHandlerPairs)
	for _, pair := range contention.Handlers[:listed] {
		report.HandlerPairs = append(report.HandlerPairs, architectureHandlerPair{
			A: kernel.TypeName(pair.A.Type), B: kernel.TypeName(pair.B.Type),
			Resources: typeNames(pair.Resources),
		})
	}
	report.HandlerPairsOmitted = len(contention.Handlers) - listed
	return report
}

func handlerNames(handlers []kernel.HandlerRef) []string {
	names := make([]string, 0, len(handlers))
	for _, handler := range handlers {
		names = append(names, kernel.TypeName(handler.Type))
	}
	return names
}

func typeNames(types []reflect.Type) []string {
	names := make([]string, 0, len(types))
	for _, value := range types {
		names = append(names, kernel.TypeName(value))
	}
	return names
}
