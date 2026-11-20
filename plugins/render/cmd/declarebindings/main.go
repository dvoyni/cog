// Command declarebindings writes the binding declaration beside each shader
// named on its command line, the shader's path with .json appended, which
// rendertest's harness answers the shader's reflection from. It acquires each
// shader through render, with no defines and with each -variant's, on wgpu's
// software adapter, with no window and no GPU, and writes what gfx reflects of
// each: the bindings, and each vertex entry point's inputs, with a section of
// its own for each -variant. Its paths are relative to the folder it runs in,
// and a shader's includes are read from there, as render reads them from
// platform's FS. It runs from //go:generate beside the shaders:
//
//	//go:generate go run github.com/dvoyni/cog/plugins/render/cmd/declarebindings -variant SKINNED,JOINTS=4 lit.wgsl
//
// With -variantsonly it declares the -variants alone, leaving out the
// variant with no defines, for a shader that compiles only with defines, as
// a 2D material's reading its argument does:
//
//	//go:generate go run github.com/dvoyni/cog/plugins/render/cmd/declarebindings -variantsonly -variant CANVAS_ARGUMENT dissolve.wgsl
//
// Usage:
//
//	declarebindings [-variantsonly] [-variant NAME[=VALUE][,NAME[=VALUE]]...]... SHADER...
//
// A shader render fails to compile or refuses, and any other error render or
// gfx logs, fails the run, writing no declaration.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gogpu"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/cmd/declarebindings/plugin"
)

// init keeps the main goroutine on the main thread, where gogpu's HostLoop
// starts and stops.
func init() { runtime.LockOSThread() }

func main() {
	var defineSets variantFlag
	flag.Var(&defineSets, "variant", "a define set to declare a variant of each shader for, its defines separated by commas, each NAME or NAME=VALUE; repeat it for more variants")
	variantsOnly := flag.Bool("variantsonly", false, "declare the -variant define sets alone, leaving out the variant with no defines, for a shader that compiles only with defines")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: declarebindings [-variantsonly] [-variant NAME[=VALUE][,NAME[=VALUE]]...]... SHADER...")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 || *variantsOnly && len(defineSets) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := declareBindings(".", flag.Args(), defineSets, *variantsOnly); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// declareBindings writes the declaration of each shader at shaderPaths,
// relative to directory, beside it, with a variant for each of defineSets,
// and with none of its own when variantsOnly is set,
// from a kernel it spawns and quits on the calling goroutine, which must be
// the main thread. It answers every error logged meanwhile, and then writes
// nothing.
func declareBindings(directory string, shaderPaths []string, defineSets [][]render.ShaderDefine, variantsOnly bool) error {
	logHandler := &errorCollector{}
	supervisor := kernel.NewSupervisor(logHandler)
	declarations := map[string][]byte{}
	configs := map[kernel.PluginName]any{
		gfx.PluginName: &gfx.Config{ForceFallbackAdapter: true},
		plugin.PluginName: &plugin.Config{
			Directory:       directory,
			ShaderPaths:     shaderPaths,
			DefineSets:      defineSets,
			VariantsOnly:    variantsOnly,
			TakeDeclaration: func(shaderPath string, declaration []byte) { declarations[shaderPath] = declaration },
		},
	}
	id, err := supervisor.Spawn(configs, 2, metronome.NewPlugin(), platform.NewPlugin(), gfx.NewPlugin(), gogpu.NewPlugin(), render.NewPlugin(), plugin.NewPlugin())
	if err != nil {
		return err
	}
	_, declared := supervisor.GetKernel(id).Publish(plugin.DeclareEvent{})
	signal := kernel.NewSignal()
	supervisor.GetKernel(id).Notify(signal, declared)
	go func() {
		<-signal.C()
		supervisor.Quit()
	}()
	for supervisor.WaitMainThread() {
		supervisor.TickMainThread()
	}
	if err := errors.Join(logHandler.errors...); err != nil {
		return err
	}
	for _, path := range shaderPaths {
		if err := os.WriteFile(filepath.Join(directory, path+".json"), declarations[path], 0o644); err != nil {
			return err
		}
	}
	return nil
}
