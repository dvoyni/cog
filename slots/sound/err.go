package sound

import (
	"github.com/dvoyni/cog/slots/sound/internal"
)

// ErrInvalidConfig reports a plugin configuration value that is not a Config.
type ErrInvalidConfig = internal.ErrInvalidConfig

// ErrInvalidMaxVoices reports a negative voice cap. Zero is the default; a
// negative table is not a table.
type ErrInvalidMaxVoices = internal.ErrInvalidMaxVoices

// ErrClipUnreadable reports a Clip named by bytes that are not there. A Clip
// named by a path that would not read is the Library's own report, made once
// under its descriptor key, and never this one.
type ErrClipUnreadable = internal.ErrClipUnreadable

// ErrClipNotPrepared reports an Adapter that answered a prepare with neither a
// prepared Clip nor an error, or installed one under the zero id.
type ErrClipNotPrepared = internal.ErrClipNotPrepared

// ErrClipFailed wraps an Adapter's own prepare or install failure with the Clip
// it happened to. Unwrap it for what the Adapter said.
type ErrClipFailed = internal.ErrClipFailed
