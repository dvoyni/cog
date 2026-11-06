package gfx_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
)

// pipelineKind is a render or a compute pipeline as the auto layout tests
// create and release it.
type pipelineKind struct {
	name    string
	reflect recording.CallKind
	create  recording.CallKind
	release recording.CallKind
	// createPipeline creates a pipeline of live's module with layout,
	// answering its descriptor and the pipeline's handle as any.
	createPipeline func(device gfx.Device, live fixture, layout gfx.PipelineLayout) (descriptor any, pipeline any, err error)
	// layoutOf answers the descriptor with its layout replaced.
	layoutOf func(descriptor any, layout gfx.PipelineLayout) any
	// createdArguments answers the arguments CreateRenderPipeline or
	// CreateComputePipeline records for pipeline and descriptor.
	createdArguments func(pipeline any, descriptor any) any
	groupLayout      func(device gfx.Device, pipeline any, group uint32) gfx.BindGroupLayout
	releasePipeline  func(device gfx.Device, pipeline any)
}

var pipelineKinds = []pipelineKind{
	{
		name:    "render",
		reflect: recording.CallKindReflectRenderPipelineLayout,
		create:  recording.CallKindCreateRenderPipeline,
		release: recording.CallKindReleaseRenderPipeline,
		createPipeline: func(device gfx.Device, live fixture, layout gfx.PipelineLayout) (any, any, error) {
			descriptor := renderPipelineDescriptor(live)
			descriptor.Layout = layout
			pipeline, err := device.CreateRenderPipeline(descriptor)
			return descriptor, pipeline, err
		},
		layoutOf: func(descriptor any, layout gfx.PipelineLayout) any {
			replaced := descriptor.(gfx.RenderPipelineDescriptor)
			replaced.Layout = layout
			return replaced
		},
		createdArguments: func(pipeline any, descriptor any) any {
			return recording.CreateRenderPipelineArguments{Pipeline: pipeline.(gfx.RenderPipeline), Descriptor: descriptor.(gfx.RenderPipelineDescriptor)}
		},
		groupLayout: func(device gfx.Device, pipeline any, group uint32) gfx.BindGroupLayout {
			return device.RenderPipelineBindGroupLayout(pipeline.(gfx.RenderPipeline), group)
		},
		releasePipeline: func(device gfx.Device, pipeline any) { device.ReleaseRenderPipeline(pipeline.(gfx.RenderPipeline)) },
	},
	{
		name:    "compute",
		reflect: recording.CallKindReflectComputePipelineLayout,
		create:  recording.CallKindCreateComputePipeline,
		release: recording.CallKindReleaseComputePipeline,
		createPipeline: func(device gfx.Device, live fixture, layout gfx.PipelineLayout) (any, any, error) {
			descriptor := computePipelineDescriptor(live)
			descriptor.Layout = layout
			pipeline, err := device.CreateComputePipeline(descriptor)
			return descriptor, pipeline, err
		},
		layoutOf: func(descriptor any, layout gfx.PipelineLayout) any {
			replaced := descriptor.(gfx.ComputePipelineDescriptor)
			replaced.Layout = layout
			return replaced
		},
		createdArguments: func(pipeline any, descriptor any) any {
			return recording.CreateComputePipelineArguments{Pipeline: pipeline.(gfx.ComputePipeline), Descriptor: descriptor.(gfx.ComputePipelineDescriptor)}
		},
		groupLayout: func(device gfx.Device, pipeline any, group uint32) gfx.BindGroupLayout {
			return device.ComputePipelineBindGroupLayout(pipeline.(gfx.ComputePipeline), group)
		},
		releasePipeline: func(device gfx.Device, pipeline any) { device.ReleaseComputePipeline(pipeline.(gfx.ComputePipeline)) },
	},
}

// reflectedGroups answers the layouts the auto layout tests queue for a
// reflection: group 0 a uniform buffer, group 1 skipped and so empty.
func reflectedGroups() []gfx.BindGroupLayoutDescriptor {
	return []gfx.BindGroupLayoutDescriptor{
		{Entries: []gfx.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: gfx.ShaderStageFlagVertex | gfx.ShaderStageFlagFragment,
			Buffer:     gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform},
		}}},
		{},
	}
}

func TestAnAutoPipelineGetsTheReflectedLayoutsCreatedAsExplicitOnes(t *testing.T) {
	for _, kind := range pipelineKinds {
		t.Run(kind.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(nil)
			h.instance.AnswerNext(kind.reflect, recording.Answer{BindGroupLayouts: reflectedGroups()})

			h.readDevice(func(device gfx.Device) {
				live := createFixture(t, device)
				before := len(h.instance.Recording().Calls)
				descriptor, pipeline, err := kind.createPipeline(device, live, gfx.PipelineLayout{})
				if err != nil {
					t.Errorf("the auto pipeline answered %v", err)
					return
				}

				calls := h.instance.Recording().Calls[before:]
				if len(calls) != 5 {
					t.Errorf("the auto pipeline made %d calls, want the reflection, two group layouts, a pipeline layout and the pipeline: %+v", len(calls), calls)
					return
				}
				if want := (recording.Call{Kind: kind.reflect, Arguments: descriptor}); !reflect.DeepEqual(calls[0], want) {
					t.Errorf("first call = %+v, want %+v", calls[0], want)
				}
				groups := []gfx.BindGroupLayout{
					calls[1].Arguments.(recording.CreateBindGroupLayoutArguments).Layout,
					calls[2].Arguments.(recording.CreateBindGroupLayoutArguments).Layout,
				}
				for group, call := range calls[1:3] {
					want := recording.Call{Kind: recording.CallKindCreateBindGroupLayout, Arguments: recording.CreateBindGroupLayoutArguments{Layout: groups[group], Descriptor: reflectedGroups()[group]}}
					if !reflect.DeepEqual(call, want) {
						t.Errorf("group %d's layout call = %+v, want %+v", group, call, want)
					}
				}
				layout := calls[3].Arguments.(recording.CreatePipelineLayoutArguments).Layout
				if want := (recording.Call{Kind: recording.CallKindCreatePipelineLayout, Arguments: recording.CreatePipelineLayoutArguments{Layout: layout, Descriptor: gfx.PipelineLayoutDescriptor{BindGroupLayouts: groups}}}); !reflect.DeepEqual(calls[3], want) {
					t.Errorf("pipeline layout call = %+v, want %+v", calls[3], want)
				}
				if want := (recording.Call{Kind: kind.create, Arguments: kind.createdArguments(pipeline, kind.layoutOf(descriptor, layout))}); !reflect.DeepEqual(calls[4], want) {
					t.Errorf("pipeline call = %+v, want %+v", calls[4], want)
				}
				for group, want := range groups {
					if got := kind.groupLayout(device, pipeline, uint32(group)); got != want {
						t.Errorf("group %d's layout = %+v, want %+v", group, got, want)
					}
				}
				if got := kind.groupLayout(device, pipeline, 2); got != (gfx.BindGroupLayout{}) {
					t.Errorf("group 2, past the last, answered %+v, want the zero layout", got)
				}
			})
		})
	}
}

func TestAnExplicitLayoutOverridesReflection(t *testing.T) {
	for _, kind := range pipelineKinds {
		t.Run(kind.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(nil)

			h.readDevice(func(device gfx.Device) {
				live := createFixture(t, device)
				before := len(h.instance.Recording().Calls)
				descriptor, pipeline, err := kind.createPipeline(device, live, live.pipelineLayout)
				if err != nil {
					t.Errorf("the pipeline answered %v", err)
					return
				}

				calls := h.instance.Recording().Calls[before:]
				want := []recording.Call{{Kind: kind.create, Arguments: kind.createdArguments(pipeline, descriptor)}}
				if !reflect.DeepEqual(calls, want) {
					t.Errorf("the pipeline made calls %+v, want %+v alone", calls, want)
				}
				if got := kind.groupLayout(device, pipeline, 0); got != live.groupLayout {
					t.Errorf("group 0's layout = %+v, want the explicit layout's %+v", got, live.groupLayout)
				}
				if got := kind.groupLayout(device, pipeline, 1); got != (gfx.BindGroupLayout{}) {
					t.Errorf("group 1, past the last, answered %+v, want the zero layout", got)
				}

				before = len(h.instance.Recording().Calls)
				kind.releasePipeline(device, pipeline)
				released := h.instance.Recording().Calls[before:]
				if want := []recording.Call{{Kind: kind.release, Arguments: pipeline}}; !reflect.DeepEqual(released, want) {
					t.Errorf("the release made calls %+v, want %+v alone, the explicit layout kept", released, want)
				}
			})
		})
	}
}

func TestReleasingAnAutoPipelineReleasesTheLayoutsCreatedForIt(t *testing.T) {
	for _, kind := range pipelineKinds {
		t.Run(kind.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(nil)
			h.instance.AnswerNext(kind.reflect, recording.Answer{BindGroupLayouts: reflectedGroups()})

			h.readDevice(func(device gfx.Device) {
				live := createFixture(t, device)
				_, pipeline, err := kind.createPipeline(device, live, gfx.PipelineLayout{})
				if err != nil {
					t.Errorf("the auto pipeline answered %v", err)
					return
				}
				groups := []gfx.BindGroupLayout{kind.groupLayout(device, pipeline, 0), kind.groupLayout(device, pipeline, 1)}
				calls := h.instance.Recording().Calls
				layout := calls[len(calls)-2].Arguments.(recording.CreatePipelineLayoutArguments).Layout

				before := len(calls)
				kind.releasePipeline(device, pipeline)

				want := []recording.Call{
					{Kind: kind.release, Arguments: pipeline},
					{Kind: recording.CallKindReleasePipelineLayout, Arguments: layout},
					{Kind: recording.CallKindReleaseBindGroupLayout, Arguments: groups[0]},
					{Kind: recording.CallKindReleaseBindGroupLayout, Arguments: groups[1]},
				}
				if released := h.instance.Recording().Calls[before:]; !reflect.DeepEqual(released, want) {
					t.Errorf("the release made calls %+v, want %+v", released, want)
				}
				if got := kind.groupLayout(device, pipeline, 0); got != (gfx.BindGroupLayout{}) {
					t.Errorf("a released pipeline's group 0 answered %+v, want the zero layout", got)
				}
			})
		})
	}
}

func TestAFailedAutoPipelineReleasesWhatWasCreatedForIt(t *testing.T) {
	failure := errors.New("refused")
	for _, kind := range pipelineKinds {
		for _, failing := range []struct {
			name  string
			queue func(instance *recording.Instance)
		}{
			{"its reflection", func(instance *recording.Instance) {
				instance.AnswerNext(kind.reflect, recording.Answer{Error: failure})
			}},
			{"its second group's layout", func(instance *recording.Instance) {
				instance.AnswerNext(kind.reflect, recording.Answer{BindGroupLayouts: reflectedGroups()})
				instance.AnswerNext(recording.CallKindCreateBindGroupLayout, recording.Answer{})
				instance.AnswerNext(recording.CallKindCreateBindGroupLayout, recording.Answer{Error: failure})
			}},
			{"its pipeline layout", func(instance *recording.Instance) {
				instance.AnswerNext(kind.reflect, recording.Answer{BindGroupLayouts: reflectedGroups()})
				instance.AnswerNext(recording.CallKindCreatePipelineLayout, recording.Answer{Error: failure})
			}},
			{"the pipeline", func(instance *recording.Instance) {
				instance.AnswerNext(kind.reflect, recording.Answer{BindGroupLayouts: reflectedGroups()})
				instance.AnswerNext(kind.create, recording.Answer{Error: failure})
			}},
		} {
			t.Run(kind.name+" pipeline, failing at "+failing.name, func(t *testing.T) {
				h := newHarness(t)
				h.mustSpawn(nil)

				h.readDevice(func(device gfx.Device) {
					live := createFixture(t, device)
					before := len(h.instance.Recording().Calls)
					failing.queue(h.instance)
					_, pipeline, err := kind.createPipeline(device, live, gfx.PipelineLayout{})

					if !errors.Is(err, failure) || !reflect.ValueOf(pipeline).IsZero() {
						t.Errorf("the auto pipeline answered %+v, %v, want the zero handle and %v", pipeline, err, failure)
					}
					created := map[any]bool{}
					for _, call := range h.instance.Recording().Calls[before:] {
						if call.Error != nil {
							continue
						}
						switch arguments := call.Arguments.(type) {
						case recording.CreateBindGroupLayoutArguments:
							created[arguments.Layout] = true
						case recording.CreatePipelineLayoutArguments:
							created[arguments.Layout] = true
						case gfx.BindGroupLayout, gfx.PipelineLayout:
							delete(created, arguments)
						}
					}
					if len(created) != 0 {
						t.Errorf("left created and not released: %v", created)
					}
				})
			})
		}
	}
}
