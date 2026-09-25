package internal

import "fmt"

// ErrInvalidConfig is returned by the plugin's Register when the config value
// handed to it is neither nil nor a gogpu.Config.
type ErrInvalidConfig struct {
	Got any
}

func (e ErrInvalidConfig) Error() string {
	return fmt.Sprintf("gogpu: invalid config: want %T, got %T", Config{}, e.Got)
}

// ErrDepthOnlyPassUnsupported reports a pass this backend declined to encode.
type ErrDepthOnlyPassUnsupported struct {
	Pass    string
	Backend string
}

func (e ErrDepthOnlyPassUnsupported) Error() string {
	return fmt.Sprintf(
		"gogpu: pass %q has a depth attachment and no colour attachment, which the %s backend cannot encode. "+
			"The pass is skipped and its depth texture is left untouched",
		e.Pass, e.Backend)
}

// ErrBindGroupRefused reports a bind group the device would not create, which
// leaves the draw encoded with nothing bound for that group. It is the backstop
// beneath gfx's own checks: a binding gfx never emitted is caught before it
// gets here, but a binding it emitted against a buffer this backend no longer
// holds - released, or from a device ago - is visible only at the point the
// group is built.
//
// It is reported once per shader and group. A refused group is a property of
// the material rather than of the HAL, so a second site is a second report.
type ErrBindGroupRefused struct {
	Shader string
	Group  int
}

func (e ErrBindGroupRefused) Error() string {
	return fmt.Sprintf(
		"gogpu: the device refused bind group %d of shader %q, so every draw using it encodes with "+
			"nothing bound for that group. A binding it declares is filled by a resource this backend "+
			"does not hold",
		e.Group, e.Shader)
}
