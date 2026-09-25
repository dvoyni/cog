package internal

import "github.com/dvoyni/cog/kernel"

// PermanentFSPort is the Port storage requires exactly one Adapter for: the
// permanent filesystem an Extension such as diskstorage or jsstorage provides.
// A composition without one fails with kernel.ErrMissingAdapter.
type PermanentFSPort kernel.RequiredPort[PermanentFS]

// ReadMountPort is the Port storage collects every read mount through, zero
// included. It is built on ReadMount itself, since a mount is plain data: any
// plugin, a game's own included, declares an Adapter for it and contributes
// its mounts during Register, one ProvideAdapter call per mount:
//
//	type StorageReadMount kernel.Adapter[storage.ReadMountPort]
//
//	registrar.ProvideAdapter[StorageReadMount](storage.ReadMount{
//		Id: "res", Priority: storage.DefaultReadPriority, FS: res,
//	})
//
// storage installs them at its Start, so they are in FileSystem before any
// plugin that depends on storage starts. A mount with an empty id or a nil FS
// fails that Start with ErrInvalidMount, PermanentMount with ErrReservedMount,
// and an id contributed twice with ErrDuplicateMount.
type ReadMountPort kernel.CollectedPort[ReadMount]
