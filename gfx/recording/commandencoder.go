package recording

import "github.com/dvoyni/cog/gfx"

// commandEncoder is the recording adapter's encoder: it keeps the commands
// recorded into it, for Finish. One goroutine records into it at a time, as
// gfx.CommandEncoder says, so it needs no mutex.
type commandEncoder struct {
	instance *Instance
	commands []Command
}

func (encoder *commandEncoder) BeginRenderPass(descriptor gfx.RenderPassDescriptor) gfx.RenderPassEncoder {
	panic("gfx listing: no bodies")
}

func (encoder *commandEncoder) BeginComputePass(descriptor gfx.ComputePassDescriptor) gfx.ComputePassEncoder {
	panic("gfx listing: no bodies")
}

func (encoder *commandEncoder) ResolveQuerySet(querySet gfx.QuerySet, firstQuery, queryCount uint32, destination gfx.Buffer, destinationOffset uint64) {
	panic("gfx listing: no bodies")
}

func (encoder *commandEncoder) CopyBufferToBuffer(source gfx.Buffer, sourceOffset uint64, destination gfx.Buffer, destinationOffset uint64, size uint64) {
	panic("gfx listing: no bodies")
}

func (encoder *commandEncoder) CopyBufferToTexture(source gfx.Buffer, destination gfx.Texture, regions []gfx.BufferTextureCopy) {
	panic("gfx listing: no bodies")
}

func (encoder *commandEncoder) CopyTextureToBuffer(source gfx.Texture, destination gfx.Buffer, regions []gfx.BufferTextureCopy) {
	panic("gfx listing: no bodies")
}

func (encoder *commandEncoder) CopyTextureToTexture(source, destination gfx.Texture, regions []gfx.TextureCopy) {
	panic("gfx listing: no bodies")
}

// ClearBuffer records the clear.
func (encoder *commandEncoder) ClearBuffer(buffer gfx.Buffer, offset, size uint64) {
	encoder.commands = append(encoder.commands, Command{
		Name:      "ClearBuffer",
		Arguments: ClearBufferArguments{Buffer: buffer, Offset: offset, Size: size},
	})
}

// Finish records the call, and answers the commands recorded, or the error
// AnswerNext queued for it.
func (encoder *commandEncoder) Finish() (gfx.CommandBuffer, error) {
	if err := encoder.instance.record(CallKindFinish, nil, nil).Error; err != nil {
		return nil, err
	}
	return &commandBuffer{commands: encoder.commands}, nil
}

// DiscardEncoding drops the commands recorded.
func (encoder *commandEncoder) DiscardEncoding() { encoder.commands = nil }

// commandBuffer is a finished commandEncoder's commands, as Submit records
// them.
type commandBuffer struct {
	commands []Command
}

// Release does nothing: the adapter holds nothing for a buffer.
func (*commandBuffer) Release() {}
