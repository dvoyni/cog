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
	"order, who owns which command, event and resource, which plugins contributed the Adapters " +
	"each Port is bound to, the subscription dependency graph, and — " +
	"the part you cannot get by reading source — the full set of resources each handler ends up " +
	"locking once the commands it declares are folded in. Use it when you need to know what a " +
	"dispatch really touches, or why one handler waits for another. Commands are listed so you can " +
	"see who owns behaviour; you cannot call them from here, and there is no tool that takes a " +
	"command name. Pass `path` to write the JSON to a file instead of returning it inline."

// architectureRequest asks for the finalized architecture.
type architectureRequest struct {
	// Path, when given, receives the JSON instead of the agent receiving it
	// inline. It is absolute because a relative path would silently resolve
	// against the game's working directory, which need not be the agent's.
	Path string `json:"path,omitempty" jsonschema:"absolute path of a .json file to write the description to instead of returning it inline; parent directories are created and an existing file is overwritten"`
}

// architectureResponse is the finalized architecture as five flat arrays, or
// just the path when one was given. There is no index: the type string is the
// address, and uses, dependsOn, reads and writes are all joins on it.
type architectureResponse struct {
	// Path is set, and the arrays empty, when the description was written to a
	// file instead.
	Path          string                     `json:"path,omitempty"`
	Plugins       []architecturePlugin       `json:"plugins,omitempty"`
	Resources     []architectureResource     `json:"resources,omitempty"`
	Ports         []architecturePort         `json:"ports,omitempty"`
	Commands      []architectureCommand      `json:"commands,omitempty"`
	Subscriptions []architectureSubscription `json:"subscriptions,omitempty"`
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
	return document
}

func typeNames(types []reflect.Type) []string {
	names := make([]string, 0, len(types))
	for _, value := range types {
		names = append(names, kernel.TypeName(value))
	}
	return names
}
