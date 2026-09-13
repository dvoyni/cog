// Package internal holds what storage's contract root and storageimpl share but
// no consumer may reach: the declarations of FileSystem, WriteFS, Values and the
// value requests, whose unexported state the plugin's handlers read, and the
// types those refer to.
//
// A contract type storageimpl reads the insides of is declared here, with its
// fields unexported, and aliased in the root (type FileSystem =
// internal.FileSystem). It stays a concrete type, and its exported methods are
// storage's public API through the alias. What the root and storageimpl need
// beyond that goes through the plain functions this package exports, which only
// they can call: Go allows nothing outside extensions/storage to import it.
// Nothing declared here imports the root, which keeps the arrangement acyclic.
//
// Like the rest of storage, it carries no platform code: no build tags and no
// os. What persists is the PermanentFS Adapter's business.
package internal
