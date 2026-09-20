package main

// THROWAWAY PROTOTYPE - not production code. See doc.go.
//
// The device, which is the part the 2026-09-12 spike already proved. It is here
// at the settings the spike landed on - 48 kHz, stereo, float32, a 10 ms buffer
// - because the point of this prototype is to judge what comes through it, not
// to re-measure it.

import (
	"fmt"
	"time"

	"github.com/ebitengine/oto/v3"
)

const (
	deviceRate   = 48000
	bufferMillis = 10
	deviceChans  = 2
)

// Device is one oto context and the one player the mixer feeds. #299 makes the
// context per process rather than per Engine; a prototype with one Engine does
// not have to care, but the shape is kept.
type Device struct {
	ctx    *oto.Context
	player *oto.Player
	Mixer  *Mixer
	Rate   int
	Buffer time.Duration
}

func NewDevice(rate, bufferMs int) (*Device, error) {
	buffer := time.Duration(bufferMs) * time.Millisecond
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:      rate,
		ChannelCount:    deviceChans,
		Format:          oto.FormatFloat32LE,
		BufferSize:      buffer,
		ApplicationName: "cog audio spatialization prototype",
	})
	if err != nil {
		return nil, err
	}
	// On the desktop this closes immediately. #299's not-ready case is the
	// browser's, and jssound's, not this prototype's.
	<-ready
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	mixer := NewMixer(rate, deviceChans)
	player := ctx.NewPlayer(mixer)
	player.SetBufferSize(int(buffer.Seconds()*float64(rate)) * 4 * deviceChans)
	player.Play()

	return &Device{ctx: ctx, player: player, Mixer: mixer, Rate: rate, Buffer: buffer}, nil
}

// Latency is what the player says it is holding, in milliseconds. #299 makes
// this a reported observable rather than a promise.
func (d *Device) Latency() float64 {
	bytesPerSecond := float64(d.Rate * 4 * deviceChans)
	return float64(d.player.BufferedSize()) / bytesPerSecond * 1000
}

func (d *Device) Err() error { return d.player.Err() }

func (d *Device) String() string {
	return fmt.Sprintf("oto %d Hz x%d f32 buf %v", d.Rate, deviceChans, d.Buffer)
}
