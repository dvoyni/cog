package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"
)

func TestAFrameSnapshotNamesABindingsKindWithItsFlag(t *testing.T) {
	for kind, want := range map[shader.ResourceKind]string{
		shader.ResourceUniformBuffer:                           "uniform",
		shader.ResourceStorageBuffer:                           "storage",
		shader.ResourceStorageBuffer | shader.ResourceWritable: "readWriteStorage",
		shader.ResourceTexture:                                 "texture",
		shader.ResourceTexture | shader.ResourceDepth:          "depthTexture",
		shader.ResourceSampler:                                 "sampler",
		shader.ResourceSampler | shader.ResourceComparison:     "comparisonSampler",
	} {
		if got := bindingKindViewName(kind); got != want {
			t.Errorf("kind %d = %q, want %q", kind, got, want)
		}
	}
}
