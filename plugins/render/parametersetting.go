package render

import (
	"fmt"
	"reflect"
)

// parameterSetting is a pass or draw parameter as its setter gave it, by its
// binding's name, resolved against each shader drawn with it, or a
// material's value as its setter gave it, resolved again when a reload
// makes its asset's gfx object or its shader's module again: a texture
// seen through view, wholeTexture saying it named its texture alone, which
// a storage binding sees at its first mip level; a sampler; a range of a
// buffer; or a uniform block of uniformType, its bytes in bufferRange, a
// block of its list's arena, or a material's own buffer when bufferRange
// names none. One of no kind leaves its binding unbound.
type parameterSetting struct {
	name         string
	kind         parameterKind
	view         TextureView
	wholeTexture bool
	sampler      SamplerDescriptor
	bufferRange  BufferRange
	uniformType  reflect.Type
}

// putSetting answers settings with setting in place of the one of its name,
// or after them when none has it.
func putSetting(settings []parameterSetting, setting parameterSetting) []parameterSetting {
	if index := findSetting(settings, setting.name); index >= 0 {
		settings[index] = setting
		return settings
	}
	return append(settings, setting)
}

// findSetting answers the index among settings of the first one named name;
// -1 for none.
func findSetting(settings []parameterSetting, name string) int {
	for index := range settings {
		if settings[index].name == name {
			return index
		}
	}
	return -1
}

// fillUniformSetting fills setting, named and of no kind, as a uniform block
// of uniformType, its bytes data copied into the arena chunks listState, of
// a list of frame, takes; it stays of no kind, leaving its binding unbound,
// when gfx refuses a chunk, which is logged.
func fillUniformSetting(setting *parameterSetting, frame Frame, listState *commandListState, uniformType reflect.Type, data []byte) {
	bufferRange, err := frame.table.Get().allocateTemporary(listState, data, frame.device.Get())
	if err != nil {
		frame.state.Get().logUnlessLost(fmt.Errorf("render: setting uniform block %q: %w", setting.name, err))
		return
	}
	setting.kind = parameterKindUniform
	setting.bufferRange = bufferRange
	setting.uniformType = uniformType
}
