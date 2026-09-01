package storage

import (
	"github.com/dvoyni/cog/kernel"
)

func registerCommands(registrar *kernel.Registrar) {
	registrar.HandleCommand[SetReadFSCmd](setReadFS)
	registrar.HandleCommand[RemoveReadFSCmd](removeReadFS)
	registrar.HandleCommand[SetWriteFSCmd](setWriteFS)
	registrar.HandleCommand[GetValueCmd](getValue)
	registrar.HandleCommand[SetValueCmd](setValue)
	registrar.HandleCommand[DeleteValueCmd](deleteValue)
	registrar.HandleCommand[FlushValuesCmd](flushValues)
}

func setReadFS() (kernel.Lock, kernel.Execute[SetReadFSRequest, SetReadFSResponse]) {
	var readFS kernel.Write[ReadFS]
	return func(access kernel.ResourceAccess) {
			readFS = access.GetWrite[ReadFS]()
		}, func(_ kernel.Kernel, request SetReadFSRequest) (SetReadFSResponse, error) {
			if request.Mount.Id == "" || request.Mount.FS == nil {
				return SetReadFSResponse{}, ErrInvalidMount{Id: request.Mount.Id}
			}
			readFS.Set(readFS.Get().withMount(request.Mount))
			return SetReadFSResponse{}, nil
		}
}

func removeReadFS() (kernel.Lock, kernel.Execute[RemoveReadFSRequest, RemoveReadFSResponse]) {
	var readFS kernel.Write[ReadFS]
	return func(access kernel.ResourceAccess) {
			readFS = access.GetWrite[ReadFS]()
		}, func(_ kernel.Kernel, request RemoveReadFSRequest) (RemoveReadFSResponse, error) {
			current := readFS.Get()
			_, found := current.mount(request.Id)
			if found {
				readFS.Set(current.withoutMount(request.Id))
			}
			return RemoveReadFSResponse{Removed: found}, nil
		}
}

func setWriteFS() (kernel.Lock, kernel.Execute[SetWriteFSRequest, SetWriteFSResponse]) {
	var writeFS kernel.Write[WriteFS]
	return func(access kernel.ResourceAccess) {
			writeFS = access.GetWrite[WriteFS]()
		}, func(_ kernel.Kernel, request SetWriteFSRequest) (SetWriteFSResponse, error) {
			if request.FS == nil {
				return SetWriteFSResponse{}, ErrInvalidWriteFS{}
			}
			writeFS.Set(request.FS)
			return SetWriteFSResponse{}, nil
		}
}

func getValue() (kernel.Lock, kernel.Execute[GetValueRequest, GetValueResponse]) {
	var readFS kernel.Read[ReadFS]
	var values kernel.Write[Values]
	return func(access kernel.ResourceAccess) {
			readFS = access.GetRead[ReadFS]()
			values = access.GetWrite[Values]()
		}, func(_ kernel.Kernel, request GetValueRequest) (GetValueResponse, error) {
			if request.key == "" {
				return GetValueResponse{}, ErrInvalidKey{}
			}
			if request.apply == nil {
				return GetValueResponse{}, ErrInvalidValueRequest{Key: request.key}
			}
			store, err := values.Get().load(readFS.Get())
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

func setValue() (kernel.Lock, kernel.Execute[SetValueRequest, SetValueResponse]) {
	var readFS kernel.Read[ReadFS]
	var writeFS kernel.Read[WriteFS]
	var values kernel.Write[Values]
	return func(access kernel.ResourceAccess) {
			readFS = access.GetRead[ReadFS]()
			writeFS = access.GetRead[WriteFS]()
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
			store, err := values.Get().load(readFS.Get())
			if err != nil {
				return SetValueResponse{}, err
			}
			store.entries[request.key] = raw
			store.dirty = true
			if !request.skipFlush {
				store, err = store.flush(writeFS.Get())
			}
			values.Set(store)
			return SetValueResponse{}, err
		}
}

func deleteValue() (kernel.Lock, kernel.Execute[DeleteValueRequest, DeleteValueResponse]) {
	var readFS kernel.Read[ReadFS]
	var writeFS kernel.Read[WriteFS]
	var values kernel.Write[Values]
	return func(access kernel.ResourceAccess) {
			readFS = access.GetRead[ReadFS]()
			writeFS = access.GetRead[WriteFS]()
			values = access.GetWrite[Values]()
		}, func(_ kernel.Kernel, request DeleteValueRequest) (DeleteValueResponse, error) {
			if request.Key == "" {
				return DeleteValueResponse{}, ErrInvalidKey{}
			}
			store, err := values.Get().load(readFS.Get())
			if err != nil {
				return DeleteValueResponse{}, err
			}
			_, existed := store.entries[request.Key]
			if existed {
				delete(store.entries, request.Key)
				store.dirty = true
			}
			if !request.SkipFlush {
				store, err = store.flush(writeFS.Get())
			}
			values.Set(store)
			return DeleteValueResponse{Existed: existed}, err
		}
}

func flushValues() (kernel.Lock, kernel.Execute[FlushValuesRequest, FlushValuesResponse]) {
	var writeFS kernel.Read[WriteFS]
	var values kernel.Write[Values]
	return func(access kernel.ResourceAccess) {
			writeFS = access.GetRead[WriteFS]()
			values = access.GetWrite[Values]()
		}, func(_ kernel.Kernel, _ FlushValuesRequest) (FlushValuesResponse, error) {
			store, err := values.Get().flush(writeFS.Get())
			values.Set(store)
			return FlushValuesResponse{}, err
		}
}
