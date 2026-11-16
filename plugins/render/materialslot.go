package render

import (
	"errors"
	"fmt"

	"github.com/dvoyni/cog/plugins/gfx"
)

// materialSlot is the slot of one material: what it was made of, the entry
// points its descriptor named among it, with the entry points of its shader
// it runs once resolved, an empty vertex one meaning unresolved and an
// empty fragment one none; whether it was refused for them; whether a draw
// of it was skipped and reported; the values bound to its shader's
// @group(1) bindings, each at its binding's index among them, made once its
// entry points are resolved, with the setting each value was set from,
// which a reload fills it from again, and what it keeps for each uniform
// block set as a value, both kept, with no values, while a reload of its
// shader leaves it refused; the bind groups its draws bound, until a value
// changes; and the frame a draw of it was last recorded in, as one more
// than the count of frames ended before it, zero for none.
type materialSlot struct {
	label                                string
	shader                               Shader
	state                                State
	namedVertexEntryPoint                string
	namedFragmentEntryPoint              string
	vertexEntryPoint, fragmentEntryPoint string
	refused, reported                    bool
	parameters                           []bindingValue
	settings                             []parameterSetting
	uniformBlocks                        []materialUniformBlock
	bindGroups                           []materialBindGroup
	drawnFrame                           uint64
}

// resolveEntryPoints resolves the entry points s's descriptor named, its
// vertex and its fragment one, against shader, the slot of s's shader, and
// makes s's values, each unbound, one for each binding of shader's
// @group(1), with a setting and a uniform block for each, in place of those
// it had; a shader that failed leaves them unresolved, with no values.
// Entry points shader cannot resolve leave s refused, with no values, its
// settings and uniform blocks kept, and the error says why.
func (s *materialSlot) resolveEntryPoints(shader shaderSlot) error {
	s.vertexEntryPoint = ""
	s.fragmentEntryPoint = ""
	s.refused = false
	s.parameters = nil
	if shader.module == (gfx.ShaderModule{}) {
		return nil
	}
	vertexEntryPoint, err := shader.findEntryPoint(gfx.ShaderStageFlagVertex, s.namedVertexEntryPoint)
	if err == nil && vertexEntryPoint == "" {
		err = errors.New("names no function, and its shader has none of the vertex stage")
	}
	if err != nil {
		s.refused = true
		return fmt.Errorf("its vertex entry point %w", err)
	}
	fragmentEntryPoint, err := shader.findEntryPoint(gfx.ShaderStageFlagFragment, s.namedFragmentEntryPoint)
	if err != nil {
		s.refused = true
		return fmt.Errorf("its fragment entry point %w", err)
	}
	s.vertexEntryPoint = vertexEntryPoint
	s.fragmentEntryPoint = fragmentEntryPoint
	bindingCount := len(shader.findGroupBindings(materialGroup))
	s.parameters = make([]bindingValue, bindingCount)
	s.settings = make([]parameterSetting, bindingCount)
	s.uniformBlocks = make([]materialUniformBlock, bindingCount)
	return nil
}

// makeShaderSelection answers s's shader and the functions of it s runs,
// its vertex and its fragment entry point.
func (s *materialSlot) makeShaderSelection() shaderSelection {
	return shaderSelection{shader: s.shader, entryPoints: [2]string{s.vertexEntryPoint, s.fragmentEntryPoint}}
}

// findOverlapMask answers the bit of each of s's textures, by its index
// among s's parameters, that overlaps one of targets, a pass's, where
// bindings, its shader's of @group(1), may not read it, as
// bindingValue.overlapsTargets says. A parameter past the 64th is never
// masked.
func (s *materialSlot) findOverlapMask(bindings []gfx.ShaderBinding, targets []drawnTarget) uint64 {
	var mask uint64
	if len(targets) == 0 {
		return mask
	}
	for index, parameter := range s.parameters {
		if parameter.overlapsTargets(bindings[index].Texture.SampleType != gfx.TextureSampleTypeUndefined, targets) {
			mask |= 1 << index
		}
	}
	return mask
}

// findBindGroup answers s's bind group for a pass whose targets leave the
// textures of mask at their defaults; false when none is made yet.
func (s *materialSlot) findBindGroup(mask uint64) (materialBindGroup, bool) {
	for _, bindGroup := range s.bindGroups {
		if bindGroup.defaultedMask == mask {
			return bindGroup, true
		}
	}
	return materialBindGroup{}, false
}

// dropBindGroups hands each bind group s made to drop, and forgets them, so
// its next draw makes them again from its values.
func (s *materialSlot) dropBindGroups(drop func(gfx.BindGroup)) {
	for _, bindGroup := range s.bindGroups {
		if bindGroup.group != (gfx.BindGroup{}) {
			drop(bindGroup.group)
		}
	}
	s.bindGroups = s.bindGroups[:0]
}

// placeLatestUniforms copies into the arena, through listState, the latest
// bytes of each uniform block of s set after a draw of the frame and not yet
// copied, and binds the block to them, for the bind group made next. table's
// mutex is held. The error is gfx's, when an arena chunk cannot be made.
func (s *materialSlot) placeLatestUniforms(listState *commandListState, table *assetTable, device gfx.Device) error {
	table.requireLocked("placeLatestUniforms")
	for index, block := range s.uniformBlocks {
		parameter := &s.parameters[index]
		if !block.bufferBehind || parameter.kind != parameterKindUniform || parameter.bufferRange.Buffer != (Buffer{}) {
			continue
		}
		bufferRange, err := listState.allocate(block.latestBytes, table, device)
		if err != nil {
			return err
		}
		parameter.bufferRange = bufferRange
	}
	return nil
}

// materialBindGroup is a material's own bind group of @group(1), for passes
// whose targets leave the textures of defaultedMask at their defaults, none
// when its shader's @group(1) is empty and the layout's default stands in;
// the bit of each binding, by its index, that draws its default, for
// whatever reason, a binding past the 64th never noted; and the name of a
// storage binding of it left unbound that a function the material runs
// reads, for which its draws are skipped, empty for none.
type materialBindGroup struct {
	defaultedMask    uint64
	group            gfx.BindGroup
	drawnDefaultMask uint64
	unboundRead      string
}

// materialUniformBlock is what a material keeps for one uniform block of its
// shader's @group(1) set as a value: the buffer holding it, made at its
// first set; its latest bytes; and whether the buffer is behind them, as
// they were set after a draw of the frame being recorded, until the frame
// ends.
type materialUniformBlock struct {
	buffer       gfx.Buffer
	latestBytes  []byte
	bufferBehind bool
}
