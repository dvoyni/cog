package config

import (
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
)

// fixtureMount stands in for storage.ReadMount: a struct carrying a live
// fs.FS, which is what makes the slice holding it unreachable from text.
type fixtureMount struct {
	Id string
	FS fs.FS
}

// fixtureConfig reproduces every field type a Config in cog holds. A Library
// may not import a plugin - kernel/archtest's "libs/* import only libs and
// kernel" holds for its tests too - so the coverage claim is made against the
// shapes rather than against gogpu.Config and storage.Config themselves.
type fixtureConfig struct {
	Title         string
	Width         int
	Height        int
	NoVSync       bool
	Fullscreen    bool
	Prewarm       uint32
	Seed          uint64
	Slop          float64
	Rate          float32
	Small         int8
	Step          time.Duration
	MaxAtlasBytes int
	ReadMounts    []fixtureMount
	secret        int
}

const fix kernel.PluginName = "fix"

func newConfig() map[kernel.PluginName]any {
	return map[kernel.PluginName]any{fix: fixtureConfig{Title: "kept", Width: 1280}}
}

func applied(t *testing.T, overlay Overlay, config map[kernel.PluginName]any) fixtureConfig {
	t.Helper()
	out, err := Apply(overlay, config)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	value, ok := out[fix].(fixtureConfig)
	if !ok {
		t.Fatalf("entry is %T, want fixtureConfig", out[fix])
	}
	return value
}

// Criterion 1: the sources stack lowest first, so the command line beats the
// environment and the environment beats COG_ENV.
func TestCollect_SourcesStackLowestFirst(t *testing.T) {
	for name, test := range map[string]struct {
		cogEnv  string
		environ []string
		args    []string
		want    int
	}{
		"nothing overrides": {want: 1280},
		"COG_ENV alone":     {cogEnv: "cog.fix.Width=1", want: 1},
		"environment beats it": {
			cogEnv:  "cog.fix.Width=1",
			environ: []string{"COG_FIX_WIDTH=2"},
			want:    2,
		},
		"command line beats both": {
			cogEnv:  "cog.fix.Width=1",
			environ: []string{"COG_FIX_WIDTH=2"},
			args:    []string{"demo", "--cog.fix.Width=3"},
			want:    3,
		},
		"command line beats COG_ENV": {
			cogEnv: "cog.fix.Width=1",
			args:   []string{"demo", "--cog.fix.Width=3"},
			want:   3,
		},
	} {
		t.Run(name, func(t *testing.T) {
			config := newConfig()
			overlay, _, err := collect(test.cogEnv, test.environ, test.args, names(config))
			if err != nil {
				t.Fatalf("collect: %v", err)
			}
			if got := applied(t, overlay, config).Width; got != test.want {
				t.Fatalf("Width = %d, want %d", got, test.want)
			}
		})
	}
}

// Criterion 2: one dash and two reach the same field.
func TestParseArgs_AcceptsOneDashAndTwo(t *testing.T) {
	for _, arg := range []string{"--cog.fix.Width=1600", "-cog.fix.Width=1600"} {
		overlay, _, err := parseArgs([]string{"demo", arg})
		if err != nil {
			t.Fatalf("%s: %v", arg, err)
		}
		if got := applied(t, overlay, newConfig()).Width; got != 1600 {
			t.Fatalf("%s: Width = %d, want 1600", arg, got)
		}
	}
}

// Criterion 3: an environment name is matched case-insensitively with its
// underscores ignored, and the plugin segment by longest match.
func TestParseEnviron_CanonicalisesTheFieldAndMatchesTheLongestPlugin(t *testing.T) {
	for _, key := range []string{"COG_FIX_MAX_ATLAS_BYTES", "COG_FIX_MAXATLASBYTES", "cog_fix_maxatlasbytes"} {
		overlay, err := parseEnviron([]string{key + "=4096"}, []kernel.PluginName{fix})
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if got := applied(t, overlay, newConfig()).MaxAtlasBytes; got != 4096 {
			t.Fatalf("%s: MaxAtlasBytes = %d, want 4096", key, got)
		}
	}

	// "fix" also prefixes "fix_it", so the longer name has to win.
	long := kernel.PluginName("fix_it")
	overlay, err := parseEnviron([]string{"COG_FIX_IT_WIDTH=7"}, []kernel.PluginName{fix, long})
	if err != nil {
		t.Fatalf("parseEnviron: %v", err)
	}
	if len(overlay) != 1 || overlay[0].Plugin != long || overlay[0].Field != "WIDTH" {
		t.Fatalf("overlay = %+v, want one assignment to %s's WIDTH", overlay, long)
	}
}

// An environment variable naming no plugin in the map is a typo, and a typo
// that does nothing is the failure this whole feature exists to avoid.
func TestParseEnviron_UnknownPluginFails(t *testing.T) {
	if _, err := parseEnviron([]string{"COG_NOSUCH_WIDTH=1"}, []kernel.PluginName{fix}); err == nil {
		t.Fatal("parseEnviron accepted a variable naming no plugin")
	}
	// Everything outside the prefix is somebody else's.
	if overlay, err := parseEnviron([]string{"PATH=/usr/bin", "HOME=/root"}, []kernel.PluginName{fix}); err != nil || len(overlay) != 0 {
		t.Fatalf("overlay, err = %v, %v, want none and no error", overlay, err)
	}
	// COG_ENV is the browser's own key and is never a field spelling.
	if overlay, err := parseEnviron([]string{"COG_ENV=cog.fix.Width=1"}, []kernel.PluginName{fix}); err != nil || len(overlay) != 0 {
		t.Fatalf("overlay, err = %v, %v, want COG_ENV ignored off the browser", overlay, err)
	}
}

// Criterion 4: the packed string's grammar.
func TestParseCogEnv_Grammar(t *testing.T) {
	overlay, err := parseCogEnv("cog.fix.Width=1600; cog.fix.Height=800;")
	if err != nil {
		t.Fatalf("parseCogEnv: %v", err)
	}
	if len(overlay) != 2 {
		t.Fatalf("overlay = %+v, want two assignments", overlay)
	}
	config := applied(t, overlay, newConfig())
	if config.Width != 1600 || config.Height != 800 {
		t.Fatalf("size = %dx%d, want 1600x800", config.Width, config.Height)
	}

	for _, empty := range []string{"", "   ", ";", " ; ; "} {
		if overlay, err := parseCogEnv(empty); err != nil || len(overlay) != 0 {
			t.Fatalf("parseCogEnv(%q) = %v, %v, want none and no error", empty, overlay, err)
		}
	}

	// Without the cog. prefix it is not an override, and saying so beats
	// dropping it.
	if _, err := parseCogEnv("fix.Width=1600"); err == nil {
		t.Fatal("parseCogEnv accepted an assignment without the cog. prefix")
	}
}

// Criterion 5: every type a Config field may have, and the duration trap.
func TestApply_SetsEveryTextParsableType(t *testing.T) {
	overlay := Overlay{
		{Plugin: fix, Field: "Title", Value: "hello"},
		{Plugin: fix, Field: "Width", Value: "640"},
		{Plugin: fix, Field: "Small", Value: "-8"},
		{Plugin: fix, Field: "NoVSync", Value: "true"},
		{Plugin: fix, Field: "Prewarm", Value: "2048"},
		{Plugin: fix, Field: "Seed", Value: "18446744073709551615"},
		{Plugin: fix, Field: "Slop", Value: "0.005"},
		{Plugin: fix, Field: "Rate", Value: "1.5"},
		{Plugin: fix, Field: "Step", Value: "33ms"},
	}
	got := applied(t, overlay, newConfig())
	want := fixtureConfig{
		Title: "hello", Width: 640, Small: -8, NoVSync: true, Prewarm: 2048,
		Seed: 18446744073709551615, Slop: 0.005, Rate: 1.5, Step: 33 * time.Millisecond,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config = %+v, want %+v", got, want)
	}
}

// A Duration is an int64 underneath, so a switch on kind alone reads 33 as 33
// nanoseconds and never parses 33ms at all. It is checked before the integers.
func TestApply_ADurationIsNotAnInteger(t *testing.T) {
	if _, err := Apply(Overlay{{Plugin: fix, Field: "Step", Value: "33"}}, newConfig()); err == nil {
		t.Fatal("Apply read a bare 33 as a duration")
	}
}

// Criterion 6: a bare key is true for a bool and an error for anything else.
func TestApply_BareIsBoolOnly(t *testing.T) {
	if got := applied(t, Overlay{{Plugin: fix, Field: "Fullscreen", Bare: true}}, newConfig()); !got.Fullscreen {
		t.Fatal("a bare bool did not set true")
	}
	if _, err := Apply(Overlay{{Plugin: fix, Field: "Width", Bare: true}}, newConfig()); err == nil {
		t.Fatal("Apply accepted a bare non-bool")
	}
}

// Criterion 7: an empty value is the empty string, and a key given twice takes
// the second.
func TestApply_EmptyValueAndLastWins(t *testing.T) {
	if got := applied(t, Overlay{{Plugin: fix, Field: "Title", Value: ""}}, newConfig()); got.Title != "" {
		t.Fatalf("Title = %q, want empty", got.Title)
	}
	overlay := Overlay{
		{Plugin: fix, Field: "Width", Value: "1"},
		{Plugin: fix, Field: "Width", Value: "2"},
	}
	if got := applied(t, overlay, newConfig()).Width; got != 2 {
		t.Fatalf("Width = %d, want the second value 2", got)
	}
}

// Criterion 8: the whole fatal taxonomy, each naming the override and why.
func TestApply_EveryMiss(t *testing.T) {
	for name, test := range map[string]struct {
		config     map[kernel.PluginName]any
		assignment Assignment
		mentions   string
	}{
		"unknown plugin": {
			config:     newConfig(),
			assignment: Assignment{Plugin: "nosuch", Field: "Width", Value: "1", Origin: "--cog.nosuch.Width=1"},
			mentions:   "nosuch",
		},
		"unknown field": {
			config:     newConfig(),
			assignment: Assignment{Plugin: fix, Field: "Nosuch", Value: "1", Origin: "--cog.fix.Nosuch=1"},
			mentions:   "Nosuch",
		},
		"unaddressable type": {
			config:     newConfig(),
			assignment: Assignment{Plugin: fix, Field: "ReadMounts", Value: "res", Origin: "--cog.fix.ReadMounts=res"},
			mentions:   "ReadMounts",
		},
		"unexported field": {
			config:     newConfig(),
			assignment: Assignment{Plugin: fix, Field: "secret", Value: "1", Origin: "--cog.fix.secret=1"},
			mentions:   "secret",
		},
		"unparsable value": {
			config:     newConfig(),
			assignment: Assignment{Plugin: fix, Field: "Width", Value: "banana", Origin: "--cog.fix.Width=banana"},
			mentions:   "banana",
		},
		"not a struct": {
			config:     map[kernel.PluginName]any{fix: 42},
			assignment: Assignment{Plugin: fix, Field: "Width", Value: "1", Origin: "--cog.fix.Width=1"},
			mentions:   "struct",
		},
		"a nil entry": {
			config:     map[kernel.PluginName]any{fix: nil},
			assignment: Assignment{Plugin: fix, Field: "Width", Value: "1", Origin: "--cog.fix.Width=1"},
			mentions:   "struct",
		},
		"bare non-bool": {
			config:     newConfig(),
			assignment: Assignment{Plugin: fix, Field: "Width", Bare: true, Origin: "--cog.fix.Width"},
			mentions:   "Width",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Apply(Overlay{test.assignment}, test.config)
			if err == nil {
				t.Fatalf("Apply accepted %+v", test.assignment)
			}
			if !strings.Contains(err.Error(), test.mentions) {
				t.Fatalf("error %q does not mention %q", err, test.mentions)
			}
			if test.assignment.Origin != "" && !strings.Contains(err.Error(), test.assignment.Origin) {
				t.Fatalf("error %q does not quote the override %q", err, test.assignment.Origin)
			}
		})
	}
}

// A miss leaves the map alone: Apply stages its writes and commits them only
// once every assignment has landed.
func TestApply_LeavesTheMapAloneOnAMiss(t *testing.T) {
	config := newConfig()
	overlay := Overlay{
		{Plugin: fix, Field: "Width", Value: "640"},
		{Plugin: fix, Field: "Nosuch", Value: "1"},
	}
	if _, err := Apply(overlay, config); err == nil {
		t.Fatal("Apply accepted an unknown field")
	}
	if got := config[fix].(fixtureConfig).Width; got != 1280 {
		t.Fatalf("Width = %d, want the untouched 1280", got)
	}
}

// Criterion 9: the overrides leave os.Args and everything else stays, in order.
func TestCollect_TakesItsOwnArgumentsOutOfArgv(t *testing.T) {
	args := []string{"demo", "--cog.fix.Width=640", "-seed", "7", "-cog.fix.Height=480", "level.map", "--verbose"}
	_, remaining, err := collect("", nil, args, []kernel.PluginName{fix})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	want := []string{"demo", "-seed", "7", "level.map", "--verbose"}
	if !slices.Equal(remaining, want) {
		t.Fatalf("remaining = %q, want %q", remaining, want)
	}
}

// Criterion 10: the map is written in place and handed back, untouched fields
// survive, and a pointer entry does not write through to the caller's struct.
func TestApply_WritesInPlaceAndCopiesAPointerEntry(t *testing.T) {
	config := newConfig()
	out, err := Apply(Overlay{{Plugin: fix, Field: "Width", Value: "640"}}, config)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if &out == &config {
		t.Fatal("Apply returned a copy of the map header rather than the map")
	}
	got := config[fix].(fixtureConfig)
	if got.Width != 640 {
		t.Fatalf("Width = %d, want the map written in place", got.Width)
	}
	if got.Title != "kept" {
		t.Fatalf("Title = %q, want the untouched %q", got.Title, "kept")
	}

	original := &fixtureConfig{Title: "kept", Width: 1280}
	pointed := map[kernel.PluginName]any{fix: original}
	if _, err := Apply(Overlay{{Plugin: fix, Field: "Width", Value: "640"}}, pointed); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if original.Width != 1280 {
		t.Fatalf("the caller's own struct was written to: Width = %d", original.Width)
	}
	if got := pointed[fix].(*fixtureConfig); got.Width != 640 || got == original {
		t.Fatalf("entry = %+v (same pointer: %v), want a new pointer at 640", got, got == original)
	}
}

// Criterion 11: every scalar a Config holds is addressable and the one field
// carrying a live fs.FS is not. Both halves are asserted over the shapes,
// since a Library may not import a plugin.
func TestApply_ReachesEveryScalarAndNoLiveValue(t *testing.T) {
	unreachable := map[string]bool{"ReadMounts": true, "secret": true}
	for _, field := range []string{
		"Title", "Width", "Height", "NoVSync", "Fullscreen", "Prewarm",
		"Seed", "Slop", "Rate", "Small", "Step", "MaxAtlasBytes",
	} {
		value := "1"
		switch field {
		case "Title":
			value = "x"
		case "NoVSync", "Fullscreen":
			value = "true"
		case "Step":
			value = "1s"
		}
		if _, err := Apply(Overlay{{Plugin: fix, Field: field, Value: value}}, newConfig()); err != nil {
			t.Fatalf("%s is not addressable: %v", field, err)
		}
	}
	for field := range unreachable {
		if _, err := Apply(Overlay{{Plugin: fix, Field: field, Value: "x"}}, newConfig()); err == nil {
			t.Fatalf("%s is addressable and should not be", field)
		}
	}
}

// A malformed override is a mistake worth a message, not something to drop.
func TestParseArgs_MalformedOverrideFails(t *testing.T) {
	for _, arg := range []string{"--cog.fix", "--cog.=1", "--cog..Width=1", "--cog."} {
		if _, _, err := parseArgs([]string{"demo", arg}); err == nil {
			t.Fatalf("parseArgs accepted %q", arg)
		}
	}
}
