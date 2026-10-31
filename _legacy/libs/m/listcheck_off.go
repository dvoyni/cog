//go:build !ecs_validate

package m

import "unsafe"

// listSetChecked is false outside the ECS's validation mode, and a constant, so
// the `if listSetChecked` in [List.Set] is unreachable and the compiler removes
// it. Build with -tags ecs_validate for the other file.
const listSetChecked = false

func checkListSet(unsafe.Pointer) {}
