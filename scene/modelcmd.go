package scene

import (
	"io/fs"
	"path"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/storage"
	"github.com/qmuntal/gltf"
)

// loadModelCmd parses one glTF file into scene's own types. It is the first of
// the load's two hops and holds no Lookup and no GPU queue: everything it does
// is CPU work over a file, so a 47 MiB model or a 24-joint three-clip rig
// costs the frame nothing.
//
// It does hold the filesystem's read lock for the length of the parse, which
// the kernel grants a command for the whole of its body. That is a shared lock
// nothing in scene's flush takes, so it blocks a filesystem writer and nothing
// else; the two locks a frame actually needs are held by the second hop alone,
// for the length of the upload.
//
// The command is dispatched with ExecuteCommandAsync and has no response.
// Completion arrives as the second hop, which is also how a failure reaches the
// error handler - from this goroutine, a frame or more after the draw that
// asked for it.
type loadModelCmd kernel.Command[loadModelRequest, loadModelResponse]

type loadModelRequest struct {
	Path string
	// Generation is the entry's counter at the moment the load was enqueued.
	// The install compares it, so an unload while this parse was running makes
	// the result discard itself rather than become resident as a ghost.
	Generation uint32
}

type loadModelResponse struct{}

// installModelCmd takes a completed parse into residency. It is the second hop,
// and the only one that holds the Lookup and the resource queue - for the
// length of the upload, with no parsing and no decoding inside it.
type installModelCmd kernel.Command[installModelRequest, installModelResponse]

type installModelRequest struct {
	Path       string
	Generation uint32
	// Model is nil exactly when Err is not: a parse either produced a model or
	// the reason it could not.
	Model *loadedModel
	Err   error
}

type installModelResponse struct{}

func loadModelCmdImpl() (kernel.Lock, kernel.Execute[loadModelRequest, loadModelResponse]) {
	var filesystem kernel.Read[storage.FileSystem]
	return func(access kernel.ResourceAccess) {
			filesystem = access.GetRead[storage.FileSystem]()
		}, func(k kernel.Kernel, request loadModelRequest) (loadModelResponse, error) {
			model, err := parseModel(filesystem.Get(), request.Path)
			k.ExecuteCommandAsync[installModelCmd](installModelRequest{
				Path: request.Path, Generation: request.Generation, Model: model, Err: err,
			})
			return loadModelResponse{}, nil
		}
}

// parseModel opens, decodes and converts one file. Everything it returns is
// scene's own types: the gltf.Document is dropped here, so it never appears in
// scene's API and never outlives the load that read it.
func parseModel(filesystem storage.FileSystem, modelPath string) (*loadedModel, error) {
	file, err := filesystem.Open(modelPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	// The decoder resolves a .gltf file's external buffers against the model's
	// own directory, which is what glTF's relative URIs are relative to. Images
	// are not the decoder's business, so the texture loader resolves those
	// itself, against the whole filesystem and the full storage path.
	decoder := gltf.NewDecoderFS(file, directoryFS(filesystem, path.Dir(modelPath)))
	document := new(gltf.Document)
	if err := decoder.Decode(document); err != nil {
		return nil, err
	}
	return convertDocument(document, modelPath, filesystem)
}

// directoryFS presents one directory of the filesystem as its own root, which
// is the shape the glTF decoder wants for relative URIs. A directory that
// cannot be subsetted - "." at the root - is the filesystem itself.
func directoryFS(filesystem storage.FileSystem, dir string) fs.FS {
	if dir == "" || dir == "." {
		return filesystem
	}
	sub, err := fs.Sub(filesystem, dir)
	if err != nil {
		return filesystem
	}
	return sub
}

func installModelCmdImpl() (kernel.Lock, kernel.Execute[installModelRequest, installModelResponse]) {
	var lookup kernel.Write[*Lookup]
	var resources kernel.Write[*gfx.ResourceQueue]
	return func(access kernel.ResourceAccess) {
			lookup = access.GetWrite[*Lookup]()
			resources = access.GetWrite[*gfx.ResourceQueue]()
		}, func(k kernel.Kernel, request installModelRequest) (installModelResponse, error) {
			lookup.Get().installModel(
				func(err error) { k.ReportError(err) }, request.Path, request.Generation,
				request.Model, request.Err, resources.Get(),
			)
			return installModelResponse{}, nil
		}
}
