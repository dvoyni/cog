// Package animimpl is the anim plugin: New, and the handler behind anim's
// AdvanceOnUpdate subscription that advances every timeline each tick. Only
// composition roots and tests import it; everything else reaches anim through
// its contract root.
//
// The plugin requires no Adapter and contributes none.
package animimpl
