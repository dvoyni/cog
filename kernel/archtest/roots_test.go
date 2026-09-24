package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// rootFiles is each kind's root allowlist: the only non-test Go files its root
// may hold.
var rootFiles = map[kind][]string{
	kindSlot:      declarationFiles,
	kindBundle:    declarationFiles,
	kindExtension: {"doc.go", "id.go", "config.go", "adapters.go", "err.go"},
}

var declarationFiles = []string{
	"doc.go", "id.go", "commands.go", "events.go", "resources.go", "ports.go",
	"adapters.go", "components.go", "types.go", "config.go", "err.go", "utils.go",
}

// fileViolations finds every non-test Go file in a root that its kind's
// allowlist does not name, whatever its build constraints.
func fileViolations(dir string, root place, m module) ([]violation, error) {
	files, err := goFiles(dir)
	if err != nil {
		return nil, err
	}
	rule := ruleRootFiles
	if root.kind == kindExtension {
		rule = ruleExtensionFiles
	}
	var violations []violation
	for _, file := range files {
		base := filepath.Base(file)
		if strings.HasSuffix(base, "_test.go") || slices.Contains(rootFiles[root.kind], base) {
			continue
		}
		violations = append(violations, violation{file: m.within(file), key: root.path + " holds " + base, rule: rule})
	}
	return violations, nil
}

// forwarderViolations finds every exported function a root declares outside
// utils.go, every unexported one outside utils.go that is not an inline anchor,
// and every function in utils.go that is not a pure forwarder. It reads the
// files the go tool loaded, so tests are not checked.
func forwarderViolations(pkg *packages.Package, root place, m module) []violation {
	typesPath := m.path + "/" + root.plugin + "/internal/types"
	target := forwardTarget(root, m)
	var violations []violation
	for _, file := range pkg.Syntax {
		inUtils := filepath.Base(pkg.Fset.File(file.Pos()).Name()) == "utils.go"
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			object := pkg.TypesInfo.Defs[function.Name]
			switch {
			case !inUtils && function.Recv == nil && function.Name.IsExported():
				violations = append(violations, declared(pkg, object, m,
					root.path+" declares "+function.Name.Name+" outside utils.go", ruleForwarder))
			case !inUtils && function.Recv == nil && !isInlineAnchor(function, pkg.TypesInfo, target):
				violations = append(violations, declared(pkg, object, m,
					root.path+" declares "+function.Name.Name+", which is not an inline anchor", ruleInlineAnchor))
			case inUtils && (function.Recv != nil || !isForwarder(function, pkg.TypesInfo, target)):
				violations = append(violations, declared(pkg, object, m,
					root.path+" declares "+function.Name.Name+" in utils.go, which is not a pure forwarder", ruleForwarder))
			case inUtils && root.kind == kindSlot:
				if foreign := foreignType(object.Type(), slotMayName(pkg.PkgPath, typesPath, m)); foreign != nil {
					violations = append(violations, declared(pkg, object, m,
						root.path+" forwards "+function.Name.Name+", whose signature names "+foreign.Pkg().Path()+"."+foreign.Name(),
						ruleSlotForwarder))
				}
			}
		}
	}
	return violations
}

// forwardTarget reports whether a package is one a root's forwarders and
// inline anchors may reach into: its own internal/types, and for an
// alias-index root its own internal/ as well.
func forwardTarget(root place, m module) func(path string) bool {
	typesPath := m.path + "/" + root.plugin + "/internal/types"
	internalPath := m.path + "/" + root.plugin + "/internal"
	return func(path string) bool {
		return path == typesPath || aliasIndexRoots[root.plugin] && path == internalPath
	}
}

// isInlineAnchor reports whether function is an inline anchor, the one piece
// of code a root may hold outside utils.go and Config's builders: unexported,
// never referenced, generic in nothing and returning nothing, with a body in
// which every statement calls an argument-free method on one of its
// parameters, discarding any results. Every parameter's type is spelled as a
// name the root declares as an alias of a type in its own internal/types.
//
// It exists for the compiler rather than for callers. Go inlines a method of a
// package the caller does not import only when a package it does import
// references that method, and a root of aliases and forwarders references none,
// so a root anchors the accessors its importers call per instance.
func isInlineAnchor(function *ast.FuncDecl, info *types.Info, target func(string) bool) bool {
	object, ok := info.Defs[function.Name].(*types.Func)
	if !ok || function.Name.IsExported() || function.Body == nil || len(function.Body.List) == 0 {
		return false
	}
	signature := object.Signature()
	if signature.TypeParams().Len() > 0 || signature.Results().Len() > 0 {
		return false
	}
	for _, used := range info.Uses {
		if used == object {
			return false
		}
	}
	parameters := map[types.Object]bool{}
	for _, field := range function.Type.Params.List {
		name, ok := field.Type.(*ast.Ident)
		if !ok {
			return false
		}
		alias, ok := info.Uses[name].(*types.TypeName)
		if !ok || !alias.IsAlias() || alias.Pkg() != object.Pkg() {
			return false
		}
		named, ok := types.Unalias(alias.Type()).(*types.Named)
		if !ok || named.Obj().Pkg() == nil || !target(named.Obj().Pkg().Path()) {
			return false
		}
		for _, parameter := range field.Names {
			parameters[info.Defs[parameter]] = true
		}
	}
	accessorCall := func(expression ast.Expr) bool {
		call, ok := expression.(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			return false
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		receiver, ok := selector.X.(*ast.Ident)
		if !ok || !parameters[info.Uses[receiver]] {
			return false
		}
		selection, ok := info.Selections[selector]
		return ok && selection.Kind() == types.MethodVal
	}
	for _, statement := range function.Body.List {
		switch statement := statement.(type) {
		case *ast.ExprStmt:
			if !accessorCall(statement.X) {
				return false
			}
		case *ast.AssignStmt:
			if statement.Tok != token.ASSIGN || len(statement.Rhs) != 1 || !accessorCall(statement.Rhs[0]) {
				return false
			}
			for _, left := range statement.Lhs {
				if blank, ok := left.(*ast.Ident); !ok || blank.Name != "_" {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

// slotMayName reports whether a Slot's forwarder may name a type declared in a
// package: the Slot's root, internal/ or internal/types, the standard library,
// a Library or the kernel.
func slotMayName(rootPath, typesPath string, m module) func(*types.Package) bool {
	return func(declaring *types.Package) bool {
		path := declaring.Path()
		first, _, _ := strings.Cut(path, "/")
		return path == rootPath || path == typesPath || strings.HasPrefix(path, typesPath+"/") ||
			path == strings.TrimSuffix(typesPath, "/types") ||
			path == m.path+"/kernel" || strings.HasPrefix(path, m.path+"/libs/") ||
			!strings.Contains(first, ".")
	}
}

// foreignType returns the first named type t spells whose package may does not
// accept, or nil. A named type's type arguments are walked and its underlying
// type is not, since naming a type is not naming what it is made of.
func foreignType(t types.Type, may func(*types.Package) bool) *types.TypeName {
	walkAll := func(ts ...types.Type) *types.TypeName {
		for _, each := range ts {
			if foreign := foreignType(each, may); foreign != nil {
				return foreign
			}
		}
		return nil
	}
	tuple := func(vars *types.Tuple) []types.Type {
		ts := make([]types.Type, vars.Len())
		for i := range vars.Len() {
			ts[i] = vars.At(i).Type()
		}
		return ts
	}
	switch t := t.(type) {
	case *types.Alias:
		return foreignType(types.Unalias(t), may)
	case *types.Named:
		if t.Obj().Pkg() != nil && !may(t.Obj().Pkg()) {
			return t.Obj()
		}
		var arguments []types.Type
		for i := range t.TypeArgs().Len() {
			arguments = append(arguments, t.TypeArgs().At(i))
		}
		return walkAll(arguments...)
	case *types.Pointer:
		return foreignType(t.Elem(), may)
	case *types.Slice:
		return foreignType(t.Elem(), may)
	case *types.Array:
		return foreignType(t.Elem(), may)
	case *types.Chan:
		return foreignType(t.Elem(), may)
	case *types.Map:
		return walkAll(t.Key(), t.Elem())
	case *types.Signature:
		return walkAll(append(tuple(t.Params()), tuple(t.Results())...)...)
	case *types.Struct:
		fields := make([]types.Type, t.NumFields())
		for i := range t.NumFields() {
			fields[i] = t.Field(i).Type()
		}
		return walkAll(fields...)
	case *types.Interface:
		var parts []types.Type
		for i := range t.NumEmbeddeds() {
			parts = append(parts, t.EmbeddedType(i))
		}
		for i := range t.NumExplicitMethods() {
			parts = append(parts, t.ExplicitMethod(i).Type())
		}
		return walkAll(parts...)
	}
	return nil // basic types and type parameters
}

// isForwarder reports whether function is exported and its body is a single
// call of a function in a package target accepts, returned when function has
// results, whose
// type arguments, if spelled, are function's type parameters and whose
// arguments are function's parameters, in order, spread when it is variadic.
func isForwarder(function *ast.FuncDecl, info *types.Info, target func(string) bool) bool {
	object, ok := info.Defs[function.Name].(*types.Func)
	if !ok || !function.Name.IsExported() || function.Body == nil || len(function.Body.List) != 1 {
		return false
	}
	signature := object.Signature()
	var call *ast.CallExpr
	switch statement := function.Body.List[0].(type) {
	case *ast.ReturnStmt:
		if signature.Results().Len() > 0 && len(statement.Results) == 1 {
			call, _ = statement.Results[0].(*ast.CallExpr)
		}
	case *ast.ExprStmt:
		if signature.Results().Len() == 0 {
			call, _ = statement.X.(*ast.CallExpr)
		}
	}
	if call == nil {
		return false
	}
	callee, typeArguments := call.Fun, []ast.Expr(nil)
	switch instance := callee.(type) {
	case *ast.IndexExpr:
		callee, typeArguments = instance.X, []ast.Expr{instance.Index}
	case *ast.IndexListExpr:
		callee, typeArguments = instance.X, instance.Indices
	}
	selector, ok := callee.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	if name, ok := info.Uses[qualifier].(*types.PkgName); !ok || !target(name.Imported().Path()) {
		return false
	}
	if _, ok := info.Uses[selector.Sel].(*types.Func); !ok {
		return false // a conversion, or a variable holding a function
	}
	passes := func(argument ast.Expr, want types.Object) bool {
		ident, ok := argument.(*ast.Ident)
		return ok && want != nil && info.Uses[ident] == want
	}
	if len(typeArguments) > 0 && len(typeArguments) != signature.TypeParams().Len() {
		return false
	}
	for i, argument := range typeArguments {
		if !passes(argument, signature.TypeParams().At(i).Obj()) {
			return false
		}
	}
	if len(call.Args) != signature.Params().Len() || call.Ellipsis.IsValid() != signature.Variadic() {
		return false
	}
	for i, argument := range call.Args {
		if !passes(argument, signature.Params().At(i)) {
			return false
		}
	}
	return true
}

// shape names the kernel shape a type is defined from, by the marker type the
// shape's function takes: "requiredPort", "collectedPort" or "adapterOf", with
// the type the function returns, which is a Port's interface or an Adapter's
// Port. Any other type has the empty shape.
func shape(t types.Type, m module) (string, types.Type) {
	if t == nil {
		return "", nil
	}
	signature, ok := t.Underlying().(*types.Signature)
	if !ok || signature.Params().Len() != 1 || signature.Results().Len() != 1 {
		return "", nil
	}
	marker, ok := types.Unalias(signature.Params().At(0).Type()).(*types.Named)
	if !ok || marker.Obj().Pkg() == nil || marker.Obj().Pkg().Path() != m.path+"/kernel" {
		return "", nil
	}
	return marker.Obj().Name(), signature.Results().At(0).Type()
}

// portViolations holds a root's declarations to its kind: a Slot declares a
// required Port and no other kind does, and an Extension declares only Name,
// Config, Err… errors and Adapters, at least one of them for a required Port.
// An Extension's functions are left to the forwarder rule.
func portViolations(pkg *packages.Package, root place, m module) []violation {
	var violations []violation
	scope := pkg.Types.Scope()
	requires, fills := false, false
	for _, name := range scope.Names() {
		object := scope.Lookup(name)
		kernelShape, returned := shape(object.Type(), m)
		_, isType := object.(*types.TypeName)
		if isType && kernelShape == "requiredPort" {
			requires = true
			if root.kind != kindSlot {
				violations = append(violations, declared(pkg, object, m,
					root.path+" declares the required Port "+name, ruleNoRequiredPort))
			}
		}
		if root.kind != kindExtension {
			continue
		}
		if portShape, _ := shape(returned, m); isType && kernelShape == "adapterOf" && portShape == "requiredPort" {
			fills = true
		}
		_, isConst := object.(*types.Const)
		_, isVar := object.(*types.Var)
		_, isFunc := object.(*types.Func)
		switch {
		case !object.Exported(), isFunc,
			name == "Name" && isConst,
			name == "Config" && isType,
			isType && kernelShape == "adapterOf",
			strings.HasPrefix(name, "Err") && (isType || isVar):
			continue
		}
		violations = append(violations, declared(pkg, object, m, root.path+" declares "+name, ruleExtensionAPI))
	}
	if root.kind == kindSlot && !requires {
		violations = append(violations, violation{file: root.path, key: root.path + " declares no required Port", rule: ruleSlotPort})
	}
	if root.kind == kindExtension && !fills {
		violations = append(violations, violation{file: root.path, key: root.path + " declares no Adapter for a required Port", rule: ruleExtensionAdapter})
	}
	return violations
}

// adapterUse is what one plugin's files say about its Adapters: the types its
// root's adapters.go declares and every ProvideAdapter call it makes.
type adapterUse struct {
	declared []adapterSite
	provided []adapterSite
}

// adapterSite is one Adapter declaration or ProvideAdapter call. name is the
// Adapter type's name when it is the plugin's own root's type, and empty
// otherwise; spelled is the type argument as written.
type adapterSite struct {
	name, spelled string
	file          string
	line          int
}

// collectAdapters reads a package of a plugin in the declaration-root shape
// into use: the type declarations of adapters.go when the package is the root,
// and every ProvideAdapter call in any non-test file, whatever its build
// constraints. It reads syntax rather than types so that a call only another
// platform builds still counts.
func collectAdapters(dir string, at place, m module, use *adapterUse) error {
	files, err := goFiles(dir)
	if err != nil {
		return err
	}
	rootPath := m.path + "/" + at.plugin
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		syntax, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", file, err)
		}
		site := func(name, spelled string, pos token.Pos) adapterSite {
			return adapterSite{name: name, spelled: spelled, file: m.within(file), line: fset.Position(pos).Line}
		}
		if at.tier == tierRoot && filepath.Base(file) == "adapters.go" {
			for _, decl := range syntax.Decls {
				if general, ok := decl.(*ast.GenDecl); ok && general.Tok == token.TYPE {
					for _, spec := range general.Specs {
						name := spec.(*ast.TypeSpec).Name
						use.declared = append(use.declared, site(name.Name, name.Name, name.Pos()))
					}
				}
			}
		}
		imports := map[string]string{}
		for _, spec := range syntax.Imports {
			path := strings.Trim(spec.Path.Value, "`\"")
			local := path[strings.LastIndex(path, "/")+1:]
			if spec.Name != nil {
				local = spec.Name.Name
			}
			imports[local] = path
		}
		ast.Inspect(syntax, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			var callee, adapter ast.Expr
			switch instance := call.Fun.(type) {
			case *ast.IndexExpr:
				callee, adapter = instance.X, instance.Index
			case *ast.IndexListExpr:
				callee, adapter = instance.X, instance.Indices[0]
			default:
				return true
			}
			if selector, ok := callee.(*ast.SelectorExpr); !ok || selector.Sel.Name != "ProvideAdapter" {
				return true
			}
			name := ""
			switch adapter := adapter.(type) {
			case *ast.SelectorExpr:
				if qualifier, ok := adapter.X.(*ast.Ident); ok && imports[qualifier.Name] == rootPath {
					name = adapter.Sel.Name
				}
			case *ast.Ident:
				// An alias-index plugin provides from its internal/, which
				// declares the Adapter under the name its root aliases.
				if at.tier == tierRoot || at.tier == tierInternal && aliasIndexRoots[at.plugin] {
					name = adapter.Name
				}
			}
			use.provided = append(use.provided, site(name, types.ExprString(adapter), call.Pos()))
			return true
		})
	}
	return nil
}

// adapterViolations finds every ProvideAdapter naming a type its plugin's
// adapters.go does not declare, and every type adapters.go declares that no
// ProvideAdapter of the plugin names.
func adapterViolations(plugin string, use *adapterUse) []violation {
	var violations []violation
	names := func(sites []adapterSite) []string {
		var all []string
		for _, s := range sites {
			all = append(all, s.name)
		}
		return all
	}
	declared, provided := names(use.declared), names(use.provided)
	for _, call := range use.provided {
		if call.name == "" || !slices.Contains(declared, call.name) {
			violations = append(violations, violation{
				file: call.file, line: call.line,
				key:  plugin + " provides " + call.spelled + ", which " + plugin + "/adapters.go does not declare",
				rule: ruleUndeclaredAdapter,
			})
		}
	}
	for _, declaration := range use.declared {
		if !slices.Contains(provided, declaration.name) {
			violations = append(violations, violation{
				file: declaration.file, line: declaration.line,
				key:  plugin + " declares " + declaration.name + " and never provides it",
				rule: ruleUnprovidedAdapter,
			})
		}
	}
	return violations
}

// pluginViolations finds every type a package declares that has the three
// methods of kernel.Plugin, on the value or on the pointer. Acting on a
// *kernel.Registrar the root is handed is not declaring a Plugin: functions
// that take one (ecs.RegisterComponent, ecs.ToHandler) and types with fewer
// than all three methods pass.
func pluginViolations(pkg *packages.Package, rel string, m module) []violation {
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
			violations = append(violations, declared(pkg, object, m, rel+" declares "+name, rulePlugin))
		}
	}
	return violations
}

// declared is a violation at an object's declaration.
func declared(pkg *packages.Package, object types.Object, m module, key, rule string) violation {
	at := pkg.Fset.Position(object.Pos())
	return violation{file: m.within(at.Filename), line: at.Line, key: key, rule: rule}
}

// isKernelPlugin reports whether t is kernel.Plugin.
func isKernelPlugin(t types.Type, m module) bool {
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == m.path+"/kernel" &&
		named.Obj().Name() == "Plugin"
}

// returnsOnly reports whether object is a function taking nothing and returning
// exactly one value that want accepts.
func returnsOnly(object types.Object, want func(types.Type) bool) bool {
	function, ok := object.(*types.Func)
	if !ok {
		return false
	}
	signature := function.Type().(*types.Signature)
	return signature.TypeParams().Len() == 0 && signature.Params().Len() == 0 &&
		signature.Results().Len() == 1 && want(signature.Results().At(0).Type())
}

// constructorExportViolations finds every exported package-scope name of a
// constructor package but New taking nothing and returning exactly
// kernel.Plugin.
func constructorExportViolations(pkg *packages.Package, rel string, m module) []violation {
	var violations []violation
	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		object := scope.Lookup(name)
		if !object.Exported() ||
			name == "New" && returnsOnly(object, func(t types.Type) bool { return isKernelPlugin(t, m) }) {
			continue
		}
		violations = append(violations, declared(pkg, object, m, rel+" exports "+name, ruleConstructorExports))
	}
	return violations
}

// aliasViolations holds an alias-index root to aliases: every type it declares
// is an alias of a type in its own internal/ or internal/types with the same
// name, and every const and var re-exports one of its own internal/'s, or its
// internal/types', of the same name. Functions are the forwarder rule's.
func aliasViolations(pkg *packages.Package, root place, m module) []violation {
	internalPath := m.path + "/" + root.plugin + "/internal"
	own := func(path string) bool { return path == internalPath || strings.HasPrefix(path, internalPath+"/") }
	var violations []violation
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			general, ok := decl.(*ast.GenDecl)
			if !ok || general.Tok == token.IMPORT {
				continue
			}
			for _, spec := range general.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					// The alias is judged by its target as written, one step:
					// internal/ may itself alias a Library's type, as ui's
					// Rect is libs/m's.
					object := pkg.TypesInfo.Defs[spec.Name]
					if !spec.Assign.IsValid() || !aliasesOwn(spec, pkg.TypesInfo, own) {
						violations = append(violations, declared(pkg, object, m,
							root.path+" declares "+spec.Name.Name+", which is not an alias of its own internal type "+spec.Name.Name,
							ruleAliasDeclarations))
					}
				case *ast.ValueSpec:
					for i, name := range spec.Names {
						object := pkg.TypesInfo.Defs[name]
						if name.Name == "_" {
							continue
						}
						if spec.Type != nil || len(spec.Values) != len(spec.Names) || !reexports(spec.Values[i], name.Name, pkg.TypesInfo, own) {
							violations = append(violations, declared(pkg, object, m,
								root.path+" declares "+name.Name+", which does not re-export its own internal "+name.Name,
								ruleAliasDeclarations))
						}
					}
				}
			}
		}
	}
	return violations
}

// aliasesOwn reports whether a type alias names a type of a package own
// accepts under its own name - or, for an unexported alias, the exported form
// of it, which is how a root keeps a type out of its API while still spelling
// it in a forwarder's signature - instantiated, when the alias is generic, with
// exactly its own type parameters in order.
func aliasesOwn(spec *ast.TypeSpec, info *types.Info, own func(string) bool) bool {
	target := spec.Type
	var arguments []ast.Expr
	switch instance := target.(type) {
	case *ast.IndexExpr:
		target, arguments = instance.X, []ast.Expr{instance.Index}
	case *ast.IndexListExpr:
		target, arguments = instance.X, instance.Indices
	}
	var parameters []*ast.Ident
	if spec.TypeParams != nil {
		for _, field := range spec.TypeParams.List {
			parameters = append(parameters, field.Names...)
		}
	}
	if len(arguments) != len(parameters) {
		return false
	}
	for i, argument := range arguments {
		ident, ok := argument.(*ast.Ident)
		if !ok || info.Uses[ident] != info.Defs[parameters[i]] {
			return false
		}
	}
	name := spec.Name.Name
	if !ast.IsExported(name) {
		name = strings.ToUpper(name[:1]) + name[1:]
	}
	return reexports(target, name, info, own)
}

// reexports reports whether value is a qualified reference to a const, var or
// type named name in a package own accepts.
func reexports(value ast.Expr, name string, info *types.Info, own func(string) bool) bool {
	selector, ok := value.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != name {
		return false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	pkgName, ok := info.Uses[qualifier].(*types.PkgName)
	if !ok || !own(pkgName.Imported().Path()) {
		return false
	}
	switch info.Uses[selector.Sel].(type) {
	case *types.Const, *types.Var, *types.TypeName:
		return true
	}
	return false
}

// logicViolations finds every function, and every method but Error and
// String, that an alias-index plugin's internal/types declares: it is where
// plain data goes, and the logic that works on it lives in internal/ with the
// Systems that drive it.
func logicViolations(pkg *packages.Package, rel string, m module) []violation {
	var violations []violation
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if function.Recv != nil && (function.Name.Name == "Error" || function.Name.Name == "String") {
				continue
			}
			violations = append(violations, declared(pkg, pkg.TypesInfo.Defs[function.Name], m,
				rel+" declares "+function.Name.Name+", which is logic", ruleTypesData))
		}
	}
	return violations
}
