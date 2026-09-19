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
}

func newGfxbShader(label string, module *wgpu.ShaderModule, layout gfx.ShaderLayout) *gfxbShader {
	return &gfxbShader{
		label: label, module: module, layout: layout,
		textureViews: textureViewIndex(layout),
	}
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
