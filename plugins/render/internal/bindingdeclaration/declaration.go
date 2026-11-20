// Package bindingdeclaration reads and writes binding declarations, the JSON
// files beside shaders that say what gfx reflects of each variant of them:
// rendertest's harness reads them to answer the reflection of a recording
// device, which parses no WGSL, and render's cmd/declarebindings writes them
// from a real device's. rendertest's documentation describes the format.
package bindingdeclaration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"slices"

	"github.com/dvoyni/cog/plugins/gfx"
)

// Variant is the reflection of a shader preprocessed with Defines.
type Variant struct {
	Defines []gfx.ShaderDefine
	Reflection
}

// declaration is the JSON of a binding declaration: what the shader declares
// with no defines, and a variant for each define set declared on its own;
// or, when VariantsOnly is set, its variants alone, its own bindings, entry
// points and inputs left out, for a shader that compiles only with defines.
type declaration struct {
	VariantsOnly bool `json:"variantsOnly,omitempty"`
	declaredShader
	Variants []declaredVariant `json:"variants,omitempty"`
}

// variantsOnlyDeclaration is the JSON of a declaration of variants alone,
// as declaration reads it.
type variantsOnlyDeclaration struct {
	VariantsOnly bool              `json:"variantsOnly"`
	Variants     []declaredVariant `json:"variants"`
}

// declaredVariant is the declaration of the variant preprocessed with
// Defines, each define's name with its value, empty for none, in place of
// the base declaration's, whole.
type declaredVariant struct {
	Defines map[string]string `json:"defines"`
	declaredShader
}

// Read answers what the declaration beside the shader at path in files,
// path with .json appended, says of its variant with defines: the variant
// whose defines are the same names with the same values, in any order, or
// the base declaration when none is, and an error when a declaration of
// variants alone has none of defines. A missing declaration answers
// fs.ErrNotExist; one that fails to parse, an unknown field or value among
// its errors, answers its error.
func Read(files fs.FS, path string, defines []gfx.ShaderDefine) (Reflection, error) {
	data, err := fs.ReadFile(files, path+".json")
	if err != nil {
		return Reflection{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var declaration declaration
	if err := decoder.Decode(&declaration); err != nil {
		return Reflection{}, err
	}
	definedValues := makeDefinedValues(defines)
	for _, variant := range declaration.Variants {
		if maps.Equal(variant.Defines, definedValues) {
			return variant.declaredShader.makeReflection()
		}
	}
	if declaration.VariantsOnly {
		return Reflection{}, fmt.Errorf("it declares variants alone, none of them of the defines %q", definedValues)
	}
	return declaration.declaredShader.makeReflection()
}

// Marshal answers the declaration of a shader whose reflection with no
// defines is base, with a section for each of variants, in their order, as
// indented JSON ending in a newline; a nil base answers a declaration of
// variants alone, for a shader that compiles only with defines. A value the
// format cannot name, such as a binding of no kind or a storage format
// WebGPU refuses a storage texture, answers an error naming it.
func Marshal(base *Reflection, variants []Variant) ([]byte, error) {
	var declaredVariants []declaredVariant
	for _, variant := range variants {
		variantShader, err := makeDeclaredShader(variant.Reflection)
		if err != nil {
			return nil, fmt.Errorf("the variant with defines %q: %w", variant.Defines, err)
		}
		declaredVariants = append(declaredVariants, declaredVariant{Defines: makeDefinedValues(variant.Defines), declaredShader: variantShader})
	}
	var marshalled any = variantsOnlyDeclaration{VariantsOnly: true, Variants: declaredVariants}
	if base != nil {
		baseShader, err := makeDeclaredShader(*base)
		if err != nil {
			return nil, err
		}
		marshalled = declaration{declaredShader: baseShader, Variants: declaredVariants}
	}
	declarationJSON, err := json.MarshalIndent(marshalled, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(declarationJSON, '\n'), nil
}

// makeDefinedValues answers each of defines' names with its value, the last
// of a name given twice.
func makeDefinedValues(defines []gfx.ShaderDefine) map[string]string {
	definedValues := map[string]string{}
	for _, define := range defines {
		definedValues[define.Name] = define.Value
	}
	return definedValues
}

// findNamed answers the value of values named name, or an error naming it as
// a what and the names there are.
func findNamed[T any](values map[string]T, what, name string) (T, error) {
	value, ok := values[name]
	if !ok {
		return value, fmt.Errorf("%q is no %s, want one of %q", name, what, slices.Sorted(maps.Keys(values)))
	}
	return value, nil
}

// findName answers the name of value in values, or an error naming it as a
// what no name spells.
func findName[T comparable](values map[string]T, what string, value T) (string, error) {
	for _, name := range slices.Sorted(maps.Keys(values)) {
		if values[name] == value {
			return name, nil
		}
	}
	return "", fmt.Errorf("%v is no %s a declaration names", value, what)
}
