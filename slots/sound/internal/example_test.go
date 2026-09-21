package internal

import (
	"fmt"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// A steal is final: nothing in sound resumes a stolen Voice, and a game that
// wants the sound back plays it again as a new Voice. VoiceEndedEvent carries
// only the handle and the reason, and an ended handle addresses nothing, so
// what the new Play needs - the Clip, where it had got to, and its Params - is
// kept by the game from the live view, once a tick, before it can be stolen.
//
// Here the cap is one Voice. A looping ambience is playing when a footstep
// arrives in the same band at the same volume; the tie goes to the newcomer, so
// the footstep takes the slot and the ambience ends as ReasonStolen. The game
// wants looping sounds back and not one-shots, so it plays the ambience again
// from where it would have been by now. That play ties with the footstep in
// turn and takes the slot straight back; a play that ranked below everything
// live would lose instead and end as ReasonStolen again, which is the game's
// to retry on a later tick or let go.
//
// The Example lives beside sound's plugin rather than in the root because it
// composes a whole engine, and the root may not import its own constructor.
func Example_playAStolenVoiceAgain() {
	ended := make(chan sound.VoiceEndedEvent, 8)
	game := &retryingGame{kept: map[sound.Voice]sound.VoiceInfo{}}
	engine := kernel.New(map[kernel.PluginName]any{
		sound.Name: sound.Config{}.WithMaxVoices(1),
	}).WithPlugins(
		storageplugin.New(), permanentAdapter{}, readMountAdapter{storage.ReadMount{Id: "game", Priority: 10, FS: clipBytes}},
		New(), soundBackendAdapter{newFakeBackend(longClip())},
		&retryingGamePlugin{game: game, ended: ended},
	)
	go engine.Run()
	defer engine.Quit()
	<-engine.Ready()
	k := engine.Executioner()

	clip := sound.ClipWithResource(bell)
	ambience := sound.Params{Loop: m.Some(true)}
	footstep := sound.Params{}

	// update is one tick: the game's own System, then sound's flush.
	update := func(record func(*sound.Queue)) {
		k.ExecuteCommand[gameUpdateCmd](gameUpdateRequest{Record: record})
		k.PublishEvent(app.UpdateEvent{Dt: step, Last: true}).Wait()
	}

	var first sound.Voice
	update(func(q *sound.Queue) { first = q.Play(clip, 0, ambience) })
	for range 31 {
		update(nil)
	}
	fmt.Printf("ambience at %.3fs\n", game.kept[first].Playhead)

	update(func(q *sound.Queue) { q.Play(clip, 0, footstep) })
	event := <-ended
	fmt.Println("ambience stolen:", event.Voice == first && event.Reason == sound.ReasonStolen)

	game.stolen = append(game.stolen, event.Voice)
	update(nil)
	<-ended // the footstep, which the ambience took the slot back from
	update(nil)

	// The copy the game took this tick is the view after the thirty-fourth
	// flush, and an ambience started in the first and never stolen would stand
	// thirty-four ticks in by then.
	for _, info := range game.kept {
		fmt.Printf("ambience back at %.3fs, looping %v\n", info.Playhead, info.Params.Loop.Or(false))
		fmt.Println("in step with a Voice never stolen:", info.Playhead == 34*step)
	}

	// Output:
	// ambience at 0.484s
	// ambience stolen: true
	// ambience back at 0.531s, looping true
	// in step with a Voice never stolen: true
}

// retryingGame is the recipe. Once a tick it plays again the stolen Voices it
// wants back, from its copy advanced by the time since the copy was taken, and
// then takes a fresh copy of the live view.
//
// The copy is a tick old by the time the ending arrives: it was taken before
// the flush that stole the Voice, which is the last moment the Voice was in the
// view. So the elapsed time is that one tick, and the new Voice lands exactly
// where the stolen one would have been.
type retryingGame struct {
	kept   map[sound.Voice]sound.VoiceInfo
	stolen []sound.Voice
}

func (g *retryingGame) update(queue *sound.Queue, voices *sound.Voices, dt float32) {
	for _, voice := range g.stolen {
		was, ok := g.kept[voice]
		if !ok || !was.Params.Loop.Or(false) {
			continue
		}
		elapsed := dt
		queue.Play(was.Clip, was.Playhead+elapsed, was.Params)
	}
	g.stolen = g.stolen[:0]
	clear(g.kept)
	for info := range voices.All() {
		g.kept[info.Voice] = info
	}
}

// gameUpdateCmd stands in for the game's System: it holds the Queue for
// writing and the live view for reading, as a System recording before sound's
// flush does.
type gameUpdateCmd kernel.Command[gameUpdateRequest, gameUpdateResponse]

type gameUpdateRequest struct{ Record func(*sound.Queue) }

type gameUpdateResponse struct{}

type retryingEndedHandler kernel.Subscription[sound.VoiceEndedEvent]

type retryingGamePlugin struct {
	game  *retryingGame
	ended chan sound.VoiceEndedEvent
}

func (*retryingGamePlugin) Name() kernel.PluginName { return "retrying-game" }

func (*retryingGamePlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{sound.Name}
}

func (p *retryingGamePlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[gameUpdateCmd](p.update)
	registrar.Subscribe[retryingEndedHandler](func() (kernel.Lock, kernel.Observe[sound.VoiceEndedEvent]) {
		return nil, func(_ kernel.Kernel, event sound.VoiceEndedEvent) { p.ended <- event }
	})
	return nil
}

func (p *retryingGamePlugin) update() (kernel.Lock, kernel.Execute[gameUpdateRequest, gameUpdateResponse]) {
	var queue kernel.Write[*sound.Queue]
	var voices kernel.Read[*sound.Voices]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*sound.Queue]()
			voices = access.GetRead[*sound.Voices]()
		}, func(_ kernel.Kernel, request gameUpdateRequest) gameUpdateResponse {
			p.game.update(queue.Get(), voices.Get(), step)
			if request.Record != nil {
				request.Record(queue.Get())
			}
			return gameUpdateResponse{}
		}
}
