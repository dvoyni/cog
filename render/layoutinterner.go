package render

import (
	"fmt"
	"strings"
	"sync"

	"github.com/dvoyni/cog/gfx"
)

// layoutInterner shares the layouts render builds for its shaders: one gfx
// bind-group layout for each set of entries, and one pipeline layout for each
// list of groups, each counted by its holders, the shaders and pipeline
// layouts made of it, and released by its last, so shaders sharing an
// include share one layout of the include's group, and a pass's parameters
// are one bind group. Its mutex guards it, taken inside the asset table's,
// and it takes no other.
type layoutInterner struct {
	mutex sync.Mutex
	// groupLayouts are the bind-group layouts held, by the key their entries
	// make, and pipelineLayouts the pipeline layouts, by the keys of their
	// groups joined.
	groupLayouts    map[string]*internedGroupLayout
	pipelineLayouts map[string]*internedPipelineLayout
}

// acquire hands out the pipeline layout of groups, counting one more holder
// of it, and makes it, and those of its groups not held, when none is held,
// for its holder to give back with release. A layout gfx refuses, as one past
// the device's limits, answers its error and leaves nothing made.
func (i *layoutInterner) acquire(device gfx.Device, groups []gfx.BindGroupLayoutDescriptor) (*internedPipelineLayout, error) {
	groupKeys := make([]string, len(groups))
	for index, group := range groups {
		groupKeys[index] = fmt.Sprint(group.Entries)
	}
	key := strings.Join(groupKeys, "\n")
	i.mutex.Lock()
	defer i.mutex.Unlock()
	if layout, ok := i.pipelineLayouts[key]; ok {
		layout.holderCount++
		return layout, nil
	}
	if i.pipelineLayouts == nil {
		i.groupLayouts = map[string]*internedGroupLayout{}
		i.pipelineLayouts = map[string]*internedPipelineLayout{}
	}
	groupLayouts := make([]gfx.BindGroupLayout, 0, len(groups))
	for index, group := range groups {
		groupLayout, err := i.acquireGroupLayout(device, groupKeys[index], group)
		if err != nil {
			i.releaseGroupLayouts(device, groupKeys[:index])
			return nil, err
		}
		groupLayouts = append(groupLayouts, groupLayout)
	}
	layout, err := device.CreatePipelineLayout(gfx.PipelineLayoutDescriptor{Label: "render: a shader's layout", BindGroupLayouts: groupLayouts})
	if err != nil {
		i.releaseGroupLayouts(device, groupKeys)
		return nil, err
	}
	internedLayout := &internedPipelineLayout{layout: layout, key: key, groupKeys: groupKeys, holderCount: 1}
	i.pipelineLayouts[key] = internedLayout
	return internedLayout, nil
}

// release counts one holder of layout, which acquire handed out, fewer, and
// at the last releases it, and each of its groups' layouts whose last holder
// it was. It panics for a layout released as often as it was acquired.
func (i *layoutInterner) release(device gfx.Device, layout *internedPipelineLayout) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	if layout.holderCount <= 0 {
		panic("render: releasing a shader's layout released as often as it was acquired")
	}
	if layout.holderCount--; layout.holderCount > 0 {
		return
	}
	device.ReleasePipelineLayout(layout.layout)
	delete(i.pipelineLayouts, layout.key)
	i.releaseGroupLayouts(device, layout.groupKeys)
}

// acquireGroupLayout hands out the bind-group layout of group, whose entries
// make key, counting one more holder of it, and makes it when none is held.
// i's mutex is held, and it panics when it is not.
func (i *layoutInterner) acquireGroupLayout(device gfx.Device, key string, group gfx.BindGroupLayoutDescriptor) (gfx.BindGroupLayout, error) {
	i.requireLocked("acquireGroupLayout")
	if internedLayout, ok := i.groupLayouts[key]; ok {
		internedLayout.holderCount++
		return internedLayout.layout, nil
	}
	layout, err := device.CreateBindGroupLayout(group)
	if err != nil {
		return gfx.BindGroupLayout{}, err
	}
	i.groupLayouts[key] = &internedGroupLayout{layout: layout, holderCount: 1}
	return layout, nil
}

// releaseGroupLayouts counts one holder fewer of the bind-group layout of
// each of keys, and releases each at its last. i's mutex is held, and it
// panics when it is not.
func (i *layoutInterner) releaseGroupLayouts(device gfx.Device, keys []string) {
	i.requireLocked("releaseGroupLayouts")
	for _, key := range keys {
		internedLayout := i.groupLayouts[key]
		if internedLayout.holderCount--; internedLayout.holderCount == 0 {
			device.ReleaseBindGroupLayout(internedLayout.layout)
			delete(i.groupLayouts, key)
		}
	}
}

// requireLocked panics, naming operation, when i's mutex is free: a call
// that must hold it does not.
func (i *layoutInterner) requireLocked(operation string) {
	if i.mutex.TryLock() {
		i.mutex.Unlock()
		panic("render: the layout interner's " + operation + " is called without its mutex held")
	}
}

// internedGroupLayout is a bind-group layout layoutInterner holds, and how
// many shaders' pipeline layouts hold it.
type internedGroupLayout struct {
	layout      gfx.BindGroupLayout
	holderCount int
}

// internedPipelineLayout is a pipeline layout layoutInterner holds, its key
// and the keys of its groups' layouts, and how many shaders hold it.
type internedPipelineLayout struct {
	layout      gfx.PipelineLayout
	key         string
	groupKeys   []string
	holderCount int
}
