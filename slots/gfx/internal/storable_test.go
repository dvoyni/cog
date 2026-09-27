package internal

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/bundles/ecs"
)

// The descriptors a Component holds to name a texture, a buffer, a material
// parameter or a set of draw params are storable: every byte run they carry is an
// assets.Blob, which the ECS admits on the contract that it is never written
// after construction.
func TestDescriptorsAreStorable(t *testing.T) {
	for _, tp := range []reflect.Type{
		reflect.TypeFor[types.ShaderParameterDescr](),
		reflect.TypeFor[types.TextureDescr](),
		reflect.TypeFor[types.BufferDescr](),
		reflect.TypeFor[types.DrawStateId](),
	} {
		if err := ecs.Storable(tp); err != nil {
			t.Errorf("ecs.Storable(%s) = %v, want nil", tp, err)
		}
	}
}
