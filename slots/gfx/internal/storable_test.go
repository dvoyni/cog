package internal

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/bundles/ecs"
)

// The descriptors a Component holds to name a texture, a buffer or a material
// parameter are storable: every byte run they carry is an assets.Blob, which the ECS
// admits on the contract that it is never written after construction.
func TestDescriptorsAreStorable(t *testing.T) {
	for _, tp := range []reflect.Type{
		reflect.TypeFor[descriptors.ParameterDescr](),
		reflect.TypeFor[descriptors.TextureDescr](),
		reflect.TypeFor[descriptors.BufferDescr](),
	} {
		if err := ecs.Storable(tp); err != nil {
			t.Errorf("ecs.Storable(%s) = %v, want nil", tp, err)
		}
	}
}
