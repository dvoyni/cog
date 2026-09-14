// Package storage declares the storage Slot: one kernel resource, FileSystem, a
// prioritized read-only overlay over every mounted filesystem, including the
// single permanent one that writes land in. Reading needs a read lock on
// FileSystem; mutating needs a write lock, because write access is reachable
// only through WriteAccess. One resource over one set of bytes is what makes the
// scheduler's read/write locks mean anything here — a reader and a writer of the
// same file conflict instead of racing.
//
// storage is a Slot: its plugin, built by storageplugin.New, requires exactly
// one PermanentFS Adapter through PermanentFSPort, which an Extension
// (diskstorage, jsstorage) provides. storage itself carries no platform code:
// no build tags, no os. Read mounts are plain fs.FS values the composition root
// chooses.
package storage
