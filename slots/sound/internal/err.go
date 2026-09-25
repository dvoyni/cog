package internal

import "fmt"

// ErrClipUnreadable reports a Clip named by bytes that are not there: a zero
// ClipRef, or a Blob of no length. A Clip named by a path that would not read
// is the Library's own report, made once under its descriptor key, and never
// this one.
type ErrClipUnreadable struct{ Clip string }

func (e ErrClipUnreadable) Error() string {
	return fmt.Sprintf("sound: %s could not be read", e.Clip)
}

// ErrClipNotPrepared reports an Adapter that answered a prepare with neither a
// prepared Clip nor an error, or installed one under the zero id. Either way
// there is nothing a Voice can be started from, and the entry is terminal.
type ErrClipNotPrepared struct{ Clip string }

func (e ErrClipNotPrepared) Error() string {
	return fmt.Sprintf("sound: %s was not prepared by the adapter", e.Clip)
}

// ErrClipFailed wraps an Adapter's own prepare or install failure with the Clip
// it happened to. The Adapter knows what went wrong and sound knows which Clip
// it was, and a report carrying only one of the two names a condition nobody
// can find.
type ErrClipFailed struct {
	Clip string
	Err  error
}

func (e ErrClipFailed) Error() string {
	return fmt.Sprintf("sound: %s could not be prepared: %v", e.Clip, e.Err)
}

func (e ErrClipFailed) Unwrap() error { return e.Err }

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
