package storage

import (
	"github.com/dvoyni/cog/kernel"
)

func registerCommands(registrar *kernel.Registrar) {
	registrar.HandleCommand[SetMountCmd](setMount)
	registrar.HandleCommand[RemoveMountCmd](removeMount)
	registrar.HandleCommand[SetPermanentFSCmd](setPermanentFS)
	registrar.HandleCommand[GetValueCmd](getValue)
	registrar.HandleCommand[SetValueCmd](setValue)
	registrar.HandleCommand[DeleteValueCmd](deleteValue)
	registrar.HandleCommand[FlushValuesCmd](flushValues)
}

func setMount() (kernel.Lock, kernel.Execute[SetMountRequest, SetMountResponse]) {
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

func removeMount() (kernel.Lock, kernel.Execute[RemoveMountRequest, RemoveMountResponse]) {
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

func setPermanentFS() (kernel.Lock, kernel.Execute[SetPermanentFSRequest, SetPermanentFSResponse]) {
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

func getValue() (kernel.Lock, kernel.Execute[GetValueRequest, GetValueResponse]) {
	var filesystem kernel.Read[FileSystem]
	var values kernel.Write[Values]
	return func(access kernel.ResourceAccess) {
			filesystem = access.GetRead[FileSystem]()
			values = access.GetWrite[Values]()
		}, func(_ kernel.Kernel, request GetValueRequest) (GetValueResponse, error) {
			if request.key == "" {
				return GetValueResponse{}, ErrInvalidKey{}
			}
			if request.apply == nil {
				return GetValueResponse{}, ErrInvalidValueRequest{Key: request.key}
			}
			store, err := values.Get().load(filesystem.Get())
			if err != nil {
				return GetValueResponse{}, err
			}
			values.Set(store)
			raw, found := store.entries[request.key]
			if !found {
				raw = nil
			}
			if err := request.apply(raw); err != nil {
				return GetValueResponse{}, err
			}
			return GetValueResponse{Found: found}, nil
		}
}

// setValue takes one write lock: the same handle reads the values file through
// the overlay and, via WriteAccess, flushes it back.
func setValue() (kernel.Lock, kernel.Execute[SetValueRequest, SetValueResponse]) {
	var filesystem kernel.Write[FileSystem]
	var values kernel.Write[Values]
	return func(access kernel.ResourceAccess) {
			filesystem = access.GetWrite[FileSystem]()
			values = access.GetWrite[Values]()
		}, func(_ kernel.Kernel, request SetValueRequest) (SetValueResponse, error) {
			if request.key == "" {
				return SetValueResponse{}, ErrInvalidKey{}
			}
			if request.marshal == nil {
				return SetValueResponse{}, ErrInvalidValueRequest{Key: request.key}
			}
			raw, err := request.marshal()
			if err != nil {
				return SetValueResponse{}, err
			}
			// Loading first keeps keys we have never read from being dropped by the flush.
			store, err := values.Get().load(filesystem.Get())
			if err != nil {
				return SetValueResponse{}, err
			}
			store.entries[request.key] = raw
			store.dirty = true
			if !request.skipFlush {
				store, err = store.flush(WriteAccess(filesystem))
			}
			values.Set(store)
			return SetValueResponse{}, err
		}
}

func deleteValue() (kernel.Lock, kernel.Execute[DeleteValueRequest, DeleteValueResponse]) {
	var filesystem kernel.Write[FileSystem]
	var values kernel.Write[Values]
	return func(access kernel.ResourceAccess) {
			filesystem = access.GetWrite[FileSystem]()
			values = access.GetWrite[Values]()
		}, func(_ kernel.Kernel, request DeleteValueRequest) (DeleteValueResponse, error) {
			if request.Key == "" {
				return DeleteValueResponse{}, ErrInvalidKey{}
			}
			store, err := values.Get().load(filesystem.Get())
			if err != nil {
				return DeleteValueResponse{}, err
			}
			_, existed := store.entries[request.Key]
			if existed {
				delete(store.entries, request.Key)
				store.dirty = true
			}
			if !request.SkipFlush {
				store, err = store.flush(WriteAccess(filesystem))
			}
			values.Set(store)
			return DeleteValueResponse{Existed: existed}, err
		}
}

func flushValues() (kernel.Lock, kernel.Execute[FlushValuesRequest, FlushValuesResponse]) {
	var filesystem kernel.Write[FileSystem]
	var values kernel.Write[Values]
	return func(access kernel.ResourceAccess) {
			filesystem = access.GetWrite[FileSystem]()
			values = access.GetWrite[Values]()
		}, func(_ kernel.Kernel, _ FlushValuesRequest) (FlushValuesResponse, error) {
			store, err := values.Get().flush(WriteAccess(filesystem))
			values.Set(store)
			return FlushValuesResponse{}, err
		}
}
