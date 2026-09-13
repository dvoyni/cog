package storage

import (
	"github.com/dvoyni/cog/kernel"
)

func registerCommands(registrar *kernel.Registrar) {
	registrar.HandleCommand[SetMountCmd](setMountCmdImpl)
	registrar.HandleCommand[RemoveMountCmd](removeMountCmdImpl)
	registrar.HandleCommand[SetPermanentFSCmd](setPermanentFSCmdImpl)
	registrar.HandleCommand[AccessValuesCmd](accessValuesCmdImpl)
}

func setMountCmdImpl() (kernel.Lock, kernel.Execute[SetMountRequest, SetMountResponse]) {
	var filesystem kernel.Write[FileSystem]
	return func(access kernel.ResourceAccess) {
			filesystem = access.GetWrite[FileSystem]()
		}, func(_ kernel.Kernel, request SetMountRequest) (SetMountResponse, error) {
			if request.Mount.Id == "" || request.Mount.FS == nil {
				return SetMountResponse{}, ErrInvalidMount{Id: request.Mount.Id}
			}
			if request.Mount.Id == PermanentMount {
				return SetMountResponse{}, ErrReservedMount{Id: request.Mount.Id}
			}
			filesystem.Set(filesystem.Get().withMount(request.Mount))
			return SetMountResponse{}, nil
		}
}

func removeMountCmdImpl() (kernel.Lock, kernel.Execute[RemoveMountRequest, RemoveMountResponse]) {
	var filesystem kernel.Write[FileSystem]
	return func(access kernel.ResourceAccess) {
			filesystem = access.GetWrite[FileSystem]()
		}, func(_ kernel.Kernel, request RemoveMountRequest) (RemoveMountResponse, error) {
			if request.Id == PermanentMount {
				return RemoveMountResponse{}, ErrReservedMount{Id: request.Id}
			}
			current := filesystem.Get()
			_, found := current.mount(request.Id)
			if found {
				filesystem.Set(current.withoutMount(request.Id))
			}
			return RemoveMountResponse{Removed: found}, nil
		}
}

func setPermanentFSCmdImpl() (kernel.Lock, kernel.Execute[SetPermanentFSRequest, SetPermanentFSResponse]) {
	var filesystem kernel.Write[FileSystem]
	return func(access kernel.ResourceAccess) {
			filesystem = access.GetWrite[FileSystem]()
		}, func(_ kernel.Kernel, request SetPermanentFSRequest) (SetPermanentFSResponse, error) {
			if request.FS == nil {
				return SetPermanentFSResponse{}, ErrInvalidPermanentFS{}
			}
			filesystem.Set(filesystem.Get().withPermanent(request.FS))
			return SetPermanentFSResponse{}, nil
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
		}, func(_ kernel.Kernel, request AccessValuesRequest) (AccessValuesResponse, error) {
			if request.op == nil {
				return AccessValuesResponse{}, ErrInvalidValueRequest{}
			}
			if err := request.op.validate(); err != nil {
				return AccessValuesResponse{}, err
			}
			store, response, err := request.op.apply(values.Get(), filesystem.Get())
			if err == nil && request.op.flushes() {
				store, err = store.flush(WriteAccess(filesystem))
			}
			// Cached even on failure: apply returns whatever it managed to load, and
			// a failed flush leaves changes pending for the next one.
			values.Set(store)
			if err != nil {
				return AccessValuesResponse{}, err
			}
			return response, nil
		}
}
