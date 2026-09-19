package kernel

import "iter"

// WithPlugins validates, orders, and registers plugins before Run starts them.
func (e *Engine) WithPlugins(plugins ...Plugin) *Engine {
	accepted := make([]Plugin, 0, len(plugins))
	for _, plugin := range plugins {
		if _, ok := e.pluginNames[plugin.Name()]; ok {
			e.failComposition(ErrConflictingPluginName{plugin.Name()})
			return e
		}
		e.pluginNames[plugin.Name()] = struct{}{}
		accepted = append(accepted, plugin)
	}

	// Every declared dependency must be among the registered plugins; the engine
	// validates presence only and never reorders or auto-adds plugins.
	for _, plugin := range accepted {
		for _, dep := range plugin.Dependencies() {
			if _, ok := e.pluginNames[dep]; ok {
				continue
			}
			e.failComposition(ErrMissingPluginDependency{Plugin: plugin.Name(), Dependency: dep})
			return e
		}
	}
	ordered, cycle := orderPlugins(accepted)
	if cycle != nil {
		e.failComposition(ErrPluginDependencyCycle{Plugins: cycle})
		return e
	}
	accepted = ordered

	for _, candidate := range pluginsOf[PluginHost](accepted) {
		if e.host != nil {
			e.failComposition(ErrMultipleHosts{First: e.host.Name(), Second: candidate.Name()})
			return e
		}
		e.host = candidate
	}

	closure := e.dependencyClosure(accepted)
	for _, plugin := range accepted {
		registrar := &Registrar{registry: e.registry, owner: plugin.Name(), allowed: closure[plugin.Name()]}
		if err := callPluginBoundary(plugin.Name(), "Register", func() error {
			return plugin.Register(registrar, e.config[plugin.Name()])
		}); err != nil {
			e.failComposition(err)
			return e
		}
	}
	if err := e.registry.finalize(closure); err != nil {
		e.failComposition(err)
		return e
	}
	e.plugins = accepted

	return e
}

// dependencyClosure maps each plugin to the plugins it may couple to: itself plus
// the transitive closure of its declared dependencies.
func (e *Engine) dependencyClosure(plugins []Plugin) map[PluginName]map[PluginName]struct{} {
	direct := make(map[PluginName][]PluginName, len(plugins))
	for _, plugin := range plugins {
		direct[plugin.Name()] = plugin.Dependencies()
	}
	closure := make(map[PluginName]map[PluginName]struct{}, len(plugins))
	for _, plugin := range plugins {
		reachable := map[PluginName]struct{}{plugin.Name(): {}}
		pending := append([]PluginName(nil), direct[plugin.Name()]...)
		for len(pending) > 0 {
			next := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if _, seen := reachable[next]; seen {
				continue
			}
			reachable[next] = struct{}{}
			pending = append(pending, direct[next]...)
		}
		closure[plugin.Name()] = reachable
	}
	return closure
}

// failComposition records the initialization failure Run answers with. It runs
// during WithPlugins, which is single-threaded, and reports it too, so that the
// handler says it the way it says everything else.
func (e *Engine) failComposition(err error) {
	e.composition = err
	e.reportError(err)
	e.markReady()
}

// pluginsOf yields every plugin satisfying T, in registration order, paired
// with its index in plugins. It is the engine's only filtered plugin lookup:
// the host, start and stop passes all ask it, and nothing outside the engine
// can. The index is what lets Run cut the list at a plugin whose Start failed.
func pluginsOf[T any](plugins []Plugin) iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for index, plugin := range plugins {
			match, ok := any(plugin).(T)
			if !ok {
				continue
			}
			if !yield(index, match) {
				return
			}
		}
	}
}

func orderPlugins(plugins []Plugin) ([]Plugin, []PluginName) {
	byName := make(map[PluginName]int, len(plugins))
	for i, plugin := range plugins {
		byName[plugin.Name()] = i
	}

	ordered := make([]Plugin, 0, len(plugins))
	completed := make(map[PluginName]struct{}, len(plugins))
	for len(ordered) < len(plugins) {
		next := -1
		for i, plugin := range plugins {
			if _, ok := completed[plugin.Name()]; ok {
				continue
			}
			ready := true
			for _, dependency := range plugin.Dependencies() {
				if _, registered := byName[dependency]; !registered {
					continue
				}
				if _, done := completed[dependency]; !done {
					ready = false
					break
				}
			}
			if ready {
				next = i
				break
			}
		}
		if next < 0 {
			cycle := make([]PluginName, 0, len(plugins)-len(ordered))
			for _, plugin := range plugins {
				if _, ok := completed[plugin.Name()]; !ok {
					cycle = append(cycle, plugin.Name())
				}
			}
			return nil, cycle
		}
		plugin := plugins[next]
		ordered = append(ordered, plugin)
		completed[plugin.Name()] = struct{}{}
	}
	return ordered, nil
}
