// Package internal is the diskfs plugin: New, the application id and data
// directory resolution, and the confined directory it provides as storage's
// PermanentFS. Composition roots and tests reach New through diskfsplugin.
//
// The plugin is built only for desktop platforms (!js). It requires no Adapter
// and provides one, diskfs.StoragePermanentFS.
package internal
