//go:build !js

// Package otosound is the backend that gives sfx sound on desktop: a plugin
// binding sfx.BackendSocket over ebitengine/oto/v3, in pure Go. A game lists
// it beside sfx, configured with a *Config under PluginName, and it opens the
// device when sfx starts, never failing the game on one.
//
// oto's context is the process's: the first kernel's otosound makes it, and
// every kernel after takes its own player on the same context, the first
// one's rate and buffer size in force for all. The context is never closed;
// each kernel's player is closed in its Destroy.
//
// Its files build for every target but js, so a web build composing otosound
// fails to compile rather than ship a mixer that starves.
//
// See docs/design.md.
package otosound
