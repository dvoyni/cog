package ecsscene

import (
	"fmt"

	"github.com/dvoyni/cog/ecs"
	"github.com/dvoyni/cog/scene"
)

// manifest is the two name tables the binding resolves a Component through.
// Their zero values are ready to use, so a manifest is usable as soon as it
// exists and a game with no assets registers none.
type manifest struct {
	models ecs.Names[ModelHash, string]
	clips  ecs.Names[ClipHash, string]
}

// fill registers the Config's manifest rows. It runs during Registration, on a
// table nothing has read yet, and a collision — two names landing on one hash —
// is returned rather than tolerated, because an undetected one silently draws
// the wrong model.
func (m *manifest) fill(config Config) error {
	for _, entry := range config.Models {
		if entry.Path == "" {
			return fmt.Errorf("ecsscene: the model %q names no path", entry.Name)
		}
		if err := m.models.Register(entry.Name, entry.Path); err != nil {
			return fmt.Errorf("ecsscene: registering the model %q: %w", entry.Name, err)
		}
	}
	for _, entry := range config.Clips {
		if entry.Clip == "" {
			return fmt.Errorf("ecsscene: the clip %q names no clip in the file", entry.Name)
		}
		if err := m.clips.Register(entry.Name, entry.Clip); err != nil {
			return fmt.Errorf("ecsscene: registering the clip %q: %w", entry.Name, err)
		}
	}
	return nil
}

// Model reports the storage path a ModelHash names, and whether anything was
// registered under it. A hash nobody declared misses and leaves the table the
// size it was, so a procedural name costs one failed probe and no memory.
func (m *manifest) Model(name ModelHash) (string, bool) { return m.models.Lookup(name) }

// Clip reports the clip name inside the file a ClipHash names.
func (m *manifest) Clip(name ClipHash) (string, bool) { return m.clips.Lookup(name) }

// ModelText and ClipText recover the string a hash was registered under, so a
// hash stays legible in a log line, a debugger or an inspector. They are the
// reason a manifest row keeps its name beside its value.
func (m *manifest) ModelText(name ModelHash) (string, bool) { return m.models.TextOf(name) }

func (m *manifest) ClipText(name ClipHash) (string, bool) { return m.clips.TextOf(name) }

// clipPlays rebuilds one drawable's play list into the recording System's own
// scratch, and returns it for the caller to keep: append may have grown it, and
// a scratch that silently reverts to its old backing every frame would allocate
// every frame.
//
// The plays are variable-length draw data, which is not a Component: the
// Component holds a fixed-capacity array because scene's own cap is four, and
// what scene takes is a slice. Scene copies it into its frame arena at record
// time and says so, so the same scratch serves the next drawable the moment the
// call returns.
//
// A play naming a clip the manifest never registered is dropped rather than
// reported. The report belongs to scene, which is the side that knows whether
// the file declares the clip at all, and a per-frame per-entity report here
// would be the same message a few thousand times.
func (m *manifest) clipPlays(scratch []scene.ClipPlay, animation *Animation) []scene.ClipPlay {
	plays := scratch[:0]
	for i := range animation.Plays {
		play := &animation.Plays[i]
		if play.Clip == ecs.NoHash {
			continue
		}
		name, ok := m.clips.Lookup(play.Clip)
		if !ok {
			continue
		}
		plays = append(plays, scene.ClipPlay{
			Clip: name, Time: play.Time, Loop: play.Loop, Weight: play.Weight,
		})
	}
	return plays
}
