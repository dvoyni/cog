package render

import (
	"fmt"
	"reflect"
)

// parameterSetting is a pass or draw parameter as its setter gave it, by its
// binding's name, resolved against each shader drawn with it: a texture
// seen through view, wholeTexture saying it named its texture alone, which
// a storage binding sees at its first mip level; a sampler; a range of a
// buffer; or a uniform block of uniformType, its bytes in bufferRange, a
// block of its list's arena. One of no kind leaves its binding unbound.
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
	for index := range settings {
		if settings[index].name == setting.name {
			settings[index] = setting
			return settings
		}
	}
	return append(settings, setting)
}

// makeUniformSetting answers the setting of a uniform block of uniformType
// named name, its bytes data copied into the arena chunks listState, of a
// list of frame, takes; one of no kind, leaving its binding unbound, when
// gfx refuses a chunk, which is logged.
func makeUniformSetting(frame Frame, listState *commandListState, name string, uniformType reflect.Type, data []byte) parameterSetting {
	setting := parameterSetting{name: name}
	bufferRange, err := frame.table.Get().allocateTemporary(listState, data, frame.device.Get())
	if err != nil {
		frame.state.Get().logUnlessLost(fmt.Errorf("render: setting uniform block %q: %w", name, err))
		return setting
	}
	setting.kind = parameterKindUniform
	setting.bufferRange = bufferRange
	setting.uniformType = uniformType
	return setting
}
