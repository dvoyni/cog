package kernel

import (
	"fmt"
	"reflect"
	"strings"
)

// TypeName renders t the way every kernel output names a type: Dump, the
// conflict report, every Err… message that prints a type, and the tools built on
// Describe. It is reflect.Type.String() with one rule added.
//
// A named type declared in a package whose import path has an internal segment
// renders under its enclosing package, the path segment before the last
// internal. The package name alone would be internal for every Bundle and Port
// that splits its declarations, so bundles/canvas/internal.OpQueue renders as
// canvas.OpQueue, the name of the alias a caller writes and greps for. An alias
// declared in an …impl renders the same way: canvasimpl.Config, an alias of
// internal.Config, reads canvas.Config.
//
// The rule applies inside pointers, slices, arrays, maps, channels, functions
// and generic type arguments. reflect spells a type argument by its full import
// path, and TypeName shortens that to the package part too. Every other type —
// predeclared, unnamed struct or interface, or named outside an internal package
// — renders as reflect renders it. A nil type renders as <nil>, as fmt prints it.
func TypeName(t reflect.Type) string {
	if t == nil {
		return "<nil>"
	}
	if name := t.Name(); name != "" {
		path := t.PkgPath()
		if path == "" {
			return t.String()
		}
		pkg := strings.TrimSuffix(t.String(), "."+name)
		if enclosing, ok := enclosingPackage(path); ok {
			pkg = enclosing
		}
		return pkg + "." + shortenQualified(name)
	}
	switch t.Kind() {
	case reflect.Pointer:
		return "*" + TypeName(t.Elem())
	case reflect.Slice:
		return "[]" + TypeName(t.Elem())
	case reflect.Array:
		return fmt.Sprintf("[%d]%s", t.Len(), TypeName(t.Elem()))
	case reflect.Map:
		return "map[" + TypeName(t.Key()) + "]" + TypeName(t.Elem())
	case reflect.Chan:
		return chanName(t)
	case reflect.Func:
		return funcName(t)
	}
	return t.String()
}

func chanName(t reflect.Type) string {
	elem := TypeName(t.Elem())
	switch t.ChanDir() {
	case reflect.RecvDir:
		return "<-chan " + elem
	case reflect.SendDir:
		return "chan<- " + elem
	}
	// chan <-chan T would parse as a send channel of chan T.
	if t.Elem().Kind() == reflect.Chan && t.Elem().Name() == "" && t.Elem().ChanDir() == reflect.RecvDir {
		return "chan (" + elem + ")"
	}
	return "chan " + elem
}

func funcName(t reflect.Type) string {
	var out strings.Builder
	out.WriteString("func(")
	for i := range t.NumIn() {
		if i > 0 {
			out.WriteString(", ")
		}
		if t.IsVariadic() && i == t.NumIn()-1 {
			out.WriteString("..." + TypeName(t.In(i).Elem()))
			continue
		}
		out.WriteString(TypeName(t.In(i)))
	}
	out.WriteString(")")
	switch t.NumOut() {
	case 0:
	case 1:
		out.WriteString(" " + TypeName(t.Out(0)))
	default:
		out.WriteString(" (")
		for i := range t.NumOut() {
			if i > 0 {
				out.WriteString(", ")
			}
			out.WriteString(TypeName(t.Out(i)))
		}
		out.WriteString(")")
	}
	return out.String()
}

// enclosingPackage reports the path segment before the last internal segment of
// an import path, and false when the path has none, or none with a segment
// before it.
func enclosingPackage(path string) (string, bool) {
	segments := strings.Split(path, "/")
	for i := len(segments) - 1; i > 0; i-- {
		if segments[i] == "internal" {
			return segments[i-1], true
		}
	}
	return "", false
}

// packagePart shortens an import path as reflect spells it inside a type
// argument: the enclosing package of an internal path, and otherwise the last
// segment.
func packagePart(path string) string {
	if enclosing, ok := enclosingPackage(path); ok {
		return enclosing
	}
	return path[strings.LastIndex(path, "/")+1:]
}

// shortenQualified rewrites every import-path-qualified name in a type's name,
// which reflect produces only inside an instantiated generic's brackets:
// Maybe[*github.com/dvoyni/cog/bundles/canvas/internal.Font] becomes
// Maybe[*canvas.Font]. A quoted struct tag is copied untouched.
func shortenQualified(name string) string {
	if !strings.Contains(name, "[") {
		return name
	}
	var out strings.Builder
	for i := 0; i < len(name); {
		c := name[i]
		switch {
		case c == '"':
			end := i + 1
			for end < len(name) && name[end] != '"' {
				if name[end] == '\\' {
					end++
				}
				end++
			}
			end = min(end+1, len(name))
			out.WriteString(name[i:end])
			i = end
		case isPathByte(c):
			end := i
			for end < len(name) && isPathByte(name[end]) {
				end++
			}
			out.WriteString(shortenToken(name[i:end]))
			i = end
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

// shortenToken shortens one run of path bytes. A run is a qualified name when it
// has a dot with an identifier after it; the variadic ... before a parameter is
// kept as it is.
func shortenToken(token string) string {
	rest := strings.TrimLeft(token, ".")
	prefix := token[:len(token)-len(rest)]
	dot := strings.LastIndexByte(rest, '.')
	if dot <= 0 || dot == len(rest)-1 {
		return token
	}
	return prefix + packagePart(rest[:dot]) + rest[dot:]
}

func isPathByte(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' ||
		c == '_' || c == '.' || c == '/' || c == '-' || c == '~'
}
