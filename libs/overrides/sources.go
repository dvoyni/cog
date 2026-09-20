package overrides

import (
	"fmt"
	"strings"

	"github.com/dvoyni/cog/kernel"
)

// How each source marks an override as ours.
const (
	argPrefix = "cog."
	envPrefix = "COG_"
)

// parseArgs takes the overrides out of an argv and returns everything else in
// the order it arrived, argv[0] included. Splitting is stateless - the value
// is always attached with =, never the next element - because the argv it
// walks also holds the game's own flags, and deciding whether the element
// after a flag belongs to it would mean knowing every flag in the program.
func parseArgs(args []string) (Overlay, []string, error) {
	if len(args) == 0 {
		return nil, nil, nil
	}
	var overlay Overlay
	remaining := make([]string, 1, len(args))
	remaining[0] = args[0]
	for _, arg := range args[1:] {
		body, ok := strings.CutPrefix(arg, "--")
		if !ok {
			body, ok = strings.CutPrefix(arg, "-")
		}
		if !ok || !strings.HasPrefix(body, argPrefix) {
			remaining = append(remaining, arg)
			continue
		}
		key, value, hasValue := strings.Cut(strings.TrimPrefix(body, argPrefix), "=")
		assignment, err := assign(key, value, hasValue, arg)
		if err != nil {
			return nil, nil, err
		}
		overlay = append(overlay, assignment)
	}
	return overlay, remaining, nil
}

// parseEnviron takes the overrides out of an environment. A variable's plugin
// segment is resolved against the plugins actually present rather than by
// splitting on the first underscore, so a plugin whose own name holds one
// still works.
func parseEnviron(environ []string, plugins []kernel.PluginName) (Overlay, error) {
	var overlay Overlay
	for _, entry := range environ {
		name, value, _ := strings.Cut(entry, "=")
		name = strings.ToUpper(name)
		if name == cogEnvKey {
			// The browser's packed key, read from localStorage and never a
			// field spelling. A desktop that has it set is not making a typo.
			continue
		}
		rest, ok := strings.CutPrefix(name, envPrefix)
		if !ok {
			continue
		}
		plugin, field, ok := longestPlugin(rest, plugins)
		if !ok {
			return nil, fmt.Errorf("%s: the engine configuration has no plugin this variable could name", name)
		}
		overlay = append(overlay, Assignment{Plugin: plugin, Field: field, Value: value, Origin: name})
	}
	return overlay, nil
}

// parseCogEnv takes the overrides out of the browser's packed string. Each
// segment carries the cog. prefix the command line spells, so a line that was
// tried in a terminal pastes in unchanged and a segment missing it is a
// mistake rather than something to drop.
func parseCogEnv(packed string) (Overlay, error) {
	var overlay Overlay
	for _, segment := range strings.Split(packed, ";") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		body, ok := strings.CutPrefix(segment, argPrefix)
		if !ok {
			return nil, fmt.Errorf("%s: an override in %s is spelled cog.<plugin>.<Field>=<value>",
				segment, cogEnvKey)
		}
		key, value, hasValue := strings.Cut(body, "=")
		assignment, err := assign(strings.TrimSpace(key), strings.TrimSpace(value), hasValue, segment)
		if err != nil {
			return nil, err
		}
		overlay = append(overlay, assignment)
	}
	return overlay, nil
}

// assign splits a <plugin>.<Field> key. The split is at the last dot, so a
// plugin name holding one is read as the plugin it is rather than as a path
// into a field.
func assign(key, value string, hasValue bool, origin string) (Assignment, error) {
	dot := strings.LastIndex(key, ".")
	if dot <= 0 || dot == len(key)-1 {
		return Assignment{}, fmt.Errorf("%s: an override is spelled <plugin>.<Field>=<value>", origin)
	}
	return Assignment{
		Plugin: kernel.PluginName(key[:dot]),
		Field:  key[dot+1:],
		Value:  value,
		Bare:   !hasValue,
		Origin: origin,
	}, nil
}

// longestPlugin splits an upper-cased variable tail into the plugin that
// begins it and the field that follows. The longest name wins, so a "fix" and
// a "fix_it" in the same engine each get their own variables.
func longestPlugin(
	tail string, plugins []kernel.PluginName,
) (plugin kernel.PluginName, field string, ok bool) {
	for _, candidate := range plugins {
		prefix := strings.ToUpper(string(candidate)) + "_"
		if !strings.HasPrefix(tail, prefix) || len(candidate) <= len(plugin) {
			continue
		}
		plugin, field, ok = candidate, tail[len(prefix):], true
	}
	return plugin, field, ok
}
