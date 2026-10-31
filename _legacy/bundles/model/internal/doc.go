// Package internal is the model plugin: New, the resolution of model.Config,
// and the registration of the *model.Lookup resource every renderer draws
// from. Composition roots and tests reach New through modelplugin; everything
// else reaches model through its root.
//
// The plugin requires no Adapter and contributes none.
package internal
