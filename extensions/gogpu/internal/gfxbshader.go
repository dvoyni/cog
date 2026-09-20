package internal

import (
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/gogpu/wgpu"
)

// gfxbShader is a compiled shader module, its reflected uniform layout, and the
// GPU bind-group + pipeline layouts built from reflection.
type gfxbShader struct {
	// label is the shader's name as it was compiled, kept so a refusal at draw
	// time can say which shader it was; nothing else on the render thread knows
	// a shader by anything but its pointer.
	label      string
	module     *wgpu.ShaderModule
	layout     gfx.ShaderLayout
	bgLayouts  []*wgpu.BindGroupLayout // indexed by bind group
	pipeLayout *wgpu.PipelineLayout
	// textureViews is the dimension each texture binding declares, indexed by
	// group and then by binding. It is what lets an unresolved texture take a
	// white view of the right shape: the id alone cannot say, because an unknown
	// id is what every unresolved texture resolves to whatever declared it.
	//
	// It is an index rather than a search of layout.Resources because it is read
	// once per texture binding per draw. Reflection is walked once, here.
	textureViews [][]gfx.TextureViewDimension
	// groupSizes is how many bind-group entries each group declares, indexed by
	// group. It is what lets flushBinds tell a group nothing filled from a group
	// with nothing to fill, and those are not the same draw: the first is a
	// binding that went missing and the second is a gap in the group numbering.
	//
	// The gap is real rather than hypothetical. buildShaderLayouts creates a
	// layout for every index below the highest group a shader uses, so an
	// unused middle group has a non-nil layout with no entries in it, and a
	// scene shader composed from frame (group 0) and morph (group 2) but not
	// material (group 1) is exactly that. Refusing it would drop every draw of
	// it for the life of the process, because a refusal is latched once and
	// dropped always.
	//
	// It is an index rather than a search of layout.Resources for the same
	// reason textureViews is: it is read once per group per draw.
	groupSizes []int
}

func newGfxbShader(label string, module *wgpu.ShaderModule, layout gfx.ShaderLayout) *gfxbShader {
	return &gfxbShader{
		label: label, module: module, layout: layout,
		textureViews: textureViewIndex(layout),
		groupSizes:   groupSizeIndex(layout),
	}
}

// declaredEntries is how many entries the group at this index declares. A group
// this shader says nothing about declares none, which is both the zero value
// and the right answer: there is nothing there to leave unfilled.
func (s *gfxbShader) declaredEntries(group int) int {
	if group < 0 || group >= len(s.groupSizes) {
		return 0
	}
	return s.groupSizes[group]
}

// groupSizeIndex counts the bindings each group declares, over the same two
// sources buildShaderLayouts builds the layouts from - the uniform block and
// the reflected resources - so the two cannot disagree about what a group holds.
func groupSizeIndex(layout gfx.ShaderLayout) []int {
	var sizes []int
	grow := func(group int) {
		for len(sizes) <= group {
			sizes = append(sizes, 0)
		}
	}
	if layout.UniformSize > 0 && layout.UniformGroup >= 0 {
		grow(layout.UniformGroup)
		sizes[layout.UniformGroup]++
	}
	for i := range layout.Resources {
		if group := layout.Resources[i].Group; group >= 0 {
			grow(group)
			sizes[group]++
		}
	}
	return sizes
}

// textureViewDimension is the dimension the binding at (group, binding)
// declares. A binding this shader does not declare as a texture answers
// TextureView2D, which is both the zero value and the right answer for every
// binding the caller has no business asking about.
func (s *gfxbShader) textureViewDimension(group, binding int) gfx.TextureViewDimension {
	if group < 0 || group >= len(s.textureViews) {
		return gfx.TextureView2D
	}
	row := s.textureViews[group]
	if binding < 0 || binding >= len(row) {
		return gfx.TextureView2D
	}
	return row[binding]
}

// textureViewIndex builds the (group, binding) -> dimension index from a
// shader's reflected resources, skipping samplers and storage buffers because
// neither has a view dimension to declare.
func textureViewIndex(layout gfx.ShaderLayout) [][]gfx.TextureViewDimension {
	var index [][]gfx.TextureViewDimension
	for i := range layout.Resources {
		r := &layout.Resources[i]
		if r.Sampler || r.StorageBuffer || r.Group < 0 || r.Binding < 0 {
			continue
		}
		for len(index) <= r.Group {
			index = append(index, nil)
		}
		for len(index[r.Group]) <= r.Binding {
			index[r.Group] = append(index[r.Group], gfx.TextureView2D)
		}
		index[r.Group][r.Binding] = r.TextureView
	}
	return index
}
