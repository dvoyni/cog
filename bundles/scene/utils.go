package scene

import (
	"github.com/dvoyni/cog/bundles/scene/internal"
	"github.com/dvoyni/cog/libs/m"
)

// Layer is the mask of one layer. There are 32 of them; an index past the end
// wraps rather than silently becoming zero, which would read as every layer.
func Layer(i uint) LayerMask { return internal.Layer(i) }

// ViewProjection is the matrix a Camera placed at at draws through, for a
// viewport of the given size in pixels: the one its screen-targeted passes
// upload, Oblique shear and Near/Far included. A caller picking or placing a
// marker passes it to m.WorldToScreen, m.ScreenToWorld or m.ScreenToRay.
//
// A camera the renderer skips is refused with the error it reports for it
// (ErrCameraClipPlanesMissing or ErrCameraProjectionDegenerate), and a
// viewport with a side of zero or less with ErrViewportUnsized, rather than a
// matrix a pick silently misses through.
func ViewProjection(camera Camera, at m.Transform, viewport m.Vec2) (m.Mat4, error) {
	return internal.ViewProjection(camera, at, viewport)
}
