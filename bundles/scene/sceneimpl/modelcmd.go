package sceneimpl

import (
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal"
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
)

// installModelCmd takes a completed parse into residency. It is the second hop,
// and the only one that holds the Lookup and the resource queue - for the
// length of the upload, with no parsing and no decoding inside it.
type installModelCmd kernel.Command[installModelRequest, installModelResponse]

type installModelRequest struct {
	Path       string
	Generation uint32
	// Model is nil exactly when Err is not: a parse either produced a model or
	// the reason it could not.
	Model *internal.LoadedModel
	Err   error
}

type installModelResponse struct{}

// loadModelCmdImpl handles internal.LoadModelCmd, the parse, which the Lookup
// enqueues for a path that is not resident. It holds only the filesystem, and
// hands its result to the install whether the parse succeeded or not.
func loadModelCmdImpl() (kernel.Lock, kernel.Execute[internal.LoadModelRequest, internal.LoadModelResponse]) {
	var filesystem kernel.Read[storage.FileSystem]
	return func(access kernel.ResourceAccess) {
			filesystem = access.GetRead[storage.FileSystem]()
		}, func(k kernel.Kernel, request internal.LoadModelRequest) (internal.LoadModelResponse, error) {
			model, err := internal.ParseModel(filesystem.Get(), request.Path, request.SampleRate)
			k.ExecuteCommandAsync[installModelCmd](installModelRequest{
				Path: request.Path, Generation: request.Generation, Model: model, Err: err,
			})
			return internal.LoadModelResponse{}, nil
		}
}

// installModelCmdImpl handles installModelCmd, holding the Lookup and the
// resource queue for the length of the upload.
func installModelCmdImpl() (kernel.Lock, kernel.Execute[installModelRequest, installModelResponse]) {
	var lookup kernel.Write[*scene.Lookup]
	var resources kernel.Write[*gfx.ResourceQueue]
	return func(access kernel.ResourceAccess) {
			lookup = access.GetWrite[*scene.Lookup]()
			resources = access.GetWrite[*gfx.ResourceQueue]()
		}, func(k kernel.Kernel, request installModelRequest) (installModelResponse, error) {
			internal.LookupInstallModel(lookup.Get(),
				func(err error) { k.ReportError(err) }, request.Path, request.Generation,
				request.Model, request.Err, resources.Get(),
			)
			return installModelResponse{}, nil
		}
}
