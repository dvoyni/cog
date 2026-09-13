// Package inputimpl is the input plugin: New, the handlers behind input's
// commands and its AdvanceOnUpdate subscription, and the mcp Provider offering
// input_send and input_state. Only composition roots and tests import it;
// everything else reaches input through its contract root.
//
// The plugin requires no Adapter. It contributes one mcp.Provider.
package inputimpl
