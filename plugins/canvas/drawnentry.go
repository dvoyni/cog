package canvas

import (
	"fmt"

	"github.com/dvoyni/cog/libs/m"
)

// drawnEntry is what the renderer found of one of a frame's entries,
// written at its entry's index: its bounds, grown by its material's
// CullMargin, its batch key, and why it is skipped, skipReasonNone for an
// entry drawn. A skipped entry's bounds and batch key are zero.
type drawnEntry struct {
	bounds m.Rect
	key    batchKey
	skip   skipReason
}

// skipReason is why an entry draws nothing.
type skipReason uint8

const (
	// skipReasonNone is an entry drawn.
	skipReasonNone skipReason = iota
	// skipReasonMaterialNotHeld is an entry of a material canvas does not
	// hold.
	skipReasonMaterialNotHeld
	// skipReasonArgumentLayoutMismatch is an entry of a material whose
	// argument's layout mismatched its shader's.
	skipReasonArgumentLayoutMismatch
	// skipReasonArgumentRefused is an entry giving its material an argument
	// it does not take.
	skipReasonArgumentRefused
	// skipReasonFrameDrawsNothing is an entry drawing an image's frame
	// found to draw nothing.
	skipReasonFrameDrawsNothing
	// skipReasonMeshFileDrawsNothing is a mesh whose file draws nothing.
	skipReasonMeshFileDrawsNothing
	// skipReasonVertexStageMissing is a mesh of another layout than
	// canvas.Vertex's drawn with a material whose vertex stage is canvas's.
	skipReasonVertexStageMissing
	// skipReasonNoGlyphDrawn is a run of text drawing no glyph or icon.
	skipReasonNoGlyphDrawn
)

// reportSkip reports once why entry, of list, is skipped, for reason, its
// material's slot of materials: a material canvas does not hold, an
// argument it refuses, or a mesh of another layout than canvas.Vertex's
// drawn with a material whose vertex stage is canvas's. A material whose
// argument's layout mismatched its shader's was reported as it was made,
// and an image's frame and a mesh file that draw nothing as they were
// placed and loaded; a run of text drawing no glyph is no mistake of its
// own.
func reportSkip(reporter *reporter, reason skipReason, list *commandList, entry uint32, materials *frameMaterials) {
	material := list.material(entry)
	switch reason {
	case skipReasonMaterialNotHeld:
		reportMaterialNotHeld(reporter, material, "drawing an entry with")
	case skipReasonArgumentRefused:
		slot, _ := materials.find(material)
		reportArgumentRefused(reporter, material, list.entries[entry].argument.goType, slot.argumentType)
	case skipReasonVertexStageMissing:
		missing := Report{Kind: ReportKindVertexStageMissing, Err: fmt.Errorf("canvas: material %v has no vertex stage of its own, so a mesh of another layout than canvas.Vertex's drawn with it is skipped", material)}
		reporter.reportOnce(vertexStageMissingLogKey+fmt.Sprint(material), missing)
	}
}

// findMeshSkip answers why a mesh, of a file or per-frame triangles, its
// image's frame as drawn image, of canvas.Vertex's layout when
// standardLayout, drawn with the material of slot, is skipped: its frame
// drawing nothing, or another layout drawn through canvas's vertex stage;
// skipReasonNone for neither.
func findMeshSkip(image *drawnImage, standardLayout bool, slot *materialSlot) skipReason {
	switch {
	case image.drawsNothing:
		return skipReasonFrameDrawsNothing
	case !slot.drawsLayout(standardLayout):
		return skipReasonVertexStageMissing
	}
	return skipReasonNone
}
