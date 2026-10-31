package storage

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

// TestWriteAccessRequiresBoundHandle covers the failure mode of the capability:
// a handle that never had its write lock declared has no backend, and must say
// so rather than panic or silently write nothing.
func TestWriteAccessRequiresBoundHandle(t *testing.T) {
	var unbound kernel.Write[FileSystem]
	write := WriteAccess(unbound)

	var noAccess ErrNoWriteAccess
	if err := write.WriteFile("save.txt", nil, 0o600); !errors.As(err, &noAccess) {
		t.Fatalf("WriteFile error = %v, want ErrNoWriteAccess", err)
	}
	if err := write.Rename("a", "b"); !errors.As(err, &noAccess) {
		t.Fatalf("Rename error = %v, want ErrNoWriteAccess", err)
	}
}
