package internal

// DefaultValuesPath is the key-value file used when Config names none.
const DefaultValuesPath = "config.json"

// Config configures storage's values file. It is supplied under Name, and its
// zero value is the default: DefaultValuesPath. Neither what is read nor what
// persists is configured here: read mounts are contributed through
// ReadMountPort, and the permanent filesystem is the PermanentFS Adapter an
// Extension provides.
type Config struct {
	// ValuesPath is the JSON object read through FileSystem and flushed to the
	// permanent filesystem by the value commands. Empty means
	// DefaultValuesPath.
	ValuesPath string
}

// WithValuesPath replaces the file backing the value commands.
func (c Config) WithValuesPath(path string) Config {
	c.ValuesPath = path
	return c
}
