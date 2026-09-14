// Package internal is the diskstorage plugin: New, the application id and data
// directory resolution, and the confined directory it provides as storage's
// PermanentFS. Composition roots and tests reach New through diskstorageplugin.
//
// The plugin is built only for desktop platforms (!js). It requires no Adapter
// and provides one, diskstorage.StoragePermanentFS.
package internal
