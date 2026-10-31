package internal

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// fakeBackend records what platform tells it. Its lock is the test's, not the
// contract's: platform serializes its own calls, and the test reads from
// another goroutine.
type fakeBackend struct {
	mu         sync.Mutex
	calls      []string
	failStore  bool
	language   string
	playerName string
}

func (b *fakeBackend) record(call string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, call)
}

func (b *fakeBackend) UnlockAchievement(name string) error {
	if name == "unknown" {
		return errors.New("refused")
	}
	b.record("unlock " + name)
	return nil
}

func (b *fakeBackend) SetStat(name string, _ int32) error {
	b.record("stat " + name)
	return nil
}

func (b *fakeBackend) Store() error {
	b.mu.Lock()
	fail := b.failStore
	b.mu.Unlock()
	b.record("store")
	if fail {
		return errors.New("offline")
	}
	return nil
}

func (b *fakeBackend) Language() string { return b.language }

func (b *fakeBackend) PlayerName() string { return b.playerName }

func (b *fakeBackend) ShowTextInput(field TextField) bool {
	b.record("show text input")
	return field.Width > 0
}

func (b *fakeBackend) HideTextInput() { b.record("hide text input") }

func (b *fakeBackend) Update() error {
	b.record("update")
	return nil
}

func (b *fakeBackend) Close() { b.record("close") }

// taken returns the calls since the last take, without the per-frame updates
// unless keepUpdates says so.
func (b *fakeBackend) taken(keepUpdates bool) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	calls := b.calls
	b.calls = nil
	if keepUpdates {
		return calls
	}
	return slices.DeleteFunc(calls, func(call string) bool { return call == "update" })
}

type testBackend kernel.Adapter[BackendPort]

type backendPlugin struct{ backend *fakeBackend }

func (backendPlugin) Name() kernel.PluginName { return "test-backend" }

func (backendPlugin) Dependencies() []kernel.PluginName { return nil }

func (p backendPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[testBackend](Backend(p.backend))
	return nil
}

// testEngine runs platform over backend, and answers the Executioner and a
// stop that waits for Run to return, so Stop's final upload is observable.
func testEngine(t *testing.T, config Config, backend *fakeBackend, reported *[]error) (kernel.Executioner, func()) {
	t.Helper()
	var mu sync.Mutex
	engine := kernel.New(map[kernel.PluginName]any{Name: config}).
		Handler(func(err error) error {
			mu.Lock()
			defer mu.Unlock()
			*reported = append(*reported, err)
			return nil
		}).
		WithPlugins(New(), backendPlugin{backend: backend})
	done := make(chan error, 1)
	go func() { done <- engine.Run() }()
	<-engine.Ready()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		engine.Quit()
		if err := <-done; err != nil {
			t.Errorf("Run = %v", err)
		}
	}
	t.Cleanup(stop)
	return engine.Executioner(), stop
}

// Changes go up together on the last tick of a frame, at most once per
// interval; what is left over goes up at Stop, before the Backend is closed.
func TestChangesUploadOncePerIntervalAndAtStop(t *testing.T) {
	backend := &fakeBackend{}
	var reported []error
	k, stop := testEngine(t, Config{StoreInterval: time.Hour}, backend, &reported)

	k.ExecuteCommand[UnlockAchievementsCmd](UnlockAchievementsRequest{Names: []string{"FIRST", "SECOND"}})
	k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()
	if calls := backend.taken(true); !slices.Equal(calls, []string{"unlock FIRST", "unlock SECOND"}) {
		t.Fatalf("after a catch-up tick: %v, want the unlocks and no pump", calls)
	}

	k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60, Last: true}).Wait()
	if calls := backend.taken(true); !slices.Equal(calls, []string{"update", "store"}) {
		t.Fatalf("after the last tick: %v, want one pump and one upload", calls)
	}

	k.ExecuteCommand[SetStatsCmd](SetStatsRequest{Stats: map[string]int32{"WINS": 3}})
	k.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60, Last: true}).Wait()
	if calls := backend.taken(false); !slices.Equal(calls, []string{"stat WINS"}) {
		t.Fatalf("inside the interval: %v, want the stat and no upload", calls)
	}

	stop()
	if calls := backend.taken(false); !slices.Equal(calls, []string{"store", "close"}) {
		t.Fatalf("at Stop: %v, want the pending upload and then close", calls)
	}
	if len(reported) != 0 {
		t.Fatalf("reported %v, want nothing", reported)
	}
}

// A failed upload stays pending and is tried again; an unknown name is said
// once however often it is sent.
func TestFailuresRetryAndRefusalsReportOnce(t *testing.T) {
	backend := &fakeBackend{failStore: true}
	var reported []error
	k, stop := testEngine(t, Config{StoreInterval: time.Nanosecond}, backend, &reported)

	k.ExecuteCommand[UnlockAchievementsCmd](UnlockAchievementsRequest{Names: []string{"unknown", "KNOWN"}})
	k.ExecuteCommand[UnlockAchievementsCmd](UnlockAchievementsRequest{Names: []string{"unknown"}})
	frame(k)
	frame(k)
	if calls := backend.taken(false); !slices.Equal(calls, []string{"unlock KNOWN", "store", "store"}) {
		t.Fatalf("while offline: %v, want the known unlock and an upload each frame", calls)
	}

	backend.mu.Lock()
	backend.failStore = false
	backend.mu.Unlock()
	frame(k)
	frame(k)
	if calls := backend.taken(false); !slices.Equal(calls, []string{"store"}) {
		t.Fatalf("back online: %v, want one upload and then nothing pending", calls)
	}

	stop()
	if len(reported) != 2 {
		t.Fatalf("reported %v, want the refusal and the upload failure once each", reported)
	}
}

func TestLanguageIsTheBackends(t *testing.T) {
	var reported []error
	k, _ := testEngine(t, Config{}, &fakeBackend{language: "pt-BR"}, &reported)
	if got := k.ExecuteCommand[LanguageCmd](LanguageRequest{}).Language; got != "pt-BR" {
		t.Fatalf("Language = %q, want pt-BR", got)
	}
}

func TestPlayerNameIsTheBackends(t *testing.T) {
	var reported []error
	k, _ := testEngine(t, Config{}, &fakeBackend{playerName: "Ash"}, &reported)
	if got := k.ExecuteCommand[PlayerNameCmd](PlayerNameRequest{}).Name; got != "Ash" {
		t.Fatalf("PlayerName = %q, want Ash", got)
	}
}

// The on-screen keyboard reaches the Backend and answers whether it opened,
// and nothing reaches the Backend once it is closed.
func TestTextInputReachesTheBackendUntilStop(t *testing.T) {
	backend := &fakeBackend{}
	var reported []error
	k, stop := testEngine(t, Config{}, backend, &reported)
	if !k.ExecuteCommand[ShowTextInputCmd](ShowTextInputRequest{Field: TextField{Width: 100, Height: 20}}).Shown {
		t.Fatal("Shown = false, want the Backend's true")
	}
	k.ExecuteCommand[HideTextInputCmd](HideTextInputRequest{})
	if calls := backend.taken(false); !slices.Equal(calls, []string{"show text input", "hide text input"}) {
		t.Fatalf("calls = %v, want show then hide", calls)
	}
	stop()
	backend.taken(false)
	if k.ExecuteCommand[ShowTextInputCmd](ShowTextInputRequest{Field: TextField{Width: 100}}).Shown {
		t.Fatal("Shown after Stop = true, want false")
	}
	if calls := backend.taken(false); len(calls) != 0 {
		t.Fatalf("calls after Stop = %v, want none", calls)
	}
}

// frame publishes the last tick of a frame, a millisecond after the one before
// it, so a nanosecond interval has passed even on a coarse clock.
func frame(k kernel.Executioner) {
	time.Sleep(time.Millisecond)
	k.PublishEvent(app.UpdateEvent{Last: true}).Wait()
}
