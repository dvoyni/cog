package storageimpl

import (
	"github.com/dvoyni/cog/extensions/storage"
	"github.com/dvoyni/cog/extensions/storage/internal"
	"github.com/dvoyni/cog/kernel"
)

func registerCommands(registrar *kernel.Registrar) {
	registrar.HandleCommand[storage.SetMountCmd](setMountCmdImpl)
	registrar.HandleCommand[storage.RemoveMountCmd](removeMountCmdImpl)
	registrar.HandleCommand[storage.AccessValuesCmd](accessValuesCmdImpl)
}

func setMountCmdImpl() (kernel.Lock, kernel.Execute[storage.SetMountRequest, storage.SetMountResponse]) {
	var filesystem kernel.Write[storage.FileSystem]
	return func(access kernel.ResourceAccess) {
			filesystem = access.GetWrite[storage.FileSystem]()
		}, func(_ kernel.Kernel, request storage.SetMountRequest) (storage.SetMountResponse, error) {
			if request.Mount.Id == "" || request.Mount.FS == nil {
				return storage.SetMountResponse{}, storage.ErrInvalidMount{Id: request.Mount.Id}
			}
			if request.Mount.Id == storage.PermanentMount {
				return storage.SetMountResponse{}, storage.ErrReservedMount{Id: request.Mount.Id}
			}
			filesystem.Set(internal.FileSystemWithMount(filesystem.Get(), request.Mount))
			return storage.SetMountResponse{}, nil
		}
}

func removeMountCmdImpl() (kernel.Lock, kernel.Execute[storage.RemoveMountRequest, storage.RemoveMountResponse]) {
	var filesystem kernel.Write[storage.FileSystem]
	return func(access kernel.ResourceAccess) {
			filesystem = access.GetWrite[storage.FileSystem]()
		}, func(_ kernel.Kernel, request storage.RemoveMountRequest) (storage.RemoveMountResponse, error) {
			if request.Id == storage.PermanentMount {
				return storage.RemoveMountResponse{}, storage.ErrReservedMount{Id: request.Id}
			}
			current := filesystem.Get()
			_, found := internal.FileSystemMount(current, request.Id)
			if found {
				filesystem.Set(internal.FileSystemWithoutMount(current, request.Id))
			}
			return storage.RemoveMountResponse{Removed: found}, nil
		}
}

// accessValuesCmdImpl takes one write lock on FileSystem for every operation, reads
// included: a read populates the value cache, and the same handle both reads
// the values file through the overlay and, via WriteAccess, flushes it back.
func accessValuesCmdImpl() (kernel.Lock, kernel.Execute[storage.AccessValuesRequest, storage.AccessValuesResponse]) {
	var filesystem kernel.Write[storage.FileSystem]
	var values kernel.Write[storage.Values]
	return func(access kernel.ResourceAccess) {
			filesystem = access.GetWrite[storage.FileSystem]()
			values = access.GetWrite[storage.Values]()
		}, func(_ kernel.Kernel, request storage.AccessValuesRequest) (storage.AccessValuesResponse, error) {
			store, response, err := internal.ApplyValues(request, values.Get(), filesystem.Get())
			values.Set(store)
			return response, err
		}
}
