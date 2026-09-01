package storage

import (
	"github.com/dvoyni/cog/kernel"
)

func registerCommands(registrar *kernel.Registrar) {
	registrar.HandleCommand[SetReadFSCmd](setReadFS)
	registrar.HandleCommand[RemoveReadFSCmd](removeReadFS)
	registrar.HandleCommand[SetWriteFSCmd](setWriteFS)
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
