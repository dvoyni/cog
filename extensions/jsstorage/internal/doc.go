// Package internal is the jsstorage plugin: New, the application id check, and
// the localStorage-backed filesystem it provides as storage's PermanentFS.
// Composition roots and tests reach New through jsstorageplugin.
//
// The plugin is built only for GOOS=js. It requires no Adapter and provides
// one, jsstorage.StoragePermanentFS.
package internal
