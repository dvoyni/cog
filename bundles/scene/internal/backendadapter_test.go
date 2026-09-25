package internal

import (
	"sync/atomic"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
)

// backendAdapter provides a test's Backend to gfx, the way a driver provides
// its own: gfx is a Slot, and a composition without one fails.
type backendAdapter struct{ backend gfx.Backend }

func (backendAdapter) Name() kernel.PluginName           { return "gfxbackendtest" }
func (backendAdapter) Dependencies() []kernel.PluginName { return nil }

func (a backendAdapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[testGfxBackend](a.backend)
	return nil
}

// testGfxBackend is the Adapter this fixture fills gfx's backend Port as.
type testGfxBackend kernel.Adapter[gfx.BackendPort]

// detachedBackend is a Backend whose device never arrives. gfx asks a backend
// that is not ready only whether it is, and for ids, so nothing else is
// implemented: a test composed with it records, and renders nothing.
type detachedBackend struct {
	gfx.Backend
	next atomic.Uint32
}

func (*detachedBackend) Ready() bool                 { return false }
func (b *detachedBackend) NewTexture() gfx.TextureID { return gfx.TextureID(b.next.Add(1)) }
func (b *detachedBackend) NewBuffer() gfx.BufferID   { return gfx.BufferID(b.next.Add(1)) }

// discardBackend is a Backend whose device is up and whose sinks throw the
// frame away: gfx compiles, bakes and records everything a frame asks for,
// the queue is replayed in full, and nothing is kept. It is what the frame
// benches render through, so their time is the engine's and not a recorder's.
//
// Its ids, shaders and layouts are testBackend's, so gfx packs parameters and
// binds scene's storage ranges exactly as it does under the drawing tests. The
// one thing it counts is the instances the last frame drew, which is how a
// bench knows its population has become resident before it starts the clock.
type discardBackend struct {
	testBackend
	sink discardSink
	// drew is the instances the last replayed frame drew.
	drew atomic.Int64
}

// Execute replays one frame into the discarding sinks.
func (b *discardBackend) Execute(queue *gfx.Queue) {
	b.sink.instances = 0
	queue.ReplayBakes(&b.sink)
	queue.ReplayPasses(&b.sink)
	queue.ReplayReleases(&b.sink)
	b.drew.Store(int64(b.sink.instances))
}

// discardSink is every sink a queue replays into, and each call is a no-op but
// Draw's, which adds up the instances.
type discardSink struct {
	instances int
}

func (*discardSink) BakeUniforms([]byte) {}

func (*discardSink) BakeBuffer(gfx.BufferID, gfx.BufferKind, int, []byte)                 {}
func (*discardSink) BakeTexture(gfx.TextureID, int, int, gfx.TextureFormat, []byte, bool) {}
func (*discardSink) AllocateTexture(gfx.TextureID, gfx.TextureDesc)                       {}
func (*discardSink) UpdateTexture(gfx.TextureID, int, gfx.Region, []byte)                 {}
func (s *discardSink) BeginPass(gfx.PassDesc) gfx.RenderPass                              { return s }
func (*discardSink) EndPass(gfx.RenderPass)                                               {}
func (*discardSink) TransitionTextures([]gfx.TextureTransition)                           {}
func (*discardSink) Present()                                                             {}
func (*discardSink) Capture(gfx.CaptureDesc)                                              {}
func (*discardSink) SetPipeline(gfx.PipelineID)                                           {}
func (*discardSink) SetUniformBlock(int, int, int, int)                                   {}
func (*discardSink) SetTexture(gfx.TextureID, int, int)                                   {}
func (*discardSink) SetSampler(gfx.SamplerID, int, int)                                   {}
func (*discardSink) SetVertexBuffer(gfx.BufferID, int)                                    {}
func (*discardSink) SetIndexBuffer(gfx.BufferID, int, gfx.IndexWidth)                     {}
func (*discardSink) SetBuffer(int, int, gfx.BufferID, int, int)                           {}
func (*discardSink) ReleaseBuffer(gfx.BufferID)                                           {}
func (*discardSink) ReleaseTexture(gfx.TextureID)                                         {}

func (s *discardSink) Draw(_, _, instances, _ int, _ bool) { s.instances += instances }
