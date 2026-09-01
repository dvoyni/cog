package wgpu

import "fmt"

// ErrInvalidConfig is returned by the plugin's Register when the config value
// handed to it is neither nil nor a wgpu.Config.
type ErrInvalidConfig struct {
	Got any
}

func (e ErrInvalidConfig) Error() string {
	return fmt.Sprintf("wgpu: invalid config: want %T, got %T", Config{}, e.Got)
}
