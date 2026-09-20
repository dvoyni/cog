// Package nosound declares the silent Extension of sound: a plugin that fills
// sound's BackendPort, accepts everything and plays nothing. It is for headless
// engines, tests and CI, and it is cog's first shipped no-op Adapter - and
// therefore a precedent as well as an Adapter: fill the Port, keep what the
// contract must be able to report, keep nothing else.
//
// It records nothing. Every start, update, stop and destroy is accepted and
// kept nowhere, because the live Voice view is sound's own and is
// Adapter-independent: an agent or a test gets the same answer whatever was
// composed, and putting the view in the silent Adapter would give it only to
// the configuration that least needs it.
//
// But it cannot install nothing, because a Voice waiting on its Clip starts
// when that Clip installs - so if nosound installed nothing, no Voice under it
// would ever play. Prepare therefore reads the Ogg headers and the stream
// length and decodes no samples, keeping sample rate, channels and duration.
// Without a real duration ReasonFinished never fires, the closure property has
// a hole, and a test cannot assert that a one-shot ended, which is most of what
// a test of sound wants to say. A header is not a recording of what played, so
// "records nothing" survives intact.
//
// It reports Ready true: it is a working Device that plays nothing, not a
// missing one, so a game gating a "click to enable sound" prompt on Ready does
// not hang forever under it and a test does not special-case it. Latency is
// zero, which is true rather than a placeholder - nothing is buffered.
//
// nosound is an Extension. Its root declares only Name, Config, its one
// Adapter, SoundBackend, and its errors; the plugin, built by nosoundplugin.New,
// is in its internal/. It is built for every platform. Its Config is supplied
// under Name.
//
//	config := map[kernel.PluginName]any{
//	    sound.Name:   sound.Config{},
//	    nosound.Name: nosound.Config{},
//	}
//	plugins := []kernel.Plugin{
//	    soundplugin.New(),
//	    nosoundplugin.New(),
//	    …
//	}
package nosound
