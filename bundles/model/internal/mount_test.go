package internal

import (
	"io/fs"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// model mounts the bundled shader itself, so a renderer drawing with it
// mounts nothing: every source the embed holds is readable through storage
// once the engine is ready, byte for byte, before any frame has run.
func TestTheBundledShaderIsMountedByModel(t *testing.T) {
	engine := kernel.New(nil).
		Handler(func(err error) error { t.Errorf("engine reported: %v", err); return nil }).
		WithPlugins(storageplugin.New(), permanentAdapter{}, New(), mountProbe{})
	go engine.Run()
	<-engine.Ready()
	names, err := fs.Glob(shaderFS, "builtin/scene/*.wgsl")
	if err != nil || len(names) == 0 {
		t.Fatalf("glob the embedded sources: %v, %v", names, err)
	}
	engine.Executioner().ExecuteCommand[mountProbeCmd](mountProbeRequest{read: func(files storage.FileSystem) {
		for _, name := range append(names, SceneShaderPath, VertexDecodePath, FramePath, PbrPath) {
			embedded, err := shaderFS.ReadFile(name)
			if err != nil {
				t.Errorf("read the embedded %s: %v", name, err)
				continue
			}
			mounted, err := fs.ReadFile(files, name)
			if err != nil {
				t.Errorf("read the mounted %s: %v", name, err)
				continue
			}
			if string(mounted) != string(embedded) {
				t.Errorf("the mounted %s differs from the embedded source", name)
			}
		}
	}})
}

// mountProbe runs a read of storage's filesystem inside a command that holds
// it, which is how a test outside storage reads a mount.
type mountProbe struct{}

type (
	mountProbeCmd      kernel.Command[mountProbeRequest, mountProbeResponse]
	mountProbeRequest  struct{ read func(storage.FileSystem) }
	mountProbeResponse struct{}
)

func (mountProbe) Name() kernel.PluginName { return "model-test-mount-probe" }
func (mountProbe) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{storage.Name, Name}
}

func (mountProbe) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[mountProbeCmd](func() (kernel.Lock, kernel.Execute[mountProbeRequest, mountProbeResponse]) {
		var filesystem kernel.Read[storage.FileSystem]
		return func(access kernel.ResourceAccess) {
				filesystem = access.GetRead[storage.FileSystem]()
			}, func(_ kernel.Kernel, req mountProbeRequest) mountProbeResponse {
				req.read(filesystem.Get())
				return mountProbeResponse{}
			}
	})
	return nil
}
