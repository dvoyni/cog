package internal

import (
	"errors"
	"fmt"
)

// ErrInvalidConfig reports a plugin configuration value that is not a Config.
type ErrInvalidConfig struct{ Got any }

func (e ErrInvalidConfig) Error() string {
	return fmt.Sprintf("steamplatform: invalid config: want %T, got %T", Config{}, e.Got)
}

// ErrNoAppID reports a Config that names no app id.
type ErrNoAppID struct{}

func (ErrNoAppID) Error() string { return "steamplatform: Config.AppID is required" }

// ErrRelaunching fails composition when the game was started outside Steam and
// Steam is starting it again through itself. It is not a failure of the game:
// the process should exit quietly and leave the start to Steam. A
// steam_appid.txt beside the executable, as in development, turns the check
// off.
type ErrRelaunching struct{ AppID uint32 }

func (e ErrRelaunching) Error() string {
	return fmt.Sprintf("steamplatform: Steam is relaunching app %d through itself", e.AppID)
}

// ErrUnavailable reports that Steam could not be reached - the library is
// missing or the client is not running - so the game runs without it, as it
// would under noplatform. It is reported once, on the first frame.
type ErrUnavailable struct{ Err error }

func (e ErrUnavailable) Error() string {
	return fmt.Sprintf("steamplatform: running without Steam: %v", e.Err)
}

func (e ErrUnavailable) Unwrap() error { return e.Err }

// ErrInitFailed reports a SteamAPI_InitFlat that did not succeed, with Steam's
// own result code and message.
type ErrInitFailed struct {
	Result  int32
	Message string
}

func (e ErrInitFailed) Error() string {
	return fmt.Sprintf("SteamAPI_InitFlat failed (%d): %s", e.Result, e.Message)
}

// ErrAchievementRefused reports an achievement Steam does not know: the name is
// missing from the app's Steamworks configuration.
type ErrAchievementRefused struct{ Name string }

func (e ErrAchievementRefused) Error() string {
	return fmt.Sprintf("steamplatform: Steam refused achievement %q", e.Name)
}

// ErrStatRefused reports a stat Steam does not know, or one it holds as a
// float or does not let the client set.
type ErrStatRefused struct{ Name string }

func (e ErrStatRefused) Error() string {
	return fmt.Sprintf("steamplatform: Steam refused stat %q", e.Name)
}

// errStoreFailed is the cause of a StoreStats call that returned false. It is
// unexported because nothing tests for it: platform retries an upload that
// failed whatever the cause.
var errStoreFailed = errors.New("steamplatform: StoreStats failed")
