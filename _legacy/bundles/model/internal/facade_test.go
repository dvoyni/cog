package internal

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// frameEvent is the event the test Systems subscribe to.
type frameEvent struct{}

// The three test Systems: two readers of the Lookup and one loader.
type (
	readerA func() (kernel.Lock, kernel.Observe[frameEvent])
	readerB func() (kernel.Lock, kernel.Observe[frameEvent])
	loader  func() (kernel.Lock, kernel.Observe[frameEvent])
)

// facadePlugin subscribes two Systems that take only Read[*model.Lookup] and
// one that takes Write, the way a renderer's recording and load Systems do.
// Each reader builds the read facade and, when overlap is set, waits for the
// other reader to have started before it returns, which only a schedule that
// runs the two at once can satisfy.
type facadePlugin struct {
	overlap  bool
	startedA chan struct{}
	startedB chan struct{}
	failed   chan string
}

func (p *facadePlugin) Name() kernel.PluginName { return "model-facade-test" }

// Dependencies names model: the kernel refuses a lock on a resource whose
// owner is not a direct dependency.
func (p *facadePlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{Name}
}

func (p *facadePlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[readerA](p.reader(p.startedA, p.startedB))
	registrar.Subscribe[readerB](p.reader(p.startedB, p.startedA))
	registrar.Subscribe[loader](func() (kernel.Lock, kernel.Observe[frameEvent]) {
		var lookup kernel.Write[*Lookup]
		return func(access kernel.ResourceAccess) {
			lookup = access.GetWrite[*Lookup]()
		}, func(kernel.Kernel, frameEvent) { _ = lookup.Get() }
	}).Before[readerA]().Before[readerB]()
	return nil
}

func (p *facadePlugin) reader(
	mine, other chan struct{},
) func() (kernel.Lock, kernel.Observe[frameEvent]) {
	return func() (kernel.Lock, kernel.Observe[frameEvent]) {
		var lookup kernel.Read[*Lookup]
		return func(access kernel.ResourceAccess) {
				lookup = access.GetRead[*Lookup]()
			}, func(kernel.Kernel, frameEvent) {
				read := NewLookupReadAccess(lookup.Get())
				if _, ok := read.Handle(ModelRef{Path: "models/absent.glb"}); ok {
					p.failed <- "the read facade reported a model nothing loaded"
				}
				if !p.overlap {
					return
				}
				close(mine)
				select {
				case <-other:
				case <-time.After(time.Second):
					p.failed <- "the two readers did not run at the same time"
				}
			}
	}
}

func composeFacade(t *testing.T, p *facadePlugin) *kernel.Engine {
	t.Helper()
	var failure error
	engine := kernel.New(nil).
		Handler(func(err error) error { failure = errors.Join(failure, err); return nil }).
		WithPlugins(storageplugin.New(), permanentAdapter{}, New(), p)
	if failure != nil {
		t.Fatalf("composing model beside the facade Systems failed: %v", failure)
	}
	return engine
}

// Two Systems that each take only Read[*model.Lookup] never serialise on it:
// the kernel's contention report names no conflict between them, while each
// of them does conflict with the one System that takes Write.
func TestTwoReadFacadeSystemsAreScheduledInParallel(t *testing.T) {
	engine := composeFacade(t, &facadePlugin{})
	lookup := reflect.TypeFor[*Lookup]()
	a, b, w := reflect.TypeFor[readerA](), reflect.TypeFor[readerB](), reflect.TypeFor[loader]()
	description := engine.Describe()
	for _, sub := range description.Subscriptions {
		if sub.Type != a && sub.Type != b {
			continue
		}
		if len(sub.Writes) != 0 || len(sub.Reads) != 1 || sub.Reads[0] != lookup {
			t.Errorf("%v reads %v and writes %v, want Read[*model.Lookup] alone", sub.Type, sub.Reads, sub.Writes)
		}
	}
	conflicts := map[[2]reflect.Type]bool{}
	for _, pair := range description.Contention.Handlers {
		conflicts[[2]reflect.Type{pair.A.Type, pair.B.Type}] = true
		conflicts[[2]reflect.Type{pair.B.Type, pair.A.Type}] = true
	}
	if conflicts[[2]reflect.Type{a, b}] {
		t.Error("the two readers serialise, want them free to run in parallel")
	}
	if !conflicts[[2]reflect.Type{a, w}] || !conflicts[[2]reflect.Type{b, w}] {
		t.Error("a reader does not serialise against the loader, want the write lock to exclude both")
	}
}

// And the schedule does run them at once: each reader waits inside its own
// handler for the other to have started, which a schedule that ran them one
// after the other would time out on.
func TestTwoReadFacadeSystemsRunAtTheSameTime(t *testing.T) {
	p := &facadePlugin{
		overlap:  true,
		startedA: make(chan struct{}),
		startedB: make(chan struct{}),
		failed:   make(chan string, 4),
	}
	engine := composeFacade(t, p)
	go engine.Run()
	<-engine.Ready()
	engine.Executioner().PublishEvent(frameEvent{}).Wait()
	close(p.failed)
	for failure := range p.failed {
		t.Error(failure)
	}
}
