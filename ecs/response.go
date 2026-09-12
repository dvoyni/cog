package ecs

import "reflect"

// Resp is how a System invoked as a command answers, and it is the reason
// ToExecute is a question and not only an instruction.
//
// A System returns nothing — reflect.Value.Call allocates for a callee that
// does, which is a hard rule rather than a style preference — so the answer
// leaves through a parameter instead of through a return. The parameter is
// recognised by its type, not by its position, so the classification stays the
// contract it is everywhere else in this package: there is no "the last
// parameter is the response" convention to remember or to get wrong.
//
//	func count(request CountRequest, q *ecs.Query[CountQ], answer *ecs.Resp[CountResponse]) {
//	    reply := CountResponse{}
//	    for range q.All() { reply.N++ }
//	    answer.Set(reply)
//	}
//
//	registrar.HandleCommand[CountCmd](ecs.ToExecute[CountRequest, CountResponse](world, count))
//
// Only *ecs.Resp[Res] is accepted, where Res is the command's own response
// type: a System naming any other Resp is refused at registration naming both
// answers, because that mistake is always a copied registration line. Naming it
// at all stays optional — a command that is an order rather than a question
// takes no Resp and answers the zero value.
//
// A ToHandler System naming one is refused too. An event has no response to
// write into, and writing into a cell nobody reads is worse than not compiling.
//
// It is T rather than *T on purpose. Every wrapper in this package is X[T] over
// the domain type — In[T], Get[T], Set[T], Query[Q], Spawn[B] — and Read[T] and
// Write[T] take a pointer only because a kernel resource is keyed by its exact
// Go type, which is the kernel's rule and not this package's. A Resp[*T] would
// also make the System supply the storage, which is either an allocation per
// invocation or a pointer to something that need not outlive the lock. Holding
// the value copies it out under the lock instead, which is what the kernel's
// standing handle rule asks for anyway.
type Resp[T any] struct {
	// value is the answer this invocation wrote. The cell is allocated once, at
	// registration, and cleared by take on the way out, so nothing is allocated
	// per invocation and no invocation can read the one before it.
	value T
}

// Set is the answer. Calling it twice keeps the last; not calling it at all
// answers the zero value.
func (r *Resp[T]) Set(value T) { r.value = value }

// take is the answer on its way to the caller, and it clears the cell behind
// it: the cell outlives the invocation, so an invocation that writes nothing
// must not inherit what the one before it wrote.
func (r *Resp[T]) take() T {
	value := r.value
	var zero T
	r.value = zero
	return value
}

// response is what every *Resp[T] satisfies whatever T is, so the builder can
// tell a response parameter from a type the ECS does not hand out at all, and
// can say which answer was named when it is the wrong one.
//
// Resp is deliberately not a systemParam, for the same reason In is not: the
// instance a System receives has to be the one the builder reads back, so it can
// never be the one reflect.New would make. Leaving it out of that interface is
// what makes the fallthrough a diagnostic rather than an answer nobody collects.
type response interface{ answers() reflect.Type }

// answers reports the type this cell holds, for the diagnostics.
func (r *Resp[T]) answers() reflect.Type { return reflect.TypeFor[T]() }

// responseCell is the cell ToExecute owns and reads back. It is the only route
// a Resp instance reaches a System by.
type responseCell interface {
	response
	// slot is the parameter type this cell fills and the argument it fills it
	// with, both fixed at registration.
	slot() (reflect.Type, reflect.Value)
}

// slot reports the parameter type and the argument. It runs once, at
// registration, never per invocation.
func (r *Resp[T]) slot() (reflect.Type, reflect.Value) {
	return reflect.TypeFor[*Resp[T]](), reflect.ValueOf(r)
}

// responseOf reports the answer type a parameter is a response for, and whether
// it is one at all.
func responseOf(paramType reflect.Type) (reflect.Type, bool) {
	if paramType.Kind() != reflect.Pointer {
		return nil, false
	}
	cell, ok := reflect.New(paramType.Elem()).Interface().(response)
	if !ok {
		return nil, false
	}
	return cell.answers(), true
}
