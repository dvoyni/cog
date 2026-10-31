// Package internal is the input plugin: New, the handlers behind input's
// commands and its AdvanceOnUpdate subscription, and the mcp Provider offering
// input_send and input_state. Composition roots and tests reach New through
// inputplugin; everything else reaches input through its root.
//
// The plugin requires no Adapter. It contributes one mcp.Provider.
package internal
