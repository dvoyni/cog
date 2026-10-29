// Package noplatform declares the storefront-less Extension of platform: a
// plugin that fills platform's BackendPort, accepts every unlock and stat and
// sends them nowhere. It is for builds that ship on no storefront - the web
// build, a plain desktop download - and for tests.
//
// It reports no language, so a game falls back to its own default.
//
// noplatform is an Extension. Its root offers only Name and its one Adapter,
// PlatformBackend, each an alias of what its internal/ declares; the plugin,
// built by noplatformplugin.New, is in its internal/ too. It is built for every
// platform and takes no Config.
//
//	plugins := []kernel.Plugin{
//	    platformplugin.New(),
//	    noplatformplugin.New(),
//	    …
//	}
package noplatform
