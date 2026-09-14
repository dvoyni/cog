// Package archtest holds the tier test: the plugin kinds, package shapes and
// import rules that .github/instructions/architecture.instructions.md
// describes, checked over every cog package instead of left to prose. It also
// holds the tests of how kernel output names types, which compose every plugin
// and need a fixture internal package of their own.
package archtest

import (
	"cmp"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// The rules, as a failure names them. The instructions file states the same
// rules in prose; change both together.
const (
	ruleNoTier             = "every package in cog belongs to a tier in architecture.instructions.md"
	rulePlugin             = "a root declares no type that implements kernel.Plugin"
	ruleKernel             = "kernel imports nothing else in cog"
	ruleLib                = "libs/* import only libs and kernel"
	ruleReach              = "nothing in cog imports a constructor package or another plugin's internal/, except from _test.go files"
	ruleRootImports        = "a root imports only libs, kernel, other plugins' roots and its own internal/types"
	ruleTypes              = "internal/types imports only libs, kernel and other plugins' roots"
	ruleInternal           = "internal/ imports only libs, kernel, any root and its own internal/"
	ruleConstructor        = "a constructor package imports only kernel and its own internal/"
	ruleRootFiles          = "a Slot or Bundle root holds only doc.go, id.go, commands.go, events.go, resources.go, ports.go, adapters.go, types.go, config.go, err.go and utils.go, besides tests"
	ruleExtensionFiles     = "an Extension root holds only doc.go, id.go, config.go, adapters.go and err.go, besides tests"
	ruleForwarder          = "a root's exported functions are in utils.go, and every function there is exported and a single return of a call into the plugin's own internal/types, its parameters passed through in order"
	ruleInlineAnchor       = "a root's unexported functions are inline anchors: never called, returning nothing, and only calling argument-free methods on parameters typed as the root's aliases of its own internal/types"
	ruleConstructorExports = "a constructor package exports only New() kernel.Plugin"
	ruleUndeclaredAdapter  = "every ProvideAdapter in a plugin names an Adapter its root declares in adapters.go"
	ruleUnprovidedAdapter  = "every Adapter a root declares in adapters.go is provided by its plugin"
	ruleSlotPort           = "a slots/ root declares at least one required Port"
	ruleNoRequiredPort     = "a Bundle or Extension root declares no required Port"
	ruleExtensionAPI       = "an Extension root declares only Name, Config, its Adapters and Err… errors"
	ruleExtensionAdapter   = "an Extension root declares an Adapter for at least one required Port"
	ruleSlotForwarder      = "a Slot's forwarders name only its own types, predeclared types, the standard library, Libraries and the kernel"
)

// kind is a plugin's kind, which its top directory says.
type kind int

const (
	kindNone kind = iota
	kindSlot
	kindBundle
	kindExtension
)

var kindDirectories = map[string]kind{"slots": kindSlot, "bundles": kindBundle, "extensions": kindExtension}

type tier int

const (
	tierNone tier = iota
	tierExempt
	tierKernel
	tierLib

	// The packages of a plugin X under slots/, bundles/ or extensions/.
	tierRoot        // X
	tierTypes       // X/internal/types/…
	tierInternal    // X/internal/…, except internal/types
	tierConstructor // X/Xplugin
)

// place is a package's tier. plugin is the module-relative path of the plugin
// directory the package sits in, and kind that plugin's kind.
type place struct {
	path   string
	tier   tier
	plugin string
	kind   kind
}

// classify places one module-relative package path.
func classify(path string) place {
	parts := strings.Split(path, "/")
	at := func(t tier) place { return place{path: path, tier: t} }
	switch {
	case path == "kernel/archtest", strings.HasPrefix(path, "kernel/archtest/"),
		path == "docs/research", strings.HasPrefix(path, "docs/research/"):
		return at(tierExempt)
	case path == "kernel":
		return at(tierKernel)
	case parts[0] == "libs" && len(parts) >= 2:
		return at(tierLib)
	}
	pluginKind, ok := kindDirectories[parts[0]]
	if !ok || len(parts) < 2 {
		return at(tierNone)
	}
	placed := place{path: path, plugin: parts[0] + "/" + parts[1], kind: pluginKind}
	switch {
	case len(parts) == 2:
		placed.tier = tierRoot
	case len(parts) == 3 && parts[2] == parts[1]+"plugin":
		placed.tier = tierConstructor
	case parts[2] == "internal" && len(parts) >= 4 && parts[3] == "types":
		placed.tier = tierTypes
	case parts[2] == "internal":
		placed.tier = tierInternal
	}
	return placed
}

// allowed reports whether from may import to, and if not, the rule it breaks.
// A _test.go file of a plugin may additionally import any constructor package
// and any internal/; every other rule holds for tests too.
func allowed(from, to place, testFile bool) (bool, string) {
	base := to.tier == tierLib || to.tier == tierKernel
	switch from.tier {
	case tierExempt:
		return true, ""
	case tierKernel:
		return false, ruleKernel
	case tierLib:
		return base, ruleLib
	}
	inside := to.tier == tierTypes || to.tier == tierInternal
	if testFile && (inside || to.tier == tierConstructor) {
		return true, ""
	}
	if to.tier == tierConstructor || inside && to.plugin != from.plugin {
		return false, ruleReach
	}
	otherRoot := to.tier == tierRoot && to.plugin != from.plugin
	switch from.tier {
	case tierRoot:
		return base || otherRoot || to.tier == tierTypes, ruleRootImports
	case tierTypes:
		return base || otherRoot || to.tier == tierTypes, ruleTypes
	case tierInternal:
		return base || to.tier == tierRoot || inside, ruleInternal
	case tierConstructor:
		return to.tier == tierKernel || to.tier == tierInternal, ruleConstructor
	}
	return false, ruleNoTier
}

// violation is one broken rule. key names the package edge or declaration that
// breaks it, independent of the file.
type violation struct {
	file string // module-relative, with the line when there is one
	line int
	key  string
	rule string
}

func (v violation) String() string {
	if v.line == 0 {
		return fmt.Sprintf("%s: %s: %s", v.file, v.key, v.rule)
	}
	return fmt.Sprintf("%s:%d: %s: %s", v.file, v.line, v.key, v.rule)
}

func joinViolations(violations []violation) string {
	lines := make([]string, len(violations))
	for i, v := range violations {
		lines[i] = "\t" + v.String()
	}
	return strings.Join(lines, "\n")
}

func requireGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the go tool is needed to load packages")
	}
}

// check loads every package of the module rooted at dir and returns every
// violation in it, sorted.
func check(t *testing.T, dir string) []violation {
	t.Helper()
	violations, err := violationsIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	return violations
}

// module is what every check needs to know about the module being checked.
type module struct {
	path string // the module path
	dir  string
}

func (m module) relative(importPath string) (string, bool) {
	return strings.CutPrefix(importPath, m.path+"/")
}

func (m module) within(file string) string {
	rel, err := filepath.Rel(m.dir, file)
	if err != nil {
		return file
	}
	return filepath.ToSlash(rel)
}

func (m module) classify(rel string) place {
	return classify(rel)
}

func violationsIn(dir string) ([]violation, error) {
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedModule |
			packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
		Dir: dir,
		Env: append(os.Environ(), "GOWORK=off"),
	}
	loaded, err := packages.Load(config, "./...")
	if err != nil {
		return nil, fmt.Errorf("loading packages in %s: %w", dir, err)
	}
	if packages.PrintErrors(loaded) > 0 {
		return nil, fmt.Errorf("packages in %s do not load cleanly", dir)
	}
	if len(loaded) == 0 || loaded[0].Module == nil {
		return nil, fmt.Errorf("no module packages found in %s", dir)
	}
	m := module{path: loaded[0].Module.Path, dir: loaded[0].Module.Dir}

	var violations []violation
	adapters := map[string]*adapterUse{}
	for _, pkg := range loaded {
		rel, ok := m.relative(pkg.PkgPath)
		if !ok {
			continue
		}
		from := m.classify(rel)
		switch from.tier {
		case tierExempt:
			continue
		case tierNone:
			violations = append(violations, violation{
				file: rel,
				key:  rel + " matches no tier",
				rule: ruleNoTier,
			})
			continue
		}
		edges, err := importViolations(pkg.Dir, from, m)
		if err != nil {
			return nil, err
		}
		violations = append(violations, edges...)
		if from.plugin != "" {
			if adapters[from.plugin] == nil {
				adapters[from.plugin] = &adapterUse{}
			}
			if err := collectAdapters(pkg.Dir, from, m, adapters[from.plugin]); err != nil {
				return nil, err
			}
		}
		if from.tier == tierRoot {
			files, err := fileViolations(pkg.Dir, from, m)
			if err != nil {
				return nil, err
			}
			violations = append(violations, files...)
			violations = append(violations, forwarderViolations(pkg, from, m)...)
			violations = append(violations, portViolations(pkg, from, m)...)
			violations = append(violations, pluginViolations(pkg, rel, m)...)
		}
		if from.tier == tierConstructor {
			violations = append(violations, constructorExportViolations(pkg, rel, m)...)
		}
	}
	for plugin, use := range adapters {
		violations = append(violations, adapterViolations(plugin, use)...)
	}
	slices.SortFunc(violations, func(a, b violation) int {
		return cmp.Or(strings.Compare(a.file, b.file), cmp.Compare(a.line, b.line), strings.Compare(a.key, b.key))
	})
	return violations, nil
}

// importViolations parses every Go file in a package directory, whatever its
// build constraints or whether it is a test, so an edge that only one platform
// compiles is still checked.
func importViolations(dir string, from place, m module) ([]violation, error) {
	files, err := goFiles(dir)
	if err != nil {
		return nil, err
	}
	var violations []violation
	fset := token.NewFileSet()
	for _, file := range files {
		syntax, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", file, err)
		}
		testFile := strings.HasSuffix(file, "_test.go")
		for _, spec := range syntax.Imports {
			importPath := strings.Trim(spec.Path.Value, "`\"")
			rel, ok := m.relative(importPath)
			if !ok {
				continue // std and third-party are not checked
			}
			if ok, rule := allowed(from, m.classify(rel), testFile); !ok {
				violations = append(violations, violation{
					file: m.within(file),
					line: fset.Position(spec.Pos()).Line,
					key:  from.path + " imports " + rel,
					rule: rule,
				})
			}
		}
	}
	return violations, nil
}

// goFiles lists every Go file in a directory the go tool would consider for
// some build, tests included: every *.go file whose name does not start with _
// or a dot.
func goFiles(dir string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(files, func(file string) bool {
		base := filepath.Base(file)
		return strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".")
	}), nil
}

func TestTiers_CogKeepsItsRules(t *testing.T) {
	requireGo(t)
	for _, v := range check(t, "../..") {
		t.Errorf("%s", v)
	}
}
