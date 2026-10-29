package internal

import "fmt"

// ErrInvalidConfig reports a plugin configuration value that is not a Config.
type ErrInvalidConfig struct{ Got any }

func (e ErrInvalidConfig) Error() string {
	return fmt.Sprintf("platform: invalid config: want %T, got %T", Config{}, e.Got)
}
