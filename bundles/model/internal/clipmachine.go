package internal

import "github.com/dvoyni/cog/libs/m"

// EaseKind names the curve a crossfade's incoming weight follows. It is an
// enum rather than a func because a ClipMachine has to be a storable value, and
// a func is not one.
//
// The curves are anim's functions of the same names, re-implemented here
// because model does not import anim.
type EaseKind int

const (
	// EaseLinear is the zero value: the weight moves at a constant rate.
	EaseLinear EaseKind = iota
	// EaseCubicIn starts slowly and accelerates.
	EaseCubicIn
	// EaseCubicOut starts fast and decelerates.
	EaseCubicOut
	// EaseCubicInOut accelerates through the first half and decelerates
	// through the second.
	EaseCubicInOut
)

// apply maps progress in [0, 1] to eased progress. A value outside the enum is
// linear.
func (e EaseKind) apply(progress float32) float32 {
	switch e {
	case EaseCubicIn:
		return progress * progress * progress
	case EaseCubicOut:
		inverse := 1 - progress
		return 1 - inverse*inverse*inverse
	case EaseCubicInOut:
		if progress < 0.5 {
			return 4 * progress * progress * progress
		}
		inverse := 2 - 2*progress
		return 1 - inverse*inverse*inverse/2
	default:
		return progress
	}
}

// ClipState is one state of a ClipMachine: a named clip, how it ends, and how
// fast it plays.
type ClipState struct {
	// Name is what a ClipTransition names the state by. The first state of a
	// name wins, as the first clip of a name does.
	Name string
	// Clip is the model's clip the state plays, by name.
	Clip string
	// Loop false clamps and holds the last frame, as ClipPlay.Loop does, and
	// is what lets the state finish.
	Loop bool
	// Rate scales the dt the caller passes to Step. Zero reads as 1, so a
	// state that does not care about pace leaves it out.
	Rate float32
}

// ClipTransition is one way out of a state. Exactly one of On and OnFinish is
// set.
type ClipTransition struct {
	From, To string
	// On names the gameplay trigger Fire takes the transition on.
	On string
	// OnFinish takes the transition on the Step a non-looping From reaches its
	// clip's duration.
	OnFinish bool
	// Crossfade is how long, in the dt the caller passes, the outgoing mix
	// takes to fade out and To to fade in. Zero cuts.
	Crossfade float32
	// Ease is the curve To's weight follows over the crossfade.
	Ease EaseKind
}

// ClipEventKind is what happened to a state on a Step.
type ClipEventKind int

const (
	// ClipEntered is a state a transition started into. Kinds start at one, so
	// the zero ClipEvent is no event.
	ClipEntered ClipEventKind = iota + 1
	// ClipExited is the state a transition left.
	ClipExited
	// ClipFinished is a non-looping state whose time has reached its clip's
	// duration. It fires once per entry into the state.
	ClipFinished
)

// ClipEvent is one thing a ClipMachine reports from a Step. State is an index
// into the machine's states, which StateName names.
type ClipEvent struct {
	Kind  ClipEventKind
	State int
}

// ClipMachine is a clip state machine: the caller keeps it, fires triggers
// into it and steps it each tick, and it hands back the frame's clip plays and
// what happened on the way. It is a plain value, so a scene game keeps one in
// a field and an ecsscene game keeps one as a Component.
//
// One type is both the definition and the runtime state. Its tables are
// m.Lists, which a copy shares read-only, so copying a machine to many
// Entities copies headers rather than tables, and each copy then steps on its
// own.
//
// The zero ClipMachine has no states. It plays nothing and fires nothing.
type ClipMachine struct {
	states      m.List[ClipState]
	transitions m.List[ClipTransition]
	// slots is each state's clip, resolved once at construction, and links
	// each transition's two states.
	slots m.List[clipSlot]
	links m.List[clipLink]

	current  int32
	time     float32
	finished bool

	// fading says a crossfade is running: elapsed of span, along ease.
	fading  bool
	elapsed float32
	span    float32
	ease    EaseKind
	// outgoing is the group fading out. Its weights are shares of the group,
	// summing to one, so the group scales as a whole by 1 - the incoming
	// weight.
	outgoing [MaxClipPlays - 1]outgoingPlay
	outCount int32

	// pending holds what a Fire started, for the next Step to report.
	pending      [pendingEvents]ClipEvent
	pendingCount int32
}

// clipSlot is one state's clip, as an index into the ClipInfo list the machine
// was built from and that clip's duration.
type clipSlot struct {
	clip     int32
	duration float32
}

// clipLink is one transition's From and To, as state indices.
type clipLink struct {
	from, to int32
}

// outgoingPlay is one play of the fading group: a state, its clock, and its
// share of the group.
type outgoingPlay struct {
	state  int32
	time   float32
	weight float32
}

// pendingEvents is how many events Fire can hold for the next Step: four
// Fires' worth. A caller firing more than that between two Steps loses the
// oldest.
const pendingEvents = 8

// NewClipMachine builds a machine from the clips a model declares, as
// LookupDeviceAccess.Clips reports them, and the states and transitions over
// them. Every name is resolved here, once, and Step never touches the lookup.
// The machine starts in states[0] at time 0.
func NewClipMachine(clips []ClipInfo, states []ClipState, transitions []ClipTransition) (ClipMachine, error) {
	if len(states) == 0 {
		return ClipMachine{}, ErrClipMachineEmpty{}
	}
	slots := make([]clipSlot, len(states))
	for i, state := range states {
		clip := clipIndex(clips, state.Clip)
		if clip < 0 {
			return ClipMachine{}, ErrClipStateClipMissing{State: state.Name, Clip: state.Clip}
		}
		slots[i] = clipSlot{clip: int32(clip), duration: clips[clip].Duration}
	}
	links := make([]clipLink, len(transitions))
	for i, transition := range transitions {
		if (transition.On != "") == transition.OnFinish {
			return ClipMachine{}, ErrClipTransitionTriggerInvalid{From: transition.From, To: transition.To}
		}
		from, to := stateIndex(states, transition.From), stateIndex(states, transition.To)
		if from < 0 {
			return ClipMachine{}, ErrClipTransitionStateMissing{
				From: transition.From, To: transition.To, State: transition.From,
			}
		}
		if to < 0 {
			return ClipMachine{}, ErrClipTransitionStateMissing{
				From: transition.From, To: transition.To, State: transition.To,
			}
		}
		links[i] = clipLink{from: int32(from), to: int32(to)}
	}
	return ClipMachine{
		states:      m.ListOf(states),
		transitions: m.ListOf(transitions),
		slots:       m.ListOf(slots),
		links:       m.ListOf(links),
	}, nil
}

func clipIndex(clips []ClipInfo, name string) int {
	for i := range clips {
		if clips[i].Name == name {
			return i
		}
	}
	return -1
}

func stateIndex(states []ClipState, name string) int {
	for i := range states {
		if states[i].Name == name {
			return i
		}
	}
	return -1
}

// StateName is the name of state i, as a ClipEvent reports it.
func (c *ClipMachine) StateName(i int) string { return c.states.At(i).Name }

// Fire starts the transition out of the current state whose On is trigger,
// the first one listed if several are. It returns false when there is none.
//
// A Fire during a crossfade freezes the mix as it stands: the incoming play and
// every outgoing play keep their relative weights and become one group, which
// fades out over the new transition while the new state fades in from time 0.
// The frozen plays' clocks keep running. When the group would push the live
// plays past MaxClipPlays, its lightest play is dropped.
//
// Fire has nowhere to report to, so the ClipExited and ClipEntered it causes
// arrive at the head of the next Step's events.
func (c *ClipMachine) Fire(trigger string) bool {
	for i := range c.transitions.Len() {
		transition := c.transitions.At(i)
		if transition.OnFinish || transition.On != trigger || c.links.At(i).from != c.current {
			continue
		}
		exited, entered := c.begin(i)
		c.hold(exited)
		c.hold(entered)
		return true
	}
	return false
}

// hold keeps one of Fire's events for the next Step, shedding the oldest when
// the buffer is full.
func (c *ClipMachine) hold(event ClipEvent) {
	if c.pendingCount == pendingEvents {
		copy(c.pending[:], c.pending[1:])
		c.pendingCount--
	}
	c.pending[c.pendingCount] = event
	c.pendingCount++
}

// Step advances every live play by dt times its state's Rate and the crossfade
// by dt, takes the OnFinish transition of a state that has just finished, and
// appends that tick's events to events.
//
// The order is fixed: whatever Fire started since the last Step, each as
// ClipExited then ClipEntered; then ClipFinished; then the ClipExited and
// ClipEntered of the OnFinish transition it takes.
func (c *ClipMachine) Step(dt float32, events []ClipEvent) []ClipEvent {
	if c.states.Len() == 0 {
		return events
	}
	events = append(events, c.pending[:c.pendingCount]...)
	c.pendingCount = 0

	c.time += dt * c.rate(c.current)
	for i := range c.outgoing[:c.outCount] {
		c.outgoing[i].time += dt * c.rate(c.outgoing[i].state)
	}
	if c.fading {
		c.elapsed += dt
		if c.elapsed >= c.span {
			c.fading, c.outCount = false, 0
		}
	}

	if c.finished || c.states.At(int(c.current)).Loop || c.time < c.slots.At(int(c.current)).duration {
		return events
	}
	c.finished = true
	events = append(events, ClipEvent{Kind: ClipFinished, State: int(c.current)})
	for i := range c.transitions.Len() {
		if c.transitions.At(i).OnFinish && c.links.At(i).from == c.current {
			exited, entered := c.begin(i)
			return append(events, exited, entered)
		}
	}
	return events
}

// Plays appends the live plays to dst, the current state first, for scene's
// ModelDraw.Plays. The weights sum to one, though nothing needs them to.
func (c *ClipMachine) Plays(dst []ClipPlay) []ClipPlay {
	if c.states.Len() == 0 {
		return dst
	}
	incoming := c.incoming()
	dst = append(dst, c.play(c.current, c.time, incoming))
	for _, out := range c.outgoing[:c.outCount] {
		dst = append(dst, c.play(out.state, out.time, out.weight*(1-incoming)))
	}
	return dst
}

// PlaysInto fills a fixed array with the live plays, for ecsscene's
// Animation. Unused slots get the empty play, whose empty Clip draws nothing.
func (c *ClipMachine) PlaysInto(dst *[MaxClipPlays]ClipPlay) {
	*dst = [MaxClipPlays]ClipPlay{}
	c.Plays(dst[:0])
}

func (c *ClipMachine) play(state int32, time, weight float32) ClipPlay {
	s := c.states.At(int(state))
	return ClipPlay{Clip: s.Clip, Time: time, Loop: s.Loop, Weight: weight}
}

// incoming is the current state's weight: its eased share of a running
// crossfade, or all of it.
func (c *ClipMachine) incoming() float32 {
	if !c.fading {
		return 1
	}
	return c.ease.apply(m.Clamp01(c.elapsed / c.span))
}

func (c *ClipMachine) rate(state int32) float32 {
	if rate := c.states.At(int(state)).Rate; rate != 0 {
		return rate
	}
	return 1
}

// begin starts transition i: the mix as it stands becomes the outgoing group,
// and To becomes the current state at time 0. It returns the two events the
// start makes.
func (c *ClipMachine) begin(i int) (exited, entered ClipEvent) {
	transition, link := c.transitions.At(i), c.links.At(i)

	var group [MaxClipPlays]outgoingPlay
	incoming := c.incoming()
	n := 0
	if incoming > 0 {
		group[n] = outgoingPlay{state: c.current, time: c.time, weight: incoming}
		n++
	}
	for _, out := range c.outgoing[:c.outCount] {
		if weight := out.weight * (1 - incoming); weight > 0 {
			group[n] = outgoingPlay{state: out.state, time: out.time, weight: weight}
			n++
		}
	}
	for n > len(c.outgoing) {
		lightest := 0
		for j := 1; j < n; j++ {
			if group[j].weight < group[lightest].weight {
				lightest = j
			}
		}
		copy(group[lightest:n], group[lightest+1:n])
		n--
	}
	var total float32
	for _, out := range group[:n] {
		total += out.weight
	}
	c.outCount = 0
	if transition.Crossfade > 0 && total > 0 {
		for _, out := range group[:n] {
			out.weight /= total
			c.outgoing[c.outCount] = out
			c.outCount++
		}
	}

	exited = ClipEvent{Kind: ClipExited, State: int(c.current)}
	c.current, c.time, c.finished = link.to, 0, false
	c.fading = c.outCount > 0
	c.elapsed, c.span, c.ease = 0, transition.Crossfade, transition.Ease
	return exited, ClipEvent{Kind: ClipEntered, State: int(link.to)}
}
