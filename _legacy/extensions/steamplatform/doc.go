// Package steamplatform declares the Steam Extension of platform: a plugin that
// fills platform's BackendPort over the Steamworks flat API, bound with purego
// so the build stays cgo-free. It reports achievements and int32 stats to Steam
// and answers the language the player runs the game in, and opens Steam's
// floating keyboard on a Steam Deck or in Big Picture mode.
//
// It needs the Steam library beside the executable: steam_api64.dll on
// Windows, libsteam_api.so on Linux, libsteam_api.dylib on macOS. The SDK 1.65
// redistributables are in redist/, for a game's build to copy. Steam is opened
// at Register, before any window exists, so the overlay can hook the graphics
// device.
//
// A release build started outside Steam fails composition with ErrRelaunching
// while Steam starts it again; a game exits quietly on it. A steam_appid.txt
// holding the app id beside the executable turns that off for development.
// When Steam cannot be reached at all - no library, no client running - the
// game runs as if under noplatform, and ErrUnavailable says why once.
//
// Nothing is read back from Steam: no callback is handled, so no Steam struct
// layout is mirrored here. Cloud saves need no code either: they are Steam
// Auto-Cloud, configured on the Steamworks site.
//
// steamplatform is an Extension. Its root offers only Name, Config, its one
// Adapter, PlatformBackend, and its errors. The plugin, built by
// steamplatformplugin.New, builds for Windows, Linux and macOS.
//
//	config := map[kernel.PluginName]any{
//	    steamplatform.Name: steamplatform.Config{AppID: 480},
//	}
//	plugins := []kernel.Plugin{
//	    platformplugin.New(),
//	    steamplatformplugin.New(),
//	    ...
//	}
package steamplatform
