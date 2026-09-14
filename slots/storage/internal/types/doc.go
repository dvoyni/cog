// Package types declares the concrete types storage's root aliases:
// FileSystem, WriteFS, Values and the value requests, whose unexported state
// the plugin's handlers read, and the types those refer to, PermanentFS
// included.
//
// Each is declared here with its fields unexported and aliased in the root
// (type FileSystem = types.FileSystem). It stays a concrete type, and its
// exported methods are storage's public API through the alias. What the root's
// forwarders and storage's internal/ need beyond that goes through the plain
// functions this package exports: Go allows nothing outside slots/storage to
// import it. Nothing declared here imports the root, which keeps the
// arrangement acyclic.
//
// Like the rest of storage, it carries no platform code: no build tags and no
// os. What persists is the PermanentFS Adapter's business.
package types
