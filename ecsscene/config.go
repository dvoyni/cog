package ecsscene

import (
	"fmt"
	"slices"
)

// DefaultDrawables is the peak drawable population the Component Stores reserve
// for when a Config names no other number. It is a hint and not a cap: a Store
// grows by doubling past it.
const DefaultDrawables = 1024

// Config is the binding's manifest and its one sizing hint.
//
// The manifest is configuration rather than an API because a name table is
// filled at Registration, before any System reads it, and is then read-only for
// the engine lifetime. A game that discovers its model list at runtime reads
// its own file and builds this Config before kernel.New, which is where the
// plugin set is fixed anyway.
type Config struct {
	// Models is the manifest: which names resolve to which storage paths.
	Models []ModelEntry
	// Clips is the same for animation clips.
	Clips []ClipEntry
	// Drawables is the peak population the three Component Stores reserve for.
	Drawables int
}

// ModelEntry is one manifest row: the name a Component hashes, and the storage
// path scene loads. They are usually different — "crate" against
// "models/props/crate.glb" — which is why the name is not derived from the
// path: a path is where a file happens to live this month, and a Component
// carrying its hash would be renamed by a directory move.
type ModelEntry struct {
	Name string
	Path string
}

// ClipEntry is a manifest row for an animation clip: the name a Component
// hashes, and the clip's name inside the glTF file. Those are usually the same
// string, and the pair exists for when they are not — a file exported with
// "Armature|Walk" is named "walk" by the game.
type ClipEntry struct {
	Name string
	Clip string
}

// DefaultConfig returns the empty manifest at the default population hint. A
// binding with no models registered is legal and draws nothing, which is what a
// game looks like before it has assets.
func DefaultConfig() Config { return Config{Drawables: DefaultDrawables} }

// WithModel adds or replaces one manifest row. The returned Config owns its
// slice and does not mutate c.
func (c Config) WithModel(name, path string) Config {
	c.Models = slices.Clone(c.Models)
	for i := range c.Models {
		if c.Models[i].Name == name {
			c.Models[i].Path = path
			return c
		}
	}
	c.Models = append(c.Models, ModelEntry{Name: name, Path: path})
	return c
}

// WithClip adds or replaces one clip row, naming the clip inside the file. Pass
// the same string twice where the game's name for a clip is the file's.
func (c Config) WithClip(name, clip string) Config {
	c.Clips = slices.Clone(c.Clips)
	for i := range c.Clips {
		if c.Clips[i].Name == name {
			c.Clips[i].Clip = clip
			return c
		}
	}
	c.Clips = append(c.Clips, ClipEntry{Name: name, Clip: clip})
	return c
}

// WithDrawables replaces the population hint the Component Stores reserve for.
func (c Config) WithDrawables(count int) Config {
	c.Drawables = count
	return c
}

func resolveConfig(value any) (Config, error) {
	config := DefaultConfig()
	if value != nil {
		var ok bool
		config, ok = value.(Config)
		if !ok {
			return Config{}, fmt.Errorf("ecsscene: invalid config %T", value)
		}
	}
	if config.Drawables < 0 {
		return Config{}, fmt.Errorf("ecsscene: Drawables must not be negative, got %d", config.Drawables)
	}
	// Zero is unspecified rather than wrong: a Config literal that names only a
	// manifest gets the default hint, and a hint is not a cap either way.
	if config.Drawables == 0 {
		config.Drawables = DefaultDrawables
	}
	return config, nil
}
