package kernel

import (
	"context"
	"reflect"
	"runtime/debug"
)

// Ordering is a registered subscription and its fluent completion-order
// configuration. It is also a scheduler task (locks/run) and an ordered item
// (orderID/orderBefore/orderAfter).
type Ordering[TEvent any] struct {
	id        reflect.Type
	owner     PluginName
	boundary  string
	resources *ResourceAccess
	before    []reflect.Type
	after     []reflect.Type
	observe   Observe[TEvent]
}

func (s *Ordering[TEvent]) Before[TSubscription SubscriptionConstraint[TEvent]]() *Ordering[TEvent] {
	s.before = append(s.before, reflect.TypeFor[TSubscription]())
	return s
}

func (s *Ordering[TEvent]) First() *Ordering[TEvent] {
	s.before = append(s.before, nil)
	return s
}

func (s *Ordering[TEvent]) After[TSubscription SubscriptionConstraint[TEvent]]() *Ordering[TEvent] {
	s.after = append(s.after, reflect.TypeFor[TSubscription]())
	return s
}

func (s *Ordering[TEvent]) Last() *Ordering[TEvent] {
	s.after = append(s.after, nil)
	return s
}

type subscription interface {
	task
	ordered[reflect.Type]
	// coupling reports the owning plugin and the lock set the subscription
	// declared, so finalization can check it against declared dependencies.
	coupling() (PluginName, *ResourceAccess)
}

func (s *Ordering[TEvent]) orderID() reflect.Type       { return s.id }
func (s *Ordering[TEvent]) orderBefore() []reflect.Type { return s.before }
func (s *Ordering[TEvent]) orderAfter() []reflect.Type  { return s.after }
func (s *Ordering[TEvent]) pluginOwner() PluginName     { return s.owner }

func (s *Ordering[TEvent]) coupling() (PluginName, *ResourceAccess) {
	return s.owner, s.resources
}

func (s *Ordering[TEvent]) locks(context.Context) (read, write map[reflect.Type]struct{}) {
	return s.resources.read, s.resources.write
}

func (s *Ordering[TEvent]) run(ctx context.Context) (err error) {
	invocation := ctx.(*eventContext[TEvent])
	// Recovery is inlined rather than routed through callPluginBoundary so the
	// dispatch path allocates neither a closure nor a boundary string.
	defer func() {
		if recovered := recover(); recovered != nil {
			err = ErrPluginPanic{
				Plugin: s.owner, Boundary: s.boundary,
				Recovered: recovered, Stack: debug.Stack(),
			}
		}
	}()
	handler := Kernel{
		engine:  invocation.engine,
		ctx:     invocation,
		scope:   invocation.scope,
		bounded: true,
	}
	return s.observe(handler, invocation.event)
}

// eventContext carries a published event through the scheduler to each subscriber.
// It is generic so the event value stays typed from PublishEvent to Observe.
type eventContext[TEvent any] struct {
	context.Context
	engine *Engine
	scope  context.Context
	event  TEvent
}

type publicationNode struct {
	task       subscription
	dependsOn  int
	dependents []int
}

type publicationPlan struct {
	nodes []publicationNode
}

func buildPublicationPlan(tasks []subscription) (*publicationPlan, []reflect.Type) {
	n := len(tasks)
	plan := &publicationPlan{nodes: make([]publicationNode, n)}
	index := make(map[reflect.Type]int, n)
	for i, task := range tasks {
		plan.nodes[i].task = task
		index[task.orderID()] = i
	}

	type edge struct{ from, to int }
	edges := make(map[edge]struct{})
	addEdge := func(from, to int) {
		if from == to {
			return
		}
		value := edge{from: from, to: to}
		if _, exists := edges[value]; exists {
			return
		}
		edges[value] = struct{}{}
		plan.nodes[from].dependents = append(plan.nodes[from].dependents, to)
		plan.nodes[to].dependsOn++
	}

	first := make([]bool, n)
	last := make([]bool, n)
	for i, task := range tasks {
		for _, id := range task.orderAfter() {
			if id == nil {
				last[i] = true
			} else if dependency, ok := index[id]; ok {
				addEdge(dependency, i)
			}
		}
		for _, id := range task.orderBefore() {
			if id == nil {
				first[i] = true
			} else if dependent, ok := index[id]; ok {
				addEdge(i, dependent)
			}
		}
	}
	for i := range tasks {
		if first[i] {
			for other := range tasks {
				if !first[other] {
					addEdge(i, other)
				}
			}
		}
		if last[i] {
			for other := range tasks {
				if !last[other] {
					addEdge(other, i)
				}
			}
		}
	}

	remaining := make([]int, n)
	for i := range plan.nodes {
		remaining[i] = plan.nodes[i].dependsOn
	}
	var ready []int
	for i, count := range remaining {
		if count == 0 {
			ready = append(ready, i)
		}
	}
	visited := 0
	for len(ready) > 0 {
		node := ready[0]
		ready = ready[1:]
		visited++
		for _, dependent := range plan.nodes[node].dependents {
			remaining[dependent]--
			if remaining[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
	}
	if visited != n {
		cycle := make([]reflect.Type, 0, n-visited)
		for i, count := range remaining {
			if count > 0 {
				cycle = append(cycle, tasks[i].orderID())
			}
		}
		return nil, cycle
	}
	return plan, nil
}
