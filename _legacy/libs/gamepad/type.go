package gamepad

import (
	"encoding/hex"
	"os"
	"strconv"
	"strings"

	"github.com/dvoyni/cog/libs/gamepad/internal/gamepad"
)

// Type is the family a gamepad belongs to, which decides the labels on its
// buttons.
type Type int

const (
	TypeUnknown Type = iota
	TypeXbox
	TypePlayStation
	TypeSwitch
	TypeSteamDeck
)

// USB vendor and product ids that place a gamepad by its maker.
const (
	vendorMicrosoft     = 0x045e
	vendorSony          = 0x054c
	vendorNintendo      = 0x057e
	vendorValve         = 0x28de
	productValveDeck    = 0x1205
	xinputSDLIDPrefix   = "78696e707574" // "xinput"
	steamDeckEnvVar     = "SteamDeck"
	steamDeckEnvVarTrue = "1"
)

// typeNameParts are the parts of a gamepad's name that place it, lower case,
// in the order they are tried, so a "Steam Deck" is not taken for something
// else. They place what the vendor id cannot: a third-party pad made for one
// family, and a pad whose id carries no vendor.
var typeNameParts = []struct {
	part string
	kind Type
}{
	{"steam deck", TypeSteamDeck},
	{"playstation", TypePlayStation},
	{"dualshock", TypePlayStation},
	{"dualsense", TypePlayStation},
	{"sony", TypePlayStation},
	{"ps3", TypePlayStation},
	{"ps4", TypePlayStation},
	{"ps5", TypePlayStation},
	{"nintendo", TypeSwitch},
	{"switch", TypeSwitch},
	{"joy-con", TypeSwitch},
	{"xbox", TypeXbox},
	{"x-box", TypeXbox},
	{"xinput", TypeXbox},
}

// DetectType places the gamepad id in its family: by the USB vendor in its SDL
// id where it carries one, or, for a browser's pad, in its name; then by its
// name. On a Steam Deck, which Steam tells its games through the SteamDeck
// environment variable, a pad placed as Xbox or not at all is taken for the
// Deck's own, since Steam hands it to games as a virtual Xbox pad. TypeUnknown
// when the gamepad is gone or cannot be placed.
func DetectType(id ID) Type {
	g := gamepad.Get(gamepad.ID(id))
	if g == nil {
		return TypeUnknown
	}
	kind := detectTypeOf(g.SDLID(), g.Name())
	if (kind == TypeUnknown || kind == TypeXbox) && os.Getenv(steamDeckEnvVar) == steamDeckEnvVarTrue {
		return TypeSteamDeck
	}
	return kind
}

// detectTypeOf places a gamepad by its SDL id and its name.
func detectTypeOf(sdlID, name string) Type {
	if strings.HasPrefix(sdlID, xinputSDLIDPrefix) {
		return TypeXbox
	}
	name = strings.ToLower(name)
	vendor, product, found := parseSDLIDVendor(sdlID)
	if !found {
		vendor, product, found = parseNameVendor(name)
	}
	if found {
		if kind := detectTypeOfVendor(vendor, product); kind != TypeUnknown {
			return kind
		}
	}
	for _, known := range typeNameParts {
		if strings.Contains(name, known.part) {
			return known.kind
		}
	}
	return TypeUnknown
}

// detectTypeOfVendor places a gamepad by its maker, TypeUnknown for a maker
// that does not tell the family.
func detectTypeOfVendor(vendor, product uint16) Type {
	switch vendor {
	case vendorMicrosoft:
		return TypeXbox
	case vendorSony:
		return TypePlayStation
	case vendorNintendo:
		return TypeSwitch
	case vendorValve:
		if product == productValveDeck {
			return TypeSteamDeck
		}
	}
	return TypeUnknown
}

// parseSDLIDVendor reads the vendor and product from an SDL id: 16 bytes in
// hex, the vendor little-endian at bytes 4-5 and the product at 8-9, the bytes
// after each zero. An id built from the device name instead has no zeros
// there, and found is false.
func parseSDLIDVendor(sdlID string) (vendor, product uint16, found bool) {
	b, err := hex.DecodeString(sdlID)
	if err != nil || len(b) != 16 || b[6] != 0 || b[7] != 0 || b[10] != 0 || b[11] != 0 {
		return 0, 0, false
	}
	vendor = uint16(b[4]) | uint16(b[5])<<8
	product = uint16(b[8]) | uint16(b[9])<<8
	return vendor, product, vendor != 0
}

// parseNameVendor reads the vendor and product from a browser's gamepad name,
// lower case: Chrome ends it with "(... vendor: 054c product: 09cc)", Firefox
// starts it with "054c-09cc-".
func parseNameVendor(name string) (vendor, product uint16, found bool) {
	if at := strings.Index(name, "vendor: "); at >= 0 {
		rest := name[at+len("vendor: "):]
		vendor, okVendor := parseHex16(rest)
		productAt := strings.Index(rest, "product: ")
		if okVendor && productAt >= 0 {
			product, okProduct := parseHex16(rest[productAt+len("product: "):])
			return vendor, product, okProduct
		}
		return vendor, 0, okVendor
	}
	if len(name) >= 10 && name[4] == '-' && name[9] == '-' {
		vendor, okVendor := parseHex16(name)
		product, okProduct := parseHex16(name[5:])
		return vendor, product, okVendor && okProduct
	}
	return 0, 0, false
}

// parseHex16 reads four hex digits at the start of s.
func parseHex16(s string) (uint16, bool) {
	if len(s) < 4 {
		return 0, false
	}
	v, err := strconv.ParseUint(s[:4], 16, 16)
	return uint16(v), err == nil
}
