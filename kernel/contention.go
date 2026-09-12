package kernel

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// ContentionDescription is the conflict report: which handlers can never
// overlap, which resources serialise the most, and which phases went
// effectively single-threaded.
//
// It is computed once, when Describe walks a registry that is already immutable
// — no reflection beyond the reflect.Type keys the registry is already keyed by,
// no resource walk, and nothing at all on the dispatch path.
//
// It reports; it never errors and never panics. A conflict is not a defect:
// two handlers writing one resource is how shared state is meant to work, and
// only the reader knows whether the serialisation it causes is worth paying
// for. Every view is therefore ranked rather than enumerated, most contended
// first, because a lock set wide enough to matter — one resource every handler
// touches — makes a raw pairwise list true, unavoidable, and useless.
type ContentionDescription struct {
	// Resources are the resources at least one handler pair serialises on,
	// most contended first. A resource nothing contends on is absent: the
	// description's own Resources field already enumerates every one of them.
	Resources []ResourceContention
	// Handlers are the handler pairs that can never overlap, and the resources
	// they cannot overlap on, the pair sharing the most first.
	Handlers []HandlerConflict
	// Phases are the subscription groups whose members may otherwise run
	// concurrently, most serialised first. A phase of one member, and a phase
	// no member pair serialises in, have nothing to report and are absent.
	Phases []PhaseContention
}

// HandlerRef names one handler in the conflict report. Kind is "command" or
// "subscription"; Event is the subscribed event type, and nil for a command.
type HandlerRef struct {
	Kind  string
	Type  reflect.Type
	Owner PluginName
	Event reflect.Type
}

// ResourceContention reports one resource and the handlers that serialise on
// it. Conflicts counts the handler pairs that can never overlap because of this
// resource alone, which is what ranks it: turning "the frame is slow" into
// "this resource is why".
type ResourceContention struct {
	Type      reflect.Type
	Owner     PluginName
	Writers   []HandlerRef
	Readers   []HandlerRef
	Conflicts int
}

// HandlerConflict reports two handlers that can never run at the same time, and
// every resource that is true of. A and B are ordered the way the report renders
// handlers: by event, then by identity type.
type HandlerConflict struct {
	A, B      HandlerRef
	Resources []reflect.Type
}

// PhaseContention reports one event's phase group — the members of which may
// otherwise run concurrently — and how much of that concurrency the lock sets
// take back. Conflicts counts the member pairs that serialise, out of the
// Members*(Members-1)/2 the phase has.
//
// It accounts for locks only. Completion-order edges declared with Before and
// After also stop two members overlapping, and SubscriptionDescription.DependsOn
// already reports those.
type PhaseContention struct {
	Event     reflect.Type
	Phase     string
	Members   int
	Conflicts int
	// SingleThreaded is true when every member pair serialises, so the phase
	// runs one member at a time however many it has.
	SingleThreaded bool
	// WidestLocks are the members that conflict with every other member. They
	// are what makes the phase serialise, and naming them is the widest-lock
	// warning: a handler here holds a lock covering everything its neighbours
	// touch.
	WidestLocks []HandlerRef
}

// handlerAccess pairs a handler's identity with the finalized lock set its
// factory produced. Reads and writes are already the transitive closure:
// resolveUses folded every declared dispatch into them at composition. phase is
// the subscription's phase label, and empty for a command.
type handlerAccess struct {
	ref    HandlerRef
	phase  string
	access *ResourceAccess
}

// describeContention computes the conflict report from registry state that
// finalize has already frozen. Subscriptions are read from the subscription
// list rather than from the compiled publication plans, so the report survives
// a composition that failed to compile one event's DAG.
func (r *registry) describeContention() ContentionDescription {
	handlers := r.handlerAccesses()
	return ContentionDescription{
		Resources: resourceContention(r, handlers),
		Handlers:  handlerConflicts(handlers),
		Phases:    phaseContention(handlers),
	}
}

// handlerAccesses collects every handler holding a lock set, in the order every
// view renders them, so the whole report reads the same way on every run.
func (r *registry) handlerAccesses() []handlerAccess {
	handlers := make([]handlerAccess, 0, len(r.commands))
	commands := 0
	for _, id := range sortedTypes(r.commands) {
		cmd := r.commands[id]
		if cmd.resources == nil {
			continue
		}
		commands++
		handlers = append(handlers, handlerAccess{
			ref:    HandlerRef{Kind: "command", Type: cmd.id, Owner: cmd.owner},
			access: cmd.resources,
		})
	}
	for _, eventType := range sortedTypes(r.subscriptions) {
		for _, task := range r.subscriptions[eventType] {
			owner, access := task.coupling()
			if access == nil {
				continue
			}
			handlers = append(handlers, handlerAccess{
				ref: HandlerRef{
					Kind: "subscription", Type: task.orderID(), Owner: owner, Event: eventType,
				},
				phase:  subscriptionPhase(task),
				access: access,
			})
		}
	}
	slices.SortFunc(handlers[commands:], func(a, b handlerAccess) int { return compareRefs(a.ref, b.ref) })
	return handlers
}

// sharedResources reports the resources two lock sets serialise on, in type
// order. Two readers never serialise; a write on either side against any hold on
// the other always does. GetWrite deletes the type from read, so the two maps of
// one set are disjoint and this is the whole of the rule.
func sharedResources(a, b *ResourceAccess) []reflect.Type {
	var shared []reflect.Type
	for _, resourceType := range sortedTypes(a.write) {
		if holds(b, resourceType) {
			shared = append(shared, resourceType)
		}
	}
	for _, resourceType := range sortedTypes(a.read) {
		if _, writes := b.write[resourceType]; writes {
			shared = append(shared, resourceType)
		}
	}
	slices.SortFunc(shared, compareTypes)
	return shared
}

func holds(access *ResourceAccess, resourceType reflect.Type) bool {
	_, reads := access.read[resourceType]
	_, writes := access.write[resourceType]
	return reads || writes
}

// resourceContention ranks the resources handler pairs serialise on. It is the
// view that survives contact with a wide lock: the resource every handler holds
// comes out on top, correctly, and the narrower ones are named underneath it.
func resourceContention(r *registry, handlers []handlerAccess) []ResourceContention {
	contended := make([]ResourceContention, 0, len(r.resources))
	for _, resourceType := range sortedTypes(r.resources) {
		entry := ResourceContention{Type: resourceType, Owner: r.resources[resourceType].owner}
		for _, handler := range handlers {
			if _, writes := handler.access.write[resourceType]; writes {
				entry.Writers = append(entry.Writers, handler.ref)
				continue
			}
			if _, reads := handler.access.read[resourceType]; reads {
				entry.Readers = append(entry.Readers, handler.ref)
			}
		}
		// Writers serialise against every other holder; readers only against
		// writers. Counting is cheaper than walking the pairs again.
		writers, readers := len(entry.Writers), len(entry.Readers)
		entry.Conflicts = writers*(writers-1)/2 + writers*readers
		if entry.Conflicts == 0 {
			continue
		}
		contended = append(contended, entry)
	}
	slices.SortFunc(contended, func(a, b ResourceContention) int {
		if a.Conflicts != b.Conflicts {
			return b.Conflicts - a.Conflicts
		}
		return compareTypes(a.Type, b.Type)
	})
	return contended
}

// handlerConflicts walks every handler pair once and records the resources the
// two can never hold at the same time. Handlers arrive in render order, so each
// pair is visited once and emitted with A before B.
func handlerConflicts(handlers []handlerAccess) []HandlerConflict {
	var conflicts []HandlerConflict
	for i, a := range handlers {
		for _, b := range handlers[i+1:] {
			if shared := sharedResources(a.access, b.access); len(shared) > 0 {
				conflicts = append(conflicts, HandlerConflict{A: a.ref, B: b.ref, Resources: shared})
			}
		}
	}
	slices.SortFunc(conflicts, func(a, b HandlerConflict) int {
		if len(a.Resources) != len(b.Resources) {
			return len(b.Resources) - len(a.Resources)
		}
		if order := compareRefs(a.A, b.A); order != 0 {
			return order
		}
		return compareRefs(a.B, b.B)
	})
	return conflicts
}

// phaseContention measures how much of each phase's concurrency its lock sets
// take back. Members of one phase are the subscriptions an event publication is
// free to run at the same time, so a phase in which every pair conflicts is one
// that runs single file however many members it has.
func phaseContention(handlers []handlerAccess) []PhaseContention {
	type phaseKey struct {
		event reflect.Type
		phase string
	}
	groups := map[phaseKey][]handlerAccess{}
	var order []phaseKey
	for _, handler := range handlers {
		if handler.ref.Kind != "subscription" {
			continue
		}
		key := phaseKey{event: handler.ref.Event, phase: handler.phase}
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], handler)
	}

	phases := make([]PhaseContention, 0, len(order))
	for _, key := range order {
		members := groups[key]
		if len(members) < 2 {
			continue
		}
		entry := PhaseContention{Event: key.event, Phase: key.phase, Members: len(members)}
		conflicting := make([]int, len(members))
		for i, a := range members {
			for j := i + 1; j < len(members); j++ {
				if len(sharedResources(a.access, members[j].access)) == 0 {
					continue
				}
				entry.Conflicts++
				conflicting[i]++
				conflicting[j]++
			}
		}
		if entry.Conflicts == 0 {
			continue
		}
		entry.SingleThreaded = entry.Conflicts == phasePairs(len(members))
		for i, count := range conflicting {
			if count == len(members)-1 {
				entry.WidestLocks = append(entry.WidestLocks, members[i].ref)
			}
		}
		phases = append(phases, entry)
	}
	slices.SortFunc(phases, func(a, b PhaseContention) int {
		// Rank by the share of pairs that serialise, not by their count, so a
		// small phase that runs single file outranks a large one that mostly
		// does not. Cross-multiplied to stay in integers.
		if left, right := a.Conflicts*phasePairs(b.Members), b.Conflicts*phasePairs(a.Members); left != right {
			return right - left
		}
		if a.Members != b.Members {
			return b.Members - a.Members
		}
		if order := compareTypes(a.Event, b.Event); order != 0 {
			return order
		}
		return phaseRank(a.Phase) - phaseRank(b.Phase)
	})
	return phases
}

func phasePairs(members int) int { return members * (members - 1) / 2 }

// phaseRank orders the phase labels the way a publication runs them, which is
// not the order their names sort in.
func phaseRank(phase string) int {
	switch phase {
	case "first":
		return 0
	case "last":
		return 2
	default:
		return 1
	}
}

// compareRefs orders handler references the way every view renders them: by
// event, then by identity type. A command names no event, so it sorts before
// the subscriptions of any event.
func compareRefs(a, b HandlerRef) int {
	if order := compareTypes(a.Event, b.Event); order != 0 {
		return order
	}
	return compareTypes(a.Type, b.Type)
}

// dumpHandlerConflicts bounds how many handler pairs Dump prints. The pairwise
// set is quadratic in the number of handlers, and one resource every handler
// touches makes nearly all of it — a true list, and an unreadable one. Dump
// prints the worst of it and counts the rest; ContentionDescription.Handlers
// still carries every pair for a caller that wants them.
const dumpHandlerConflicts = 10

// dumpContention renders the conflict report under the architecture table. It
// leads with per-resource contention, which is the view that stays useful
// however wide the widest lock is, and ends with the pairwise list, which is the
// view that does not.
func dumpContention(out *strings.Builder, contention ContentionDescription) {
	out.WriteString("contention:\n")
	if len(contention.Resources) == 0 && len(contention.Phases) == 0 {
		out.WriteString("  none\n")
		return
	}
	if len(contention.Resources) > 0 {
		out.WriteString("  resources (most contended first):\n")
		for _, entry := range contention.Resources {
			fmt.Fprintf(out, "    %v (%s): %s, %s, %s\n", entry.Type, entry.Owner,
				plural(entry.Conflicts, "pair"), plural(len(entry.Writers), "writer"),
				plural(len(entry.Readers), "reader"))
		}
	}
	if len(contention.Phases) > 0 {
		out.WriteString("  phases (most serialised first):\n")
		for _, phase := range contention.Phases {
			fmt.Fprintf(out, "    %v %s: %d members, %d/%d pairs serialise",
				phase.Event, phase.Phase, phase.Members, phase.Conflicts, phasePairs(phase.Members))
			if phase.SingleThreaded {
				out.WriteString(", single-threaded")
			}
			if len(phase.WidestLocks) > 0 {
				fmt.Fprintf(out, ", widest %v", refNames(phase.WidestLocks))
			}
			out.WriteString("\n")
		}
	}
	if len(contention.Handlers) > 0 {
		out.WriteString("  handler pairs (most shared first):\n")
		listed := min(len(contention.Handlers), dumpHandlerConflicts)
		for _, pair := range contention.Handlers[:listed] {
			fmt.Fprintf(out, "    %v / %v: %v\n", pair.A.Type, pair.B.Type, pair.Resources)
		}
		if omitted := len(contention.Handlers) - listed; omitted > 0 {
			noun := "more pairs"
			if omitted == 1 {
				noun = "more pair"
			}
			fmt.Fprintf(out, "    and %d %s, which Describe reports in full\n", omitted, noun)
		}
	}
}

// refNames renders handler references as the identity types a reader greps for.
func refNames(refs []HandlerRef) []reflect.Type {
	types := make([]reflect.Type, 0, len(refs))
	for _, ref := range refs {
		types = append(types, ref.Type)
	}
	return types
}

func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}
