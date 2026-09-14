package diskstorage

// Config configures the desktop permanent filesystem. It is supplied under
// Name, and its zero value is the default: the executable's name as the
// application id.
type Config struct {
	// AppId names the directory under the user's data directory that writes land
	// in. It must be a single directory name. Empty means the executable's name
	// without its extension.
	AppId string
}
