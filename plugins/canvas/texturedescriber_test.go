package canvas_test

import (
	"slices"
	"sync"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/render"
)

// textureDescriber is a test plugin whose task, in each frame, reads
// render's descriptors of the textures it was handed, as render holds them
// as the frame begins.
type textureDescriber struct {
	testPlugin
	// mutex guards textures, the textures to describe, and descriptors,
	// each one's descriptor as last read, at its index, the zero descriptor
	// for one render does not hold.
	mutex       sync.Mutex
	textures    []render.Texture
	descriptors []render.TextureDescriptor
}

func (*textureDescriber) Name() kernel.PluginName { return "texture describer" }

func (d *textureDescriber) Init(configurator *kernel.Configurator, _ any) error {
	describe := configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, inventory render.Inventory) kernel.NoResult {
		d.mutex.Lock()
		defer d.mutex.Unlock()
		d.descriptors = d.descriptors[:0]
		for _, texture := range d.textures {
			descriptor, _ := inventory.TextureDescriptor(texture)
			d.descriptors = append(d.descriptors, descriptor)
		}
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(describe, kernel.NoArg{}, event.Ready).Handle())
	})
	return nil
}

// describe hands the describer textures, which the frames after describe.
func (d *textureDescriber) describe(textures ...render.Texture) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.textures = textures
}

// describedTextures answers the descriptors of the textures handed, as the
// last frame read them, empty before one has.
func (d *textureDescriber) describedTextures() []render.TextureDescriptor {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return slices.Clone(d.descriptors)
}
