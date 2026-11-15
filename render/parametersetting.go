package render

import "reflect"

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
