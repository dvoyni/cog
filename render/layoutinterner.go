package render

import (
	"fmt"
	"strings"
	"sync"

	"github.com/dvoyni/cog/gfx"
)

// zeroDynamicOffsets are the dynamic offsets a bind group binds at while
// nothing fills its blocks, as many as a group may have by WebGPU's default
// limits and more.
var zeroDynamicOffsets [16]uint32

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
	internedGroups := make([]*internedGroupLayout, 0, len(groups))
	for index, group := range groups {
		groupLayout, err := i.acquireGroupLayout(device, groupKeys[index], group)
		if err != nil {
			i.releaseGroupLayouts(device, internedGroups)
			return nil, err
		}
		groupLayouts = append(groupLayouts, groupLayout.layout)
		internedGroups = append(internedGroups, groupLayout)
	}
	layout, err := device.CreatePipelineLayout(gfx.PipelineLayoutDescriptor{Label: "render: a shader's layout", BindGroupLayouts: groupLayouts})
	if err != nil {
		i.releaseGroupLayouts(device, internedGroups)
		return nil, err
	}
	internedLayout := &internedPipelineLayout{layout: layout, key: key, groups: internedGroups, holderCount: 1}
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
	i.releaseGroupLayouts(device, layout.groups)
}

// findDefaultBindGroup answers the bind group of group, a layout a held
// pipeline layout holds, binding each of its entries as it draws unbound,
// made from bindingResources the first time.
func (i *layoutInterner) findDefaultBindGroup(device gfx.Device, bindingResources *bindingResourceCache, group *internedGroupLayout) (gfx.BindGroup, error) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	if group.defaultBindGroup != (gfx.BindGroup{}) {
		return group.defaultBindGroup, nil
	}
	entries := make([]gfx.BindGroupEntry, len(group.entries))
	for index, layoutEntry := range group.entries {
		entry, err := bindingResources.makeEntry(device, layoutEntry)
		if err != nil {
			return gfx.BindGroup{}, err
		}
		entries[index] = entry
	}
	bindGroup, err := device.CreateBindGroup(gfx.BindGroupDescriptor{Label: "render: the default bind group", Layout: group.layout, Entries: entries})
	if err != nil {
		return gfx.BindGroup{}, err
	}
	group.defaultBindGroup = bindGroup
	return bindGroup, nil
}

// acquireGroupLayout hands out the bind-group layout of group, whose entries
// make key, counting one more holder of it, and makes it when none is held.
// i's mutex is held, and it panics when it is not.
func (i *layoutInterner) acquireGroupLayout(device gfx.Device, key string, group gfx.BindGroupLayoutDescriptor) (*internedGroupLayout, error) {
	i.requireLocked("acquireGroupLayout")
	if internedLayout, ok := i.groupLayouts[key]; ok {
		internedLayout.holderCount++
		return internedLayout, nil
	}
	layout, err := device.CreateBindGroupLayout(group)
	if err != nil {
		return nil, err
	}
	internedLayout := &internedGroupLayout{layout: layout, key: key, entries: group.Entries, holderCount: 1}
	for _, entry := range group.Entries {
		if entry.Buffer.HasDynamicOffset {
			internedLayout.dynamicOffsetCount++
		}
	}
	i.groupLayouts[key] = internedLayout
	return internedLayout, nil
}

// releaseGroupLayouts counts one holder fewer of each of groups, and
// releases each at its last. i's mutex is held, and it panics when it is
// not.
func (i *layoutInterner) releaseGroupLayouts(device gfx.Device, groups []*internedGroupLayout) {
	i.requireLocked("releaseGroupLayouts")
	for _, internedLayout := range groups {
		if internedLayout.holderCount--; internedLayout.holderCount == 0 {
			if internedLayout.defaultBindGroup != (gfx.BindGroup{}) {
				device.ReleaseBindGroup(internedLayout.defaultBindGroup)
			}
			device.ReleaseBindGroupLayout(internedLayout.layout)
			delete(i.groupLayouts, internedLayout.key)
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

// internedGroupLayout is a bind-group layout layoutInterner holds: the
// layout, the key its entries make, its entries, how many of them bind at a
// dynamic offset, the bind group of its entries as they draw unbound, none
// until first bound, and how many shaders' pipeline layouts hold it.
type internedGroupLayout struct {
	layout             gfx.BindGroupLayout
	key                string
	entries            []gfx.BindGroupLayoutEntry
	dynamicOffsetCount int
	defaultBindGroup   gfx.BindGroup
	holderCount        int
}

// internedPipelineLayout is a pipeline layout layoutInterner holds, its key,
// its groups' layouts, and how many shaders hold it.
type internedPipelineLayout struct {
	layout      gfx.PipelineLayout
	key         string
	groups      []*internedGroupLayout
	holderCount int
}
