// Package storage provides prioritized read-only filesystems and one writable
// permanent filesystem as kernel resources.
package storage

import (
	"io/fs"
)

// MountId identifies a filesystem mounted in ReadFS.
type MountId string

// ReadMount is one filesystem in a ReadFS overlay. Higher priorities are
// searched first; equal priorities retain registration order.
type ReadMount struct {
	Id       MountId
	Priority int
	FS       fs.FS
}
