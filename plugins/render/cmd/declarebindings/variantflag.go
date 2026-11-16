package main

import (
	"fmt"
	"strings"

	"github.com/dvoyni/cog/plugins/render"
)

// variantFlag is the define sets the -variant flags name, one for each.
type variantFlag [][]render.ShaderDefine

// String answers the define sets as -variant spells them, separated by
// spaces.
func (f *variantFlag) String() string {
	var sets []string
	for _, defines := range *f {
		var spelt []string
		for _, define := range defines {
			if define.Value == "" {
				spelt = append(spelt, define.Name)
			} else {
				spelt = append(spelt, define.Name+"="+define.Value)
			}
		}
		sets = append(sets, strings.Join(spelt, ","))
	}
	return strings.Join(sets, " ")
}

// Set adds the define set text spells, its defines separated by commas, each
// NAME or NAME=VALUE, a define of no name an error.
func (f *variantFlag) Set(text string) error {
	var defines []render.ShaderDefine
	for spelt := range strings.SplitSeq(text, ",") {
		name, value, _ := strings.Cut(spelt, "=")
		if name == "" {
			return fmt.Errorf("the define %q has no name", spelt)
		}
		defines = append(defines, render.ShaderDefine{Name: name, Value: value})
	}
	*f = append(*f, defines)
	return nil
}
