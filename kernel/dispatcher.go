package kernel

import (
	"fmt"
	"reflect"
)

// dispatcher holds the handlers of one event type E, in subscription order
// until sortHandlers sorts them, and runs them in that order on every publication.
type dispatcher[E any] struct {
	handlers []SubscriptionFunc[E]
	// orders are the OrderSubscriptions declarations, as handler indexes:
	// orders[i][0] runs before orders[i][1].
	orders [][2]int32
}

// eventDispatcher is what every dispatcher shares, so that init sorts each
// event's handlers without knowing its type.
type eventDispatcher interface {
	sortHandlers()
}

// sortHandlers sorts handlers to satisfy every declaration in orders, keeping
// subscription order wherever none decides, and panics on a cycle.
func (d *dispatcher[E]) sortHandlers() {
	count := len(d.handlers)
	// waiting counts, per handler, the handlers still to run before it.
	waiting := make([]int, count)
	followers := make([][]int32, count)
	for _, order := range d.orders {
		followers[order[0]] = append(followers[order[0]], order[1])
		waiting[order[1]]++
	}
	sorted := make([]SubscriptionFunc[E], 0, count)
	placed := make([]bool, count)
	for len(sorted) < count {
		next := -1
		for i := range count {
			if !placed[i] && waiting[i] == 0 {
				next = i
				break
			}
		}
		if next < 0 {
			panic(fmt.Sprintf("kernel: the handlers of %v are ordered in a cycle", reflect.TypeFor[E]()))
		}
		placed[next] = true
		sorted = append(sorted, d.handlers[next])
		for _, after := range followers[next] {
			waiting[after]--
		}
	}
	d.handlers = sorted
	d.orders = nil
}
