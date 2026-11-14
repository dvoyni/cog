package recording

import "fmt"

// role is what a checked object or value is to the call, as an error names
// it: a string, or an indexedRole, formatted only when a check fails, so a
// check that passes allocates nothing.
type role interface {
	string | indexedRole
}

// indexedRole is a role numbered by an index, as "vertex buffer %d" and 2.
type indexedRole struct {
	format string
	index  int
}

// String answers the role with its index.
func (r indexedRole) String() string { return fmt.Sprintf(r.format, r.index) }
