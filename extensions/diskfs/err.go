package diskfs

import "fmt"

// ErrInvalidConfig reports a plugin configuration value that is not a Config.
type ErrInvalidConfig struct{ Got any }

func (e ErrInvalidConfig) Error() string {
	return fmt.Sprintf("diskfs: invalid config: want %T, got %T", Config{}, e.Got)
}

// ErrInvalidAppId reports an application id that is not a single directory name.
type ErrInvalidAppId struct{ AppId string }

func (e ErrInvalidAppId) Error() string {
	return fmt.Sprintf("diskfs: invalid app id %q", e.AppId)
}
