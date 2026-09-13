package gpu

import "strconv"

// The Name methods on this package's enums spell a value for a message or a
// debug document: gfx's frame snapshot and its view types read them. They are
// Name rather than String on purpose, so that formatting an enum with %v still
// prints its number and nothing else changes spelling by accident. An unknown
// value names its ordinal rather than falling back to a legal-looking name, so
// a member added without a name is visible instead of mislabelled.

// unknownName spells a value no table names, so an enum that grew reads as
// unknown rather than as whichever name happened to be last.
func unknownName(value int) string {
	return "unknown(" + strconv.Itoa(value) + ")"
}
