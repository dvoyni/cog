package storage

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
)

func registerCommands(registrar *kernel.Registrar) {
	registrar.HandleCommand[SetReadFSCmd](setReadFS)
	registrar.HandleCommand[RemoveReadFSCmd](removeReadFS)
	registrar.HandleCommand[SetWriteFSCmd](setWriteFS)
	registrar.HandleCommand[GetReadFSCmd](getReadFS)
	registrar.HandleCommand[ReadFileCmd](readFile)
	registrar.HandleCommand[WriteFileCmd](writeFile)
	registrar.HandleCommand[MkdirAllCmd](mkdirAll)
	registrar.HandleCommand[RemoveCmd](remove)
	registrar.HandleCommand[RenameCmd](rename)
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

func getReadFS() (kernel.Lock, kernel.Execute[GetReadFSRequest, GetReadFSResponse]) {
	var readFS kernel.Read[ReadFS]
	return func(access kernel.ResourceAccess) {
			readFS = access.GetRead[ReadFS]()
		}, func(_ kernel.Kernel, request GetReadFSRequest) (GetReadFSResponse, error) {
			if request.Use == nil {
				return GetReadFSResponse{}, ErrInvalidReadFSCallback{}
			}
			return GetReadFSResponse{}, request.Use(readFS.Get())
		}
}

func readFile() (kernel.Lock, kernel.Execute[ReadFileRequest, ReadFileResponse]) {
	var readFS kernel.Read[ReadFS]
	return func(access kernel.ResourceAccess) {
			readFS = access.GetRead[ReadFS]()
		}, func(_ kernel.Kernel, request ReadFileRequest) (ReadFileResponse, error) {
			data, err := fs.ReadFile(readFS.Get(), request.Name)
			return ReadFileResponse{Data: data}, err
		}
}

func writeFile() (kernel.Lock, kernel.Execute[WriteFileRequest, WriteFileResponse]) {
	var writeFS kernel.Write[WriteFS]
	return func(access kernel.ResourceAccess) {
			writeFS = access.GetWrite[WriteFS]()
		}, func(_ kernel.Kernel, request WriteFileRequest) (WriteFileResponse, error) {
			return WriteFileResponse{}, writeFS.Get().WriteFile(request.Name, request.Data, request.Perm)
		}
}

func mkdirAll() (kernel.Lock, kernel.Execute[MkdirAllRequest, MkdirAllResponse]) {
	var writeFS kernel.Write[WriteFS]
	return func(access kernel.ResourceAccess) {
			writeFS = access.GetWrite[WriteFS]()
		}, func(_ kernel.Kernel, request MkdirAllRequest) (MkdirAllResponse, error) {
			return MkdirAllResponse{}, writeFS.Get().MkdirAll(request.Path, request.Perm)
		}
}

func remove() (kernel.Lock, kernel.Execute[RemoveRequest, RemoveResponse]) {
	var writeFS kernel.Write[WriteFS]
	return func(access kernel.ResourceAccess) {
			writeFS = access.GetWrite[WriteFS]()
		}, func(_ kernel.Kernel, request RemoveRequest) (RemoveResponse, error) {
			return RemoveResponse{}, writeFS.Get().Remove(request.Name)
		}
}

func rename() (kernel.Lock, kernel.Execute[RenameRequest, RenameResponse]) {
	var writeFS kernel.Write[WriteFS]
	return func(access kernel.ResourceAccess) {
			writeFS = access.GetWrite[WriteFS]()
		}, func(_ kernel.Kernel, request RenameRequest) (RenameResponse, error) {
			return RenameResponse{}, writeFS.Get().Rename(request.OldName, request.NewName)
		}
}
