package recording_test

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
)

func TestAReflectionAnswersTheLayoutsQueuedForItCopiedAndNoneByDefault(t *testing.T) {
	for _, test := range []struct {
		name      string
		kind      recording.CallKind
		arguments func(live objects) any
		reflect   func(device gfx.BackendDevice, arguments any) ([]gfx.BindGroupLayoutDescriptor, error)
	}{
		{"a render pipeline's", recording.CallKindReflectRenderPipelineLayout,
			func(live objects) any {
				descriptor := renderPipelineDescriptor(live)
				descriptor.Layout = gfx.PipelineLayout{}
				return descriptor
			},
			func(device gfx.BackendDevice, arguments any) ([]gfx.BindGroupLayoutDescriptor, error) {
				return device.ReflectRenderPipelineLayout(arguments.(gfx.RenderPipelineDescriptor))
			}},
		{"a compute pipeline's", recording.CallKindReflectComputePipelineLayout,
			func(live objects) any { return gfx.ComputePipelineDescriptor{Module: live.module, EntryPoint: "cs"} },
			func(device gfx.BackendDevice, arguments any) ([]gfx.BindGroupLayoutDescriptor, error) {
				return device.ReflectComputePipelineLayout(arguments.(gfx.ComputePipelineDescriptor))
			}},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance, device := requestDevice(t)
			live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
			arguments := test.arguments(live)
			queued := []gfx.BindGroupLayoutDescriptor{
				{Entries: []gfx.BindGroupLayoutEntry{{
					Binding:    0,
					Visibility: gfx.ShaderStageFlagVertex | gfx.ShaderStageFlagFragment,
					Buffer:     gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform},
				}}},
				{},
			}
			want := []gfx.BindGroupLayoutDescriptor{
				{Entries: []gfx.BindGroupLayoutEntry{{
					Binding:    0,
					Visibility: gfx.ShaderStageFlagVertex | gfx.ShaderStageFlagFragment,
					Buffer:     gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform},
				}}},
				{},
			}
			instance.AnswerNext(test.kind, recording.Answer{BindGroupLayouts: queued})

			layouts, err := test.reflect(device, arguments)
			mustSucceed(t, err)
			queued[0].Entries[0].Binding = 7

			if !reflect.DeepEqual(layouts, want) {
				t.Errorf("the reflection answered %+v, want %+v", layouts, want)
			}
			calls := instance.Recording().Calls
			if last := calls[len(calls)-1]; last.Kind != test.kind || !reflect.DeepEqual(last.Arguments, arguments) || last.Error != nil {
				t.Errorf("last call = %+v, want %v of %+v", last, test.kind, arguments)
			}
			unqueued, err := test.reflect(device, arguments)
			if err != nil || unqueued != nil {
				t.Errorf("with nothing queued the reflection answered %+v, %v, want no layouts", unqueued, err)
			}
		})
	}
}

func TestAVertexReflectionAnswersTheInputsQueuedForItCopiedAndNoneByDefault(t *testing.T) {
	instance, device := requestDevice(t)
	live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
	queued := []gfx.VertexInput{{Location: 0, Type: gfx.ScalarTypeFloat32, Components: 3}}
	want := []gfx.VertexInput{{Location: 0, Type: gfx.ScalarTypeFloat32, Components: 3}}
	instance.AnswerNext(recording.CallKindReflectVertexInputs, recording.Answer{VertexInputs: queued})

	inputs, err := device.ReflectVertexInputs(live.module, "vs")
	mustSucceed(t, err)
	queued[0].Location = 7

	if !reflect.DeepEqual(inputs, want) {
		t.Errorf("the reflection answered %+v, want %+v", inputs, want)
	}
	calls := instance.Recording().Calls
	wantCall := recording.Call{Kind: recording.CallKindReflectVertexInputs, Arguments: recording.ReflectVertexInputsArguments{Module: live.module, EntryPoint: "vs"}}
	if last := calls[len(calls)-1]; !reflect.DeepEqual(last, wantCall) {
		t.Errorf("last call = %+v, want %+v", last, wantCall)
	}
	unqueued, err := device.ReflectVertexInputs(live.module, "vs")
	if err != nil || unqueued != nil {
		t.Errorf("with nothing queued the reflection answered %+v, %v, want no inputs", unqueued, err)
	}
}

func TestABindingReflectionAnswersTheBindingsQueuedForItCopiedAndNoneByDefault(t *testing.T) {
	instance, device := requestDevice(t)
	live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
	rows := func() []gfx.ShaderBinding {
		row := gfx.ShaderType{Size: 16, Members: []gfx.ShaderStructMember{{Name: "position", Type: gfx.ShaderType{Size: 12}}}}
		return []gfx.ShaderBinding{{Name: "rows", Type: gfx.ShaderType{Size: 16, Element: &row, ElementStride: 16}, EntryPoints: []string{"vs"}}}
	}
	queued := rows()
	want := rows()
	instance.AnswerNext(recording.CallKindReflectBindings, recording.Answer{Bindings: queued})

	bindings, err := device.ReflectBindings(live.module)
	mustSucceed(t, err)
	queued[0].Type.Element.Members[0].Offset = 7
	queued[0].EntryPoints[0] = "changed"

	if !reflect.DeepEqual(bindings, want) {
		t.Errorf("the reflection answered %+v, want %+v", bindings, want)
	}
	calls := instance.Recording().Calls
	wantCall := recording.Call{Kind: recording.CallKindReflectBindings, Arguments: live.module}
	if last := calls[len(calls)-1]; !reflect.DeepEqual(last, wantCall) {
		t.Errorf("last call = %+v, want %+v", last, wantCall)
	}
	unqueued, err := device.ReflectBindings(live.module)
	if err != nil || unqueued != nil {
		t.Errorf("with nothing queued the reflection answered %+v, %v, want no bindings", unqueued, err)
	}
}

func TestAnEntryPointReflectionAnswersTheEntryPointsQueuedForItCopiedAndNoneByDefault(t *testing.T) {
	instance, device := requestDevice(t)
	live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
	queued := []gfx.ShaderEntryPoint{{Name: "vs", Stage: gfx.ShaderStageFlagVertex}}
	want := []gfx.ShaderEntryPoint{{Name: "vs", Stage: gfx.ShaderStageFlagVertex}}
	instance.AnswerNext(recording.CallKindReflectEntryPoints, recording.Answer{EntryPoints: queued})

	entryPoints, err := device.ReflectEntryPoints(live.module)
	mustSucceed(t, err)
	queued[0].Name = "changed"

	if !reflect.DeepEqual(entryPoints, want) {
		t.Errorf("the reflection answered %+v, want %+v", entryPoints, want)
	}
	calls := instance.Recording().Calls
	wantCall := recording.Call{Kind: recording.CallKindReflectEntryPoints, Arguments: live.module}
	if last := calls[len(calls)-1]; !reflect.DeepEqual(last, wantCall) {
		t.Errorf("last call = %+v, want %+v", last, wantCall)
	}
	unqueued, err := device.ReflectEntryPoints(live.module)
	if err != nil || unqueued != nil {
		t.Errorf("with nothing queued the reflection answered %+v, %v, want no entry points", unqueued, err)
	}
}
