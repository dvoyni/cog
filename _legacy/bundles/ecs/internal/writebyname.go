package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/dvoyni/cog/kernel"
)

// Writing the world by Component name: the three write Command factories the
// ecs plugin registers beside the reads, for an Agent that sets up a situation
// in a running game. See ecs.md § Writing the world by name and
// bundles/ecs/docs/specs/mcp.md § Writing.
//
// They hold what the reads hold, write{*Entities} and nothing else, so a write
// lands between Systems and never inside one's run, and no frame's lock set
// widens. They declare no Store: a Spawn[S] declares write{*Store[C]} for each
// C it carries, which is what makes a plugin fabricating another's Components
// declare a dependency on their owner, and these do not. That is the one
// exception to the ownership rule, and it is ecs's: the authority over every
// Store, acting as a debugger an app composes by composing the broker.
//
// Every act is recorded in the Hook logs exactly as the same act from a System
// would be - a Spawn, a Despawn, an addition, a change, a removal - so an index
// or a cache built on Hooks stays true. A change names no System (writer 0), so
// every reader is given it. An act here is not a System's run, so it counts
// toward no reader's pace.
//
// A request is atomic. Every name is resolved, the Entity checked and every
// value decoded before anything is applied; any refusal answers the whole
// request, naming every entry that failed, and applies nothing.

// Outcomes of one entry of an update.
const (
	outcomeAdded     = "added"
	outcomeChanged   = "changed"
	outcomeUnchanged = "unchanged"
	outcomeRemoved   = "removed"
	outcomeAbsent    = "absent"
)

// SpawnRequest names the Components a new Entity carries, each with its value
// as ecs_entity renders one.
type SpawnRequest struct {
	Components map[string]any `json:"components" jsonschema:"Component name, as ecs_world lists it, to its value as ecs_entity shows one; give none for a bare Entity"`
}

// SpawnResponse is the Entity spawned.
type SpawnResponse struct {
	// Entity is the new Entity as Entity.String renders it.
	Entity string `json:"entity"`
	// Refusal is non-empty exactly when the request was refused, and every
	// other field is then zero.
	Refusal string `json:"-"`
}

// DespawnRequest names one Entity, as the reads name one.
type DespawnRequest struct {
	Entity string `json:"entity" jsonschema:"the Entity as 7v2 or Entity(7v2) or its decimal handle: any form a log or an earlier answer gave"`
}

// DespawnResponse says whether the Entity was alive to be despawned.
type DespawnResponse struct {
	// Entity is the Entity as Entity.String renders it.
	Entity string `json:"entity"`
	// WasAlive is false when there was nothing to despawn: the Entity had
	// already gone.
	WasAlive bool `json:"wasAlive" jsonschema:"false when the Entity was already gone, so nothing was despawned"`
	// Refusal is non-empty exactly when the request was refused.
	Refusal string `json:"-"`
}

// UpdateRequest gives one Entity Components or new values for its own, and
// takes Components away.
type UpdateRequest struct {
	Entity string         `json:"entity" jsonschema:"the Entity as 7v2 or Entity(7v2) or its decimal handle: any form a log or an earlier answer gave"`
	Set    map[string]any `json:"set,omitempty" jsonschema:"Component name to value: a Component the Entity carries takes the fields given and keeps the rest, and one it lacks is added with the fields given and the rest zero"`
	Remove []string       `json:"remove,omitempty" jsonschema:"Component names to take away from the Entity"`
}

// UpdateResponse is what each entry of an update did.
type UpdateResponse struct {
	// Entity is the Entity as Entity.String renders it.
	Entity string `json:"entity"`
	// Components is one outcome per named Component, sorted by name.
	Components []ComponentOutcome `json:"components"`
	// Refusal is non-empty exactly when the request was refused, and every
	// other field is then zero.
	Refusal string `json:"-"`
}

// ComponentOutcome is what an update did to one Component.
type ComponentOutcome struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome" jsonschema:"added, changed, unchanged (the value given is the value it had, so nothing was written), removed, or absent (removed, but the Entity did not carry it)"`
}

// UnmarshalJSON decodes the values with UseNumber, as the reads encode them,
// so an Entity Reference past 2^53 reaches its field exact rather than through
// a float64.
func (r *SpawnRequest) UnmarshalJSON(data []byte) error {
	type plain SpawnRequest
	return decodeNumbers(data, (*plain)(r))
}

// UnmarshalJSON decodes as SpawnRequest's does.
func (r *UpdateRequest) UnmarshalJSON(data []byte) error {
	type plain UpdateRequest
	return decodeNumbers(data, (*plain)(r))
}

func decodeNumbers(data []byte, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(into)
}

// stagedWrite is one decoded value, checked and waiting to be applied: what
// applying it will do, and the application, which takes the Entity so a spawn
// can stage before its Entity exists.
type stagedWrite struct {
	outcome string
	apply   func(e Entity)
}

// writesFor bakes the two per-type closures the write Commands ask of a Store
// they reach by name: stage and drop. They are baked for the reason encode is:
// the Commands hold the Component only as a name, and a generic cannot be
// instantiated from one. Both run only under write{*Entities}.
func writesFor[C any](store *Store[C], class *componentClass) {
	// stage decodes given over e's value, or over the zero value when fresh or
	// when e has none, and refuses a value that does not decode or names a
	// field C does not have. A value that encodes as the one e has is
	// unchanged and applies nothing, which is what keeps an unedited round
	// trip byte-identical: a decoded List is a new array, so a byte compare
	// would call it a change.
	class.stage = func(e Entity, fresh bool, given any) (stagedWrite, error) {
		var value C
		had := false
		if !fresh {
			value, had = store.Get(e)
		}
		before := value
		encodedGiven, err := json.Marshal(given)
		if err != nil {
			return stagedWrite{}, err
		}
		if err := json.Unmarshal(encodedGiven, &value); err != nil {
			return stagedWrite{}, fmt.Errorf("%w; give it in the shape ecs_entity shows it", err)
		}
		after, err := json.Marshal(value)
		if err != nil {
			return stagedWrite{}, err
		}
		if stray := strayFields(given, after); len(stray) > 0 {
			return stagedWrite{}, fmt.Errorf("it has no field %s; its fields are as ecs_entity shows them: %s",
				strings.Join(stray, ", "), after)
		}
		if !had {
			return stagedWrite{outcome: outcomeAdded, apply: func(e Entity) {
				switch {
				case fresh && store.watch&recordsSpawn != 0:
					store.setRecorded(e, value)
				case !fresh && store.watch&recordsAddition != 0:
					store.add(e, value)
					store.hooks.added(e, kindAdded|kindChanged)
				default:
					store.add(e, value)
				}
			}}, nil
		}
		if current, err := json.Marshal(before); err == nil && bytes.Equal(current, after) {
			return stagedWrite{outcome: outcomeUnchanged}, nil
		}
		return stagedWrite{outcome: outcomeChanged, apply: func(e Entity) {
			store.update(e, value)
			if class.size > 0 && store.watch&kindChanged != 0 {
				store.hooks.changed(e, 0)
			}
		}}, nil
	}
	// drop is Remove.From: the removal recorded with C's last value where a
	// reader watches for it.
	class.drop = func(e Entity) bool {
		if validate && len(store.lists) > 0 {
			releaseLists(store.erase(), e)
		}
		if store.watch&recordsRemoval != 0 {
			return store.removeRecorded(e)
		}
		return store.remove(e)
	}
}

// strayFields is every object key given names that the value, decoded and
// encoded again, does not have: a field C lacks, which encoding/json would
// otherwise ignore and leave an Agent believing it wrote. It descends into
// objects and arrays alike. A field hidden from the JSON (unexported) cannot be
// named, and one a Blob renders ({"len":N}) is named and kept.
func strayFields(given any, encoded []byte) []string {
	var got any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if decoder.Decode(&got) != nil {
		return nil
	}
	var stray []string
	var walk func(given, got any, path string)
	walk = func(given, got any, path string) {
		switch g := given.(type) {
		case map[string]any:
			have, _ := got.(map[string]any)
			for key, value := range g {
				field := strings.TrimPrefix(path+"."+key, ".")
				inner, ok := have[key]
				if !ok {
					stray = append(stray, field)
					continue
				}
				walk(value, inner, field)
			}
		case []any:
			have, _ := got.([]any)
			for i := range min(len(g), len(have)) {
				walk(g[i], have[i], fmt.Sprintf("%s[%d]", path, i))
			}
		}
	}
	walk(given, got, "")
	slices.Sort(stray)
	return stray
}

// spawnCommand is the factory of the spawn by name.
func spawnCommand() (kernel.Lock, kernel.Execute[SpawnRequest, SpawnResponse]) {
	c := &readCommand{}
	return c.lock, func(_ kernel.Kernel, request SpawnRequest) SpawnResponse {
		return spawnByName(c.entities.Get(), request)
	}
}

// despawnCommand is the factory of the despawn by handle.
func despawnCommand() (kernel.Lock, kernel.Execute[DespawnRequest, DespawnResponse]) {
	c := &readCommand{}
	return c.lock, func(_ kernel.Kernel, request DespawnRequest) DespawnResponse {
		return despawnByName(c.entities.Get(), request)
	}
}

// updateCommand is the factory of the update by name.
func updateCommand() (kernel.Lock, kernel.Execute[UpdateRequest, UpdateResponse]) {
	c := &readCommand{}
	return c.lock, func(_ kernel.Kernel, request UpdateRequest) UpdateResponse {
		return updateByName(c.entities.Get(), request)
	}
}

// stagedEntry is one resolved, decoded entry of a request.
type stagedEntry struct {
	class *componentClass
	write stagedWrite
}

// stageAll resolves and decodes every named value, for e or for a fresh Entity,
// and answers them sorted by name, or every refusal among them.
func stageAll(en *Entities, e Entity, fresh bool, values map[string]any, claimed map[*componentClass]string) ([]stagedEntry, []string) {
	var staged []stagedEntry
	var refusals []string
	for _, name := range slices.Sorted(maps.Keys(values)) {
		class := en.classNamed(name)
		if class == nil {
			refusals = append(refusals, unresolved(en, name))
			continue
		}
		if earlier, ok := claimed[class]; ok {
			refusals = append(refusals, fmt.Sprintf("%q and %q name one Component, %s: name it once", earlier, name, class.owner))
			continue
		}
		claimed[class] = name
		write, err := class.stage(e, fresh, values[name])
		if err != nil {
			refusals = append(refusals, fmt.Sprintf("the value for %s does not fit it: %v", class.owner, err))
			continue
		}
		staged = append(staged, stagedEntry{class, write})
	}
	return staged, refusals
}

// refusedAll joins every refusal of one request into the one reason it answers.
func refusedAll(refusals []string) string {
	return "nothing was applied: " + strings.Join(refusals, "; ")
}

func spawnByName(en *Entities, request SpawnRequest) SpawnResponse {
	staged, refusals := stageAll(en, NoEntity, true, request.Components, map[*componentClass]string{})
	if len(refusals) > 0 {
		return SpawnResponse{Refusal: refusedAll(refusals)}
	}
	e := en.alloc()
	for _, entry := range staged {
		entry.write.apply(e)
	}
	en.countAgentWrite()
	return SpawnResponse{Entity: e.String()}
}

func despawnByName(en *Entities, request DespawnRequest) DespawnResponse {
	e, err := parseEntity(request.Entity)
	if err != nil {
		return DespawnResponse{Refusal: err.Error()}
	}
	if !readable(en, e) {
		return DespawnResponse{Entity: e.String()}
	}
	en.despawn(e)
	en.countAgentWrite()
	return DespawnResponse{Entity: e.String(), WasAlive: true}
}

func updateByName(en *Entities, request UpdateRequest) UpdateResponse {
	e, err := parseEntity(request.Entity)
	if err != nil {
		return UpdateResponse{Refusal: err.Error()}
	}
	if !readable(en, e) {
		return UpdateResponse{Refusal: notAlive(en, e)}
	}
	if len(request.Set) == 0 && len(request.Remove) == 0 {
		return UpdateResponse{Refusal: "an update names at least one Component to set or remove; registered: " + registeredNames(en)}
	}
	claimed := map[*componentClass]string{}
	staged, refusals := stageAll(en, e, false, request.Set, claimed)
	var removed []*componentClass
	for _, name := range request.Remove {
		class := en.classNamed(name)
		if class == nil {
			refusals = append(refusals, unresolved(en, name))
			continue
		}
		if earlier, ok := claimed[class]; ok {
			refusals = append(refusals, fmt.Sprintf("%q and %q name one Component, %s, to set and to remove: name it once", earlier, name, class.owner))
			continue
		}
		claimed[class] = name
		removed = append(removed, class)
	}
	if len(refusals) > 0 {
		return UpdateResponse{Refusal: refusedAll(refusals)}
	}
	outcomes := make([]ComponentOutcome, 0, len(staged)+len(removed))
	wrote := false
	for _, entry := range staged {
		if entry.write.apply != nil {
			entry.write.apply(e)
			wrote = true
		}
		outcomes = append(outcomes, ComponentOutcome{Name: entry.class.owner, Outcome: entry.write.outcome})
	}
	for _, class := range removed {
		outcome := outcomeAbsent
		if class.drop(e) {
			outcome, wrote = outcomeRemoved, true
		}
		outcomes = append(outcomes, ComponentOutcome{Name: class.owner, Outcome: outcome})
	}
	slices.SortStableFunc(outcomes, func(a, b ComponentOutcome) int { return strings.Compare(a.Name, b.Name) })
	if wrote {
		en.countAgentWrite()
	}
	return UpdateResponse{Entity: e.String(), Components: outcomes}
}

// countAgentWrite counts one write call that changed the world, which the
// census reports as agentWrites.
func (en *Entities) countAgentWrite() { en.shrinkables().agentWrites++ }

// agentWrites is how many write calls changed the world.
func (en *Entities) agentWrites() uint64 {
	if en.shrinkable == nil {
		return 0
	}
	return en.shrinkable.agentWrites
}
