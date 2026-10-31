//go:build !ecs_validate

package internal

import "unsafe"

// validate is this build's answer to whether the List write check exists at
// all, and it is a constant so that the answer is "no" in the strongest sense
// available: every `if validate` block in the package is unreachable, the
// compiler removes it, and the binary contains no branch, no table and no load
// for any of it. A Query's fill path is byte for byte the one measured in the
// spec.
//
// Build with -tags ecs_validate for the other file, which is where the check,
// its cost and its gaps are described.
const validate = false

// runToken is what a Query stamps its rows with, so a write through a value a
// finished run handed out can be told from a write through a live one. It is
// empty here, a Query's field for it is nil, and the no-op methods below cost
// the run nothing.
type runToken struct{}

func newRunToken(string) *runToken { return nil }

func (*runToken) begin() {}
func (*runToken) end()   {}

func stampStored(unsafe.Pointer, []listSite, string)                   {}
func stampRun(unsafe.Pointer, []listSite, string, *runToken, listMode) {}
func stampHook(unsafe.Pointer, []listSite, string, string, string)     {}
func holdLists(*storeHeader, uintptr, []Entity)                        {}
func releaseLists(*storeHeader, Entity)                                {}
func releaseEntityLists(Entity)                                        {}

// hookUnder is the kind set a delivered Hook carries for IsX to check, which a
// release build does not, so it is zero-size and a Hook is as wide as it was.
type hookUnder = struct{}

func underOf(hookKind) hookUnder        { return hookUnder{} }
func deliveredUnder(hookUnder) hookKind { return 0 }
