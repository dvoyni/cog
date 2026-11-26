package canvas_test

import "github.com/dvoyni/cog/plugins/canvas/canvastest"

// convertedBatch is a batch as the converter tests read it: the label of
// the texture it draws from, and its entries' orders.
type convertedBatch struct {
	atlas  string
	orders []int32
}

// readConvertedBatches answers the batches of the first camera of the
// harness's first frame, each as a convertedBatch.
func readConvertedBatches(h *canvastest.Harness) []convertedBatch {
	frame := h.Frames()[0]
	labels := findTextureLabels(frame.Render)
	var batches []convertedBatch
	for _, inspectedBatch := range frame.Cameras[0].Batches {
		batch := convertedBatch{atlas: labels[inspectedBatch.Texture]}
		for _, entry := range inspectedBatch.Entries {
			batch.orders = append(batch.orders, entry.Order)
		}
		batches = append(batches, batch)
	}
	return batches
}
