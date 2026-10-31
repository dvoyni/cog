package internal

import (
	"fmt"
	"time"

	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
)

// voicesName is the capability rendered as the tool sound_voices.
const voicesName = "voices"

// voicesDescription is prompt text, and it is reproduced in
// slots/sound/docs/specs/mcp.md so it is reviewed as prompt text rather than
// buried as a string literal.
//
// Three things it says on purpose. It names the failure audibility exists to
// catch, because a reader handed a number without being told what a zero means
// will read it as a volume. It names the 2D Listener failure in the words the
// reader will see, since the symptom is a value that looks ordinary in
// isolation. And it says playheads advance with no Device, because otherwise a
// reader that finds ready false assumes the game is frozen rather than merely
// silent.
const voicesDescription = "List every sound the game is playing right now, plus the last 32 that " +
	"ended, the listener they are heard from, and the audio device. Use it when the game looks " +
	"stuck or unchanged on screen but may be waiting on a sound, when you need to know whether " +
	"an alarm, a voice line or a cue actually played, or when you want to check a sound you " +
	"triggered was audible rather than merely started.\n\n" +
	"Each playing sound reports its clip, its bus, how far through it is, and **audibility** — " +
	"the gain the engine actually derived, after the bus volume, the distance falloff and the " +
	"cone. A sound with `audibility: 0` is playing and cannot be heard, which is a different bug " +
	"from one that never played. A positional sound also reports its position, its distance from " +
	"the listener and its bearing as `azimuth` and `elevation` in degrees; `azimuth: 90` on every " +
	"sound at once means the listener is oriented wrongly, not that everything is to the " +
	"right.\n\n" +
	"A one-shot is usually over before you can look, so read the endings list: it says which " +
	"clip ended, on which tick, and why — `finished`, `stopped`, `stolen` by the voice cap, " +
	"`failed` to load, or `released`. `voicesInUse` against `maxVoices` tells you the game is at " +
	"its cap and sounds are being stolen.\n\n" +
	"If `device.ready` is false nothing is audible at all — on the web that usually means the " +
	"player has not clicked yet — and the game is still running normally: playheads advance and " +
	"sounds still end on time whether or not anyone can hear them.\n\n" +
	"This costs no tick and never changes the game. When you are pairing observations under " +
	"`app_time hold`, take this one **last**."

// provider is what sound contributes to the mcp Port, from its own Register
// rather than through a separate plugin, per the rule that every package hosts
// its own provider. It holds nothing: the capability body reaches sound by
// dispatch.
type provider struct{}

// Capabilities reports what sound offers an agent: what is playing, what just
// ended, and whether anything could have been heard at all.
//
// One capability and not three. The blocks below are one answer to one
// question, and splitting them would make a reader take three calls to learn
// that the reason it heard nothing is that there is no Device - which is the
// confusion the whole shape exists to prevent.
//
// It is ReadOnly, which in cog's reading means the capability does not change
// the game, and here that is true without qualification: it reads retained
// state and costs no tick. Unlike a Snapshot, which must step a paused engine
// because there is nothing to record otherwise, retained Voices are already
// there to read.
//
// It is an mcp.Func rather than an mcp.Command with zero glue, and the
// difference is not ceremony: a Command would have to be declared in
// slots/sound, where its response would become a game-facing type, and the
// endings ring is the one thing in this listing that must not be. The Func
// dispatches a command sound keeps to itself.
func (provider) Capabilities() []mcp.Capability {
	return []mcp.Capability{
		mcp.Func(voicesName, voicesDescription, voices, mcp.ReadOnly()),
	}
}

// voicesRequest asks what is playing. It has no fields: filtering the Voices
// block is additive and left until something asks for it, and there is nothing
// to name - a listing capped at MaxVoices plus 32 endings is small enough to
// come back inline, so there is no path to write it to either.
type voicesRequest struct{}

// voicesResponse is the listing: the moment it describes, what is sounding,
// what just stopped sounding, where it is all heard from, and whether anything
// could be heard at all.
//
// Three blocks, because *nothing is playing* and *there is no device* are
// different answers, and a reader that confuses them chases the wrong bug for a
// long time. Nothing here is a Snapshot: a Snapshot is one tick's recorded
// declarations rendered while they are still alive, and a retained Voice was
// recorded once, possibly minutes ago, and has been alive since.
type voicesResponse struct {
	// Tick is the tick the last flush ran on, which is the moment every figure
	// below describes. It is the same number canvas_draws, ui_layout and
	// gfx_frame report, which is what makes observations pair.
	//
	// The playheads under it are tick-accurate and not sample-accurate: a
	// Voice reported 1.2 s in supports no conclusion finer than a tick.
	Tick int64 `json:"tick"`
	// Voices is every live Voice, in slot order. It is never omitted: an empty
	// list is the answer "nothing is playing", which is a different answer
	// from a device that is not ready, and a field that vanished would make
	// the two look alike.
	Voices []voiceView `json:"voices"`
	// Endings is the last 32 endings, oldest first - the order they happened
	// in. It is what makes "did the alarm sound" answerable: a 400 ms one-shot
	// is invisible between two calls, and the live view alone reports nothing
	// and means two different things by it.
	Endings []endingView `json:"endings"`
	// VoicesInUse and MaxVoices are the cap, read together under one lock. A
	// game at its cap is a game where sounds are being stolen, and without the
	// count that cannot be told from sounds that were never played.
	VoicesInUse int `json:"voicesInUse"`
	MaxVoices   int `json:"maxVoices"`
	// Listener is where all of this is heard from. Every distance above is
	// relative to it, so a reader told a Voice is 12 m away needs to know from
	// where.
	Listener listenerView `json:"listener"`
	// Device is whether anything is audible at all. A reader asking why it
	// heard nothing should find ready false here rather than conclude the game
	// is silent.
	Device deviceView `json:"device"`
}

// voiceView is one live Voice. It carries no gain matrix: four numbers per
// Voice would be a reader re-deriving the pan, and azimuth is the same fact in
// the form a reader can act on.
type voiceView struct {
	// Clip is the storage path the Voice is playing, or the byte length of the
	// encoded bytes it was handed.
	Clip string `json:"clip"`
	// Bus is the group the Voice's volume is folded into, as the game numbers
	// its own Buses. 0 is Master, which sound declares and every other Bus sits
	// directly under.
	Bus int `json:"bus"`
	// Playhead and Duration are seconds, tick-accurate. Duration is 0 while
	// the Voice is still waiting on its Clip, which is a sound that has been
	// played and is not yet making any.
	Playhead float32 `json:"playhead"`
	Duration float32 `json:"duration"`
	// Looping is whether the Voice repeats rather than ending, and Paused
	// whether its playhead is stopped - by the game, or by an engine pause,
	// which suspends every Voice at once.
	Looping bool `json:"looping"`
	Paused  bool `json:"paused"`
	// Audibility is the point of the whole capability: the final computed gain
	// before panning - volume x bus x falloff x cone. A reader asking whether
	// the player can hear this wants the number the engine derived, not the
	// raw volume the game set, because a Voice at full volume on a Bus at zero
	// is playing and inaudible and only one number says so.
	//
	// It is the scalar the cap steals by, reported rather than recomputed, and
	// it is never max(L, R) off the gain matrix: equal-power panning preserves
	// power, so reading it off the matrix would score a centred Voice 0.1414
	// against a hard-panned one's 0.2000 at the same distance - 3 dB invented
	// out of nothing.
	Audibility float32 `json:"audibility"`
	// Position, Distance, Azimuth and Elevation are a Positional Voice's, and
	// are absent on a Voice that has no position: one that is heard centred has
	// no bearing, as it has no distance to anything. Position is x, y, z in the
	// game's own units, Distance is from the Listener in those units, and the
	// bearing is in degrees.
	//
	// The bearing is reported although the game-facing view does not carry it.
	// It costs what audibility costs - nothing, since the W3C equations produce
	// it on the way to the gain - and it is the one field that catches a 2D
	// game whose Listener was never rotated: every sprite hard-panned to one
	// ear, the arithmetic correct throughout, and every other field looking
	// entirely normal.
	Position  []float32        `json:"position,omitempty"`
	Distance  m.Maybe[float32] `json:"distance,omitzero"`
	Azimuth   m.Maybe[float32] `json:"azimuth,omitzero"`
	Elevation m.Maybe[float32] `json:"elevation,omitzero"`
}

// endingView is one entry of the ring: which Clip ended, why, and when.
//
// The reason distinguishes finished from stolen from failed, which is three
// different bugs: a sound that played out, a sound the cap took away, and a
// sound whose Clip could not be read.
type endingView struct {
	Clip   string `json:"clip"`
	Reason string `json:"reason"`
	Tick   int64  `json:"tick"`
}

// listenerView is where the game is heard from.
//
// Forward and Up are the orientation resolved to axes rather than the
// quaternion the game set, because they are what the equations read and what
// makes an unrotated Listener one glance: a 2D game must rotate its Listener,
// and one that did not is forward (0,0,-1) with every sound beside the player.
type listenerView struct {
	Position []float32 `json:"position"`
	Forward  []float32 `json:"forward"`
	Up       []float32 `json:"up"`
}

// deviceView is the sound device as it is now. Ready false means absent, not
// opened yet, or lost, and the game does not know which - it keeps running
// either way, playheads advancing and Voices ending on time.
type deviceView struct {
	Ready bool `json:"ready"`
	// Name is the Adapter that got the device: otosound, jssound, nosound. The
	// listing answers identically under all three, which is what makes it
	// usable in the headless engine an agent actually drives.
	Name string `json:"name"`
	// SampleRate, Channels and LatencyMs are the device's own and are absent
	// until it is ready. The latency is the real output latency, not a figure
	// from a Config.
	SampleRate int     `json:"sampleRate,omitempty"`
	Channels   int     `json:"channels,omitempty"`
	LatencyMs  float32 `json:"latencyMs,omitempty"`
}

// voices is the sound_voices body: one dispatch, and nothing else. There is no
// arm, no step and no wait, because a retained Voice is already sitting still -
// which is the same reason the live view is a resource in sound rather than a
// command.
//
// It is a package function rather than a method to keep the capability-body
// rule visible at the call site: the plugin is one pointer away and the body
// still reaches sound only by dispatch.
func voices(k kernel.Executioner, request voicesRequest) (voicesResponse, error) {
	response := k.ExecuteCommand[voicesCmd](request)
	// MaxVoices is the table's fixed size and is never zero in a composed
	// engine, so a zero one is the dispatch that did not happen: a scheduler
	// that has stopped answers with the zero response. Reporting that as a
	// game playing nothing would be the worse answer by far.
	if response.MaxVoices == 0 {
		return voicesResponse{}, mcp.Unavailable{Reason: "the game is shutting down"}
	}
	return response, nil
}

// voicesCmd renders the listing from the state the last flush left. It is
// declared here rather than in slots/sound because its response carries the
// endings ring, which has no game-facing type on purpose.
type voicesCmd kernel.Command[voicesRequest, voicesResponse]

// voicesCmdImpl reads the four resources the listing is assembled from, under
// read locks, and builds it.
//
// It costs no tick and it reports one. sound holds the Voices and computes
// their playheads at its flush, so this describes the last flush and names its
// tick. The Device thread is not on the tick clock at all, and nothing here is
// read from it.
func voicesCmdImpl() (kernel.Lock, kernel.Execute[voicesRequest, voicesResponse]) {
	var live kernel.Read[*Voices]
	var heardFrom kernel.Read[*Listener]
	var device kernel.Read[*Device]
	var flush kernel.Read[*lastFlush]
	return func(access kernel.ResourceAccess) {
			live = access.GetRead[*Voices]()
			heardFrom = access.GetRead[*Listener]()
			device = access.GetRead[*Device]()
			flush = access.GetRead[*lastFlush]()
		}, func(_ kernel.Kernel, _ voicesRequest) voicesResponse {
			table, last := live.Get(), flush.Get()
			response := voicesResponse{
				Tick:        last.tick,
				Voices:      make([]voiceView, 0, table.Len()),
				Endings:     make([]endingView, 0, last.held),
				VoicesInUse: table.Len(),
				MaxVoices:   table.Cap(),
				Listener:    listenerViewOf(heardFrom.Get()),
				Device:      deviceViewOf(*device.Get()),
			}
			for detail := range VoicesDetails(table) {
				response.Voices = append(response.Voices, voiceViewOf(detail))
			}
			last.each(func(e ending) {
				response.Endings = append(response.Endings, endingView{
					Clip: clipName(e.clip), Reason: e.reason.String(), Tick: e.tick,
				})
			})
			return response
		}
}

// voiceViewOf renders one live Voice. A Positional Voice is one that has a
// position at all, which is Params.Position and nothing else: the first
// position a Voice receives makes it positional for the rest of its life, so
// there is no second flag to disagree with.
func voiceViewOf(detail VoiceDetail) voiceView {
	view := voiceView{
		Clip:       clipName(detail.Clip),
		Bus:        int(detail.Bus),
		Playhead:   detail.Playhead,
		Duration:   detail.Duration,
		Looping:    detail.Params.Loop.Or(false),
		Paused:     detail.Paused,
		Audibility: detail.Audibility,
	}
	if position, ok := detail.Params.Position.Get(); ok {
		view.Position = []float32{position.X, position.Y, position.Z}
		view.Distance = m.Some(detail.Distance)
		view.Azimuth = m.Some(detail.Azimuth)
		view.Elevation = m.Some(detail.Elevation)
	}
	return view
}

// listenerViewOf renders the Listener with its orientation resolved to the
// axes the equations are handed.
func listenerViewOf(listener *Listener) listenerView {
	position := listener.Position()
	front, up := ListenerAxes(listener)
	return listenerView{
		Position: []float32{position.X, position.Y, position.Z},
		Forward:  []float32{front.X, front.Y, front.Z},
		Up:       []float32{up.X, up.Y, up.Z},
	}
}

// deviceViewOf renders the Device, with its latency in milliseconds because
// that is the unit a reader thinks in and nanoseconds is the unit a Duration
// marshals as.
func deviceViewOf(device Device) deviceView {
	return deviceView{
		Ready:      device.Ready,
		Name:       device.Name,
		SampleRate: device.SampleRate,
		Channels:   device.Channels,
		LatencyMs:  float32(device.Latency) / float32(time.Millisecond),
	}
}

// clipName renders a ClipRef as the one string that says which sound it is: the
// storage path it names, or the size of the bytes it carries, which is all
// there is to say about a Clip nothing gave a name to.
func clipName(ref ClipRef) string {
	path, bytes := ClipRefParts(ref)
	switch {
	case path != "":
		return path
	case bytes > 0:
		return fmt.Sprintf("blob (%d bytes)", bytes)
	}
	return "no clip"
}
