package types

import (
	"reflect"
	"sync"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

// countCmd is the shape this whole concept exists for: a System invoked as a
// command whose Query reads and writes nothing. Its lock set is read{*Entities}
// plus a read on one Store, so two invocations do not conflict and the
// scheduler would run them at the same time - over one shared args, one shared
// driven cell and one shared Resp.
type countCmd kernel.Command[countRequest, countResponse]

type countRequest struct{ Tag int }

// countResponse carries the tag back out. The count alone would not catch the
// failure: what crosses between two overlapping invocations is the request each
// one was handed and the answer each one wrote, so the answer has to name the
// request it belongs to.
type countResponse struct {
	Tag int
	N   int
}

// countQuery names its Component by value, so the Query takes a read lock and
// the System declares no write anywhere.
type countQuery struct{ Velocity velocity }

func subscribeCount(registrar *kernel.Registrar) {
	registrar.HandleCommand[countCmd](ToExecute[countRequest, countResponse](registrar,
		func(tag *In[int], q *Query[countQuery], answer *Resp[countResponse]) {
			reply := countResponse{Tag: tag.Get()}
			for range q.All() {
				reply.N++
			}
			answer.Set(reply)
		},
		Feed(func(r countRequest) int { return r.Tag })))
}

// A read-only System really does declare no write. This is the premise the rest
// of the file rests on: if it ever gained one it would be self-serialised by
// that write, and the exclusion below would stop proving anything.
func TestAReadOnlySystemDeclaresNoWriteAndIsStillExclusive(t *testing.T) {
	_, _, engine := newWorld(t, 64, subscribeCount)

	for _, cmd := range engine.Describe().Commands {
		if cmd.Type != reflect.TypeFor[countCmd]() {
			continue
		}
		if len(cmd.Writes) != 0 {
			t.Fatalf("a read-only System writes %v, want nothing", cmd.Writes)
		}
		if !cmd.SelfExclusive {
			t.Fatal("a System is not reported as self-exclusive")
		}
		return
	}
	t.Fatal("no countCmd in the description")
}

// Every caller gets the answer to its own question. Without the exclusion the
// invocations overlap on the cells prepareSystem allocated once, and a caller
// receives the tag another caller asked with - or the zero value, because the
// other one's take already emptied the Resp.
func TestConcurrentInvocationsOfAReadOnlySystemEachAnswerTheirOwnRequest(t *testing.T) {
	entities, components, engine := newWorld(t, 64, subscribeCount)

	const entityCount = 6
	for range entityCount {
		components.velocities.Set(entities.alloc(), velocity{})
	}

	const callers = 16
	var group sync.WaitGroup
	wrong := make(chan countResponse, callers)
	for tag := 1; tag <= callers; tag++ {
		group.Add(1)
		go func() {
			defer group.Done()
			response := engine.Executioner().ExecuteCommand[countCmd](countRequest{Tag: tag})
			if response.Tag != tag || response.N != entityCount {
				wrong <- response
			}
		}()
	}
	group.Wait()
	close(wrong)
	for response := range wrong {
		t.Errorf("a caller received %+v, want its own tag and N=%d", response, entityCount)
	}
}

// A System's exclusion is against itself alone. Two different Systems over the
// same read-only Query still run at the same time, which is the property the
// ECS may never trade away.
func TestTwoReadOnlySystemsStillRunConcurrently(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	body := func(*Query[countQuery]) {
		entered <- struct{}{}
		<-release
	}

	_, _, engine := newWorld(t, 64, func(registrar *kernel.Registrar) {
		registrar.HandleCommand[countCmd](ToExecute[countRequest, countResponse](registrar, body))
		registrar.HandleCommand[otherCountCmd](ToExecute[countRequest, countResponse](registrar, body))
	})

	go engine.Executioner().ExecuteCommand[countCmd](countRequest{})
	go engine.Executioner().ExecuteCommand[otherCountCmd](countRequest{})

	for range 2 {
		<-entered
	}
	close(release)
}

type otherCountCmd kernel.Command[countRequest, countResponse]
