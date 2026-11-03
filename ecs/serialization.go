package ecs

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"io"
	"reflect"
	"strings"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// Transient marks a component that is not saved: a cache, or state that
// lives a frame. Embed it before the component's fields.
type Transient struct{}

// SerializeEntity answers e as a number, the form saves write it in.
func SerializeEntity(e Entity) uint64 {
	return uint64(e)
}

// SerializeRef answers r as a number, the form saves write it in.
func SerializeRef[T any](r Ref[T]) uint64 {
	return r.serialized()
}

// Save writes the world to writer as JSON: every entity alive, its
// components and the refs, but transient components and refs. Entity and Ref
// fields are written as SerializeEntity and SerializeRef answer them.
//
//	{"refs": {"<ref name>": [{"ref": 4294967297, "value": …}]},
//	 "entities": [{"entity": 4294967297, "<component name>": {"X": 1}}]}
func (w World[W]) Save(writer io.Writer) error {
	world := w.world
	options := json.JoinOptions(
		jsontext.WithIndent("\t"),
		json.Deterministic(true),
		json.WithMarshalers(json.JoinMarshalers(
			json.MarshalToFunc(func(encoder *jsontext.Encoder, e Entity) error {
				return encoder.WriteToken(jsontext.Uint(SerializeEntity(e)))
			}),
			json.MarshalToFunc(func(encoder *jsontext.Encoder, r serializedRef) error {
				return encoder.WriteToken(jsontext.Uint(r.serialized()))
			}),
		)),
	)
	save := &saveWriter{encoder: jsontext.NewEncoder(writer, options), options: options}
	save.token(jsontext.BeginObject)
	save.token(jsontext.String("refs"))
	save.token(jsontext.BeginObject)
	for _, refs := range world.refs {
		if !refs.info().Transient {
			refs.save(save)
		}
	}
	save.token(jsontext.EndObject)
	save.token(jsontext.String("entities"))
	save.token(jsontext.BeginArray)
	trees := make([]*fieldNode, len(world.stores))
	for i, s := range world.stores {
		if !HasMarker[Transient](s.component) {
			trees[i] = makeFieldTree(s.component.Fields)
		}
	}
	for p, page := range world.entities.slots.pages {
		if page == nil {
			continue
		}
		for i := range page {
			slot := &page[i]
			if slot.liveGeneration == 0 {
				continue
			}
			e := makeEntity(uint32(p<<indexPageShift|i), slot.liveGeneration)
			save.token(jsontext.BeginObject)
			save.token(jsontext.String("entity"))
			save.token(jsontext.Uint(SerializeEntity(e)))
			for id := range maskIDs(&slot.componentMask) {
				if tree := trees[id]; tree != nil {
					s := world.stores[id]
					save.token(jsontext.String(string(s.component.Name)))
					save.fields(tree, s.get(e))
				}
			}
			save.token(jsontext.EndObject)
		}
	}
	save.token(jsontext.EndArray)
	save.token(jsontext.EndObject)
	return save.err
}

// Load adds the entities and refs of a save read from reader to the world.
// Saved entities take fresh indices past every index in use, keeping their
// order, spacing and generations, and indices between them go on the free
// list; plain refs do the same. A keyed ref takes the ref its key has in the
// world, if any, keeping that value. Every Entity and Ref field is remapped
// to match, and one naming nothing in the save becomes NoEntity or the zero
// Ref.
//
// Fields the save has and the components lack are ignored, and fields it
// lacks are zero. A component or refs name the world does not know is
// warned of through logger and skipped. Load fails, changing nothing, on
// malformed JSON, a value of the wrong type, an entity or ref without a
// handle or with generation 0, two at one index, or saved indices that do
// not fit the allocation range.
func (w World[W]) Load(logger *kernel.Logger, reader io.Reader) error {
	load := &worldLoad[W]{
		world:       w.world,
		logger:      logger,
		loadsByRefs: map[serializedRefs]*refsLoad{},
		entityRemap: map[uint64]Entity{},
		refRemaps:   map[reflect.Type]map[uint64]uint64{},
		warned:      map[string]bool{},
	}
	load.options = json.WithUnmarshalers(json.JoinUnmarshalers(
		json.UnmarshalFromFunc(func(decoder *jsontext.Decoder, e *Entity) error {
			n, err := readUint(decoder)
			*e = load.entityRemap[n]
			return err
		}),
		json.UnmarshalFromFunc(func(decoder *jsontext.Decoder, r deserializedRef) error {
			n, err := readUint(decoder)
			r.deserialize(load.refRemaps[r.valueType()][n])
			return err
		}),
	))
	if err := load.read(jsontext.NewDecoder(reader)); err != nil {
		return fmt.Errorf("ecs: load: %w", err)
	}
	if err := load.decode(); err != nil {
		return fmt.Errorf("ecs: load: %w", err)
	}
	load.apply()
	return nil
}

// serializedRef is a Ref of any type, as a save writes it.
type serializedRef interface {
	serialized() uint64
	valueType() reflect.Type
}

// deserializedRef is a Ref of any type, as a load sets it.
type deserializedRef interface {
	deserialize(n uint64)
	valueType() reflect.Type
}

// serializedRefType is the type of serializedRef, which a Ref implements.
var serializedRefType = reflect.TypeFor[serializedRef]()

// serializedRefs is a world's refs of one type, as the schema and saves
// reach them without the type.
type serializedRefs interface {
	info() RefInfo
	// save writes the refs as a member of the save's refs object.
	save(save *saveWriter)
	// prepareLoad takes the indices load's saved refs load at, and fills its
	// remap with their handles there.
	prepareLoad(load *refsLoad) error
	// resolveKeys points the remap of each saved keyed ref whose key has a
	// ref at that ref, so it is not stored. It does nothing for plain refs.
	resolveKeys(load *refsLoad, options json.Options) error
	// decodeLoad decodes the values of the saved refs that are stored.
	decodeLoad(load *refsLoad, options json.Options) error
	// applyLoad stores the decoded values.
	applyLoad(load *refsLoad)
}

// loadedKeys is the key map of keyed refs of T, as a load reaches it without
// the key type.
type loadedKeys[T any] interface {
	keyOf(value *T) any
	find(key any) (Ref[T], bool)
	set(key any, h Ref[T])
}

// saveWriter writes a save, keeping the first error and writing nothing
// after it.
type saveWriter struct {
	encoder *jsontext.Encoder
	options json.Options
	err     error
}

// fieldNode is a component's fields as a tree, by path: a leaf is a field,
// any other node a nested struct.
type fieldNode struct {
	name     string
	field    *FieldInfo
	children []*fieldNode
}

// worldLoad is a save being loaded into world W: what was read of it, and
// where it goes.
type worldLoad[W WorldID] struct {
	world   *worldResource[W]
	logger  *kernel.Logger
	options json.Options
	// entities holds the saved entities, and refs the saved refs of each
	// type, in the order their names came.
	entities    []savedEntity
	refs        []*refsLoad
	loadsByRefs map[serializedRefs]*refsLoad
	// entityRemap maps each saved entity to the entity it loads as, and
	// refRemaps each saved ref, by value type, to the ref it loads as.
	entityRemap map[uint64]Entity
	refRemaps   map[reflect.Type]map[uint64]uint64
	// firstIndex is the index the lowest saved entity loads at, and
	// indexCount how many indices the saved entities span.
	firstIndex, indexCount uint32
	// warned holds the unknown names warned of.
	warned map[string]bool
}

// savedEntity is an entity of a save: its handle there and its components.
type savedEntity struct {
	handle     uint64
	components []savedComponent
}

// savedComponent is a component of a saved entity: its JSON and, once
// decoded, its value.
type savedComponent struct {
	store *storeResource
	raw   jsontext.Value
	value unsafe.Pointer
}

// refsLoad is the saved refs of one type being loaded.
type refsLoad struct {
	refs  serializedRefs
	saved []savedRef
	// remap maps each saved ref to the ref it loads as.
	remap map[uint64]uint64
	// firstIndex is the index the lowest saved ref loads at, and indexCount
	// how many indices the saved refs span.
	firstIndex, indexCount uint32
	// stored tells, for each saved ref, whether it is stored, not taking
	// the ref its key has. values holds the decoded []T.
	stored []bool
	values any
}

// savedRef is a ref of a save: its handle there and its value's JSON.
type savedRef struct {
	handle uint64
	raw    jsontext.Value
}

func (r Ref[T]) serialized() uint64 {
	return uint64(r.generation)<<32 | uint64(r.index)
}

func (r Ref[T]) valueType() reflect.Type {
	return reflect.TypeFor[T]()
}

func (r *Ref[T]) deserialize(n uint64) {
	r.index = uint32(n)
	r.generation = uint32(n >> 32)
}

func (m keyMap[K, T]) keyOf(value *T) any {
	return (*value).Key()
}

func (m keyMap[K, T]) find(key any) (Ref[T], bool) {
	h, ok := m[key.(K)]
	return h, ok
}

func (m keyMap[K, T]) set(key any, h Ref[T]) {
	m[key.(K)] = h
}

// token writes t.
func (s *saveWriter) token(t jsontext.Token) {
	if s.err == nil {
		s.err = s.encoder.WriteToken(t)
	}
}

// fields writes the fields of node from the value at base.
func (s *saveWriter) fields(node *fieldNode, base unsafe.Pointer) {
	if s.err != nil {
		return
	}
	if node.field != nil {
		s.err = json.MarshalEncode(s.encoder, reflect.NewAt(node.field.Type, unsafe.Add(base, node.field.Offset)).Interface(), s.options)
		return
	}
	s.token(jsontext.BeginObject)
	for _, child := range node.children {
		s.token(jsontext.String(child.name))
		s.fields(child, base)
	}
	s.token(jsontext.EndObject)
}

// makeFieldTree makes the tree of fields.
func makeFieldTree(fields []FieldInfo) *fieldNode {
	root := &fieldNode{}
	for i := range fields {
		node := root
		if fields[i].Path != "" {
			for name := range strings.SplitSeq(fields[i].Path, ".") {
				child := node.child(name)
				if child == nil {
					child = &fieldNode{name: name}
					node.children = append(node.children, child)
				}
				node = child
			}
		}
		node.field = &fields[i]
	}
	return root
}

// child answers the child of n named name, or nil.
func (n *fieldNode) child(name string) *fieldNode {
	for _, child := range n.children {
		if child.name == name {
			return child
		}
	}
	return nil
}

// info describes the refs.
func (r *refsResource[W, T]) info() RefInfo {
	return RefInfo{Name: r.name, Type: reflect.TypeFor[T](), Keyed: r.keys != nil, Transient: r.transient}
}

// save writes the refs as a member of the save's refs object: their name,
// and each live ref with its value, in index order.
func (r *refsResource[W, T]) save(save *saveWriter) {
	save.token(jsontext.String(string(r.name)))
	save.token(jsontext.BeginArray)
	for p, page := range r.slots.pages {
		if page == nil {
			continue
		}
		for i := range page {
			slot := &page[i]
			if !slot.live {
				continue
			}
			save.token(jsontext.BeginObject)
			save.token(jsontext.String("ref"))
			save.token(jsontext.Uint(Ref[T]{index: uint32(p<<indexPageShift | i), generation: slot.generation}.serialized()))
			save.token(jsontext.String("value"))
			if save.err == nil {
				save.err = json.MarshalEncode(save.encoder, &slot.value, save.options)
			}
			save.token(jsontext.EndObject)
		}
	}
	save.token(jsontext.EndArray)
}

// prepareLoad takes fresh indices for load's saved refs, keeping their
// spacing, and fills its remap with their handles there, keeping their
// generations.
func (r *refsResource[W, T]) prepareLoad(load *refsLoad) error {
	r.settleReservations()
	handles := make([]uint64, len(load.saved))
	for i, saved := range load.saved {
		handles[i] = saved.handle
	}
	lowest, first, count, err := spanOf(handles, r.nextFreshIndex, r.lastIndex)
	if err != nil {
		return fmt.Errorf("refs %q: %w", r.name, err)
	}
	load.firstIndex = first
	load.indexCount = count
	for _, h := range handles {
		load.remap[h] = Ref[T]{index: first + uint32(h) - lowest, generation: uint32(h >> 32)}.serialized()
	}
	load.stored = make([]bool, len(load.saved))
	for i := range load.stored {
		load.stored[i] = true
	}
	return nil
}

// resolveKeys points the remap of each saved keyed ref whose key has a ref
// in the world, or in an earlier saved ref, at that ref, and marks it not
// stored.
func (r *refsResource[W, T]) resolveKeys(load *refsLoad, options json.Options) error {
	keys, ok := r.keys.(loadedKeys[T])
	if !ok {
		return nil
	}
	saved := map[any]uint64{}
	for i, s := range load.saved {
		var value T
		if err := json.Unmarshal(s.raw, &value, options); err != nil {
			return fmt.Errorf("refs %q: %w", r.name, err)
		}
		key := keys.keyOf(&value)
		if h, ok := keys.find(key); ok {
			load.remap[s.handle] = h.serialized()
			load.stored[i] = false
		} else if h, ok := saved[key]; ok {
			load.remap[s.handle] = h
			load.stored[i] = false
		} else {
			saved[key] = load.remap[s.handle]
		}
	}
	return nil
}

// decodeLoad decodes the values of load's stored refs.
func (r *refsResource[W, T]) decodeLoad(load *refsLoad, options json.Options) error {
	values := make([]T, len(load.saved))
	for i, s := range load.saved {
		if !load.stored[i] {
			continue
		}
		if err := json.Unmarshal(s.raw, &values[i], options); err != nil {
			return fmt.Errorf("refs %q: %w", r.name, err)
		}
	}
	load.values = values
	return nil
}

// applyLoad stores load's decoded refs at their indices, puts the indices
// between them on the free list and moves nextFreshIndex past them.
func (r *refsResource[W, T]) applyLoad(load *refsLoad) {
	values := load.values.([]T)
	keys, _ := r.keys.(loadedKeys[T])
	used := make([]bool, load.indexCount)
	for i, s := range load.saved {
		if !load.stored[i] {
			continue
		}
		var h Ref[T]
		h.deserialize(load.remap[s.handle])
		r.store(h, values[i])
		if keys != nil {
			keys.set(keys.keyOf(&values[i]), h)
		}
		used[h.index-load.firstIndex] = true
	}
	for i, isUsed := range used {
		if !isUsed {
			r.freeRefs.Append(Ref[T]{index: load.firstIndex + uint32(i), generation: 1})
		}
	}
	r.nextFreshIndex = int64(load.firstIndex) + int64(load.indexCount)
}

// read reads a save, keeping each entity's and ref's JSON for decode.
func (l *worldLoad[W]) read(decoder *jsontext.Decoder) error {
	if err := readBegin(decoder, jsontext.KindBeginObject); err != nil {
		return err
	}
	for decoder.PeekKind() != jsontext.KindEndObject {
		name, err := decoder.ReadToken()
		if err != nil {
			return err
		}
		switch name.String() {
		case "refs":
			err = l.readRefs(decoder)
		case "entities":
			err = l.readEntities(decoder)
		default:
			err = decoder.SkipValue()
		}
		if err != nil {
			return err
		}
	}
	_, err := decoder.ReadToken()
	return err
}

// readRefs reads the save's refs object.
func (l *worldLoad[W]) readRefs(decoder *jsontext.Decoder) error {
	if err := readBegin(decoder, jsontext.KindBeginObject); err != nil {
		return err
	}
	for decoder.PeekKind() != jsontext.KindEndObject {
		name, err := decoder.ReadToken()
		if err != nil {
			return err
		}
		refs, ok := l.world.refsByName[RefName(name.String())]
		if !ok {
			l.warn("refs", name.String())
			if err := decoder.SkipValue(); err != nil {
				return err
			}
			continue
		}
		load := l.loadsByRefs[refs]
		if load == nil {
			load = &refsLoad{refs: refs, remap: map[uint64]uint64{}}
			l.loadsByRefs[refs] = load
			l.refs = append(l.refs, load)
			l.refRemaps[refs.info().Type] = load.remap
		}
		if err := readBegin(decoder, jsontext.KindBeginArray); err != nil {
			return err
		}
		for decoder.PeekKind() != jsontext.KindEndArray {
			var saved savedRef
			handled := false
			err := readMembers(decoder, func(member string) error {
				switch member {
				case "ref":
					handled = true
					var err error
					saved.handle, err = readUint(decoder)
					return err
				case "value":
					value, err := decoder.ReadValue()
					saved.raw = value.Clone()
					return err
				}
				return decoder.SkipValue()
			})
			if err != nil {
				return err
			}
			if !handled {
				return fmt.Errorf("a ref of %q has no handle", name.String())
			}
			load.saved = append(load.saved, saved)
		}
		if _, err := decoder.ReadToken(); err != nil {
			return err
		}
	}
	_, err := decoder.ReadToken()
	return err
}

// readEntities reads the save's entities array.
func (l *worldLoad[W]) readEntities(decoder *jsontext.Decoder) error {
	if err := readBegin(decoder, jsontext.KindBeginArray); err != nil {
		return err
	}
	for decoder.PeekKind() != jsontext.KindEndArray {
		var saved savedEntity
		handled := false
		err := readMembers(decoder, func(member string) error {
			if member == "entity" {
				handled = true
				var err error
				saved.handle, err = readUint(decoder)
				return err
			}
			s, ok := l.world.storesByName[ComponentName(member)]
			if !ok {
				l.warn("component", member)
				return decoder.SkipValue()
			}
			value, err := decoder.ReadValue()
			saved.components = append(saved.components, savedComponent{store: s, raw: value.Clone()})
			return err
		})
		if err != nil {
			return err
		}
		if !handled {
			return fmt.Errorf("an entity has no handle")
		}
		l.entities = append(l.entities, saved)
	}
	_, err := decoder.ReadToken()
	return err
}

// warn warns of an unknown name of kind, once per load.
func (l *worldLoad[W]) warn(kind, name string) {
	if l.warned[kind+" "+name] {
		return
	}
	l.warned[kind+" "+name] = true
	l.logger.LogWarn("ecs: load skips a "+kind+" the world does not have", "world", reflect.TypeFor[W](), kind, name)
}

// decode takes the indices the save loads at and decodes every value,
// changing nothing the world holds but settling its reservations.
func (l *worldLoad[W]) decode() error {
	table := &l.world.entities
	table.settleReservations(false)
	handles := make([]uint64, len(l.entities))
	for i, saved := range l.entities {
		handles[i] = saved.handle
	}
	lowest, first, count, err := spanOf(handles, table.nextFreshIndex, table.lastIndex)
	if err != nil {
		return fmt.Errorf("entities: %w", err)
	}
	l.firstIndex = first
	l.indexCount = count
	for _, h := range handles {
		l.entityRemap[h] = makeEntity(first+uint32(h)-lowest, uint32(h>>32))
	}
	for _, load := range l.refs {
		if err := load.refs.prepareLoad(load); err != nil {
			return err
		}
	}
	for _, load := range l.refs {
		if err := load.refs.resolveKeys(load, l.options); err != nil {
			return err
		}
	}
	for _, load := range l.refs {
		if err := load.refs.decodeLoad(load, l.options); err != nil {
			return err
		}
	}
	trees := make([]*fieldNode, len(l.world.stores))
	for i := range l.entities {
		for j := range l.entities[i].components {
			saved := &l.entities[i].components[j]
			id := saved.store.component.ID
			if trees[id] == nil {
				trees[id] = makeFieldTree(saved.store.component.Fields)
			}
			saved.value = reflect.New(saved.store.component.Type).UnsafePointer()
			decoder := jsontext.NewDecoder(bytes.NewReader(saved.raw))
			if err := l.fields(decoder, trees[id], saved.value); err != nil {
				return fmt.Errorf("component %q: %w", saved.store.component.Name, err)
			}
		}
	}
	return nil
}

// fields decodes the fields of node into the value at base. Members naming
// no field are skipped.
func (l *worldLoad[W]) fields(decoder *jsontext.Decoder, node *fieldNode, base unsafe.Pointer) error {
	if node.field != nil {
		return json.UnmarshalDecode(decoder, reflect.NewAt(node.field.Type, unsafe.Add(base, node.field.Offset)).Interface(), l.options)
	}
	return readMembers(decoder, func(member string) error {
		child := node.child(member)
		if child == nil {
			return decoder.SkipValue()
		}
		return l.fields(decoder, child, base)
	})
}

// apply makes the decoded entities alive with their components, and stores
// the decoded refs.
func (l *worldLoad[W]) apply() {
	table := &l.world.entities
	used := make([]bool, l.indexCount)
	for _, saved := range l.entities {
		e := l.entityRemap[saved.handle]
		table.setAlive(e)
		used[e.index()-l.firstIndex] = true
		for _, component := range saved.components {
			s := component.store
			copy(unsafe.Slice((*byte)(l.world.put(e, s)), s.component.Size), unsafe.Slice((*byte)(component.value), s.component.Size))
		}
	}
	for i, isUsed := range used {
		if !isUsed {
			table.freeEntities.Append(makeEntity(l.firstIndex+uint32(i), 1))
		}
	}
	if l.indexCount > 0 {
		table.nextFreshIndex = int64(l.firstIndex) + int64(l.indexCount)
	}
	for _, load := range l.refs {
		load.refs.applyLoad(load)
	}
}

// spanOf answers the lowest index of handles, the fresh index it loads at,
// nextFreshIndex, and how many indices they span. It fails for a generation
// of 0, two handles at one index, or a span past lastIndex.
func spanOf(handles []uint64, nextFreshIndex int64, lastIndex uint32) (lowest, first, count uint32, err error) {
	if len(handles) == 0 {
		return 0, 0, 0, nil
	}
	lowest = uint32(handles[0])
	highest := lowest
	indices := make(map[uint32]bool, len(handles))
	for _, h := range handles {
		index := uint32(h)
		if h>>32 == 0 {
			return 0, 0, 0, fmt.Errorf("handle %#x has generation 0", h)
		}
		if indices[index] {
			return 0, 0, 0, fmt.Errorf("two handles at index %d", index)
		}
		indices[index] = true
		lowest = min(lowest, index)
		highest = max(highest, index)
	}
	if nextFreshIndex+int64(highest-lowest) > int64(lastIndex) {
		return 0, 0, 0, fmt.Errorf("%d indices do not fit the allocation range past index %d", int64(highest-lowest)+1, nextFreshIndex)
	}
	return lowest, uint32(nextFreshIndex), highest - lowest + 1, nil
}

// readBegin reads the token beginning an object or array, of kind.
func readBegin(decoder *jsontext.Decoder, kind jsontext.Kind) error {
	token, err := decoder.ReadToken()
	if err != nil {
		return err
	}
	if token.Kind() != kind {
		return fmt.Errorf("%v where %v begins, at offset %d", token.Kind(), kind, decoder.InputOffset())
	}
	return nil
}

// readMembers reads an object, calling member with each member's name to
// read its value.
func readMembers(decoder *jsontext.Decoder, member func(name string) error) error {
	if err := readBegin(decoder, jsontext.KindBeginObject); err != nil {
		return err
	}
	for decoder.PeekKind() != jsontext.KindEndObject {
		name, err := decoder.ReadToken()
		if err != nil {
			return err
		}
		if err := member(name.String()); err != nil {
			return err
		}
	}
	_, err := decoder.ReadToken()
	return err
}

// readUint reads a number that is a uint64.
func readUint(decoder *jsontext.Decoder) (uint64, error) {
	token, err := decoder.ReadToken()
	if err != nil {
		return 0, err
	}
	return token.Uint()
}
