package render

import (
	"slices"

	"github.com/dvoyni/cog/gfx"
)

// materialSlot is the slot of one material: what it was made of, with the
// entry points of its shader it runs once resolved, an empty vertex one
// meaning unresolved and an empty fragment one none; whether it was refused
// for them; whether a draw of it was skipped and reported; the values bound
// to its shader's @group(1) bindings, each at its binding's index among
// them, made once its entry points are resolved, with the buffer holding
// each uniform block set as a value, made at its first set and kept for the
// next; and the bind groups its draws bound, until a value changes.
type materialSlot struct {
	label                                string
	shader                               Shader
	state                                State
	vertexEntryPoint, fragmentEntryPoint string
	refused, reported                    bool
	parameters                           []materialParameter
	uniformBuffers                       []gfx.Buffer
	bindGroups                           []materialBindGroup
}

// usesBinding answers whether binding is used by a function s runs, its
// vertex or its fragment entry point.
func (s *materialSlot) usesBinding(binding gfx.ShaderBinding) bool {
	return slices.ContainsFunc(binding.EntryPoints, func(name string) bool {
		return name == s.vertexEntryPoint || (name != "" && name == s.fragmentEntryPoint)
	})
}

// findOverlapMask answers the bit of each of s's textures, by its index
// among s's parameters, that overlaps one of targets, a pass's, where
// bindings, its shader's of @group(1), may not read it: a sampled texture
// overlapping a target the pass writes, or a storage texture overlapping
// any. A parameter past the 64th is never masked.
func (s *materialSlot) findOverlapMask(bindings []gfx.ShaderBinding, targets []drawnTarget) uint64 {
	var mask uint64
	if len(targets) == 0 {
		return mask
	}
	for index, parameter := range s.parameters {
		if parameter.kind != parameterKindTexture {
			continue
		}
		sampled := bindings[index].Texture.SampleType != gfx.TextureSampleTypeUndefined
		for _, target := range targets {
			if target.texture == parameter.texture && (!target.readOnly || !sampled) &&
				target.mipLevel-parameter.view.BaseMipLevel < parameter.view.MipLevelCount &&
				target.layer-parameter.view.BaseArrayLayer < parameter.view.ArrayLayerCount {
				mask |= 1 << index
			}
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

// materialBindGroup is a material's own bind group of @group(1), for passes
// whose targets leave the textures of defaultedMask at their defaults, none
// when its shader's @group(1) is empty and the layout's default stands in;
// and the name of a storage binding of any group left unbound that a
// function the material runs reads, for which its draws are skipped, empty
// for none.
type materialBindGroup struct {
	defaultedMask uint64
	group         gfx.BindGroup
	unboundRead   string
}

// drawBindGroupSet are the bind groups a draw binds, one for each group of its
// shader's layout, and the name of a storage binding left unbound that a
// function it runs reads, for which it is skipped, empty for none.
type drawBindGroupSet struct {
	groups      [reservedGroup]gfx.BindGroup
	unboundRead string
}
