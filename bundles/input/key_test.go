package input

import (
	"encoding/json"
	"testing"
)

// A Key is a JSON string wherever it appears — in a step and in the down-set —
// rather than the integer reflecting on its Go kind would infer.
func TestKey_CrossesAsAJSONString(t *testing.T) {
	encoded, err := json.Marshal(Action{Do: ActionKeyDown, Key: KeyW})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"do":"key_down","key":"w"}` {
		t.Errorf("encoded as %s", encoded)
	}

	var step Action
	if err := json.Unmarshal([]byte(`{"do":"key_up","key":"mouse_left"}`), &step); err != nil {
		t.Fatal(err)
	}
	if step.Key != KeyMouseLeft {
		t.Errorf("key = %d, want KeyMouseLeft", step.Key)
	}

	seam, err := json.Marshal(StateResponse{Down: []Key{KeyLeftControl, Key(9999)}})
	if err != nil {
		t.Fatal(err)
	}
	if string(seam) != `{"down":["left_control","#9999"],"pointer":{"x":0,"y":0}}` {
		t.Errorf("the seam encoded as %s", seam)
	}

	if err := json.Unmarshal([]byte(`{"do":"key_down","key":"ctrl"}`), &step); err == nil {
		t.Error("a key name nothing names was accepted")
	}
}
