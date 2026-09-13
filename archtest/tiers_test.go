// Package archtest holds the tier test: the import rules between plugin kinds
// that .github/instructions/architecture.instructions.md describes, checked over
// every cog-internal import edge instead of left to prose.
package archtest

import (
	"cmp"
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
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
	ruleImpl      = "nothing in cog imports an …impl package, except from _test.go files"
	ruleExtension = "nothing in cog imports an extensions/* directory that is not a Port, except from _test.go files"
	rulePlugin    = "contract roots and slots/* declare no type that implements kernel.Plugin"
	ruleNoTier    = "every package in cog belongs to a tier in architecture.instructions.md"

	ruleKernel    = "kernel imports nothing else in cog"
	ruleLib       = "libs/* import only libs and kernel"
	ruleSlot      = "slots/* import only libs, kernel, slots/* and contract roots"
	ruleRoot      = "a contract root imports only libs, kernel, slots/*, other contract roots and its own internal/…"
	ruleInternal  = "internal/… imports only libs, kernel, slots/* and other contract roots"
	ruleImplTable = "…impl imports only what its contract root may, plus that root"
	ruleOther     = "an extensions/* directory that is not a Port imports only libs, kernel, slots/* and contract roots"
)

type tier int

const (
	tierNone tier = iota
	tierExempt
	tierKernel
	tierLib
	tierSlot
	tierRoot     // bundles/X, or extensions/P when P has a Pimpl child
	tierInternal // bundles/X/internal/…, extensions/P/internal/…
	tierImpl     // bundles/X/Ximpl, extensions/P/Pimpl
	tierOther    // any other extensions/* directory: wgpu, diskfs, jsfs
)

// place is a package's tier. For internal/… and …impl, root is the
// module-relative path of the contract root they belong to.
type place struct {
	path string
	tier tier
	root string
}

// classify places one module-relative package path. present reports whether a
// module-relative package exists, which is how a Port is told from any other
// extension: by its …impl child.
func classify(path string, present func(string) bool) place {
	parts := strings.Split(path, "/")
	at := func(t tier, root string) place { return place{path: path, tier: t, root: root} }
	switch {
	case path == "archtest", path == "docs/research", strings.HasPrefix(path, "docs/research/"):
		return at(tierExempt, "")
	case path == "kernel":
		return at(tierKernel, "")
	case parts[0] == "libs" && len(parts) >= 2:
		return at(tierLib, "")
	case parts[0] == "slots" && len(parts) >= 2:
		return at(tierSlot, "")
	case parts[0] == "bundles" && len(parts) >= 2:
		return nested(path, parts)
	case parts[0] == "extensions" && len(parts) >= 2:
		root := "extensions/" + parts[1]
		if !present(root + "/" + parts[1] + "impl") {
			if len(parts) == 2 {
				return at(tierOther, "")
			}
			return at(tierNone, "")
		}
		return nested(path, parts)
	}
	return at(tierNone, "")
}

// nested places a path under a contract root: the root itself, its …impl, or
// its internal/… tree.
func nested(path string, parts []string) place {
	root := parts[0] + "/" + parts[1]
	switch {
	case len(parts) == 2:
		return place{path: path, tier: tierRoot}
	case len(parts) == 3 && parts[2] == parts[1]+"impl":
		return place{path: path, tier: tierImpl, root: root}
	case parts[2] == "internal":
		return place{path: path, tier: tierInternal, root: root}
	}
	return place{path: path, tier: tierNone}
}

// allowed reports whether from may import to, and if not, the rule it breaks.
// A _test.go file may additionally import any …impl and any extension that is
// not a Port; every other rule holds for tests too.
func allowed(from, to place, testFile bool) (bool, string) {
	if from.tier == tierExempt {
		return true, ""
	}
	switch to.tier {
	case tierImpl:
		if testFile {
			return true, ""
		}
		return false, ruleImpl
	case tierOther:
		if testFile {
			return true, ""
		}
		return false, ruleExtension
	}
	base := func(t tier) bool { return t == tierLib || t == tierKernel || t == tierSlot }
	switch from.tier {
	case tierKernel:
		return false, ruleKernel
	case tierLib:
		return to.tier == tierLib || to.tier == tierKernel, ruleLib
	case tierSlot:
		return base(to.tier) || to.tier == tierRoot, ruleSlot
	case tierRoot:
		return base(to.tier) || to.tier == tierRoot ||
			to.tier == tierInternal && to.root == from.path, ruleRoot
	case tierInternal:
		return base(to.tier) || to.tier == tierRoot && to.path != from.root, ruleInternal
	case tierImpl:
		return base(to.tier) || to.tier == tierRoot ||
			to.tier == tierInternal && to.root == from.root, ruleImplTable
	case tierOther:
		return base(to.tier) || to.tier == tierRoot, ruleOther
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
	module := loaded[0].Module
	relative := func(importPath string) (string, bool) {
		rest, ok := strings.CutPrefix(importPath, module.Path+"/")
		return rest, ok
	}
	within := func(file string) string {
		rel, err := filepath.Rel(module.Dir, file)
		if err != nil {
			return file
		}
		return filepath.ToSlash(rel)
	}

	present := map[string]bool{}
	for _, pkg := range loaded {
		if rel, ok := relative(pkg.PkgPath); ok {
			present[rel] = true
		}
	}
	has := func(path string) bool { return present[path] }

	var violations []violation
	for _, pkg := range loaded {
		rel, ok := relative(pkg.PkgPath)
		if !ok {
			continue
		}
		from := classify(rel, has)
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
		edges, err := importViolations(pkg.Dir, from, relative, has, within)
		if err != nil {
			return nil, err
		}
		violations = append(violations, edges...)
		if from.tier == tierRoot || from.tier == tierSlot {
			violations = append(violations, pluginViolations(pkg, rel, within)...)
		}
	}
	slices.SortFunc(violations, func(a, b violation) int {
		return cmp.Or(strings.Compare(a.file, b.file), cmp.Compare(a.line, b.line), strings.Compare(a.key, b.key))
	})
	return violations, nil
}

// importViolations parses every Go file in a package directory, whatever its
// build constraints or whether it is a test, so an edge that only one platform
// compiles is still checked.
func importViolations(
	dir string,
	from place,
	relative func(string) (string, bool),
	present func(string) bool,
	within func(string) string,
) ([]violation, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	var violations []violation
	fset := token.NewFileSet()
	for _, file := range files {
		if base := filepath.Base(file); strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".") {
			continue
		}
		syntax, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", file, err)
		}
		testFile := strings.HasSuffix(file, "_test.go")
		for _, spec := range syntax.Imports {
			importPath := strings.Trim(spec.Path.Value, "`\"")
			rel, ok := relative(importPath)
			if !ok {
				continue // std and third-party are not checked
			}
			if ok, rule := allowed(from, classify(rel, present), testFile); !ok {
				violations = append(violations, violation{
					file: within(file),
					line: fset.Position(spec.Pos()).Line,
					key:  from.path + " imports " + rel,
					rule: rule,
				})
			}
		}
	}
	return violations, nil
}

// pluginViolations finds every type a package declares that has the three
// methods of kernel.Plugin, on the value or on the pointer. Acting on a
// *kernel.Registrar the root is handed is not declaring a Plugin: functions
// that take one (ecs.RegisterComponent, ecs.ToHandler) and types with fewer
// than all three methods pass.
func pluginViolations(pkg *packages.Package, rel string, within func(string) string) []violation {
	var violations []violation
	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		object, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || types.IsInterface(object.Type()) {
			continue
		}
		methods := types.NewMethodSet(types.NewPointer(object.Type()))
		missing := func(method string) bool { return methods.Lookup(nil, method) == nil }
		if !slices.ContainsFunc([]string{"Name", "Dependencies", "Register"}, missing) {
			at := pkg.Fset.Position(object.Pos())
			violations = append(violations, violation{
				file: within(at.Filename),
				line: at.Line,
				key:  rel + " declares " + name,
				rule: rulePlugin,
			})
		}
	}
	return violations
}

func TestTiers_CogKeepsItsImportRules(t *testing.T) {
	requireGo(t)
	for _, v := range check(t, "..") {
		t.Errorf("%s", v)
	}
}
