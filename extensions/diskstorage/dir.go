//go:build !js

package diskstorage

import "github.com/dvoyni/cog/extensions/diskstorage/internal"

// Dir is the directory the plugin keeps its permanent filesystem in when
// configured with config, for what a program writes there before, or outside,
// an engine: a log opened ahead of composition. It does not create it.
func Dir(config Config) (string, error) { return internal.Dir(config) }
