package platform_test

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/platform"
)

// expectLabels checks the input state's label of each key against labels.
func expectLabels(t *testing.T, h *harness, labels map[platform.Key]rune) {
	t.Helper()
	for key, label := range labels {
		if got := h.input.Label(key); got != label {
			t.Errorf("the label of %v is %q, want %q", key, got, label)
		}
	}
}

func TestKeyLabelsFollowTheLayout(t *testing.T) {
	f := newFake()
	f.layout = map[platform.Key]rune{platform.KeyQ: 'q', platform.KeyA: 'a', platform.Key2: '2'}
	h := spawn(t, f)
	window := openFocused(t, h)

	expectLabels(t, h, map[platform.Key]rune{
		platform.KeyQ:         'q',
		platform.KeyA:         'a',
		platform.Key2:         '2',
		platform.KeyF1:        0,
		platform.KeyMouseLeft: 0,
		platform.KeyUnknown:   0,
	})
	if got := countKey(f.labelled, platform.KeyQ); got != 1 {
		t.Errorf("two runs asked the label of KeyQ %d times, want once, at start", got)
	}
	for _, key := range f.labelled {
		if key.IsMouseButton() || key == platform.KeyUnknown {
			t.Errorf("the input source was asked the label of %v", key)
		}
	}

	f.layout = map[platform.Key]rune{platform.KeyQ: 'a', platform.KeyA: 'q', platform.Key2: 'é'}
	f.labelled = nil
	h.fake.inputChanges = append(h.fake.inputChanges,
		keyChange(window, platform.KeyQ, true, false, 0),
		platform.InputChange{Kind: platform.InputChangeLayout},
		keyChange(window, platform.KeyQ, false, false, 0),
		keyChange(window, platform.KeyA, true, false, 0),
		keyChange(window, platform.KeyMouseLeft, true, false, 0),
	)
	h.pump()
	want := []any{
		platform.KeyEvent{Window: window, Key: platform.KeyQ, Pressed: true, Label: 'q'},
		platform.LayoutChangedEvent{},
		platform.KeyEvent{Window: window, Key: platform.KeyQ, Label: 'a'},
		platform.KeyEvent{Window: window, Key: platform.KeyA, Pressed: true, Label: 'q'},
		platform.KeyEvent{Window: window, Key: platform.KeyMouseLeft, Pressed: true},
	}
	if got := h.takePublished(); !slices.Equal(got, want) {
		t.Fatalf("the layout change published\n%v\nwant\n%v", got, want)
	}
	expectLabels(t, h, map[platform.Key]rune{platform.KeyQ: 'a', platform.KeyA: 'q', platform.Key2: 'é'})
	if got := countKey(f.labelled, platform.KeyQ); got != 1 {
		t.Errorf("the layout change asked the label of KeyQ %d times, want once", got)
	}

	f.labelled = nil
	h.fake.inputChanges = append(h.fake.inputChanges, platform.InputChange{Kind: platform.InputChangeFocus, Window: window})
	h.pump()
	want = []any{
		platform.KeyEvent{Window: window, Key: platform.KeyA, Label: 'q'},
		platform.KeyEvent{Window: window, Key: platform.KeyMouseLeft},
		platform.FocusEvent{Window: window},
	}
	if got := h.takePublished(); !slices.Equal(got, want) {
		t.Fatalf("losing focus published\n%v\nwant\n%v", got, want)
	}
	if len(f.labelled) != 0 {
		t.Errorf("a run without a layout change asked the labels of %v", f.labelled)
	}
}

func TestWithoutAnInputSourceEveryLabelIsZero(t *testing.T) {
	f := newFake()
	f.installInputSource = false
	f.layout = map[platform.Key]rune{platform.KeyQ: 'q'}
	h := spawn(t, f)
	h.pump()
	expectLabels(t, h, map[platform.Key]rune{platform.KeyQ: 0})
}

// countKey answers how many times key is in keys.
func countKey(keys []platform.Key, key platform.Key) int {
	count := 0
	for _, each := range keys {
		if each == key {
			count++
		}
	}
	return count
}
