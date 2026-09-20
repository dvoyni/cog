package main

// THROWAWAY PROTOTYPE - not production code. See doc.go.
//
// The instrumentation. Every number the arithmetic produced this tick is on
// screen, because the point of a listening test is to be able to say what you
// were listening to when you heard the thing.

import (
	"fmt"
	"math"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/libs/m"
)

// The top-down view. World +X is screen right and world -Z is screen up, so
// the listener's forward points up the screen.
const (
	viewCenterX   = 360
	viewCenterY   = 330
	pixelsPerUnit = 42
)

var (
	colBg       = m.NewColorSrgb(0.07, 0.08, 0.10, 1)
	colGrid     = m.NewColorSrgb(0.16, 0.18, 0.22, 1)
	colListener = m.NewColorSrgb(0.36, 0.72, 0.53, 1)
	colSource   = m.NewColorSrgb(0.90, 0.76, 0.33, 1)
	colCone     = m.NewColorSrgb(0.87, 0.42, 0.35, 1)
	colRef      = m.NewColorSrgb(0.24, 0.53, 0.60, 1)
	colText     = m.NewColorSrgb(0.82, 0.85, 0.90, 1)
	colDim      = m.NewColorSrgb(0.48, 0.52, 0.58, 1)
	colWarn     = m.NewColorSrgb(0.95, 0.55, 0.35, 1)
	colMeterL   = m.NewColorSrgb(0.36, 0.60, 0.88, 1)
	colMeterR   = m.NewColorSrgb(0.88, 0.50, 0.70, 1)
)

func worldToScreen(v m.Vec3) m.Vec2 {
	return m.Vec2{X: viewCenterX + v.X*pixelsPerUnit, Y: viewCenterY - (-v.Z)*pixelsPerUnit}
}

func (p *Demo) draw(q *canvas.OpQueue) {
	q.Clear(layerMain, colBg)
	p.drawPlan(q)
	p.drawMeters(q)
	p.drawReadout(q)
	p.drawKeys(q)
}

// drawPlan is the top-down plan view: listener, source, the ref/max distance
// rings, the cone, and the source's elevation as a separate marker because a
// plan view cannot show height.
func (p *Demo) drawPlan(q *canvas.OpQueue) {
	// Grid, one line per world unit.
	for i := -8; i <= 8; i++ {
		x := viewCenterX + float32(i)*pixelsPerUnit
		y := viewCenterY + float32(i)*pixelsPerUnit
		q.Line(layerMain, m.Vec2{X: x, Y: 40}, m.Vec2{X: x, Y: 620}, canvas.ShapeDraw{Color: colGrid, Thickness: 1})
		q.Line(layerMain, m.Vec2{X: 40, Y: y}, m.Vec2{X: 680, Y: y}, canvas.ShapeDraw{Color: colGrid, Thickness: 1})
	}

	lp := worldToScreen(p.listener.Position)

	// The refDistance ring: inside it, inverse falloff does nothing at all,
	// which is the single most important thing to be able to see while judging
	// item 2.
	ring(q, lp, p.falloff.RefDistance*pixelsPerUnit, colRef, 1.5)
	if d := p.falloff.MaxDistance * pixelsPerUnit; d < 400 {
		ring(q, lp, d, colDim, 1)
	}

	// Listener: a box with a forward tick.
	q.FillRect(layerMain, m.Rect{X: lp.X - 7, Y: lp.Y - 7, Width: 14, Height: 14},
		canvas.ShapeDraw{Color: colListener})
	q.Line(layerMain, lp, m.Vec2{X: lp.X, Y: lp.Y - 28},
		canvas.ShapeDraw{Color: colListener, Thickness: 2})

	pos := p.sourcePosition()
	sp := worldToScreen(pos)

	// The cone, drawn as two edges from the source along its orientation.
	if c := conePresets[p.conePre].cone; c.InnerAngle != 360 {
		for _, half := range []float32{c.InnerAngle / 2, c.OuterAngle / 2} {
			for _, sign := range []float32{-1, 1} {
				a := p.orient + sign*half*math.Pi/180
				end := m.Vec2{
					X: sp.X + float32(math.Sin(float64(a)))*90,
					Y: sp.Y - float32(math.Cos(float64(a)))*90,
				}
				q.Line(layerMain, sp, end, canvas.ShapeDraw{Color: colCone, Thickness: 1})
			}
		}
	}

	// The line to the listener, and the source itself sized by audibility so
	// distance gain is visible as well as audible.
	q.Line(layerMain, lp, sp, canvas.ShapeDraw{Color: colDim, Thickness: 1})
	r := 4 + 14*clamp32(p.last.Audibility, 0, 1)
	q.FillRect(layerMain, m.Rect{X: sp.X - r, Y: sp.Y - r, Width: 2 * r, Height: 2 * r},
		canvas.ShapeDraw{Color: colSource})

	// Elevation, which the plan view flattens away. A vertical strip beside it.
	const ex, ey, eh = 700, 150, 220
	q.Line(layerMain, m.Vec2{X: ex, Y: ey}, m.Vec2{X: ex, Y: ey + eh},
		canvas.ShapeDraw{Color: colGrid, Thickness: 2})
	q.Line(layerMain, m.Vec2{X: ex - 8, Y: ey + eh/2}, m.Vec2{X: ex + 8, Y: ey + eh/2},
		canvas.ShapeDraw{Color: colDim, Thickness: 1})
	ty := ey + eh/2 - (p.last.Elevation/90)*(eh/2)
	q.FillRect(layerMain, m.Rect{X: ex - 6, Y: ty - 3, Width: 12, Height: 6},
		canvas.ShapeDraw{Color: colSource})
	text(q, m.Vec2{X: ex - 26, Y: ey - 22}, 13, colDim, "elevation")
}

// drawMeters shows the two output columns of the gain matrix as bars. This is
// the thing that makes a pan hole visible: watch the pair cross as the source
// goes by, and watch them stop moving when it passes 90 degrees.
func (p *Demo) drawMeters(q *canvas.OpQueue) {
	mat := p.last.Matrix
	left := mat[0][0] + mat[1][0]
	right := mat[0][1] + mat[1][1]

	const bx, by, bw, bh = 700, 430, 150, 22
	for i, v := range []struct {
		label string
		gain  float32
		col   m.Color
	}{{"L", left, colMeterL}, {"R", right, colMeterR}} {
		y := float32(by + i*34)
		q.StrokeRect(layerMain, m.Rect{X: bx, Y: y, Width: bw, Height: bh},
			canvas.ShapeDraw{Color: colGrid, Thickness: 1})
		w := clamp32(v.gain, 0, 1) * bw
		q.FillRect(layerMain, m.Rect{X: bx, Y: y, Width: w, Height: bh},
			canvas.ShapeDraw{Color: v.col})
		text(q, m.Vec2{X: bx - 18, Y: y + 3}, 15, colText, v.label)
		text(q, m.Vec2{X: bx + bw + 10, Y: y + 4}, 13, colDim, fmt.Sprintf("%.3f", v.gain))
	}

	// Equal-power's invariant: L^2 + R^2 should hold constant as a source pans,
	// and the number drifting is the arithmetic being wrong rather than the
	// speakers being odd.
	power := left*left + right*right
	col := colDim
	if p.last.DistanceGain > 0 && p.last.ConeGain > 0 && !p.bypass {
		norm := power / max32(p.last.Audibility*p.last.Audibility, 1e-9)
		if norm < 0.9 || norm > 1.45 {
			col = colWarn
		}
	}
	text(q, m.Vec2{X: bx, Y: by + 74}, 13, col, fmt.Sprintf("L^2+R^2  %.4f", power))
}

func (p *Demo) drawReadout(q *canvas.OpQueue) {
	mx := p.device.Mixer
	clip := p.clips[p.clip]
	pos := p.sourcePosition()

	playing := "STOPPED  (space to play)"
	if mx.Active(0) {
		playing = "PLAYING"
		if p.paused {
			playing = "PAUSED"
		}
	}

	lines := []string{
		fmt.Sprintf("state      %s   peak out %.3f", playing, mx.Peak()),
		fmt.Sprintf("scenario   %s", p.scenario),
		fmt.Sprintf("clip       %s  %dch  src %d Hz  %.1fs", clip.Name, clip.Channels, clip.SourceRate, clip.Duration),
		fmt.Sprintf("device     %s   latency %.1f ms   pending %d", p.device, p.device.Latency(), mx.Pending()),
		"",
		fmt.Sprintf("source     (%+.2f %+.2f %+.2f)   listener z %+.2f", pos.X, pos.Y, pos.Z, p.listenerZ),
		fmt.Sprintf("azimuth    %+7.2f deg      elevation %+7.2f deg", p.last.Azimuth, p.last.Elevation),
		fmt.Sprintf("distance   %7.3f          audibility %6.4f", p.last.Distance, p.last.Audibility),
		fmt.Sprintf("dist gain  %7.4f          cone gain  %6.4f", p.last.DistanceGain, p.last.ConeGain),
		fmt.Sprintf("matrix     %s", p.last.Matrix),
		"",
		fmt.Sprintf("falloff    %s  ref %.2f  rolloff %.2f  max %.0f",
			p.falloff.Model, p.falloff.RefDistance, p.falloff.RolloffFactor, p.falloff.MaxDistance),
		fmt.Sprintf("cone       %s   facing %+.0f deg", conePresets[p.conePre].name, p.orient*180/math.Pi),
		fmt.Sprintf("rate       %.3f   volume %.2f   radius %.2f   speed %.2f", p.rate, p.volume, p.radius, p.speed),
		"",
		fmt.Sprintf("A/B        spatialize %s   declick %s   interp %s   downmix %s",
			onOff(!p.bypass), onOff(mx.Declick()), mx.Interp(), onOff(p.downmix)),
		fmt.Sprintf("mixer      reads %d  frames %d  peak %.3f  clipped %d",
			mx.Reads(), mx.FramesOut(), mx.Peak(), mx.Clipped()),
	}

	y := float32(16)
	for _, line := range lines {
		col := colText
		switch {
		case len(line) > 10 && line[:10] == "A/B       ":
			col = colSource
		case len(line) > 10 && line[:10] == "state     ":
			col = colListener
			if !mx.Active(0) {
				col = colWarn
			}
		}
		text(q, m.Vec2{X: 14, Y: y}, 14, col, line)
		y += 18
	}
	if p.err != "" {
		text(q, m.Vec2{X: 14, Y: y + 8}, 14, colWarn, p.err)
	}
	if err := p.device.Err(); err != nil {
		text(q, m.Vec2{X: 14, Y: y + 8}, 14, colWarn, "device: "+err.Error())
	}
	if p.bypass {
		text(q, m.Vec2{X: 700, Y: 560}, 16, colWarn, "BYPASS: no spatialization")
	}
}

func (p *Demo) drawKeys(q *canvas.OpQueue) {
	help := []string{
		"F1-F5 scenario   1-5 clip   space (re)play   S stop   L loop   P pause   R reset   Esc quit",
		"B bypass spatialization   K declick   I interpolator   M downmix stereo   D distance model   C cone",
		"[ ] refDistance   - = rolloff   , . rate (pitch)   Q E cone facing   Z X path speed   up/down radius   G H listener back-off",
		"shift = fine",
	}
	y := float32(632)
	for _, line := range help {
		text(q, m.Vec2{X: 14, Y: y}, 12, colDim, line)
		y += 16
	}
}

// ring approximates a circle with line segments; canvas has no circle stroke
// and a throwaway does not need one.
func ring(q *canvas.OpQueue, center m.Vec2, radius float32, col m.Color, thickness float32) {
	if radius <= 0 || radius > 2000 {
		return
	}
	const segments = 48
	prev := m.Vec2{X: center.X + radius, Y: center.Y}
	for i := 1; i <= segments; i++ {
		a := float64(i) / segments * 2 * math.Pi
		next := m.Vec2{
			X: center.X + radius*float32(math.Cos(a)),
			Y: center.Y + radius*float32(math.Sin(a)),
		}
		q.Line(layerMain, prev, next, canvas.ShapeDraw{Color: col, Thickness: thickness})
		prev = next
	}
}

func text(q *canvas.OpQueue, at m.Vec2, size float32, col m.Color, s string) {
	q.Text(layerMain, "", s, canvas.TextDraw{Position: at, Size: size, Color: col})
}

func onOff(v bool) string {
	if v {
		return "ON "
	}
	return "off"
}
