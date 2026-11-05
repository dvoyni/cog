package kernel

import (
	"fmt"
	"testing"
)

// physicsConfig is a plugin's config in the tests of PluginConfigOrDefault.
type physicsConfig struct{ Gravity float32 }

func TestPluginConfigOrDefaultAnswersThePartOrAZeroConfig(t *testing.T) {
	s := NewSupervisor(nil)
	t.Cleanup(s.Quit)
	given := &physicsConfig{Gravity: -9.8}
	received := map[PluginName]*physicsConfig{}
	receiver := func(name PluginName) hookPlugin {
		return hookPlugin{name: name, receive: func(config any) { received[name] = PluginConfigOrDefault[physicsConfig](config) }}
	}
	config := map[PluginName]any{"physics": given, "nil pointer": (*physicsConfig)(nil)}
	if _, err := s.Spawn(config, 2, receiver("physics"), receiver("nil pointer"), receiver("missing")); err != nil {
		t.Fatal(err)
	}
	if received["physics"] != given {
		t.Errorf("the physics plugin got %v, want the pointer given", received["physics"])
	}
	for _, name := range []PluginName{"nil pointer", "missing"} {
		if got := received[name]; got == nil || *got != (physicsConfig{}) {
			t.Errorf("the %s plugin got %v, want a zero config", name, got)
		}
	}
	if received["nil pointer"] == received["missing"] {
		t.Error("two plugins share one zero config")
	}
}

func TestPluginConfigOrDefaultPanicsOnAnythingButAPointerToTheConfig(t *testing.T) {
	for _, config := range []any{physicsConfig{}, "vulkan", &struct{ Gravity float32 }{}} {
		t.Run(fmt.Sprintf("%T", config), func(t *testing.T) {
			s := NewSupervisor(nil)
			t.Cleanup(s.Quit)
			want := fmt.Sprintf("config is a %T, want a *kernel.physicsConfig", config)
			expectPanicSaying(t, want, func() {
				s.Spawn(map[PluginName]any{"physics": config}, 2, hookPlugin{name: "physics", receive: func(config any) {
					PluginConfigOrDefault[physicsConfig](config)
				}})
			})
		})
	}
}

func TestPluginConfigOrDefaultAnswersTheDefaultGivenWhenThereIsNoPart(t *testing.T) {
	s := NewSupervisor(nil)
	t.Cleanup(s.Quit)
	given := &physicsConfig{Gravity: -9.8}
	fallback := &physicsConfig{Gravity: -1.6}
	received := map[PluginName]*physicsConfig{}
	receiver := func(name PluginName) hookPlugin {
		return hookPlugin{name: name, receive: func(config any) { received[name] = PluginConfigOrDefault(config, fallback) }}
	}
	config := map[PluginName]any{"physics": given, "nil pointer": (*physicsConfig)(nil)}
	if _, err := s.Spawn(config, 2, receiver("physics"), receiver("nil pointer"), receiver("missing")); err != nil {
		t.Fatal(err)
	}
	if received["physics"] != given {
		t.Errorf("the physics plugin got %v, want the pointer given over the default", received["physics"])
	}
	for _, name := range []PluginName{"nil pointer", "missing"} {
		if received[name] != fallback {
			t.Errorf("the %s plugin got %v, want the default", name, received[name])
		}
	}
}

func TestPluginConfigOrDefaultPanicsOnTwoDefaults(t *testing.T) {
	expectPanicSaying(t, "at most one default", func() {
		PluginConfigOrDefault(nil, &physicsConfig{}, &physicsConfig{})
	})
}
