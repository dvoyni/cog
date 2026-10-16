package gamepad

import "testing"

// A gamepad is placed by its maker where its id or browser name carries one,
// and by its name otherwise.
func TestDetectTypeOf(t *testing.T) {
	for _, c := range []struct {
		label, sdlID, name string
		want               Type
	}{
		{"xinput", "78696e70757401000000000000000000", "Xbox Controller", TypeXbox},
		{"sony by vendor", "030000004c050000e60c000000000000", "Wireless Controller", TypePlayStation},
		{"nintendo by vendor", "030000007e0500000920000000000000", "Pro Controller", TypeSwitch},
		{"microsoft by vendor", "030000005e040000130b000000000000", "Controller", TypeXbox},
		{"steam deck by vendor", "03000000de2800000512000000000000", "Steam Deck", TypeSteamDeck},
		{"other valve by name", "03000000de280000ff11000000000000", "Steam Virtual Gamepad", TypeUnknown},
		{"third-party by name", "030000000d0f0000ee00000000000000", "Horipad 4 PS4", TypePlayStation},
		{"name-built id", "050000004e696e74656e646f20537700", "Nintendo Switch Pro Controller", TypeSwitch},
		{"chrome name", "", "Wireless Controller (STANDARD GAMEPAD Vendor: 054c Product: 05c4)", TypePlayStation},
		{"firefox name", "", "057e-2009-Pro Controller", TypeSwitch},
		{"unknown", "03000000790000000600000000000000", "Generic USB Joystick", TypeUnknown},
	} {
		if got := detectTypeOf(c.sdlID, c.name); got != c.want {
			t.Errorf("%s: detectTypeOf(%q, %q) = %v, want %v", c.label, c.sdlID, c.name, got, c.want)
		}
	}
}
