// Package platform stands in for the few platform types canvas's API names.
package platform

import "io/fs"

// Window names one of platform's windows; the zero Window is the main one.
type Window struct{ id uint32 }

// FS is the FileSystem: every read mount's files, read through one fs.FS.
type FS = fs.FS
