package internal

import (
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

type pasteProbeCmd kernel.Command[pasteProbeRequest, pasteProbeResponse]
type pasteProbeRequest struct{}
type pasteProbeResponse struct {
	Text   string
	Pasted bool
}

type pastePlugin struct {
	pastec chan<- ClipboardPasteEvent
}

type testClipboardPasteEventHandler kernel.Subscription[ClipboardPasteEvent]

func (pastePlugin) Name() kernel.PluginName { return "test" }

func (pastePlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (p pastePlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[pasteProbeCmd](func() (kernel.Lock, kernel.Execute[pasteProbeRequest, pasteProbeResponse]) {
		var state kernel.Read[*State]
		return func(access kernel.ResourceAccess) {
				state = access.GetRead[*State]()
			}, func(kernel.Kernel, pasteProbeRequest) pasteProbeResponse {
				text, pasted := state.Get().ClipboardPaste()
				return pasteProbeResponse{Text: text, Pasted: pasted}
			}
	})
	registrar.Subscribe[testClipboardPasteEventHandler](func() (kernel.Lock, kernel.Observe[ClipboardPasteEvent]) {
		return nil, func(_ kernel.Kernel, event ClipboardPasteEvent) {
			p.pastec <- event
		}
	})
	return nil
}

// A paste is published as it arrives and read, like a key's edge, on the tick
// after it: that tick and no other.
func TestClipboardPasteIsPublishedAndLastsOneTick(t *testing.T) {
	pastec := make(chan ClipboardPasteEvent, 1)
	engine := kernel.New(nil).Handler(func(err error) error {
		t.Errorf("unexpected kernel error: %v", err)
		return err
	}).WithPlugins(New(), pastePlugin{pastec: pastec})
	go engine.Run()
	t.Cleanup(engine.Quit)
	<-engine.Ready()
	k := engine.Executioner()

	k.ExecuteCommand[ApplyCmd](ApplyRequest{Changes: []Change{ClipboardPasteChange(`{"units":{}}`)}})

	select {
	case event := <-pastec:
		if event.Text != `{"units":{}}` {
			t.Fatalf("ClipboardPasteEvent.Text = %q", event.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for input.ClipboardPasteEvent")
	}

	if got := k.ExecuteCommand[pasteProbeCmd](pasteProbeRequest{}); got.Pasted {
		t.Fatalf("before a tick: ClipboardPaste() = %q, true; want nothing yet", got.Text)
	}
	k.PublishEvent(app.UpdateEvent{Dt: 0.016}).Wait()
	if got := k.ExecuteCommand[pasteProbeCmd](pasteProbeRequest{}); !got.Pasted || got.Text != `{"units":{}}` {
		t.Fatalf("after a tick: ClipboardPaste() = %q, %v; want the pasted text", got.Text, got.Pasted)
	}
	k.PublishEvent(app.UpdateEvent{Dt: 0.016}).Wait()
	if got := k.ExecuteCommand[pasteProbeCmd](pasteProbeRequest{}); got.Pasted {
		t.Fatalf("a tick later: ClipboardPaste() = %q, true; want it gone", got.Text)
	}
}
