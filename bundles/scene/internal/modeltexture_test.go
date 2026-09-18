package internal

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal/types"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// countingFS counts how many times each path was opened, which is what makes
// "the second model reads nothing" an assertion rather than a claim about
// internals. A cache is consulted before the read, so a hit never reaches here.
type countingFS struct {
	files fstest.MapFS
	opens map[string]int
}

func counting(files fstest.MapFS) *countingFS {
	return &countingFS{files: files, opens: map[string]int{}}
}

func (c *countingFS) Open(name string) (fs.File, error) {
	file, err := c.files.Open(name)
	if err == nil {
		c.opens[name]++
	}
	return file, err
}

// externalImageModel is a file whose one material binds an image by URI, so the
// picture is a storage path of its own rather than bytes inside the container.
func externalImageModel(t testing.TB, uri string) *gltf.Document {
	t.Helper()
	doc := onePrimitiveModel(t)
	doc.Images = []*gltf.Image{{URI: uri}}
	doc.Textures = []*gltf.Texture{{Source: gltf.Index(0)}}
	doc.Materials = []*gltf.Material{{PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
		BaseColorTexture: &gltf.TextureInfo{Index: 0},
	}}}
	doc.Meshes[0].Primitives[0].Material = gltf.Index(0)
	return doc
}

// filePictures reports the bakes of pictures a file supplied, which is every
// durable texture upload except the two 1x1 defaults scene bakes for itself.
func (b *testBackend) filePictures() []textureBake {
	var found []textureBake
	for _, bake := range b.bakedTextures {
		if bake.width > 1 || bake.height > 1 {
			found = append(found, bake)
		}
	}
	return found
}

// Two models naming one external image decode it once. The table they share is
// consulted **before** the read now, where it used to be consulted after the
// decode at install time - so the second model neither opens the file nor
// decodes its pixels, which is the defect scene documented and lived with.
func TestTwoModelsSharingAnExternalImageReadItOnce(t *testing.T) {
	const second = "models/other.glb"
	const image = "models/colour.png"
	files := counting(fstest.MapFS{
		modelPath: {Data: glb(t, externalImageModel(t, "colour.png"))},
		second:    {Data: glb(t, externalImageModel(t, "colour.png"))},
		image:     {Data: onePixelPNG(t)},
	})
	h := newHarnessWithFS(t, files, func(q *scene.OpQueue) {
		q.Camera(cameraMain, modelCamera())
		q.Model(scene.LayersAll, modelPath, scene.ModelDraw{})
		q.Model(scene.LayersAll, second, scene.ModelDraw{})
	})
	h.frameUntil(t, "both models to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 2
	})
	if got := files.opens[image]; got != 1 {
		t.Errorf("the shared image was opened %d times, want once for both models", got)
	}
	if got := h.backend.filePictures(); len(got) != 1 {
		t.Errorf("the shared image was baked %d times, want one GPU texture", len(got))
	}
}

// A picture that did not arrive binds magenta, because a missing base colour
// rendering white looks deliberate. Magenta is only right for a picture: the
// data slots keep the per-slot defaults the material binding already had, since
// magenta as a normal map is a surface lit from nowhere.
func TestAMissingPictureBindsMagentaAndLeavesTheDataSlotsAlone(t *testing.T) {
	doc := onePrimitiveModel(t)
	doc.Images = []*gltf.Image{{URI: "absent.png"}}
	doc.Textures = []*gltf.Texture{{Source: gltf.Index(0)}}
	doc.Materials = []*gltf.Material{{
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorTexture: &gltf.TextureInfo{Index: 0},
		},
		NormalTexture: &gltf.NormalTexture{Index: gltf.Index(0)},
	}}
	doc.Meshes[0].Primitives[0].Material = gltf.Index(0)
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), drawModel(modelPath, scene.ModelDraw{}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 1
	})
	if len(h.errors()) == 0 {
		t.Fatal("a missing image must be reported, whichever half of the load found it")
	}
	// All ten bindings, always: WGSL requires every declared binding bound and
	// gfx does no preprocessing, so a slot whose image is gone binds something.
	for _, slot := range types.PbrSlots {
		if len(h.backend.texturesBoundTo(slot.Texture)) == 0 {
			t.Errorf("%s was never bound", slot.Texture)
		}
		if len(h.backend.samplersBoundTo(slot.Sampler)) == 0 {
			t.Errorf("%s was never bound", slot.Sampler)
		}
	}
	base := h.backend.texturesBoundTo("baseColorTexture")
	normal := h.backend.texturesBoundTo("normalTexture")
	if len(base) == 0 || len(normal) == 0 {
		t.Fatalf("base = %v, normal = %v; both slots must bind", base, normal)
	}
	magenta, ok := h.backend.bakeOf(base[len(base)-1])
	if !ok {
		t.Fatalf("the base colour bound texture %d, which nothing baked", base[len(base)-1])
	}
	if want := []byte{0xff, 0x00, 0xff, 0xff}; string(magenta.pixels) != string(want) {
		t.Errorf("the missing base colour is %v, want the magenta placeholder %v", magenta.pixels, want)
	}
	if magenta.format != gfx.FormatRGBA8Srgb {
		t.Errorf("the placeholder is %v, want the sRGB format the slot asked for", magenta.format)
	}
	// The normal slot asked for data, so it took no placeholder at all and
	// bound the flat normal the material binding already had for it.
	flat, ok := h.backend.bakeOf(normal[len(normal)-1])
	if !ok {
		t.Fatalf("the normal slot bound texture %d, which nothing baked", normal[len(normal)-1])
	}
	if want := []byte{0x80, 0x80, 0xff, 0xff}; string(flat.pixels) != string(want) {
		t.Errorf("the missing normal map is %v, want the flat normal %v", flat.pixels, want)
	}
}

// An image that opens and does not decode is the loader's own fault rather than
// the Library's, so it keeps scene's own report type - and it keeps the
// string-keyed dedup with it, so one broken picture bound in two colour spaces
// is one report between them.
func TestABrokenImageIsOneReportAcrossItsColourSpaces(t *testing.T) {
	doc := onePrimitiveModel(t)
	index, err := modeler.WriteImage(doc, "broken", "image/png", strings.NewReader("this is not a png"))
	if err != nil {
		t.Fatalf("write image: %v", err)
	}
	doc.Textures = []*gltf.Texture{{Source: gltf.Index(index)}}
	doc.Materials = []*gltf.Material{{
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorTexture: &gltf.TextureInfo{Index: 0},
		},
		NormalTexture: &gltf.NormalTexture{Index: gltf.Index(0)},
	}}
	doc.Meshes[0].Primitives[0].Material = gltf.Index(0)
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), drawModel(modelPath, scene.ModelDraw{}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 1
	})
	var broken scene.ErrModelTextureUnavailable
	if !anyErrorAs(h.errors(), &broken) {
		t.Fatalf("errors = %v, want the decode failure reported as scene's own", h.errors())
	}
	if got := len(h.errors()); got != 1 {
		t.Fatalf("reports = %v, want one for the image however many slots bound it", h.errors())
	}
}

// UnloadTexture frees every variant a path baked and lets the next load report
// afresh. The two are one lever: the entries go, and the report keys that were
// gated on them go with them, so a picture that was broken, was fixed and is
// asked for again can speak.
func TestUnloadTextureFreesEveryVariantAndUnmutesThePath(t *testing.T) {
	doc := onePrimitiveModel(t)
	index, err := modeler.WriteImage(doc, "broken", "image/png", strings.NewReader("this is not a png"))
	if err != nil {
		t.Fatalf("write image: %v", err)
	}
	doc.Textures = []*gltf.Texture{{Source: gltf.Index(index)}}
	doc.Materials = []*gltf.Material{{
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorTexture: &gltf.TextureInfo{Index: 0},
		},
		NormalTexture: &gltf.NormalTexture{Index: gltf.Index(0)},
	}}
	doc.Meshes[0].Primitives[0].Material = gltf.Index(0)
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), func(*scene.OpQueue) {})
	h.device(func(la scene.LookupDeviceAccess) { la.Preload(modelPath) })
	if len(h.errors()) != 1 {
		t.Fatalf("reports = %v, want the broken image reported once", h.errors())
	}
	h.lookup(func(la scene.LookupAccess) { la.UnloadModel(modelPath) })
	h.device(func(la scene.LookupDeviceAccess) {
		la.UnloadTexture(modelPath)
		la.Preload(modelPath)
	})
	if len(h.errors()) != 2 {
		t.Fatalf("reports = %v, want the unload to have let the image break again", h.errors())
	}
}

// UnloadModel does not cascade to textures, because with no refcount the lookup
// cannot know whether another loaded model binds the same image by path, and
// freeing one that is still bound is a dead texture in a live bind group rather
// than a missing picture. The wart is deliberate and asserted so nobody quietly
// fixes it into a use-after-free.
//
// UnloadTexture is the lever that does free them, and it frees every colour
// space the path baked, because the path is the whole of what a caller can
// name. One image bound as a picture and as data is two GPU textures and one
// unload.
func TestUnloadTextureFreesEveryColourSpaceOnePathBaked(t *testing.T) {
	doc := onePrimitiveModel(t)
	index, err := modeler.WriteImage(doc, "colour", "image/png", bytes.NewReader(onePixelPNG(t)))
	if err != nil {
		t.Fatalf("write image: %v", err)
	}
	doc.Textures = []*gltf.Texture{{Source: gltf.Index(index)}}
	doc.Materials = []*gltf.Material{{
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorTexture: &gltf.TextureInfo{Index: 0},
		},
		NormalTexture: &gltf.NormalTexture{Index: gltf.Index(0)},
	}}
	doc.Meshes[0].Primitives[0].Material = gltf.Index(0)
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), func(*scene.OpQueue) {})
	h.device(func(la scene.LookupDeviceAccess) { la.Preload(modelPath) })
	h.frame()
	baked := h.backend.filePictures()
	if len(baked) != 2 {
		t.Fatalf("one image bound as a picture and as data baked %d textures, want two", len(baked))
	}

	h.lookup(func(la scene.LookupAccess) { la.UnloadModel(modelPath) })
	h.frame()
	if got := len(h.backend.releasedTextures); got != 0 {
		t.Fatalf("UnloadModel released %d textures, want none: it must not cascade", got)
	}

	h.device(func(la scene.LookupDeviceAccess) { la.UnloadTexture(modelPath) })
	h.frame()
	released := map[gfx.TextureID]bool{}
	for _, id := range h.backend.releasedTextures {
		released[id] = true
	}
	for _, bake := range baked {
		if !released[bake.id] {
			t.Errorf("texture %d survived UnloadTexture, want every variant the path baked gone", bake.id)
		}
	}
}

// The material slot is not part of a texture's key, and must not be made to be.
// One ORM image - occlusion, roughness and metalness packed into one picture,
// which is glTF's standard packing - is one GPU texture however many linear
// slots sample it. Putting the slot in the key would make it three.
func TestOneORMImageIsOneGPUTexture(t *testing.T) {
	doc := onePrimitiveModel(t)
	index, err := modeler.WriteImage(doc, "orm", "image/png", bytes.NewReader(onePixelPNG(t)))
	if err != nil {
		t.Fatalf("write image: %v", err)
	}
	doc.Textures = []*gltf.Texture{{Source: gltf.Index(index)}}
	doc.Materials = []*gltf.Material{{
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			MetallicRoughnessTexture: &gltf.TextureInfo{Index: 0},
		},
		OcclusionTexture: &gltf.OcclusionTexture{Index: gltf.Index(0)},
	}}
	doc.Meshes[0].Primitives[0].Material = gltf.Index(0)
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), drawModel(modelPath, scene.ModelDraw{}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 1
	})
	if got := h.backend.filePictures(); len(got) != 1 {
		t.Fatalf("one ORM image baked %d textures, want one for both linear slots", len(got))
	}
	occlusion := h.backend.texturesBoundTo("occlusionTexture")
	roughness := h.backend.texturesBoundTo("metallicRoughnessTexture")
	if len(occlusion) == 0 || len(roughness) == 0 {
		t.Fatalf("occlusion = %v, roughness = %v; both slots must bind", occlusion, roughness)
	}
	if occlusion[len(occlusion)-1] != roughness[len(roughness)-1] {
		t.Errorf("the two slots bound %d and %d, want one texture for both",
			occlusion[len(occlusion)-1], roughness[len(roughness)-1])
	}
}

// Two models naming one broken image report it once between them, and they do
// so even when they bind it in different colour spaces - which is two cache
// entries and one fact. That dedup is across entries rather than within one, so
// it is the kernel's and keyed by string, which is why scene keeps a texture
// report namespace of its own even though the cache owns the per-entry half.
func TestTwoModelsNamingOneBrokenImageReportItOnce(t *testing.T) {
	const second = "models/other.glb"
	// The first binds the picture as a base colour and the second as a normal
	// map, so the two share the path and share nothing else.
	normal := externalImageModel(t, "colour.png")
	normal.Materials[0] = &gltf.Material{
		NormalTexture: &gltf.NormalTexture{Index: gltf.Index(0)},
	}
	files := fstest.MapFS{
		modelPath:           {Data: glb(t, externalImageModel(t, "colour.png"))},
		second:              {Data: glb(t, normal)},
		"models/colour.png": {Data: []byte("this is not a png")},
	}
	h := newHarnessWithFiles(t, files, func(q *scene.OpQueue) {
		q.Camera(cameraMain, modelCamera())
		q.Model(scene.LayersAll, modelPath, scene.ModelDraw{})
		q.Model(scene.LayersAll, second, scene.ModelDraw{})
	})
	h.frameUntil(t, "both models to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 2
	})
	var broken scene.ErrModelTextureUnavailable
	if !anyErrorAs(h.errors(), &broken) {
		t.Fatalf("errors = %v, want the decode failure reported", h.errors())
	}
	if got := len(h.errors()); got != 1 {
		t.Fatalf("reports = %v, want one for the image between the two models", h.errors())
	}
}
