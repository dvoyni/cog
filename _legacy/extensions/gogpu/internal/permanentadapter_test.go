package internal

import (
	"errors"
	"io/fs"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
)

// permanentAdapter provides storage's PermanentFS Adapter to the engines these
// tests compose: an empty filesystem that reads nothing and refuses writes,
// since nothing here persists anything.
type permanentAdapter struct{}

func (permanentAdapter) Name() kernel.PluginName           { return "test-permanent-fs" }
func (permanentAdapter) Dependencies() []kernel.PluginName { return nil }
func (permanentAdapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[testPermanentFS](storage.PermanentFS(emptyPermanentFS{}))
	return nil
}

// testPermanentFS is the Adapter this fixture fills storage's permanent
// filesystem Port as.
type testPermanentFS kernel.Adapter[storage.PermanentFSPort]

// readMountAdapter contributes one read mount through storage's Port, standing
// in for the game plugin that would contribute its assets.
type readMountAdapter struct{ mount storage.ReadMount }

func (readMountAdapter) Name() kernel.PluginName           { return "test-read-mount" }
func (readMountAdapter) Dependencies() []kernel.PluginName { return nil }
func (a readMountAdapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[testReadMount](a.mount)
	return nil
}

// testReadMount is the Adapter readMountAdapter contributes its mount as.
type testReadMount kernel.Adapter[storage.ReadMountPort]

type emptyPermanentFS struct{ fstest.MapFS }

func (emptyPermanentFS) WriteFile(string, []byte, fs.FileMode) error { return errors.ErrUnsupported }
func (emptyPermanentFS) MkdirAll(string, fs.FileMode) error          { return errors.ErrUnsupported }
func (emptyPermanentFS) Remove(string) error                         { return errors.ErrUnsupported }
func (emptyPermanentFS) Rename(string, string) error                 { return errors.ErrUnsupported }
