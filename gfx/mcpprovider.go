package gfx

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/mcp"
)

// gfx offers its capabilities itself rather than through a separate plugin,
// per the rule that every package hosts its own provider: a capture must live
// where the Backend internals are, and this is where they are.
var _ mcp.Provider = (*Plugin)(nil)

// captureName is the capability rendered as the tool gfx_capture.
const captureName = "capture"

// captureFloorDeadline is the fixed part of a capture's own wait, and
// captureTick the per-still part. Two seconds is a hundred and twenty frames
// at 60 Hz: anything slower is not slow, it is not rendering, and saying so in
// two seconds beats the broker's generic thirty. The tick figure is nominal -
// the step belongs to whichever host owns the loop, and gfx has no way to read
// it - so it sizes the wait rather than measuring anything.
const (
	captureFloorDeadline = 2 * time.Second
	captureTick          = time.Second / 60
)

// captureDescription is prompt text, and it is reproduced in
// gfx/docs/specs/mcp.md so it is reviewed as prompt text rather than buried as
// a string literal.
const captureDescription = "Take a screenshot of what the game is drawing and write it to a PNG " +
	"file at the absolute path you give. Blocks about 50 ms while the next frame is recorded, " +
	"rendered and read back; the image is guaranteed to show the game *after* anything you did " +
	"before calling this. Returns the path plus the image size in pixels and the window size in " +
	"the units input capabilities use — divide one by the other to convert a point you can see " +
	"in the image into a point you can click. Open the file to look at it; the picture is never " +
	"returned inline.\n\n" +
	"For a sequence, set `amount` (up to 60) and `interval` (ticks between stills, so " +
	"`interval: 60` is about a second apart at 60 Hz) and put `%04d` in the path — " +
	"`frame-%04d.png` writes `frame-0000.png` onward, and the response lists the ordinals " +
	"actually written. A burst spans many moments on purpose; to describe one moment, pause " +
	"first and take a single capture last, after any snapshots.\n\n" +
	"While the game is paused a single capture costs no tick and two captures are identical; " +
	"`amount` above 1 is refused, because there would be nothing new to photograph."

// CaptureRequest asks for one or more screenshots, written to files the agent
// names. There is no default directory and no fallback: a default is a guess
// at a directory the agent may not be able to see, and the agent already holds
// an unrestricted write tool, so naming the path grants it nothing it did not
// have. What it buys is that the game's working directory and the agent's need
// not coincide, and that files get meaningful names.
type CaptureRequest struct {
	Path     string `json:"path" jsonschema:"absolute path ending in .png; put %04d in it for a burst"`
	Amount   int    `json:"amount,omitempty" jsonschema:"how many stills to take; default 1, maximum 60"`
	Interval int    `json:"interval,omitempty" jsonschema:"ticks between stills; default 1"`
}

// CaptureResponse reports what was written and how to read a point off it.
//
// There is no frame number, because cog has none to give and inventing a
// public one for a debug response is the wrong direction; no timestamp and no
// format, because the format is always PNG; and no account of what was
// captured, because it is always the screen.
type CaptureResponse struct {
	// Path is the file with the lowest ordinal actually written, so it always
	// names a file that exists. The rest follow the template that was asked
	// for, at the ordinals in Indices.
	Path string `json:"path"`
	// Indices are the ordinals actually written, zero-based. It is required
	// rather than a nicety: a deadline, a refusal and a game exiting all
	// truncate a burst, and a silent short one reads as nothing happening
	// between two frames.
	Indices []int `json:"indices"`
	// PixelWidth and PixelHeight are the image's own size, and WindowWidth and
	// WindowHeight the window's in the units input capabilities use. On a
	// HiDPI display the two differ by the scale factor, and together they are
	// the conversion from a point in the picture to a point that can be
	// clicked.
	PixelWidth   int     `json:"pixelWidth"`
	PixelHeight  int     `json:"pixelHeight"`
	WindowWidth  float32 `json:"windowWidth"`
	WindowHeight float32 `json:"windowHeight"`
}

// Capabilities reports what gfx offers an agent. Capture is screen-only:
// GpuCaptureDesc addresses any colour texture and that generality is right for
// gfx, but nothing lists textures to an agent and a TextureID is an opaque
// handle it has no way to obtain.
func (p *Plugin) Capabilities() []mcp.Capability {
	return []mcp.Capability{mcp.Func(captureName, captureDescription, captureScreen, mcp.ReadOnly())}
}

// captureScreen is the gfx_capture body: validate, arm, wait, and do the
// expensive part here. Un-striding, PNG encoding and the disk write all happen
// on this goroutine, which is the broker's; the engine's threads hand over
// bytes and nothing more.
//
// It is a package function rather than a method to keep the capability-body
// rule visible at the call site: the plugin is one pointer away and the body
// still reaches gfx only by dispatch.
func captureScreen(k kernel.Executioner, request CaptureRequest) (CaptureResponse, error) {
	amount, interval := max(request.Amount, 1), max(request.Interval, 1)
	// Every check, the numbering verb included, happens before a frame is
	// spent, so a typo costs microseconds rather than three frames and a
	// burst is refused whole or armed whole.
	numbered, err := validateCapturePath(request.Path, amount)
	if err != nil {
		return CaptureResponse{}, err
	}
	if err := validateCaptureSpan(amount, interval); err != nil {
		return CaptureResponse{}, err
	}
	paused := enginePaused(k)
	if amount > 1 && paused {
		return CaptureResponse{}, mcp.Unavailable{Reason: "the game is paused, so a burst would " +
			"write identical files; resume it, or ask for a single capture"}
	}
	if err := os.MkdirAll(filepath.Dir(request.Path), 0o755); err != nil {
		return CaptureResponse{}, mcp.Unavailable{Reason: fmt.Sprintf(
			"the directory for %s could not be created: %v", request.Path, err)}
	}

	armed, err := k.ExecuteCommand[ArmCaptureCmd](ArmCaptureRequest{
		Target: GpuCaptureDesc{Screen: true}, Amount: amount, Interval: interval, Paused: paused,
	})
	if err != nil {
		return CaptureResponse{}, captureRefusal(err, amount, interval)
	}
	return collectCapture(k, armed, request.Path, numbered, amount, interval)
}

// collectCapture writes each still as it arrives and reports the ordinals that
// reached disk. Expiry truncates rather than failing: zero frames is an error,
// one or more is a short success, and shutdown mid-burst behaves the same way.
func collectCapture(
	k kernel.Executioner, armed ArmCaptureResponse,
	template string, numbered bool, amount, interval int,
) (CaptureResponse, error) {
	deadline := time.NewTimer(captureFloorDeadline + time.Duration(amount*interval)*captureTick)
	defer deadline.Stop()

	response := CaptureResponse{
		WindowWidth: armed.Viewport.WindowWidth, WindowHeight: armed.Viewport.WindowHeight,
	}
	var refused error
	for len(response.Indices) < amount {
		select {
		case capture := <-armed.Done:
			if capture.Err != nil {
				refused = capture.Err
			} else {
				ordinal := len(response.Indices)
				path := template
				if numbered {
					path = fmt.Sprintf(template, ordinal)
				}
				if err := writeCapturePNG(path, capture); err != nil {
					return CaptureResponse{}, mcp.Unavailable{Reason: fmt.Sprintf(
						"the capture could not be written to %s: %v", path, err)}
				}
				if ordinal == 0 {
					response.Path = path
					response.PixelWidth, response.PixelHeight = capture.Width, capture.Height
				}
				response.Indices = append(response.Indices, ordinal)
				continue
			}
		case <-deadline.C:
		case <-k.Context().Done():
			refused = k.Context().Err()
		}
		break
	}
	if len(response.Indices) == 0 {
		return CaptureResponse{}, captureRefusal(refused, amount, interval)
	}
	return response, nil
}

// writeCapturePNG un-strides one readback and puts it on disk. An existing
// file is overwritten without complaint: re-writing the same name is the
// iterate-and-look loop.
func writeCapturePNG(path string, capture GpuCapture) error {
	picture := capture.Image()
	if picture == nil {
		return ErrCaptureUnsupported{Format: capture.Format}
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(file, picture); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// captureRefusal turns whatever went wrong into words an agent reads and acts
// on. A nil reason is the deadline, which is the one worth naming a cause for:
// a minimised window renders nothing at all and reports no shutdown, so the
// wait would otherwise be unbounded.
func captureRefusal(reason error, amount, interval int) error {
	switch {
	case reason == nil:
		return mcp.Unavailable{Reason: fmt.Sprintf(
			"no frame was rendered within %s — the game may be paused, minimised, or not rendering",
			captureFloorDeadline+time.Duration(amount*interval)*captureTick)}
	case errors.Is(reason, ErrCaptureBusy{}):
		return mcp.Unavailable{Reason: "a capture is already in flight; ask again"}
	case errors.Is(reason, ErrCaptureAbandoned{}), errors.Is(reason, kernel.ErrSchedulerStopped{}),
		errors.Is(reason, context.Canceled):
		// A game exiting is the normal case, not a fault: an engine that
		// terminated while shutting down normally would be the worse answer.
		return mcp.Unavailable{Reason: "the game is shutting down"}
	case errors.Is(reason, ErrCaptureNoTarget{}):
		return mcp.Unavailable{Reason: "the game drew nothing to the screen in that frame"}
	}
	var unsupported ErrCaptureUnsupported
	if errors.As(reason, &unsupported) {
		return mcp.Unavailable{Reason: unsupported.Error()}
	}
	return mcp.Unavailable{Reason: reason.Error()}
}

// validateCapturePath checks what the agent named and reports whether the path
// carries a numbering verb. A relative path would silently resolve against the
// game's working directory, which need not be the agent's.
func validateCapturePath(path string, amount int) (numbered bool, err error) {
	if path == "" {
		return false, mcp.Unavailable{Reason: "path is required: name an absolute path ending in .png"}
	}
	if !filepath.IsAbs(path) {
		return false, mcp.Unavailable{Reason: fmt.Sprintf(
			"%s is relative; give an absolute path, because the game's working directory is not yours",
			path)}
	}
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		return false, mcp.Unavailable{Reason: fmt.Sprintf(
			"%s does not end in .png, and a capture is always a PNG", path)}
	}
	// A %d in an agent-supplied string is a small injection surface and a
	// large footgun: with no verb every still of a burst overwrites the last,
	// and with the wrong verb fmt writes %!s(MISSING) into a filename instead
	// of failing.
	verbs, offender := countCaptureVerbs(path)
	switch {
	case offender != "":
		return false, mcp.Unavailable{Reason: fmt.Sprintf(
			"%s in the path is not a numbering verb; the only one allowed is %%d, and %%04d is "+
				"what makes a directory listing sort", offender)}
	case verbs > 1:
		return false, mcp.Unavailable{Reason: fmt.Sprintf(
			"the path has %d numbering verbs; use exactly one, such as frame-%%04d.png", verbs)}
	case verbs == 0 && amount > 1:
		return false, mcp.Unavailable{Reason: "a burst needs a numbering verb in the path, or " +
			"every still overwrites the last; use something like frame-%04d.png"}
	}
	return verbs == 1, nil
}

// countCaptureVerbs counts the %d verbs in a path and reports the first verb
// that is not one. A doubled %% is an escape rather than a verb.
func countCaptureVerbs(path string) (verbs int, offender string) {
	const flags = "+-# 0123456789."
	for i := 0; i < len(path); i++ {
		if path[i] != '%' {
			continue
		}
		end := i + 1
		for end < len(path) && strings.IndexByte(flags, path[end]) >= 0 {
			end++
		}
		if end >= len(path) {
			return verbs, "%"
		}
		switch path[end] {
		case '%':
		case 'd':
			verbs++
		default:
			return verbs, path[i : end+1]
		}
		i = end
	}
	return verbs, ""
}

// validateCaptureSpan checks the burst caps. interval is what buys a long
// window, never amount.
func validateCaptureSpan(amount, interval int) error {
	if amount > maxCaptureAmount {
		return mcp.Unavailable{Reason: fmt.Sprintf(
			"amount is %d; ask for up to %d stills and space them with interval", amount, maxCaptureAmount)}
	}
	if amount*interval > maxCaptureSpan {
		return mcp.Unavailable{Reason: fmt.Sprintf(
			"amount x interval is %d ticks; one burst may span up to %d — about ten seconds at 60 Hz",
			amount*interval, maxCaptureSpan)}
	}
	return nil
}

// enginePaused asks the tick source whether it is stopped, which decides both
// whether a burst is legal and whether the capture waits for a tick that can
// never come. A game composed without time control cannot be paused, so an
// engine that does not handle the command is answering rather than failing.
func enginePaused(k kernel.Executioner) bool {
	state, err := k.ExecuteCommand[app.TimeCmd](app.TimeRequest{Action: app.TimeStatus})
	if err != nil {
		return false
	}
	return state.Paused
}
