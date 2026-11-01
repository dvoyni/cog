package kernel

import (
	"reflect"
	"strings"
	"testing"
)

type position struct{ X, Y float32 }

type backend interface{ Name() string }

func newTestConfigurator() *Configurator {
	return &Configurator{kernel: newKernel(NewSupervisor(nil), 7, nil, false, nil)}
}

func TestConfiguratorID(t *testing.T) {
	if got := newTestConfigurator().ID(); got != 7 {
		t.Fatalf("ID answered %d, want 7", got)
	}
}

func TestResourceAnswersInitializedValue(t *testing.T) {
	c := newTestConfigurator()
	c.InitResource(position{X: 1, Y: 2})
	got, ok := c.Resource[position]()
	if !ok || got != (position{X: 1, Y: 2}) {
		t.Fatalf("Resource answered %v, %v", got, ok)
	}
}

func TestResourceOfEmptyInterface(t *testing.T) {
	c := newTestConfigurator()
	c.InitResource[backend](nil)
	got, ok := c.Resource[backend]()
	if !ok || got != nil {
		t.Fatalf("Resource answered %v, %v, want nil, true", got, ok)
	}
}

func TestResourceNotInitializedFails(t *testing.T) {
	if _, ok := newTestConfigurator().Resource[position](); ok {
		t.Fatal("Resource of an uninitialized type answered true")
	}
}

func TestInitResourceTwicePanics(t *testing.T) {
	c := newTestConfigurator()
	c.InitResource(position{})
	expectPanic(t, "initializing a resource twice", func() { c.InitResource(position{}) })
}

type hit struct{ Damage int32 }

// handlerNamed returns a handler that appends name to order when it runs.
func handlerNamed(order *[]string, name string) SubscriptionFunc[hit] {
	return func(k *Kernel, ex *Executor, in JobHandle, event hit) (hit, JobHandle) {
		*order = append(*order, name)
		return event, in
	}
}

// runSorted sorts c's handlers of hit and calls each in order.
func runSorted(c *Configurator) {
	d := c.kernel.dispatcher[hit]()
	d.sortHandlers()
	for _, handler := range d.handlers {
		handler(nil, nil, JobHandle{}, hit{})
	}
}

func TestSubscriptionsKeepSubscriptionOrder(t *testing.T) {
	c := newTestConfigurator()
	var order []string
	c.Subscribe(handlerNamed(&order, "a"))
	c.Subscribe(handlerNamed(&order, "b"))
	c.Subscribe(handlerNamed(&order, "c"))
	runSorted(c)
	if got := strings.Join(order, ""); got != "abc" {
		t.Fatalf("handlers ran %q, want abc", got)
	}
}

func TestOrderSubscriptionsReorders(t *testing.T) {
	c := newTestConfigurator()
	var order []string
	a := c.Subscribe(handlerNamed(&order, "a"))
	b := c.Subscribe(handlerNamed(&order, "b"))
	cc := c.Subscribe(handlerNamed(&order, "c"))
	c.OrderSubscriptions(cc, a) // c before a
	c.OrderSubscriptions(b, cc) // b before c
	runSorted(c)
	if got := strings.Join(order, ""); got != "bca" {
		t.Fatalf("handlers ran %q, want bca", got)
	}
}

func expectPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s did not panic", what)
		}
	}()
	f()
}

func TestOrderSubscriptionsCyclePanics(t *testing.T) {
	c := newTestConfigurator()
	var order []string
	a := c.Subscribe(handlerNamed(&order, "a"))
	b := c.Subscribe(handlerNamed(&order, "b"))
	c.OrderSubscriptions(a, b)
	c.OrderSubscriptions(b, a)
	expectPanic(t, "a cycle", func() { c.kernel.dispatcher[hit]().sortHandlers() })
}

func TestOrderSubscriptionsZeroSubPanics(t *testing.T) {
	c := newTestConfigurator()
	var order []string
	a := c.Subscribe(handlerNamed(&order, "a"))
	expectPanic(t, "ordering a zero Sub", func() { c.OrderSubscriptions(a, Sub[hit]{}) })
}

func TestSubscribeNotPlainOldDataPanics(t *testing.T) {
	c := newTestConfigurator()
	type named struct{ Name string }
	expectPanic(t, "a string event", func() {
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, event named) (named, JobHandle) { return event, in })
	})
}

func TestSubscribeNilHandlerPanics(t *testing.T) {
	expectPanic(t, "a nil handler", func() { newTestConfigurator().Subscribe[hit](nil) })
}

func TestIsPointerFree(t *testing.T) {
	type plain struct {
		A [4]int32
		B struct{ C float64 }
	}
	type nested struct{ Inner [2]struct{ P *int } }
	if !isPointerFree(reflect.TypeFor[plain]()) {
		t.Fatal("a struct of numbers and arrays is not pointer-free")
	}
	if isPointerFree(reflect.TypeFor[nested]()) {
		t.Fatal("a pointer nested in an array of structs is pointer-free")
	}
}

type health struct{ Points int32 }

type damageArg struct{ Amount int32 }

// registerDamage registers a task writing health and reading position.
func registerDamage(c *Configurator) *Task[damageArg, health] {
	return c.RegisterTask(func(access *Access) TaskBody[damageArg, health] {
		hp := access.Write[health]()
		pos := access.Read[position]()
		return func(k *Kernel, arg damageArg) health {
			h := hp.Get()
			h.Points -= arg.Amount + int32(pos.Get().X)
			hp.Set(h)
			return h
		}
	})
}

func nothing(access *Access) TaskBody[NoArg, NoResult] {
	return func(k *Kernel, arg NoArg) NoResult { return NoResult{} }
}

func TestRegisterTaskBindsResources(t *testing.T) {
	c := newTestConfigurator()
	c.InitResource(position{X: 1})
	c.InitResource(health{Points: 10})
	task := registerDamage(c)
	if got := task.run(c.kernel, damageArg{Amount: 3}); got.Points != 6 {
		t.Fatalf("task answered %d points, want 6", got.Points)
	}
	if got, _ := c.Resource[health](); got.Points != 6 {
		t.Fatalf("resource holds %d points after the task, want 6", got.Points)
	}
}

func TestRegisterTaskBuildsLockMasks(t *testing.T) {
	c := newTestConfigurator()
	c.InitResource(position{}) // resource 0, read
	c.InitResource(health{})   // resource 1, written
	task := registerDamage(c)
	if len(task.reads) != 1 || task.reads[0] != 0b01 || task.writes[0] != 0b10 {
		t.Fatalf("masks are reads %b writes %b, want 01 and 10", task.reads, task.writes)
	}
}

func TestRegisterTaskReadAndWriteIsWrite(t *testing.T) {
	c := newTestConfigurator()
	c.InitResource(health{})
	task := c.RegisterTask(func(access *Access) TaskBody[NoArg, NoResult] {
		access.Read[health]()
		access.Write[health]()
		access.Read[health]()
		return func(k *Kernel, arg NoArg) NoResult { return NoResult{} }
	})
	if task.reads[0] != 0 || task.writes[0] != 1 {
		t.Fatalf("masks are reads %b writes %b, want 0 and 1", task.reads, task.writes)
	}
}

func TestRegisterTaskLockingNothing(t *testing.T) {
	task := newTestConfigurator().RegisterTask(nothing)
	if len(task.reads) != 0 || len(task.writes) != 0 {
		t.Fatal("a task locking nothing has lock masks")
	}
}

func TestRegisterTaskHighResourceGrowsMasks(t *testing.T) {
	c := newTestConfigurator()
	c.kernel.resources[reflect.TypeFor[health]()] = &resource{index: 130, value: &health{}}
	task := c.RegisterTask(func(access *Access) TaskBody[NoArg, NoResult] {
		access.Read[health]()
		return func(k *Kernel, arg NoArg) NoResult { return NoResult{} }
	})
	if len(task.reads) != 3 || task.reads[2] != 1<<2 {
		t.Fatalf("masks are reads %b, want bit 2 of word 2", task.reads)
	}
}

func TestRegisterTaskPanics(t *testing.T) {
	type named struct{ Name string }
	c := newTestConfigurator()
	expectPanic(t, "a nil declare function", func() { c.RegisterTask[NoArg, NoResult](nil) })
	expectPanic(t, "a nil body", func() {
		c.RegisterTask(func(access *Access) TaskBody[NoArg, NoResult] { return nil })
	})
	expectPanic(t, "a string arg", func() {
		c.RegisterTask(func(access *Access) TaskBody[named, NoResult] { return nil })
	})
	expectPanic(t, "a string result", func() {
		c.RegisterTask(func(access *Access) TaskBody[NoArg, named] { return nil })
	})
	expectPanic(t, "an uninitialized resource", func() {
		c.RegisterTask(func(access *Access) TaskBody[NoArg, NoResult] {
			access.Read[position]()
			return nil
		})
	})
	var kept *Access
	c.RegisterTask(func(access *Access) TaskBody[NoArg, NoResult] {
		kept = access
		return func(k *Kernel, arg NoArg) NoResult { return NoResult{} }
	})
	c.InitResource(health{})
	expectPanic(t, "an Access kept past its declare function", func() { kept.Read[health]() })
}

type armor struct{ Value int32 }

func TestUseFoldsLocksAndCallsDirectly(t *testing.T) {
	c := newTestConfigurator()
	c.InitResource(position{X: 1}) // resource 0
	c.InitResource(health{Points: 10})
	c.InitResource(armor{Value: 2}) // resource 2
	damage := registerDamage(c)
	shielded := c.RegisterTask(func(access *Access) TaskBody[damageArg, health] {
		shield := access.Read[armor]()
		applyDamage := access.Use(damage)
		return func(k *Kernel, arg damageArg) health {
			return applyDamage(k, damageArg{Amount: max(arg.Amount-shield.Get().Value, 0)})
		}
	})
	if shielded.reads[0] != 0b101 || shielded.writes[0] != 0b010 {
		t.Fatalf("masks are reads %b writes %b, want 101 and 010", shielded.reads, shielded.writes)
	}
	if got := shielded.run(c.kernel, damageArg{Amount: 5}); got.Points != 6 {
		t.Fatalf("task answered %d points, want 6", got.Points)
	}
}

func TestUseWriteSupersedesUsedRead(t *testing.T) {
	c := newTestConfigurator()
	c.InitResource(health{})
	reader := c.RegisterTask(func(access *Access) TaskBody[NoArg, NoResult] {
		access.Read[health]()
		return func(k *Kernel, arg NoArg) NoResult { return NoResult{} }
	})
	writer := c.RegisterTask(func(access *Access) TaskBody[NoArg, NoResult] {
		access.Write[health]()
		access.Use(reader)
		return func(k *Kernel, arg NoArg) NoResult { return NoResult{} }
	})
	if writer.reads[0] != 0 || writer.writes[0] != 1 {
		t.Fatalf("masks are reads %b writes %b, want 0 and 1", writer.reads, writer.writes)
	}
}

func TestUsePinnedTaskInEitherOrder(t *testing.T) {
	c := newTestConfigurator()
	pinned := c.RegisterTask(nothing)
	c.PinToMainThread(pinned)
	user := c.RegisterTask(func(access *Access) TaskBody[NoArg, NoResult] {
		access.Use(pinned)
		return func(k *Kernel, arg NoArg) NoResult { return NoResult{} }
	})
	c.PinToMainThread(user)
	used := c.RegisterTask(nothing)
	c.RegisterTask(func(access *Access) TaskBody[NoArg, NoResult] {
		access.Use(used)
		return func(k *Kernel, arg NoArg) NoResult { return NoResult{} }
	})
	c.PinToMainThread(used)
	if !used.pinnedToMainThread {
		t.Fatal("pinning a used task did not pin it")
	}
}

func TestUseAnotherKernelsTaskPanics(t *testing.T) {
	c := newTestConfigurator()
	other := newTestConfigurator().RegisterTask(nothing)
	expectPanic(t, "using another kernel's task", func() {
		c.RegisterTask(func(access *Access) TaskBody[NoArg, NoResult] {
			access.Use(other)
			return nil
		})
	})
}

func TestPinToMainThread(t *testing.T) {
	c := newTestConfigurator()
	task := c.RegisterTask(nothing)
	c.PinToMainThread(task)
	if !task.pinnedToMainThread {
		t.Fatal("task is not pinned")
	}
	other := newTestConfigurator()
	expectPanic(t, "pinning another kernel's task", func() { other.PinToMainThread(task) })
	expectPanic(t, "pinning a nil task", func() { c.PinToMainThread[NoArg, NoResult](nil) })
}

func TestTaskRunAllocatesNothing(t *testing.T) {
	c := newTestConfigurator()
	c.InitResource(position{})
	c.InitResource(health{Points: 1 << 30})
	task := registerDamage(c)
	if got := testing.AllocsPerRun(1000, func() { task.run(c.kernel, damageArg{Amount: 1}) }); got != 0 {
		t.Fatalf("running a task allocated %v times", got)
	}
}
