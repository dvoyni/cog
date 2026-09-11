package mcpserver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/mcp"
)

// architectureName is the broker's one capability, rendered as the tool
// mcpserver_architecture: the prefix names who offers it, the name says what it
// is about.
const architectureName = "architecture"

// architectureDescription is prompt text, and it is reproduced in
// mcpserver/docs/specs/mcp.md so it is reviewed as prompt text rather than
// buried as a string literal.
const architectureDescription = "What this engine is actually composed of: the plugins in start " +
	"order, who owns which command, event and resource, the subscription dependency graph, and — " +
	"the part you cannot get by reading source — the full set of resources each handler ends up " +
	"locking once the commands it declares are folded in. Use it when you need to know what a " +
	"dispatch really touches, or why one handler waits for another. Commands are listed so you can " +
	"see who owns behaviour; you cannot call them from here, and there is no tool that takes a " +
	"command name. Pass `path` to write the JSON to a file instead of returning it inline."

// ArchitectureRequest asks for the finalized architecture.
type ArchitectureRequest struct {
	// Path, when given, receives the JSON instead of the agent receiving it
	// inline. It is absolute because a relative path would silently resolve
	// against the game's working directory, which need not be the agent's.
	Path string `json:"path,omitempty" jsonschema:"absolute path of a .json file to write the description to instead of returning it inline; parent directories are created and an existing file is overwritten"`
}

// ArchitectureResponse is the finalized architecture as four flat arrays, or
// just the path when one was given. There is no index: the type string is the
// address, and uses, dependsOn, reads and writes are all joins on it.
type ArchitectureResponse struct {
	// Path is set, and the arrays empty, when the description was written to a
	// file instead.
	Path          string                     `json:"path,omitempty"`
	Plugins       []ArchitecturePlugin       `json:"plugins,omitempty"`
	Resources     []ArchitectureResource     `json:"resources,omitempty"`
	Commands      []ArchitectureCommand      `json:"commands,omitempty"`
	Subscriptions []ArchitectureSubscription `json:"subscriptions,omitempty"`
}

// ArchitecturePlugin is one plugin in start order.
type ArchitecturePlugin struct {
	Name         string   `json:"name"`
	Dependencies []string `json:"dependencies,omitempty"`
	Host         bool     `json:"host,omitempty"`
	Starts       bool     `json:"starts,omitempty"`
	Stops        bool     `json:"stops,omitempty"`
}

// ArchitectureResource is one resource and the plugin that owns it.
type ArchitectureResource struct {
	Type  string `json:"type"`
	Owner string `json:"owner"`
}

// ArchitectureCommand is one command, its owner, and the resolved lock set its
// handler ends up holding once the commands it uses are folded in.
type ArchitectureCommand struct {
	Type   string   `json:"type"`
	Owner  string   `json:"owner"`
	Reads  []string `json:"reads,omitempty"`
	Writes []string `json:"writes,omitempty"`
	Uses   []string `json:"uses,omitempty"`
}

// ArchitectureSubscription is one subscription, its place in its event's
// dependency graph, and the same resolved lock set.
type ArchitectureSubscription struct {
	Event     string   `json:"event"`
	Type      string   `json:"type"`
	Owner     string   `json:"owner"`
	Phase     string   `json:"phase"`
	DependsOn []string `json:"dependsOn,omitempty"`
	Reads     []string `json:"reads,omitempty"`
	Writes    []string `json:"writes,omitempty"`
	Uses      []string `json:"uses,omitempty"`
}

// architecture is the one capability body that dispatches nothing. It reads
// registry state that is immutable after finalization and returns a detached
// value, which is the narrow exception the capability-body rule names: no
// handle, no lock, no tick and no scheduler.
func (p *Plugin) architecture(k kernel.Executioner, request ArchitectureRequest) (ArchitectureResponse, error) {
	document := describe(k.Describe())
	if request.Path == "" {
		return document, nil
	}
	if err := checkPath(request.Path); err != nil {
		return ArchitectureResponse{}, err
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return ArchitectureResponse{}, err
	}
	if err := os.MkdirAll(filepath.Dir(request.Path), 0o755); err != nil {
		return ArchitectureResponse{}, mcp.Unavailable{
			Reason: fmt.Sprintf("cannot create the directory for %s: %v", request.Path, err),
		}
	}
	if err := os.WriteFile(request.Path, encoded, 0o644); err != nil {
		return ArchitectureResponse{}, mcp.Unavailable{
			Reason: fmt.Sprintf("cannot write %s: %v", request.Path, err),
		}
	}
	return ArchitectureResponse{Path: request.Path}, nil
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
// Type.String() — gfx.RenderEvent, package-qualified by short name — because
// that is the form appearing in the source the agent greps next. The
// fully-qualified spelling is unambiguous but unsearchable, and where a short
// name ever collides, the owner disambiguates in the same record.
func describe(description kernel.ArchitectureDescription) ArchitectureResponse {
	document := ArchitectureResponse{
		Plugins:       make([]ArchitecturePlugin, 0, len(description.Plugins)),
		Resources:     make([]ArchitectureResource, 0, len(description.Resources)),
		Commands:      make([]ArchitectureCommand, 0, len(description.Commands)),
		Subscriptions: make([]ArchitectureSubscription, 0, len(description.Subscriptions)),
	}
	for _, plugin := range description.Plugins {
		dependencies := make([]string, 0, len(plugin.Dependencies))
		for _, dependency := range plugin.Dependencies {
			dependencies = append(dependencies, string(dependency))
		}
		document.Plugins = append(document.Plugins, ArchitecturePlugin{
			Name: string(plugin.Name), Dependencies: dependencies,
			Host: plugin.Host, Starts: plugin.Starts, Stops: plugin.Stops,
		})
	}
	for _, resource := range description.Resources {
		document.Resources = append(document.Resources, ArchitectureResource{
			Type: resource.Type.String(), Owner: string(resource.Owner),
		})
	}
	for _, command := range description.Commands {
		document.Commands = append(document.Commands, ArchitectureCommand{
			Type: command.Type.String(), Owner: string(command.Owner),
			Reads: typeNames(command.Reads), Writes: typeNames(command.Writes),
			Uses: typeNames(command.Uses),
		})
	}
	for _, subscription := range description.Subscriptions {
		document.Subscriptions = append(document.Subscriptions, ArchitectureSubscription{
			Event: subscription.Event.String(), Type: subscription.Type.String(),
			Owner: string(subscription.Owner), Phase: subscription.Phase,
			DependsOn: typeNames(subscription.DependsOn),
			Reads:     typeNames(subscription.Reads), Writes: typeNames(subscription.Writes),
			Uses: typeNames(subscription.Uses),
		})
	}
	return document
}

func typeNames(types []reflect.Type) []string {
	names := make([]string, 0, len(types))
	for _, value := range types {
		names = append(names, value.String())
	}
	return names
}
