package steamplatform

import "github.com/dvoyni/cog/extensions/steamplatform/internal"

// ErrInvalidConfig reports a plugin configuration value that is not a Config.
type ErrInvalidConfig = internal.ErrInvalidConfig

// ErrNoAppID reports a Config that names no app id.
type ErrNoAppID = internal.ErrNoAppID

// ErrRelaunching fails composition when the game was started outside Steam and
// Steam is starting it again through itself. It is not a failure of the game:
// the process should exit quietly and leave the start to Steam. A
// steam_appid.txt beside the executable, as in development, turns the check
// off.
type ErrRelaunching = internal.ErrRelaunching

// ErrUnavailable reports that Steam could not be reached - the library is
// missing or the client is not running - so the game runs without it, as it
// would under noplatform. It is reported once, on the first frame.
type ErrUnavailable = internal.ErrUnavailable

// ErrInitFailed reports a SteamAPI_InitFlat that did not succeed, with Steam's
// own result code and message.
type ErrInitFailed = internal.ErrInitFailed

// ErrAchievementRefused reports an achievement Steam does not know: the name is
// missing from the app's Steamworks configuration.
type ErrAchievementRefused = internal.ErrAchievementRefused

// ErrStatRefused reports a stat Steam does not know, or one it holds as a
// float or does not let the client set.
type ErrStatRefused = internal.ErrStatRefused
