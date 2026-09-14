package platform

// The pad becomes keys. The engine's input is W3C key codes (sim.Input, scenarios,
// goldens), so a gamepad press is translated here rather than adding a second kind of
// input to every game: a recorded appliance session then replays through the existing
// simulate tooling unchanged.
//
// This table is provisional. Clone pads permute their button order freely, and no kernel
// call reports which silkscreen letter a code belongs to, so it has to be corrected against
// the real Rii GP100 with cmd/padprobe before it can be trusted.

// evdev event types and the codes this file cares about (linux/input-event-codes.h).
const (
	evSyn = 0x00
	evKey = 0x01
	evAbs = 0x03

	synDropped = 0x03 // the kernel dropped events: what is held is no longer known

	absX     = 0x00
	absY     = 0x01
	absHat0X = 0x10
	absHat0Y = 0x11

	// Gamepads report buttons from BTN_SOUTH, joystick-style pads from BTN_TRIGGER.
	btnSouth   = 0x130
	btnTrigger = 0x120
)

// padButtons maps a button code to a W3C key code. Both bases are listed because both
// occur on pads of this kind.
var padButtons = map[uint16]string{
	btnSouth:     "Space",  // A: jump, confirm
	btnSouth + 1: "Escape", // B: back
	btnSouth + 3: "KeyF",   // X
	btnSouth + 4: "KeyR",   // Y
	0x136:        "KeyQ",   // L1
	0x137:        "KeyE",   // R1
	0x13a:        "Tab",    // Select
	0x13b:        "Enter",  // Start

	btnTrigger:     "Space",
	btnTrigger + 1: "Escape",
	btnTrigger + 2: "KeyF",
	btnTrigger + 3: "KeyR",
	btnTrigger + 4: "KeyQ",
	btnTrigger + 5: "KeyE",
	btnTrigger + 6: "Tab",
	btnTrigger + 7: "Enter",
}

// exitChord closes the window when held together: on a console with no keyboard there has
// to be a way back to the dashboard that no game can swallow.
var exitChord = [2]uint16{0x13a, 0x13b} // Select and Start

// directions are the four keys a D-pad produces, whether it arrives as a hat, as a stick
// or as buttons.
const (
	dirLeft  = "ArrowLeft"
	dirRight = "ArrowRight"
	dirUp    = "ArrowUp"
	dirDown  = "ArrowDown"
)

// padDPad maps the D-pad-as-buttons encoding some pads use.
var padDPad = map[uint16]string{
	0x220: dirUp,    // BTN_DPAD_UP
	0x221: dirDown,  // BTN_DPAD_DOWN
	0x222: dirLeft,  // BTN_DPAD_LEFT
	0x223: dirRight, // BTN_DPAD_RIGHT
}
