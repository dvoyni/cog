package internal

import (
	"github.com/dvoyni/cog/kernel"
)

func registerCommands(registrar *kernel.Registrar) {
	registrar.HandleCommand[SetMountCmd](setMountCmdImpl)
	registrar.HandleCommand[RemoveMountCmd](removeMountCmdImpl)
	registrar.HandleCommand[AccessValuesCmd](accessValuesCmdImpl)
}

func setMountCmdImpl() (kernel.Lock, kernel.Execute[SetMountRequest, SetMountResponse]) {
	var filesystem kernel.Write[FileSystem]
	return func(access kernel.ResourceAccess) {
			filesystem = access.GetWrite[FileSystem]()
		}, func(_ kernel.Kernel, request SetMountRequest) SetMountResponse {
			if request.Mount.Id == "" || request.Mount.FS == nil {
				return SetMountResponse{Err: ErrInvalidMount{Id: request.Mount.Id}}
			}
			if request.Mount.Id == PermanentMount {
				return SetMountResponse{Err: ErrReservedMount{Id: request.Mount.Id}}
			}
			filesystem.Set(FileSystemWithMount(filesystem.Get(), request.Mount))
			return SetMountResponse{}
		}
}

func removeMountCmdImpl() (kernel.Lock, kernel.Execute[RemoveMountRequest, RemoveMountResponse]) {
	var filesystem kernel.Write[FileSystem]
	return func(access kernel.ResourceAccess) {
			filesystem = access.GetWrite[FileSystem]()
		}, func(_ kernel.Kernel, request RemoveMountRequest) RemoveMountResponse {
			if request.Id == PermanentMount {
				return RemoveMountResponse{Err: ErrReservedMount{Id: request.Id}}
			}
			current := filesystem.Get()
			_, found := FileSystemMount(current, request.Id)
			if found {
				filesystem.Set(FileSystemWithoutMount(current, request.Id))
			}
			return RemoveMountResponse{Removed: found}
		}
}

// accessValuesCmdImpl takes one write lock on FileSystem for every operation, reads
// included: a read populates the value cache, and the same handle both reads
// the values file through the overlay and, via WriteAccess, flushes it back.
func accessValuesCmdImpl() (kernel.Lock, kernel.Execute[AccessValuesRequest, AccessValuesResponse]) {
	var filesystem kernel.Write[FileSystem]
	var values kernel.Write[Values]
	return func(access kernel.ResourceAccess) {
			filesystem = access.GetWrite[FileSystem]()
			values = access.GetWrite[Values]()
		}, func(_ kernel.Kernel, request AccessValuesRequest) AccessValuesResponse {
			store, response, err := ApplyValues(request, values.Get(), filesystem.Get())
			values.Set(store)
			response.Err = err
			return response
		}
}
