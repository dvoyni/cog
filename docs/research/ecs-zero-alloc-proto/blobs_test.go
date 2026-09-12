package proto

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"

	"protoecs/ecs"
)

// The case a hash cannot answer, from review: an Entity needs a []byte buffer.
//
// Content-addressing it is the wrong tool, and the cleanup question is exactly
// what shows why. A table keyed by the buffer's hash has to hold the buffer, so
// it grows with every distinct value the game ever produces, and the only way
// to empty it is to count how many Entities still refer to each entry --
// refcounting on a Component write, which stops the Store being a memcpy.
//
// Keyed by Entity instead there is exactly one owner and nothing to count. The
// buffer dies with its Entity, under the Despawn that already empties every
// Store. Sharing is what costs; ownership is free.

// Buffered is the Component, and it holds no buffer at all: the bytes live in
// the side store, and what the Component carries is how many of them there are,
// so a Query can filter without reaching for the store.
type Buffered struct{ Len uint32 }

type BufferedQ struct{ B *Buffered }

type blobSub kernel.Subscription[app.UpdateEvent]

type blobWorld struct {
	en    *ecs.Entities
	blobs *ecs.Blobs
	bufs  *ecs.Store[Buffered]
	made  []ecs.Entity
}

type blobBundle struct{ B Buffered }

type blobPlug struct {
	w *blobWorld
	// perTick is how many Entities the System spawns, fills and despawns each
	// frame -- the churn the cleanup claim is about.
	perTick int
}

func (blobPlug) Name() kernel.PluginName           { return "blobs" }
func (blobPlug) Dependencies() []kernel.PluginName { return nil }

func (p blobPlug) Register(r *kernel.Registrar, _ any) error {
	ids := uint32(p.perTick*2 + 64)
	en := ecs.NewEntities(ids)
	r.InitResource[*ecs.Entities](en)
	bufs := ecs.RegisterComponent[Buffered](r, en, ids)

	// The side store is the plugin's own resource, declared like any other, and
	// enrolled in Despawn so it is emptied with everything else.
	blobs := ecs.NewBlobs(ids)
	r.InitResource[*ecs.Blobs](blobs)
	en.RegisterSideStore(blobs)
	p.w.en, p.w.blobs, p.w.bufs = en, blobs, bufs

	if p.perTick == 0 {
		return nil
	}
	scratch := make([]ecs.Entity, 0, p.perTick)
	payload := bytes.Repeat([]byte{0xAB}, 96)
	r.Subscribe[blobSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en,
		func(sp *ecs.Spawn[blobBundle], we *ecs.WriteableEntities, store *ecs.Write[*ecs.Blobs]) {
			b := store.Get()
			scratch = scratch[:0]
			for range p.perTick {
				e := sp.New(blobBundle{B: Buffered{Len: uint32(len(payload))}})
				b.Set(e, payload)
				scratch = append(scratch, e)
			}
			for _, e := range scratch {
				we.Despawn(e)
			}
		}))
	return nil
}

func startBlobs(tb testing.TB, perTick int) (*kernel.Engine, *blobWorld) {
	tb.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(cancel)
	w := &blobWorld{}
	e := kernel.New(nil).
		Handler(func(err error) bool { tb.Errorf("kernel error: %v", err); return true }).
		WithPlugins(blobPlug{w: w, perTick: perTick})
	go e.Run(ctx)
	<-e.Ready()
	return e, w
}

// A Despawn empties the side store as it empties every Store, so a buffer needs
// no reference count and no sweep: it is gone when its Entity is.
func TestDespawnEmptiesASideStore(t *testing.T) {
	e, w := startBlobs(t, 0)
	_ = e

	payload := []byte{1, 2, 3, 4, 5}
	var made []ecs.Entity
	for range 4 {
		en := w.en.Alloc()
		w.bufs.Add(en, Buffered{Len: uint32(len(payload))})
		w.blobs.Set(en, payload)
		made = append(made, en)
	}
	if w.blobs.Len() != 4 {
		t.Fatalf("store holds %d blobs, want 4", w.blobs.Len())
	}
	got, ok := w.blobs.Get(made[2])
	if !ok || !bytes.Equal(got, payload) {
		t.Fatalf("blob read back as %v ok=%v", got, ok)
	}

	for _, en := range made {
		w.en.Despawn(en)
	}
	if w.blobs.Len() != 0 {
		t.Fatalf("after despawning everything the store still holds %d blobs", w.blobs.Len())
	}
	if _, ok := w.blobs.Get(made[2]); ok {
		t.Fatalf("a despawned Entity still has a blob")
	}
	if w.blobs.Pooled() != 4 {
		t.Fatalf("%d buffers went back to the pool, want 4", w.blobs.Pooled())
	}
	t.Logf("4 blobs, 4 despawns, 0 left and 4 buffers kept for reuse")
}

// The steady state: spawn, fill and despawn every frame forever. Nothing
// accumulates, and after the high-water mark nothing is allocated either --
// the removed rows' buffers are reissued.
func TestBlobChurnReachesASteadyState(t *testing.T) {
	const perTick = 200
	e, w := startBlobs(t, perTick)
	ex := e.Executioner()
	for range 4 {
		ex.PublishEvent(tick).Wait()
	}
	pooled := w.blobs.Pooled()
	for range 200 {
		ex.PublishEvent(tick).Wait()
		if w.blobs.Len() != 0 {
			t.Fatalf("the store kept %d blobs past the frame that made them", w.blobs.Len())
		}
		if w.blobs.Pooled() != pooled {
			t.Fatalf("the pool moved from %d to %d after warm-up", pooled, w.blobs.Pooled())
		}
	}
	if pooled != perTick {
		t.Fatalf("pool settled at %d, want %d", pooled, perTick)
	}
	t.Logf("200 frames of %d spawn-fill-despawn: 0 live, pool flat at %d", perTick, pooled)
}

// What the churn costs, whole-frame, so the allocation claim is measured rather
// than inferred from the pool count.
func BenchmarkBlobChurn(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("perTick=%d", n), func(b *testing.B) {
			e, _ := startBlobs(b, n)
			ex := e.Executioner()
			for range 8 {
				ex.PublishEvent(tick).Wait()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				ex.PublishEvent(tick).Wait()
			}
		})
	}
}
