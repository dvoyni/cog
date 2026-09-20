package main

// THROWAWAY PROTOTYPE - not production code. See doc.go.

import (
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/canvasplugin"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/input/inputplugin"
	"github.com/dvoyni/cog/extensions/diskstorage"
	"github.com/dvoyni/cog/extensions/diskstorage/diskstorageplugin"
	"github.com/dvoyni/cog/extensions/gogpu"
	"github.com/dvoyni/cog/extensions/gogpu/gogpuplugin"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

const (
	screenWidth               = 1100
	screenHeight              = 720
	layerMain    canvas.Layer = 0
)

func main() {
	device, err := NewDevice(deviceRate, bufferMillis)
	if err != nil {
		fmt.Fprintln(os.Stderr, "audio device:", err)
		os.Exit(1)
	}

	demo, err := New(device)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	config := map[kernel.PluginName]any{
		storage.Name:     storage.Config{},
		diskstorage.Name: diskstorage.Config{},
		gogpu.Name:       gogpu.Config{}.WithTitle("cog proto: audio spatialization (#304)"),
	}

	plugins := []kernel.Plugin{
		storageplugin.New(),
		diskstorageplugin.New(),
		inputplugin.New(),
		appplugin.New(),
		gfxplugin.New(),
		canvasplugin.New(),
		gogpuplugin.New(),
		demo,
	}

	engine := kernel.New(config).WithPlugins(plugins...)
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	go func() {
		<-interrupt
		engine.Quit()
	}()
	if err := engine.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// Name is the prototype plugin's name.
const Name kernel.PluginName = "audio-spatialize"

type windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
type updateEventHandler kernel.Subscription[app.UpdateEvent]

// Scenario is one of the listening cases the ticket names. They are scripted
// rather than hand-flown because you want to hear the same pass twice: an
// A/B where the path differs between the two halves is not an A/B.
type Scenario int

const (
	// ScenarioOrbit circles the listener in the horizontal plane. This is
	// item 1's first case: the source goes behind, where W3C's equal-power pan
	// folds and stops being able to say "behind".
	ScenarioOrbit Scenario = iota
	// ScenarioFlyover passes straight over the listener's head, through the
	// zenith, where the horizontal projection vanishes and the spec defines
	// azimuth as 0. Item 1's second case.
	ScenarioFlyover
	// ScenarioPassBy is a car going past: a straight line left to right at a
	// fixed offset in front. The everyday case.
	ScenarioPassBy
	// ScenarioApproach runs straight at the listener and back out again, so
	// distance attenuation is heard with no pan movement at all. This is where
	// item 2 is answered.
	ScenarioApproach
	// ScenarioMouse2D is the canvas case: the Z=0 plane, mouse-driven, listener
	// unrotated. Item 5's "when the source is driven by input".
	ScenarioMouse2D
	scenarioCount
)

func (s Scenario) String() string {
	switch s {
	case ScenarioFlyover:
		return "flyover (through the zenith)"
	case ScenarioPassBy:
		return "pass-by (straight line, in front)"
	case ScenarioApproach:
		return "approach / recede (no pan)"
	case ScenarioMouse2D:
		return "2D plane, mouse-driven"
	default:
		return "orbit (goes behind)"
	}
}

type clipChoice struct {
	key  input.Key
	file string
	name string
	loop bool
}

// The kit. Mono first, because positional audio is a mono story and the stereo
// pair is there to test the advice rather than to be spatialized well.
var clipChoices = []clipChoice{
	{input.Key1, "bellsmall.ogg", "bell (mono, transient)", false},
	{input.Key2, "stirling.ogg", "stirling engine (mono, sustained)", true},
	{input.Key3, "engine5cyl.ogg", "5-cyl engine (mono, was 96k)", true},
	{input.Key4, "footsteps.ogg", "footsteps (STEREO)", true},
	{input.Key5, "pianoroll.ogg", "piano roll (STEREO, 176s)", true},
}

// scenarioKeys selects a scenario. F1..F5 in the order Scenario declares them.
var scenarioKeys = []input.Key{
	input.KeyF1, input.KeyF2, input.KeyF3, input.KeyF4, input.KeyF5,
}

var conePresets = []struct {
	name string
	cone Cone
}{
	{"off (W3C default 360/360)", DefaultCone()},
	{"wide 90/270 outer 0.15", Cone{InnerAngle: 90, OuterAngle: 270, OuterGain: 0.15}},
	{"tight 30/90 outer 0.05", Cone{InnerAngle: 30, OuterAngle: 90, OuterGain: 0.05}},
	{"megaphone 20/40 outer 0.0", Cone{InnerAngle: 20, OuterAngle: 40, OuterGain: 0}},
}

// Demo is the game. Its state is the scenario clock and the knobs.
type Demo struct {
	device *Device
	clips  []*PreparedClip

	elapsed  float32
	scenario Scenario
	clip     int
	playing  bool
	loop     bool

	listener  Listener
	listenerZ float32 // how far back the listener sits on +Z; see ScenarioMouse2D
	falloff   Falloff
	conePre   int
	orient    float32 // cone facing, radians in the horizontal plane
	rate      float32
	volume    float32
	radius    float32
	speed     float32
	paused    bool

	// A/B switches.
	bypass  bool // no spatialization at all: centred, unattenuated
	downmix bool

	// mouse2D maps the pointer into world units.
	pointer m.Vec2

	last Spatialized
	err  string
}

func New(device *Device) (*Demo, error) {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "clips")

	d := &Demo{
		device:   device,
		listener: DefaultListener(),
		falloff:  DefaultFalloff(),
		rate:     1,
		volume:   0.8,
		radius:   4,
		speed:    0.35,
		loop:     true,
	}
	for _, c := range clipChoices {
		clip, err := PrepareOgg(filepath.Join(dir, c.file), c.name, deviceRate)
		if err != nil {
			return nil, fmt.Errorf("preparing clips: %w", err)
		}
		d.clips = append(d.clips, clip)
	}
	d.loop = clipChoices[0].loop
	return d, nil
}

func (p *Demo) Name() kernel.PluginName { return Name }

func (p *Demo) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, gfx.Name, input.Name, storage.Name}
}

func (p *Demo) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[windowSizeChangeEventHandler](setViewport)
	registrar.Subscribe[updateEventHandler](p.tick)
	return nil
}

func setViewport() (kernel.Lock, kernel.Observe[app.WindowSizeChangeEvent]) {
	var setDesiredViewport func(kernel.Kernel, gfx.SetDesiredViewportRequest) gfx.SetDesiredViewportResponse
	return func(access kernel.ResourceAccess) {
			setDesiredViewport = access.Uses[gfx.SetDesiredViewportCmd]()
		}, func(k kernel.Kernel, event app.WindowSizeChangeEvent) {
			if event.Width <= 0 || event.Height <= 0 {
				return
			}
			setDesiredViewport(k, gfx.SetDesiredViewportRequest{
				Mode: gfx.ViewportFit, Width: screenWidth, Height: screenHeight,
			})
		}
}

func (p *Demo) tick() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var canvasQueue kernel.Write[*canvas.OpQueue]
	var inputState kernel.Read[*input.State]
	var quit func(kernel.Kernel, app.QuitRequest) app.QuitResponse
	return func(access kernel.ResourceAccess) {
			canvasQueue = access.GetWrite[*canvas.OpQueue]()
			inputState = access.GetRead[*input.State]()
			quit = access.Uses[app.QuitCmd]()
		}, func(k kernel.Kernel, event app.UpdateEvent) {
			state := inputState.Get()
			if state != nil && state.JustPressed(input.KeyEscape) {
				quit(k, app.QuitRequest{})
			}
			p.readInput(state)
			if !p.paused {
				p.elapsed += float32(event.Dt)
			}
			p.record()
			p.draw(canvasQueue.Get())
		}
}

// sourcePosition is the scenario, evaluated at the current clock. Units are
// metres-ish, which is what W3C's refDistance of 1 assumes - #297 left units to
// the game and this game has chosen.
func (p *Demo) sourcePosition() m.Vec3 {
	t := p.elapsed * p.speed
	switch p.scenario {
	case ScenarioFlyover:
		// Straight over the top, back to front, passing exactly through the
		// zenith so the projection really does hit zero.
		z := float32(math.Cos(float64(t))) * p.radius
		y := float32(math.Sin(float64(t))) * p.radius
		return m.Vec3{X: 0, Y: absf(y), Z: z}
	case ScenarioPassBy:
		x := float32(math.Sin(float64(t))) * p.radius * 2
		return m.Vec3{X: x, Y: 0, Z: -p.radius * 0.6}
	case ScenarioApproach:
		d := (1 - float32(math.Cos(float64(t)))) * 0.5 // 0..1..0
		return m.Vec3{X: 0, Y: 0, Z: -(0.2 + d*p.radius*3)}
	case ScenarioMouse2D:
		// #297's rule taken literally: a 2D sound is a 3D sound on the Z=0
		// plane, so the sprite's screen position is its world X and Y and Z
		// stays 0.
		//
		// Which is worth stating plainly, because it is the finding this
		// scenario exists to produce: with the listener also at Z=0 and facing
		// -Z, every source with a non-zero X projects onto the listener's right
		// axis alone, so azimuth is +-90 for every sprite on the screen and
		// nothing is ever anywhere but hard left or hard right. G / H pull the
		// listener back along +Z, which is what a canvas camera would do, and
		// the azimuth readout stops being a two-valued function as soon as it
		// is non-zero.
		return m.Vec3{X: p.pointer.X, Y: p.pointer.Y, Z: 0}
	default: // orbit
		return m.Vec3{
			X: float32(math.Sin(float64(t))) * p.radius,
			Y: 0,
			Z: -float32(math.Cos(float64(t))) * p.radius,
		}
	}
}

// record is the tick's write into the queue: compute the arithmetic, hand the
// matrix down, publish the batch. One Emit per tick, which is #376's shape.
func (p *Demo) record() {
	clip := p.clips[p.clip]
	pos := p.sourcePosition()

	e := Emitter{
		Positional: !p.bypass,
		Position:   pos,
		Falloff:    p.falloff,
		Cone:       conePresets[p.conePre].cone,
		Volume:     p.volume,
	}
	if conePresets[p.conePre].cone.InnerAngle != 360 {
		e.Orientation = m.Vec3{
			X: float32(math.Sin(float64(p.orient))),
			Z: -float32(math.Cos(float64(p.orient))),
		}
	}

	channels := clip.Channels
	if p.downmix {
		channels = 1
	}
	s := Spatialize(e, p.listener, channels)
	p.last = s

	p.device.Mixer.SetParams(0, slotParams{
		Matrix:  s.Matrix,
		Rate:    p.rate,
		Paused:  p.paused,
		Downmix: p.downmix && clip.Channels == 2,
	})
	p.device.Mixer.Emit()
}

func (p *Demo) readInput(state *input.State) {
	if state == nil {
		return
	}
	mx := p.device.Mixer

	for i, c := range clipChoices {
		if state.JustPressed(c.key) {
			p.clip, p.loop = i, c.loop
			p.restart()
		}
	}
	for i, key := range scenarioKeys {
		if state.JustPressed(key) {
			p.scenario = Scenario(i)
			p.elapsed = 0
		}
	}

	switch {
	case state.JustPressed(input.KeySpace):
		p.restart()
	case state.JustPressed(input.KeyS):
		p.device.Mixer.Stop(0)
		p.playing = false
	case state.JustPressed(input.KeyL):
		p.loop = !p.loop
		p.restart()
	case state.JustPressed(input.KeyP):
		p.paused = !p.paused
	case state.JustPressed(input.KeyB):
		p.bypass = !p.bypass
	case state.JustPressed(input.KeyK):
		mx.SetDeclick(!mx.Declick())
	case state.JustPressed(input.KeyI):
		if mx.Interp() == InterpLinear {
			mx.SetInterp(InterpCubic)
		} else {
			mx.SetInterp(InterpLinear)
		}
	case state.JustPressed(input.KeyM):
		p.downmix = !p.downmix
	case state.JustPressed(input.KeyD):
		p.falloff.Model = (p.falloff.Model + 1) % 3
	case state.JustPressed(input.KeyC):
		p.conePre = (p.conePre + 1) % len(conePresets)
	case state.JustPressed(input.KeyR):
		p.falloff = DefaultFalloff()
		p.rate, p.volume, p.conePre = 1, 0.8, 0
		mx.ResetPeak()
	}

	step := float32(1)
	if state.Pressed(input.KeyLeftShift) {
		step = 0.1
	}
	if state.Pressed(input.KeyLeftBracket) {
		p.falloff.RefDistance = max32(0.05, p.falloff.RefDistance-0.02*step)
	}
	if state.Pressed(input.KeyRightBracket) {
		p.falloff.RefDistance += 0.02 * step
	}
	if state.Pressed(input.KeyMinus) {
		p.falloff.RolloffFactor = max32(0, p.falloff.RolloffFactor-0.02*step)
	}
	if state.Pressed(input.KeyEqual) {
		p.falloff.RolloffFactor += 0.02 * step
	}
	if state.Pressed(input.KeyComma) {
		p.rate = max32(0.25, p.rate-0.005*step)
	}
	if state.Pressed(input.KeyPeriod) {
		p.rate = min32(4, p.rate+0.005*step)
	}
	if state.Pressed(input.KeyQ) {
		p.orient -= 0.02
	}
	if state.Pressed(input.KeyE) {
		p.orient += 0.02
	}
	if state.Pressed(input.KeyZ) {
		p.speed = max32(0, p.speed-0.004)
	}
	if state.Pressed(input.KeyX) {
		p.speed += 0.004
	}
	if state.Pressed(input.KeyUp) {
		p.radius = min32(60, p.radius+0.04)
	}
	if state.Pressed(input.KeyDown) {
		p.radius = max32(0.1, p.radius-0.04)
	}
	if state.Pressed(input.KeyG) {
		p.listenerZ = max32(0, p.listenerZ-0.05)
	}
	if state.Pressed(input.KeyH) {
		p.listenerZ += 0.05
	}
	p.listener.Position = m.Vec3{Z: p.listenerZ}

	// The pointer in world units, with the listener at the middle of the view.
	// Screen Y grows downward and world +Y is up, so the sign flips.
	pos := state.Pointer()
	p.pointer = m.Vec2{
		X: (float32(pos.X) - viewCenterX) / pixelsPerUnit,
		Y: -(float32(pos.Y) - viewCenterY) / pixelsPerUnit,
	}
}

func (p *Demo) restart() {
	p.device.Mixer.Start(0, p.clips[p.clip], p.loop, 0)
	p.playing = true
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
