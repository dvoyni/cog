package storage

import "github.com/dvoyni/cog/slots/storage/internal"

// FileSystem is the single storage resource. It reads through a prioritized
// overlay covering every mounted filesystem, including the permanent one, so a
// caller never chooses a filesystem to read from. It exposes no mutators at
// all: changing files means holding a write lock and passing the handle to
// WriteAccess. Access it only while a handler holds its declared resource lock;
// do not retain the resource or files opened from it after the handler returns.
type FileSystem = internal.FileSystem

// Values is the key-value store backing the value commands. It caches the
// values file after the first read, so every later operation is in memory until
// a flush. Access it only while a handler holds its declared resource lock;
// because a read also populates the cache, handlers declare write access.
type Values = internal.Values
