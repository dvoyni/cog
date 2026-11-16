package bindingdeclaration

import (
	"cmp"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/dvoyni/cog/plugins/gfx"
)

// Reflection is what gfx reflects of one variant of a shader: its bindings
// and its entry points, each in the order it declares them, and the inputs
// of each vertex entry point, by name.
type Reflection struct {
	Bindings     []gfx.ShaderBinding
	EntryPoints  []gfx.ShaderEntryPoint
	VertexInputs map[string][]gfx.VertexInput
}

// Reflect answers the reflection of a shader module from what a device
// reflects of it: bindings answers its bindings, entryPoints its entry
// points, and vertexInputs the inputs of the entry point it names, asked of
// each vertex entry point. The first error answered is answered.
func Reflect(bindings func() ([]gfx.ShaderBinding, error), entryPoints func() ([]gfx.ShaderEntryPoint, error), vertexInputs func(entryPoint string) ([]gfx.VertexInput, error)) (Reflection, error) {
	reflection := Reflection{VertexInputs: map[string][]gfx.VertexInput{}}
	var err error
	if reflection.Bindings, err = bindings(); err != nil {
		return Reflection{}, err
	}
	if reflection.EntryPoints, err = entryPoints(); err != nil {
		return Reflection{}, err
	}
	for _, entryPoint := range reflection.EntryPoints {
		if entryPoint.Stage&gfx.ShaderStageFlagVertex == 0 {
			continue
		}
		if reflection.VertexInputs[entryPoint.Name], err = vertexInputs(entryPoint.Name); err != nil {
			return Reflection{}, err
		}
	}
	return reflection, nil
}

// FindDifference answers the first difference between declared, what a
// binding declaration says of a shader's variant, and reflected, what a
// device reflects of it, in words naming what differs, or "" when they
// agree: a binding, by group and number; then the entry points; then a
// vertex entry point's inputs, by its name. A slice left empty agrees with
// nil.
func FindDifference(declared, reflected Reflection) string {
	compareBindings := func(a, b gfx.ShaderBinding) int {
		return cmp.Or(cmp.Compare(a.Group, b.Group), cmp.Compare(a.Binding, b.Binding))
	}
	declaredBindings := slices.SortedFunc(slices.Values(declared.Bindings), compareBindings)
	reflectedBindings := slices.SortedFunc(slices.Values(reflected.Bindings), compareBindings)
	for len(declaredBindings) > 0 || len(reflectedBindings) > 0 {
		switch {
		case len(reflectedBindings) == 0 || len(declaredBindings) > 0 && compareBindings(declaredBindings[0], reflectedBindings[0]) < 0:
			binding := declaredBindings[0]
			return fmt.Sprintf("binding %q at group %d, binding %d, is declared and not reflected", binding.Name, binding.Group, binding.Binding)
		case len(declaredBindings) == 0 || compareBindings(declaredBindings[0], reflectedBindings[0]) > 0:
			binding := reflectedBindings[0]
			return fmt.Sprintf("binding %q at group %d, binding %d, is reflected and not declared", binding.Name, binding.Group, binding.Binding)
		}
		declaredBinding := clipBinding(declaredBindings[0])
		reflectedBinding := clipBinding(reflectedBindings[0])
		if !reflect.DeepEqual(declaredBinding, reflectedBinding) {
			return fmt.Sprintf("binding %q at group %d, binding %d, is declared\n%+v\nand reflected\n%+v", reflectedBinding.Name, reflectedBinding.Group, reflectedBinding.Binding, declaredBinding, reflectedBinding)
		}
		declaredBindings = declaredBindings[1:]
		reflectedBindings = reflectedBindings[1:]
	}
	if !slices.Equal(declared.EntryPoints, reflected.EntryPoints) {
		return fmt.Sprintf("the entry points are declared %+v and reflected %+v", declared.EntryPoints, reflected.EntryPoints)
	}
	names := slices.AppendSeq(slices.Collect(maps.Keys(declared.VertexInputs)), maps.Keys(reflected.VertexInputs))
	slices.Sort(names)
	for _, name := range slices.Compact(names) {
		if !slices.Equal(declared.VertexInputs[name], reflected.VertexInputs[name]) {
			return fmt.Sprintf("the inputs of vertex entry point %q are declared %+v and reflected %+v", name, declared.VertexInputs[name], reflected.VertexInputs[name])
		}
	}
	return ""
}

// clipBinding answers binding with its members and entry points nil when it
// has none, so a binding read from a declaration compares equal to one a
// device reflects.
func clipBinding(binding gfx.ShaderBinding) gfx.ShaderBinding {
	if len(binding.Members) == 0 {
		binding.Members = nil
	}
	if len(binding.EntryPoints) == 0 {
		binding.EntryPoints = nil
	}
	return binding
}
