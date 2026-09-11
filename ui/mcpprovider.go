package ui

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

// ui offers its capability itself rather than through a separate plugin, per
// the rule that every package hosts its own provider: the snapshot is taken
// from a subscriber on ui's own processor, and this is where that processor
// is.
//
// It is the only agent-facing surface ui has. There is no overlay and nothing
// here writes content into the game's frame; see docs/specs/mcp.md.
var _ mcp.Provider = (*Plugin)(nil)

// layoutCapabilityName is the capability rendered as the tool ui_layout. It
// is spelled out rather than named layoutName because that name already
// belongs to the Layout enum's own table in snapshot.go.
const layoutCapabilityName = "layout"

// layoutDeadline is the snapshot's own wait, below the broker's thirty seconds
// and the client's five minutes so the specific message wins the race against
// both generic ones. Two seconds is a hundred and twenty ticks at 60 Hz:
// anything slower is not slow, it is not ticking.
const layoutDeadline = 2 * time.Second

// layoutDescription is prompt text, and it is reproduced in
// ui/docs/specs/mcp.md so it is reviewed as prompt text rather than buried as
// a string literal.
const layoutDescription = "The UI element tree for one tick, flattened, with what layout actually " +
	"resolved each element to: its rect, content rect, clip, layer and draw order, whether it is " +
	"active, which visual it uses, and its id and user data when it has them. Elements that " +
	"declared a size or constraint also report what was declared, so you can see a stretch or a " +
	"minimum that did not take effect — which is what the source cannot show you.\n\n" +
	"Use it when something is in the wrong place, the wrong size, or invisible. Each element " +
	"carries its index and its parent's index; the tree is depth-first, so a subtree is a " +
	"contiguous range of indices. Indices are stable under filtering and are what you pass back " +
	"in `subtree`. Note that ids are optional and are matched by prefix elsewhere in the engine — " +
	"the index is the reliable address, not the id.\n\n" +
	"Rects are in viewport units. `input_send` takes window units, and a capture is in image " +
	"pixels; every response reports all three sizes, so a rect becomes a clickable point as " +
	"`window = viewport × windowWidth / width`.\n\n" +
	"Blocks until the next tick has been processed, so it reflects anything you did before " +
	"calling it. Pass `path` to write the JSON to a file instead of returning it inline. While " +
	"the game is paused this performs one step, and says so in the response; arm it together " +
	"with `canvas_draws` and `gfx_frame` to describe one moment, and take `gfx_capture` last."

// LayoutRequest asks what one tick's layout resolved the element tree to.
type LayoutRequest struct {
	// Path is optional, per the family's delivery contract: omit it and the
	// JSON comes back inline, supply it and a greppable file is written and
	// the path returned. A small ui frame with a dozen elements is better
	// inline, which is why this is the capability where the default is the
	// useful one.
	Path string `json:"path,omitempty" jsonschema:"absolute path ending in .json; omit to get the JSON inline"`
	// Subtree and MaxDepth are the two axes a tree wants, and they travel with
	// the arm because the filter is what bounds the work done inside the tick.
	Subtree  *int `json:"subtree,omitempty" jsonschema:"index of the element to report, with its descendants; omit for the whole tree"`
	MaxDepth *int `json:"maxDepth,omitempty" jsonschema:"how many levels below the reported root to keep; 0 is that element alone"`
}

// LayoutResponse is one tick's resolved element tree, the three coordinate
// sizes it is to be read against, and whether producing it cost a step.
//
// It is flat: LayoutView and gfx.SnapshotView are embedded rather than nested,
// so an agent reads one object rather than reaching through two.
type LayoutResponse struct {
	// Path is the file the JSON was written to, when one was asked for. The
	// file holds the whole document; what comes back inline then carries the
	// counts, the filter and the viewport but not the element array.
	//
	// Only the elements are dropped. canvas keeps its layers inline because
	// they are the coordinate frame its ops are measured in; ui's equivalent
	// is the viewport block, which is in every response already, and there is
	// no second small array to keep - the tree is the payload.
	Path string `json:"path,omitempty"`
	LayoutView
	gfx.SnapshotView
}

// Capabilities reports what ui offers an agent: what layout resolved, beside
// what was declared.
//
// It is Func rather than Command because a snapshot arms a flag and then waits
// for the engine, which cannot be one dispatch. It is ReadOnly, which in cog's
// reading means the capability does not change the game - with the one
// asterisk that under pause it costs a step, which its description states
// rather than its annotation.
func (p *Plugin) Capabilities() []mcp.Capability {
	return []mcp.Capability{
		mcp.Func(layoutCapabilityName, layoutDescription, layoutSnapshot, mcp.ReadOnly()),
	}
}

// layoutSnapshot is the ui_layout body: validate, arm, step if the engine is
// paused, wait, and do the marshalling and the disk write here. The engine's
// own goroutine builds the view and hands it over; nothing else.
//
// It is a package function rather than a method to keep the capability-body
// rule visible at the call site: the plugin is one pointer away and the body
// still reaches ui only by dispatch.
func layoutSnapshot(k kernel.Executioner, request LayoutRequest) (LayoutResponse, error) {
	// Every check that can be made without the engine happens before anything
	// is armed, so a typo costs microseconds rather than a tick. A subtree
	// index is not one of them: the tree is declared afresh every tick and
	// nothing outside it knows how large it is, so that refusal is raised in
	// the tick and travels the delivery channel.
	armRequest, err := validateLayoutRequest(request)
	if err != nil {
		return LayoutResponse{}, err
	}
	if request.Path != "" {
		if err := os.MkdirAll(filepath.Dir(request.Path), 0o755); err != nil {
			return LayoutResponse{}, mcp.Unavailable{Reason: fmt.Sprintf(
				"the directory for %s could not be created: %v", request.Path, err)}
		}
	}
	paused := app.Paused(k)

	armed, err := k.ExecuteCommand[ArmLayoutCmd](armRequest)
	if err != nil {
		return LayoutResponse{}, layoutRefusal(err)
	}
	response := LayoutResponse{SnapshotView: gfx.SnapshotViewOf(armed.Viewport)}
	// The arm is placed first so that the tick the step produces is one that
	// began after it. Joining a step another arm already raised is what makes
	// three snapshots armed together describe one tick instead of three.
	if paused {
		if response.Stepped, response.Joined, err = stepForSnapshot(k); err != nil {
			return LayoutResponse{}, err
		}
	}

	deadline := time.NewTimer(layoutDeadline)
	defer deadline.Stop()
	select {
	case snapshot := <-armed.Done:
		if snapshot.Err != nil {
			return LayoutResponse{}, layoutRefusal(snapshot.Err)
		}
		response.LayoutView = snapshot.Layout
	case <-deadline.C:
		return LayoutResponse{}, layoutRefusal(nil)
	case <-k.Context().Done():
		return LayoutResponse{}, layoutRefusal(k.Context().Err())
	}

	if request.Path != "" {
		response.Path = request.Path
		if err := writeSnapshotJSON(request.Path, response); err != nil {
			return LayoutResponse{}, mcp.Unavailable{Reason: fmt.Sprintf(
				"the snapshot could not be written to %s: %v", request.Path, err)}
		}
		response.Elements = nil
	}
	return response, nil
}

// stepForSnapshot runs the one tick a paused engine owes a snapshot, or joins
// the one another arm already raised. Refusing instead would make a snapshot
// unreachable under pause, since a blocking arm cannot ask the agent to step
// for it; waiting instead would be a guaranteed deadline expiry. The ui frame
// is *empty* between ticks rather than stale - processUpdate ends in
// defer frame.clear() - so producing a snapshot without running a tick is not
// a thing that exists.
func stepForSnapshot(k kernel.Executioner) (stepped, joined bool, err error) {
	ctx, cancel := context.WithTimeout(k.Context(), layoutDeadline)
	defer cancel()
	answer, err := k.WithContext(ctx).ExecuteCommand[app.TimeCmd](app.TimeRequest{
		Action: app.TimeStep, Steps: 1, Join: true,
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return false, false, mcp.Unavailable{Reason: fmt.Sprintf(
				"the paused game published no tick within %s — the window may be minimised or "+
					"the game may have stopped drawing; the step will run when it draws again",
				layoutDeadline)}
		}
		return false, false, layoutRefusal(err)
	}
	return answer.Stepped > 0, answer.Joined, nil
}

// writeSnapshotJSON puts the whole document on disk, indented because the
// point of a file is that a person or a grep can read it. An existing file is
// overwritten without complaint: re-writing the same name is the
// iterate-and-look loop.
func writeSnapshotJSON(path string, response LayoutResponse) error {
	document, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, document, 0o644)
}

// validateLayoutRequest checks what the agent named and turns it into the arm.
func validateLayoutRequest(request LayoutRequest) (ArmLayoutRequest, error) {
	if err := validateSnapshotPath(request.Path); err != nil {
		return ArmLayoutRequest{}, err
	}
	if request.Subtree != nil && *request.Subtree < 0 {
		return ArmLayoutRequest{}, mcp.Unavailable{Reason: fmt.Sprintf(
			"subtree %d is negative; an element index is its position in the tree, counted from 0",
			*request.Subtree)}
	}
	if request.MaxDepth != nil && *request.MaxDepth < 0 {
		return ArmLayoutRequest{}, mcp.Unavailable{Reason: fmt.Sprintf(
			"maxDepth %d is negative, which keeps no element at all; 0 keeps the root alone",
			*request.MaxDepth)}
	}
	return ArmLayoutRequest{Subtree: request.Subtree, MaxDepth: request.MaxDepth}, nil
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

// layoutRefusal turns whatever went wrong into words an agent reads and acts
// on. A nil reason is the deadline, which is the one worth naming a cause for:
// a paused engine nothing steps, or a window that has stopped updating,
// produces no tick at all and reports nothing about it.
func layoutRefusal(reason error) error {
	var missing ErrLayoutNoSuchElement
	switch {
	case reason == nil:
		return mcp.Unavailable{Reason: fmt.Sprintf(
			"no tick was processed within %s — the game may be paused with nothing stepping it, "+
				"minimised, or not updating", layoutDeadline)}
	case errors.Is(reason, ErrLayoutBusy{}):
		return mcp.Unavailable{Reason: "a layout snapshot is already in flight; ask again. A " +
			"capture and the other snapshots may run alongside it, and arming them together is " +
			"how they describe one tick."}
	case errors.As(reason, &missing):
		return mcp.Unavailable{Reason: missing.Error() +
			". The tree is declared afresh every tick, so an index from an older snapshot may " +
			"name nothing; take one without a subtree filter to see what is there."}
	case errors.Is(reason, ErrLayoutAbandoned{}), errors.Is(reason, kernel.ErrSchedulerStopped{}),
		errors.Is(reason, context.Canceled):
		// A game exiting is the normal case, not a fault.
		return mcp.Unavailable{Reason: "the game is shutting down"}
	}
	return mcp.Unavailable{Reason: reason.Error()}
}
