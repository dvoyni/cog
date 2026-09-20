//go:build !js

package overrides

// readCogEnv finds nothing off the browser. COG_ENV exists because a page has
// neither a command line nor an environment to put an override in; a desktop
// has both, and a packed string there would be a third spelling of what the
// other two already say.
func readCogEnv() string { return "" }
