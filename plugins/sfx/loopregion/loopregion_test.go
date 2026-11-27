package loopregion_test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/loopregion"
)

// The rate and length the comments are read against: a 44.1 kHz clip of
// 48704 frames, the length of the backends' fixture.
const (
	clipRate   = 44100
	clipFrames = 48704
)

// A Vorbis comment's field name is case-insensitive by the format's own
// definition, and LOOPSTART alone is an intro running into a loop that
// carries on to the end of the clip. A repeated tag is read by its first.
func TestLoopTagsAreCaseInsensitiveAndLoopstartAloneRunsToTheClipsEnd(t *testing.T) {
	wantRegions(t, []regionCase{
		{"start alone runs to the end", []string{"LOOPSTART=11025"}, 11025, clipFrames},
		{"in mixed case", []string{"loopStart=11025", "looplength=22050"}, 11025, 33075},
		{"a repeated tag is read by its first", []string{"LOOPSTART=11025", "LOOPSTART=0", "LOOPEND=33075"}, 11025, 33075},
		{"a value padded with spaces", []string{"LOOPSTART= 11025 ", "LOOPEND=33075"}, 11025, 33075},
		{"among other comments", []string{"TITLE=Pleasant Moments", "LOOPSTART=11025", "NOEQUALSIGN"}, 11025, clipFrames},
	})
}

// LOOPEND is where a clip without LOOPLENGTH says its loop ends, and
// LOOPLENGTH wins where a clip carries both.
func TestLoopendIsUsedWhenLooplengthIsAbsentAndLooplengthWinsWhenItIsNot(t *testing.T) {
	wantRegions(t, []regionCase{
		{"start and end", []string{"LOOPSTART=11025", "LOOPEND=33075"}, 11025, 33075},
		{"start and length", []string{"LOOPSTART=11025", "LOOPLENGTH=22050"}, 11025, 33075},
		{"length wins over end", []string{"LOOPSTART=11025", "LOOPEND=44100", "LOOPLENGTH=22050"}, 11025, 33075},
	})
}

// A malformed region is dropped whole and never clamped, and answered as an
// sfx.ErrLoopRegionIgnored with no path, naming the span the tags asked for
// against the clip's frames, or the parse failure.
func TestAMalformedLoopRegionIsDroppedWholeAndNeverClamped(t *testing.T) {
	cases := []struct {
		name     string
		comments []string
		want     sfx.ErrLoopRegionIgnored
		// unparsed says the error carries the parse failure of a tag.
		unparsed bool
	}{
		{"a start before the beginning", []string{"LOOPSTART=-1", "LOOPLENGTH=22050"},
			sfx.ErrLoopRegionIgnored{Start: -1, End: 22049, Frames: clipFrames}, false},
		{"an end at the start", []string{"LOOPSTART=11025", "LOOPEND=11025"},
			sfx.ErrLoopRegionIgnored{Start: 11025, End: 11025, Frames: clipFrames}, false},
		{"an end before the start", []string{"LOOPSTART=22050", "LOOPEND=11025"},
			sfx.ErrLoopRegionIgnored{Start: 22050, End: 11025, Frames: clipFrames}, false},
		{"an end past the clip", []string{"LOOPSTART=11025", "LOOPEND=48705"},
			sfx.ErrLoopRegionIgnored{Start: 11025, End: 48705, Frames: clipFrames}, false},
		{"a length past the clip", []string{"LOOPSTART=11025", "LOOPLENGTH=48704"},
			sfx.ErrLoopRegionIgnored{Start: 11025, End: 59729, Frames: clipFrames}, false},
		{"an end alone, at the start it implies", []string{"LOOPEND=0"},
			sfx.ErrLoopRegionIgnored{Start: 0, End: 0, Frames: clipFrames}, false},
		{"a start that is not a number", []string{"LOOPSTART=bar", "LOOPLENGTH=22050"},
			sfx.ErrLoopRegionIgnored{Frames: clipFrames}, true},
		{"a length that is not a number", []string{"LOOPSTART=11025", "LOOPLENGTH=3.5"},
			sfx.ErrLoopRegionIgnored{Frames: clipFrames}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			region, err := loopregion.Read(c.comments, clipRate, clipFrames)
			if got, kept := region.Get(); kept {
				t.Fatalf("a malformed region was kept as %+v, and it must be dropped whole", got)
			}
			var ignored sfx.ErrLoopRegionIgnored
			if !errors.As(err, &ignored) {
				t.Fatalf("the drop answered %v, want an sfx.ErrLoopRegionIgnored", err)
			}
			if ignored.Path != "" {
				t.Fatalf("the error names the clip %q, and only sfx knows its path", ignored.Path)
			}
			if ignored.Start != c.want.Start || ignored.End != c.want.End || ignored.Frames != c.want.Frames {
				t.Fatalf("the error names [%d, %d) against %d frames, want [%d, %d) against %d",
					ignored.Start, ignored.End, ignored.Frames, c.want.Start, c.want.End, c.want.Frames)
			}
			var parse *strconv.NumError
			if unparsed := errors.As(ignored.Err, &parse); unparsed != c.unparsed {
				t.Fatalf("the error carries %v as its parse failure, want one: %v", ignored.Err, c.unparsed)
			}
		})
	}
}

// Comments that name no loop tag declare no region, which means the whole
// clip, and say nothing about it.
func TestAnUntaggedClipReportsNoRegionAndNothingIsDropped(t *testing.T) {
	for name, comments := range map[string][]string{
		"no comments":        nil,
		"other comments":     {"TITLE=Pleasant Moments", "ARTIST=unknown"},
		"a tag's name alone": {"LOOPSTART"},
	} {
		t.Run(name, func(t *testing.T) {
			region, err := loopregion.Read(comments, clipRate, clipFrames)
			if region.Present() || err != nil {
				t.Fatalf("comments %v answer %+v and %v, want no region and no error", comments, region, err)
			}
		})
	}
}

// regionCase is comments that declare the region [start, end), in frames.
type regionCase struct {
	name       string
	comments   []string
	start, end int64
}

// wantRegions fails the test unless each case's comments answer its region,
// in seconds, and no error.
func wantRegions(t *testing.T, cases []regionCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			region, err := loopregion.Read(c.comments, clipRate, clipFrames)
			if err != nil {
				t.Fatalf("a well-formed region answered %v", err)
			}
			got, ok := region.Get()
			if !ok {
				t.Fatalf("comments %v answer no loop region", c.comments)
			}
			want := sfx.LoopRegion{Start: float32(float64(c.start) / clipRate), End: float32(float64(c.end) / clipRate)}
			if got != want {
				t.Fatalf("the region is %+v, want %+v", got, want)
			}
		})
	}
}
