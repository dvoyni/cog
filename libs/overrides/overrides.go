// Package overrides names a cog engine's configuration from outside the
// binary, so a run can be re-pointed without editing the composition root and
// rebuilding.
//
// It is a tool for developers and for automation. Player-facing settings are
// not this: those persist, are validated and round-trip, which is storage's
// values file.
//
// A composition root gains one line, after every contributor has added its
// entry and before the game parses its own flags:
//
//	config := map[kernel.PluginName]any{ /* ... */ }
//	permanentfs.Configure(config)
//	config = overrides.Inject(config)
//	flag.Parse()
//	engine := kernel.New(config).WithPlugins(plugins...)
//
// The order matters twice. An override naming a plugin that is not in the map
// is fatal, so every contributor must have run first; and Inject takes its own
// arguments out of os.Args, so it must run before a flag.Parse that would
// otherwise die on them.
//
// Those two together say that a program parsing its own flags builds its
// config map first and calls flag.Parse afterwards, which is the one shape
// this imposes: a flag's value cannot decide what goes in the config map,
// because the map has to exist before the flags are read. Declaring the flags
// early and reading them late is enough, and is what
// cog-examples/cmd/ecs/physics2dtable does with its -seed.
//
// # Spelling
//
// On the command line, --cog.<plugin>.<Field>=<value>, with a single dash
// accepted equally. In the environment, COG_<PLUGIN>_<FIELD>=<value>: the
// plugin segment resolves by longest match against the plugins actually in the
// map, and the field is matched case-insensitively with its underscores
// ignored, so COG_CANVAS_MAX_ATLAS_BYTES and COG_CANVAS_MAXATLASBYTES both
// reach MaxAtlasBytes.
//
// On GOOS=js there is neither, so a page sets one localStorage key, COG_ENV,
// holding the same assignments packed into a string:
//
//	cog.gogpu.Width=1600;cog.gogpu.Height=800
//
// The cog. prefix is required there, so a line tested on the command line
// pastes in unchanged. Whitespace around each ; and each = is trimmed and
// empty segments are ignored. There is no quoting: a value containing a
// semicolon, or one whose leading or trailing spaces are meaningful, cannot be
// written in COG_ENV. COG_ENV is read from localStorage on the browser and
// nowhere else - a desktop has both of the other two sources.
//
// The sources stack lowest first: the config built in Go, then COG_ENV, then
// the environment, then the command line. COG_ENV is beneath the other two
// because it is the sticky one - set once in devtools, it survives every
// reload and will be forgotten - and what is typed for one run has to be able
// to beat it.
//
// # What can be set
//
// An exported, top-level field whose type is parsable from text: string, bool,
// the signed and unsigned integer kinds, the floats, and time.Duration. Not a
// nested struct, a slice or a map, which is what keeps a field holding a live
// value - storage.Config.ReadMounts carries fs.FS - out of reach.
//
// A key with no value means true for a bool field and is an error for any
// other type. An empty value sets the empty string. A key given twice keeps
// the last.
//
// # When an override misses
//
// Six ways: the plugin is not in the map, the field does not exist, the
// field's type takes no text, the value does not parse, the entry is not a
// struct, or a bare key named something other than a bool. Inject treats all
// six as fatal, printing the override and the reason and exiting 2, the way
// the flag package does. A miss that passed quietly would leave a caller
// concluding the setting has no effect, which costs more than the whole
// mechanism saves.
//
// Apply is the same work without the exit, for a test harness or a capture
// runner that sets values programmatically and must not die.
package overrides

import (
	"fmt"
	"os"

	"github.com/dvoyni/cog/kernel"
)

// cogEnvKey is the localStorage key a page packs its overrides into, and the
// one COG_ name that is never a field spelling.
const cogEnvKey = "COG_ENV"

// Inject applies every override the process was given to config, and returns
// it. The map is written in place as well as returned, so a bare call works
// too.
//
// It reads os.Args and removes the arguments it consumed from it, so it has to
// run before anything else parses them, and it has to run after every
// contributor has put its entry in the map. Any override that cannot be
// applied is fatal: the reason goes to stderr and the process exits 2.
func Inject(config map[kernel.PluginName]any) map[kernel.PluginName]any {
	overlay, remaining, err := collect(readCogEnv(), os.Environ(), os.Args, names(config))
	if err == nil {
		os.Args = remaining
		config, err = Apply(overlay, config)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	return config
}

// Apply writes overlay into config and returns it, in place. It is Inject
// without the process exit: every miss comes back as an error naming the
// override and what was wrong with it.
//
// Nothing is written until every assignment has landed, so a config that
// failed is the config that was passed in.
func Apply(overlay Overlay, config map[kernel.PluginName]any) (map[kernel.PluginName]any, error) {
	staged := make(map[kernel.PluginName]any, len(overlay))
	for _, assignment := range overlay {
		entry, ok := staged[assignment.Plugin]
		if !ok {
			if entry, ok = config[assignment.Plugin]; !ok {
				return nil, fmt.Errorf("%s: the engine configuration has no plugin %q",
					assignment.Origin, assignment.Plugin)
			}
		}
		updated, err := set(entry, assignment)
		if err != nil {
			return nil, err
		}
		staged[assignment.Plugin] = updated
	}
	for name, entry := range staged {
		config[name] = entry
	}
	return config, nil
}

// collect stacks the three sources lowest first and hands back the arguments
// that were not ours, in their original order. It takes what it reads rather
// than reaching for the process, so every rule above is a table test.
func collect(
	cogEnv string, environ, args []string, plugins []kernel.PluginName,
) (Overlay, []string, error) {
	overlay, err := parseCogEnv(cogEnv)
	if err != nil {
		return nil, nil, err
	}
	fromEnviron, err := parseEnviron(environ, plugins)
	if err != nil {
		return nil, nil, err
	}
	fromArgs, remaining, err := parseArgs(args)
	if err != nil {
		return nil, nil, err
	}
	return append(append(overlay, fromEnviron...), fromArgs...), remaining, nil
}

// names is the plugins a config map holds, which the environment source needs
// to tell a plugin segment from the field that follows it.
func names(config map[kernel.PluginName]any) []kernel.PluginName {
	plugins := make([]kernel.PluginName, 0, len(config))
	for name := range config {
		plugins = append(plugins, name)
	}
	return plugins
}
