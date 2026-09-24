package types

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/dvoyni/cog/kernel"
)

// Reading the world by Component name: three read-only Command factories the
// ecs plugin registers, for a caller outside Go that knows a Component only as
// the string kernel.TypeName renders for it. See ecs.md § Reading the world by
// name.
//
// The price, stated where it is paid. Each factory declares write{*Entities}
// and nothing else. Every handler that touches a Store holds read{*Entities},
// so a call waits for every running ECS System to let go of the authority, and
// under Conflict-aware FIFO every ECS System queued behind it waits until it
// returns. readLimit bounds how long that is. No frame's lock set widens: these
// are Commands, not Systems, and a frame nobody reads from pays nothing. They
// do appear in Describe().Contention as writers of *Entities that conflict with
// every System.
//
// Every value is encoded to JSON while the lock is held and decoded back into
// plain maps, slices, strings, json.Number and bools before the handler
// returns, so an answer shares no memory with any Store: a copied List shares
// its backing array, and a System may Set through it the moment the lock is
// released.
//
// An unknown name, a malformed or dead Entity and a limit out of range are
// expected outcomes, so they are answered in the response as a Refusal naming
// what would have worked, and every other field is then zero.

const (
	// defaultReadLimit is how many Entities a query encodes when it asks for no
	// limit, and maxReadLimit the most it may ask for. Together they bound the
	// stall: a query holds write{*Entities} for its walk plus that many
	// Entities' encodings, and every ECS System waits that long.
	defaultReadLimit = 50
	maxReadLimit     = 500
)

// CensusRequest asks for the census: every Component name and population. It
// has no fields.
type CensusRequest struct{}

// CensusResponse is every registered Component with its Store's population,
// and the authority's index accounting.
type CensusResponse struct {
	// Entities is how many Entities are alive.
	Entities int `json:"entities"`
	// FreeIndices is how many indices wait on the free list for reuse.
	FreeIndices int `json:"freeIndices"`
	// IndexSpace is how many indices have been allocated, alive or free.
	IndexSpace int `json:"indexSpace"`
	// AgentWrites is how many write Commands changed the world since the
	// Engine started: a run it is not zero for is not the run the game alone
	// would have made.
	AgentWrites uint64 `json:"agentWrites" jsonschema:"how many ecs_spawn, ecs_despawn and ecs_update calls have changed the world since the game started; not zero means this run is not the one the game alone would have made"`
	// Components is every registered Component, by name and then by its
	// type's package path.
	Components []ComponentPopulation `json:"components"`
	// Refusal is non-empty exactly when the request was refused. A census
	// request never is.
	Refusal string `json:"-"`
}

// ComponentPopulation is one Component name and how many Entities carry it.
type ComponentPopulation struct {
	Name       string `json:"name"`
	Population int    `json:"population"`
}

// EntityRequest names one Entity, as "7v2", "Entity(7v2)" or its decimal
// handle, and optionally the Components to answer of it.
type EntityRequest struct {
	Entity     string   `json:"entity" jsonschema:"the Entity as 7v2 or Entity(7v2) or its decimal handle: any form a log or an earlier answer gave"`
	Components []string `json:"components,omitempty" jsonschema:"only these Components, named as ecs_world lists them; a named Component the Entity does not carry is left out. Omit for every Component it carries"`
}

// EntityResponse is one Entity and every Component it carries, by name.
type EntityResponse struct {
	// Entity is the Entity as Entity.String renders it.
	Entity string `json:"entity"`
	// Components is every Component the Entity carries, sorted by name. An
	// Entity carrying none answers an empty list.
	Components []ComponentValue `json:"components"`
	// Refusal is non-empty exactly when the request was refused, and every
	// other field is then zero.
	Refusal string `json:"-"`
}

// ComponentValue is one Component and its value, decoded from JSON: plain
// maps, slices, strings, json.Number and bools, sharing no memory with the
// Store. A Tag's value is an empty object. When the value could not be
// encoded, Error says why and Value is nil.
type ComponentValue struct {
	Name  string `json:"name"`
	Value any    `json:"value" jsonschema:"the Component's exported fields as JSON; unexported fields are not shown, and it is null when error is set"`
	Error string `json:"error,omitempty" jsonschema:"present when the value could not be encoded as JSON, saying why"`
}

// QueryRequest names the Components an Entity must carry every one of, and
// how many matches to encode: 0 is defaultReadLimit, and more than
// maxReadLimit is refused.
type QueryRequest struct {
	Components []string `json:"components" jsonschema:"Component names spelled as kernel.TypeName renders them and as ecs_world lists them; an Entity must carry every one"`
	Limit      int      `json:"limit,omitempty" jsonschema:"how many matching Entities to return: omit or 0 for 50; at most 500"`
}

// QueryResponse is the Entities carrying every named Component, in ascending
// index order, each with only the named Components' values.
type QueryResponse struct {
	// Total is how many Entities matched, encoded or not.
	Total int `json:"total" jsonschema:"how many Entities matched in all, including those not returned"`
	// Truncated is true when Total is more than the Entities encoded.
	Truncated bool `json:"truncated" jsonschema:"true when fewer Entities were returned than matched: the list is not the whole answer"`
	// Entities is the first limit matches.
	Entities []EntityComponents `json:"entities"`
	// Refusal is non-empty exactly when the request was refused, and every
	// other field is then zero.
	Refusal string `json:"-"`
}

// EntityComponents is one Entity of a query answer and the named Components'
// values it carries.
type EntityComponents struct {
	Entity     string           `json:"entity"`
	Components []ComponentValue `json:"components"`
}

// readCommand is what one by-name Command's two closures share, a read's or a
// write's (writebyname.go): the handle its Lock binds and its Execute reads
// through. It is allocated once, when the plugin registers the Command, and it
// is a value the factory allocates rather than a local both closures capture,
// so the package's one registration-time move to the heap stays ShrinkCmd's.
type readCommand struct{ entities kernel.Write[*Entities] }

func (c *readCommand) lock(access kernel.ResourceAccess) {
	c.entities = access.GetWrite[*Entities]()
}

// censusCommand is the factory of the census: see the price above.
func censusCommand() (kernel.Lock, kernel.Execute[CensusRequest, CensusResponse]) {
	c := &readCommand{}
	return c.lock, func(_ kernel.Kernel, _ CensusRequest) CensusResponse {
		return c.entities.Get().readCensus()
	}
}

// entityCommand is the factory of the one-Entity read: see the price above.
func entityCommand() (kernel.Lock, kernel.Execute[EntityRequest, EntityResponse]) {
	c := &readCommand{}
	return c.lock, func(_ kernel.Kernel, request EntityRequest) EntityResponse {
		return readEntity(c.entities.Get(), request)
	}
}

// queryCommand is the factory of the read by Component names: see the price
// above. Its stall is linear in the smallest named Store's population plus the
// limit's encodings.
func queryCommand() (kernel.Lock, kernel.Execute[QueryRequest, QueryResponse]) {
	c := &readCommand{}
	return c.lock, func(_ kernel.Kernel, request QueryRequest) QueryResponse {
		return readQuery(c.entities.Get(), request)
	}
}

// namedClass is a class with the type it was registered for, which is what
// orders two classes rendering one name.
type namedClass struct {
	componentType reflect.Type
	class         *componentClass
}

// sortedClasses is every class, or those keep admits, by name and then by
// package path.
func sortedClasses(en *Entities, keep func(*componentClass) bool) []namedClass {
	classes := make([]namedClass, 0, len(en.classes))
	for componentType, class := range en.classes {
		if keep == nil || keep(class) {
			classes = append(classes, namedClass{componentType, class})
		}
	}
	slices.SortFunc(classes, func(a, b namedClass) int {
		return cmp.Or(cmp.Compare(a.class.owner, b.class.owner),
			cmp.Compare(a.componentType.PkgPath(), b.componentType.PkgPath()))
	})
	return classes
}

func (en *Entities) readCensus() CensusResponse {
	classes := sortedClasses(en, nil)
	components := make([]ComponentPopulation, len(classes))
	for i, c := range classes {
		components[i] = ComponentPopulation{Name: c.class.owner, Population: c.class.population()}
	}
	return CensusResponse{
		Entities:    len(en.gens) - len(en.free),
		FreeIndices: len(en.free),
		IndexSpace:  len(en.gens),
		AgentWrites: en.agentWrites(),
		Components:  components,
	}
}

func readEntity(en *Entities, request EntityRequest) EntityResponse {
	e, err := parseEntity(request.Entity)
	if err != nil {
		return EntityResponse{Refusal: err.Error()}
	}
	if !readable(en, e) {
		return EntityResponse{Refusal: notAlive(en, e)}
	}
	var named []*componentClass
	for _, name := range request.Components {
		class := en.classNamed(name)
		if class == nil {
			return EntityResponse{Refusal: unresolved(en, name)}
		}
		named = append(named, class)
	}
	return EntityResponse{
		Entity: e.String(),
		Components: values(e, sortedClasses(en, func(class *componentClass) bool {
			return class.has(e) && (named == nil || slices.Contains(named, class))
		})),
	}
}

func readQuery(en *Entities, request QueryRequest) QueryResponse {
	limit := request.Limit
	switch {
	case limit < 0 || limit > maxReadLimit:
		return QueryResponse{Refusal: fmt.Sprintf("limit %d is out of range: give 1 to %d, or 0 for %d", limit, maxReadLimit, defaultReadLimit)}
	case limit == 0:
		limit = defaultReadLimit
	}
	if len(request.Components) == 0 {
		return QueryResponse{Refusal: "a query names at least one Component; registered: " + registeredNames(en)}
	}
	var named []*componentClass
	for _, name := range request.Components {
		class := en.classNamed(name)
		if class == nil {
			return QueryResponse{Refusal: unresolved(en, name)}
		}
		if !slices.Contains(named, class) {
			named = append(named, class)
		}
	}
	driver := named[0]
	for _, class := range named[1:] {
		if class.population() < driver.population() {
			driver = class
		}
	}
	var matches []Entity
walk:
	for _, e := range driver.owners() {
		for _, class := range named {
			if class != driver && !class.has(e) {
				continue walk
			}
		}
		matches = append(matches, e)
	}
	slices.SortFunc(matches, func(a, b Entity) int { return cmp.Compare(a.idx(), b.idx()) })
	carried := sortedClasses(en, func(class *componentClass) bool { return slices.Contains(named, class) })
	shown := matches[:min(limit, len(matches))]
	entities := make([]EntityComponents, len(shown))
	for i, e := range shown {
		entities[i] = EntityComponents{Entity: e.String(), Components: values(e, carried)}
	}
	return QueryResponse{Total: len(matches), Truncated: len(shown) < len(matches), Entities: entities}
}

// values encodes e's value of each class under the lock the caller holds and
// decodes it back detached. UseNumber keeps an Entity Reference, a uint64,
// exact. A value that cannot be encoded - a NaN, say - is reported on that
// Component alone.
func values(e Entity, classes []namedClass) []ComponentValue {
	answer := make([]ComponentValue, 0, len(classes))
	for _, c := range classes {
		// The decode target is allocated rather than taken as the address of a
		// field: a read answer is built on the heap anyway, and this keeps the
		// package's moves to the heap to the one at registration.
		detached := new(any)
		value := ComponentValue{Name: c.class.owner}
		encoded, _, err := c.class.encode(e)
		if err == nil {
			decoder := json.NewDecoder(bytes.NewReader(encoded))
			decoder.UseNumber()
			err = decoder.Decode(detached)
		}
		if err != nil {
			value.Error = err.Error()
		} else {
			value.Value = *detached
		}
		answer = append(answer, value)
	}
	return answer
}

// unresolved is the refusal for a name classNamed did not resolve: the
// package-qualified forms of the types rendering it, when more than one does,
// and otherwise every registered name.
func unresolved(en *Entities, name string) string {
	colliding := sortedClasses(en, func(class *componentClass) bool { return class.owner == name })
	if len(colliding) > 1 {
		forms := make([]string, len(colliding))
		for i, c := range colliding {
			forms[i] = qualifiedName(c.componentType)
		}
		return fmt.Sprintf("Component %q names %d types; name one of: %s", name, len(colliding), strings.Join(forms, ", "))
	}
	return fmt.Sprintf("no Component is named %q; registered: %s", name, registeredNames(en))
}

// registeredNames is every registered Component name, sorted, once each.
func registeredNames(en *Entities) string {
	var names []string
	for _, c := range sortedClasses(en, nil) {
		if len(names) == 0 || names[len(names)-1] != c.class.owner {
			names = append(names, c.class.owner)
		}
	}
	return strings.Join(names, ", ")
}

// qualifiedName is a type's package-qualified form, PkgPath.Name, which tells
// apart two types kernel.TypeName renders alike.
func qualifiedName(t reflect.Type) string {
	if t.PkgPath() == "" {
		return t.String()
	}
	return t.PkgPath() + "." + t.Name()
}

// readable is liveness on the read path: Alive, over a generation the authority
// could have issued. Alive is exact for every handle Go code can hold, because
// newEntity is the only way to make one; a string parser is the one caller that
// can name the free generation of a free index, which Alive matches because it
// is the word that index stores. So this path drops it first, and everything
// else — a despawned handle, the handle a free index will carry next, an index
// past the index space — Alive answers on its own.
func readable(en *Entities, e Entity) bool {
	return e.gen()&freeGeneration == 0 && en.Alive(e)
}

// notAlive is the refusal for a handle that is not alive, naming the Entity
// that holds its index now where one does.
func notAlive(en *Entities, e Entity) string {
	refusal := e.String() + " is not alive"
	if now, ok := holder(en, e.idx()); ok {
		refusal += fmt.Sprintf("; index %d now holds %s", e.idx(), now)
	}
	return refusal
}

// holder is the Entity that holds index now, if any does: an index beyond the
// index space, or free, has none. Free is the bit on its generation.
func holder(en *Entities, index uint32) (Entity, bool) {
	if int(index) >= len(en.gens) || en.gens[index]&freeGeneration != 0 {
		return NoEntity, false
	}
	return newEntity(index, en.gens[index]), true
}

// parseEntity reads an Entity back from a string: "7v2", "Entity(7v2)" or the
// decimal handle. It lives here rather than in the root because reading an
// Entity back in code is what the type refuses; only a caller outside Go has
// nothing but the string.
func parseEntity(text string) (Entity, error) {
	refuse := func() (Entity, error) {
		return NoEntity, fmt.Errorf("%q is not an Entity: give it as 7v2, Entity(7v2) or its decimal handle", text)
	}
	inner := strings.TrimSpace(text)
	if strings.HasPrefix(inner, "Entity(") && strings.HasSuffix(inner, ")") {
		inner = inner[len("Entity(") : len(inner)-1]
	}
	var e Entity
	if index, generation, ok := strings.Cut(inner, "v"); ok {
		i, err := strconv.ParseUint(index, 10, 32)
		if err != nil {
			return refuse()
		}
		g, err := strconv.ParseUint(generation, 10, 32)
		if err != nil {
			return refuse()
		}
		e = newEntity(uint32(i), uint32(g))
	} else {
		handle, err := strconv.ParseUint(inner, 10, 64)
		if err != nil {
			return refuse()
		}
		e = Entity(handle)
	}
	if e == NoEntity {
		return NoEntity, fmt.Errorf("%q is NoEntity, which names no Entity", text)
	}
	return e, nil
}
