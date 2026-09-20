package config

import "github.com/dvoyni/cog/kernel"

// Assignment is one override: the plugin whose Config it names, the field of
// that Config, and the text the field is set from.
//
// Bare records that the source gave a key with no value at all, which only a
// bool field accepts. Origin is the override as it was written - the argument,
// the variable name, the segment of COG_ENV - so that a failure can quote what
// the caller typed rather than a normalised echo of it.
type Assignment struct {
	Plugin kernel.PluginName
	Field  string
	Value  string
	Bare   bool
	Origin string
}

// Overlay is what a source produces, in the order it produced it. A source
// never writes to a config map: it yields an Overlay and Apply consumes it, so
// a source added later is a new producer here rather than a change to the
// applier.
//
// Applying walks the Overlay in order, so a field assigned twice keeps the
// last assignment. Stacking the sources lowest-precedence first is therefore
// all the precedence there is.
type Overlay []Assignment
