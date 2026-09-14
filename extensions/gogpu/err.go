package gogpu

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
