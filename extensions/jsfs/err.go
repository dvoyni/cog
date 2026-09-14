package jsfs

import "fmt"

// ErrInvalidConfig reports a plugin configuration value that is not a Config.
type ErrInvalidConfig struct{ Got any }

func (e ErrInvalidConfig) Error() string {
	return fmt.Sprintf("jsfs: invalid config: want %T, got %T", Config{}, e.Got)
}

// ErrInvalidAppId reports an application id that is empty or not a single name.
type ErrInvalidAppId struct{ AppId string }

func (e ErrInvalidAppId) Error() string {
	return fmt.Sprintf("jsfs: invalid app id %q", e.AppId)
}
