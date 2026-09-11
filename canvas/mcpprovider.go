package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/mcp"
)

// canvas offers its capability itself rather than through a separate plugin,
// per the rule that every package hosts its own provider: the snapshot is
// taken from a subscriber on canvas's own queue, and this is where that queue
// is.
var _ mcp.Provider = (*Plugin)(nil)

// drawsName is the capability rendered as the tool canvas_draws.
const drawsName = "draws"

// drawsDeadline is the snapshot's own wait, below the broker's thirty seconds
// and the client's five minutes so the specific message wins the race against
// both generic ones. Two seconds is a hundred and twenty ticks at 60 Hz:
// anything slower is not slow, it is not ticking.
const drawsDeadline = 2 * time.Second

// drawsDescription is prompt text, and it is reproduced in
// canvas/docs/specs/mcp.md so it is reviewed as prompt text rather than buried
// as a string literal.
const drawsDescription = "Everything drawn into the 2D canvas during one tick, in the order it " +
	"was recorded: sprites, text, shapes and textures, with their layers, transforms, materials " +
	"and resolved parameters. This includes what the UI drew, since UI elements record into the " +
	"same queue — use it when something should be on screen and is not, to find out whether it " +
	"was ever recorded at all, and on which layer.\n\n" +
	"Blocks until the next tick has been recorded, so it reflects anything you did before " +
	"calling it. Each op carries its index in record order; that index is stable under filtering " +
	"and is what you pass back. Triangle vertices are summarised as a count and a bounding box — " +
	"pass an op's index in `vertices` to get the full list for that op. Filter by " +
	"`fromLayer`/`toLayer` and `kinds` to cut a busy frame down, and pass `path` to write the " +
	"JSON to a file instead of returning it inline.\n\n" +
	"Every response reports the three coordinate spaces — image pixels, window units and the " +
	"logical viewport — because canvas coordinates are in a layer's own world window, which is " +
	"also reported per layer.\n\n" +
	"While the game is paused this performs one step to have something to record, and says so in " +
	"the response. Arm it together with `ui_layout` and `gfx_frame` to describe one moment: they " +
	"share that single step. Take `gfx_capture` last."

// DrawsRequest asks what one tick recorded into the canvas queue.
type DrawsRequest struct {
	// Path is optional, per the family's delivery contract: omit it and the
	// JSON comes back inline, supply it and a greppable file is written and the
	// path returned. A large dump becomes a file either way - an oversized text
	// result is spilled by the client under a name nobody chose - so the only
	// question is whether cog controls it.
	Path string `json:"path,omitempty" jsonschema:"absolute path ending in .json; omit to get the JSON inline"`
	// FromLayer and ToLayer bound the layers reported, inclusive.
	FromLayer *int `json:"fromLayer,omitempty" jsonschema:"lowest layer to include; omit for no lower bound"`
	ToLayer   *int `json:"toLayer,omitempty" jsonschema:"highest layer to include; omit for no upper bound"`
	// Kinds and Vertices are the other two filter axes. They travel with the
	// arm, because the filter is what bounds the work done inside the tick.
	Kinds    []string `json:"kinds,omitempty" jsonschema:"keep only these kinds: sprite, text, triangles"`
	Vertices []int    `json:"vertices,omitempty" jsonschema:"record indices of triangle ops whose vertices to return in full"`
}

// DrawsResponse is one tick's recorded canvas operations, the three coordinate
// sizes they are to be read against, and whether producing them cost a step.
//
// It is flat: DrawsView and gfx.SnapshotView are embedded rather than nested,
// so an agent reads one object rather than reaching through two.
type DrawsResponse struct {
	// Path is the file the JSON was written to, when one was asked for. The
	// file holds the whole document; what comes back inline then carries the
	// counts, the layers and the viewport but not the op array, so the reply
	// still says what the frame was without repeating it. The layers stay:
	// they are one entry per layer, and they are the coordinate frame the file
	// is to be read in.
	Path string `json:"path,omitempty"`
	DrawsView
	gfx.SnapshotView
}

// Capabilities reports what canvas offers an agent: what one tick actually
// recorded.
//
// It is Func rather than Command because a snapshot arms a flag and then waits
// for the engine, which cannot be one dispatch. It is ReadOnly, which in cog's
// reading means the capability does not change the game - with the one
// asterisk that under pause it costs a step, which its description states
// rather than its annotation.
func (p *Plugin) Capabilities() []mcp.Capability {
	return []mcp.Capability{
		mcp.Func(drawsName, drawsDescription, drawsSnapshot, mcp.ReadOnly()),
	}
}

// drawsSnapshot is the canvas_draws body: validate, arm, step if the engine is
// paused, wait, and do the marshalling and the disk write here. The engine's
// own goroutine builds the view and hands it over; nothing else.
//
// It is a package function rather than a method to keep the capability-body
// rule visible at the call site: the plugin is one pointer away and the body
// still reaches canvas only by dispatch.
func drawsSnapshot(k kernel.Executioner, request DrawsRequest) (DrawsResponse, error) {
	// Every check happens before anything is armed, so a typo costs
	// microseconds rather than a tick and an empty array an agent reads as a
	// game that drew nothing.
	armRequest, err := validateDrawsRequest(request)
	if err != nil {
		return DrawsResponse{}, err
	}
	if request.Path != "" {
		if err := os.MkdirAll(filepath.Dir(request.Path), 0o755); err != nil {
			return DrawsResponse{}, mcp.Unavailable{Reason: fmt.Sprintf(
				"the directory for %s could not be created: %v", request.Path, err)}
		}
	}
	paused := app.Paused(k)

	armed, err := k.ExecuteCommand[ArmDrawsCmd](armRequest)
	if err != nil {
		return DrawsResponse{}, drawsRefusal(err)
	}
	response := DrawsResponse{SnapshotView: gfx.SnapshotViewOf(armed.Viewport)}
	// The arm is placed first so that the tick the step produces is one that
	// began after it. Joining a step another arm already raised is what makes
	// three snapshots armed together describe one tick instead of three.
	if paused {
		if response.Stepped, response.Joined, err = stepForSnapshot(k); err != nil {
			return DrawsResponse{}, err
		}
	}

	deadline := time.NewTimer(drawsDeadline)
	defer deadline.Stop()
	select {
	case snapshot := <-armed.Done:
		if snapshot.Err != nil {
			return DrawsResponse{}, drawsRefusal(snapshot.Err)
		}
		response.DrawsView = snapshot.Draws
	case <-deadline.C:
		return DrawsResponse{}, drawsRefusal(nil)
	case <-k.Context().Done():
		return DrawsResponse{}, drawsRefusal(k.Context().Err())
	}

	if request.Path != "" {
		response.Path = request.Path
		if err := writeSnapshotJSON(request.Path, response); err != nil {
			return DrawsResponse{}, mcp.Unavailable{Reason: fmt.Sprintf(
				"the snapshot could not be written to %s: %v", request.Path, err)}
		}
		response.Ops = nil
	}
	return response, nil
}

// stepForSnapshot runs the one tick a paused engine owes a snapshot, or joins
// the one another arm already raised. Refusing instead would make a snapshot
// unreachable under pause, since a blocking arm cannot ask the agent to step
// for it; waiting instead would be a guaranteed deadline expiry. The canvas
// queue is *empty* between ticks rather than stale, so producing a snapshot
// without running a tick is not a thing that exists.
func stepForSnapshot(k kernel.Executioner) (stepped, joined bool, err error) {
	ctx, cancel := context.WithTimeout(k.Context(), drawsDeadline)
	defer cancel()
	answer, err := k.WithContext(ctx).ExecuteCommand[app.TimeCmd](app.TimeRequest{
		Action: app.TimeStep, Steps: 1, Join: true,
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return false, false, mcp.Unavailable{Reason: fmt.Sprintf(
				"the paused game published no tick within %s — the window may be minimised or "+
					"the game may have stopped drawing; the step will run when it draws again",
				drawsDeadline)}
		}
		return false, false, drawsRefusal(err)
	}
	return answer.Stepped > 0, answer.Joined, nil
}

// writeSnapshotJSON puts the whole document on disk, indented because the
// point of a file is that a person or a grep can read it. An existing file is
// overwritten without complaint: re-writing the same name is the
// iterate-and-look loop.
func writeSnapshotJSON(path string, response DrawsResponse) error {
	document, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, document, 0o644)
}

// validateDrawsRequest checks what the agent named and turns it into the arm.
// The kinds are resolved to their enum here rather than in the tick, so a typo
// is words the agent can act on instead of an empty array, and the in-tick
// filter compares integers.
func validateDrawsRequest(request DrawsRequest) (ArmDrawsRequest, error) {
	if err := validateSnapshotPath(request.Path); err != nil {
		return ArmDrawsRequest{}, err
	}
	if request.FromLayer != nil && request.ToLayer != nil && *request.FromLayer > *request.ToLayer {
		return ArmDrawsRequest{}, mcp.Unavailable{Reason: fmt.Sprintf(
			"fromLayer %d is above toLayer %d, which keeps no layer at all",
			*request.FromLayer, *request.ToLayer)}
	}
	arm := ArmDrawsRequest{
		FromLayer: request.FromLayer, ToLayer: request.ToLayer, Vertices: request.Vertices,
	}
	for _, name := range request.Kinds {
		kind, ok := opKindFor(name)
		if !ok {
			return ArmDrawsRequest{}, mcp.Unavailable{Reason: ErrDrawsUnknownKind{Kind: name}.Error()}
		}
		arm.Kinds = append(arm.Kinds, kind)
	}
	return arm, nil
}

// validateSnapshotPath checks what the agent named. The path is optional,
// unlike a capture's, because a structured dump can come back inline.
func validateSnapshotPath(path string) error {
	if path == "" {
		return nil
	}
	if !filepath.IsAbs(path) {
		return mcp.Unavailable{Reason: fmt.Sprintf(
			"%s is relative; give an absolute path, because the game's working directory is not yours",
			path)}
	}
	if !strings.EqualFold(filepath.Ext(path), ".json") {
		return mcp.Unavailable{Reason: fmt.Sprintf(
			"%s does not end in .json, and a snapshot is always JSON", path)}
	}
	return nil
}

// drawsRefusal turns whatever went wrong into words an agent reads and acts
// on. A nil reason is the deadline, which is the one worth naming a cause for:
// a paused engine nothing steps, or a window that has stopped updating,
// produces no tick at all and reports nothing about it.
func drawsRefusal(reason error) error {
	switch {
	case reason == nil:
		return mcp.Unavailable{Reason: fmt.Sprintf(
			"no tick was recorded within %s — the game may be paused with nothing stepping it, "+
				"minimised, or not updating", drawsDeadline)}
	case errors.Is(reason, ErrDrawsBusy{}):
		return mcp.Unavailable{Reason: "a draw snapshot is already in flight; ask again. A " +
			"capture and the other snapshots may run alongside it, and arming them together is " +
			"how they describe one tick."}
	case errors.Is(reason, ErrDrawsAbandoned{}), errors.Is(reason, kernel.ErrSchedulerStopped{}),
		errors.Is(reason, context.Canceled):
		// A game exiting is the normal case, not a fault.
		return mcp.Unavailable{Reason: "the game is shutting down"}
	}
	return mcp.Unavailable{Reason: reason.Error()}
}
