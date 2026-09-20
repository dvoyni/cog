package sound

import (
	"fmt"

	"github.com/dvoyni/cog/slots/sound/internal/types"
)

// ErrInvalidConfig reports a plugin configuration value that is not a Config.
type ErrInvalidConfig struct{ Got any }

func (e ErrInvalidConfig) Error() string {
	return fmt.Sprintf("sound: invalid config: want %T, got %T", Config{}, e.Got)
}

// ErrInvalidMaxVoices reports a negative voice cap. Zero is the default; a
// negative table is not a table.
type ErrInvalidMaxVoices struct{ MaxVoices int }

func (e ErrInvalidMaxVoices) Error() string {
	return fmt.Sprintf("sound: invalid MaxVoices %d", e.MaxVoices)
}

// ErrClipUnreadable reports a Clip named by bytes that are not there. A Clip
// named by a path that would not read is the Library's own report, made once
// under its descriptor key, and never this one.
type ErrClipUnreadable = types.ErrClipUnreadable

// ErrClipNotPrepared reports an Adapter that answered a prepare with neither a
// prepared Clip nor an error, or installed one under the zero id.
type ErrClipNotPrepared = types.ErrClipNotPrepared

// ErrClipFailed wraps an Adapter's own prepare or install failure with the Clip
// it happened to. Unwrap it for what the Adapter said.
type ErrClipFailed = types.ErrClipFailed
