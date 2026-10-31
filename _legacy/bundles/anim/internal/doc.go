// Package internal is the anim plugin: New, and the handler behind anim's
// AdvanceOnUpdate subscription that advances every timeline each tick.
// Composition roots and tests reach New through animplugin; everything else
// reaches anim through its root.
//
// The plugin requires no Adapter and contributes none.
package internal
