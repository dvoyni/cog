package canvasimpl

import (
	"errors"
	"io/fs"
	"testing/fstest"

	"github.com/dvoyni/cog/extensions/storage"
	"github.com/dvoyni/cog/kernel"
)

// permanentAdapter provides storage's PermanentFS Adapter to the engines these
// tests compose: an empty filesystem that reads nothing and refuses writes,
// since nothing here persists anything.
type permanentAdapter struct{}

func (permanentAdapter) Name() kernel.PluginName           { return "test-permanent-fs" }
func (permanentAdapter) Dependencies() []kernel.PluginName { return nil }
func (permanentAdapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[storage.PermanentFS](emptyPermanentFS{})
	return nil
}

type emptyPermanentFS struct{ fstest.MapFS }

func (emptyPermanentFS) WriteFile(string, []byte, fs.FileMode) error { return errors.ErrUnsupported }
func (emptyPermanentFS) MkdirAll(string, fs.FileMode) error          { return errors.ErrUnsupported }
func (emptyPermanentFS) Remove(string) error                         { return errors.ErrUnsupported }
func (emptyPermanentFS) Rename(string, string) error                 { return errors.ErrUnsupported }
