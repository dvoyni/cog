// Package internal is the jsfs plugin: New, the application id check, and the
// localStorage-backed filesystem it provides as storage's PermanentFS.
// Composition roots and tests reach New through jsfsplugin.
//
// The plugin is built only for GOOS=js. It requires no Adapter and provides
// one, jsfs.StoragePermanentFS.
package internal
