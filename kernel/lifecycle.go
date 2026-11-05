package kernel

import "fmt"

// lifecycle runs a kernel's plugins through Init, Start, Stop and Destroy: up
// in plugin order, down in reverse. A step that fails rolls back what that
// step already brought up, and every failure it answers or logs names the
// plugin.
type lifecycle struct {
	// kernel owns the lifecycle: each Init registers into it, Start and Stop
	// are handed it, and failures are logged through its Logger.
	kernel *Kernel
	// plugins is the order Init and Start run in; Stop and Destroy run it in
	// reverse.
	plugins []Plugin
	// config holds each plugin's part by its name, which its Init is handed.
	config map[PluginName]any
}

// init runs every plugin's Init in order, each with its part of config. Two
// plugins of one name are a composition error, and panic before any Init
// runs. If an Init fails, init destroys the plugins already initialized, in
// reverse, and answers that failure.
func (l *lifecycle) init() error {
	for i, plugin := range l.plugins {
		for _, earlier := range l.plugins[:i] {
			if earlier.Name() == plugin.Name() {
				panic(fmt.Sprintf("kernel: spawning a kernel with two plugins named %q", plugin.Name()))
			}
		}
	}
	for i, plugin := range l.plugins {
		if err := plugin.Init(&Configurator{kernel: l.kernel, plugin: plugin.Name()}, l.config[plugin.Name()]); err != nil {
			l.destroyFrom(i - 1)
			return fmt.Errorf("kernel: plugin %q failed to init: %w", plugin.Name(), err)
		}
	}
	return nil
}

// start runs every plugin's Start in order. If a Start fails, start stops the
// plugins already started, in reverse, not the one that failed, and answers
// that failure.
func (l *lifecycle) start() error {
	for i, plugin := range l.plugins {
		if err := plugin.Start(l.kernel); err != nil {
			l.stopFrom(i - 1)
			return fmt.Errorf("kernel: plugin %q failed to start: %w", plugin.Name(), err)
		}
	}
	return nil
}

// stop runs every plugin's Stop in reverse, logging each failure.
func (l *lifecycle) stop() {
	l.stopFrom(len(l.plugins) - 1)
}

// destroy runs every plugin's Destroy in reverse, logging each failure.
func (l *lifecycle) destroy() {
	l.destroyFrom(len(l.plugins) - 1)
}

// stopFrom runs Stop on plugins[last] down to the first, logging each
// failure.
func (l *lifecycle) stopFrom(last int) {
	for i := last; i >= 0; i-- {
		if err := l.plugins[i].Stop(l.kernel); err != nil {
			l.kernel.logger.LogErr(fmt.Errorf("kernel: plugin %q failed to stop: %w", l.plugins[i].Name(), err))
		}
	}
}

// destroyFrom runs Destroy on plugins[last] down to the first, logging each
// failure.
func (l *lifecycle) destroyFrom(last int) {
	for i := last; i >= 0; i-- {
		if err := l.plugins[i].Destroy(); err != nil {
			l.kernel.logger.LogErr(fmt.Errorf("kernel: plugin %q failed to destroy: %w", l.plugins[i].Name(), err))
		}
	}
}
