//go:build ecs_validate

package m

import "unsafe"

// listSetChecked is true under the ECS's validation mode, so [List.Set] hands
// its backing array to ListSetCheck before it writes.
const listSetChecked = true

// ListSetCheck is the check [List.Set] makes under -tags ecs_validate, given the
// List's backing array, or nil for the zero List. The ECS installs it when its
// validation mode is built, because what a backing array may be written through
// is ECS knowledge: which Store holds it, and under which access mode it was
// last handed out. It exists only in a validating build, and a List written
// while it is nil is unchecked.
var ListSetCheck func(data unsafe.Pointer)

func checkListSet(data unsafe.Pointer) {
	if ListSetCheck != nil {
		ListSetCheck(data)
	}
}
