package platform

import "github.com/riftbane/veduta/v2/sim"

// The pad becomes the console's buttons: the D-pad, A and B go to the game as themselves,
// Start is the game's menu (sim.ButtonSelect), and Select leaves the game, as Home does. A
// gamepad has more buttons than that (X, Y, shoulders, sticks); they mean nothing on the
// console and are ignored, and so is a pad's lack of Cancel.
//
// Leaving on Select alone, rather than on a chord, is what a USB pad with no Home button
// needs: one button no game can swallow. Games therefore never see a pad's Select; their
// menu is on Start.
//
// Joystick-style pads (buttons from BTN_TRIGGER) follow the order the cheap SNES-style USB
// pads report, DragonRise 0079:0011 and 081f:e401 among them: X, A, B, Y, L, R, then
// Select and Start ninth and tenth (SDL's game controller database agrees). The numbers are
// in padmap_win32.go, which WinMM reads in the same order. The handheld's
// own buttons, wired to gpio-keys, report BTN_DPAD_*, BTN_SOUTH, BTN_EAST, BTN_START for
// its menu button, KEY_BACK and BTN_MODE.

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

	btnSelect   = 0x13a // BTN_SELECT
	btnStart    = 0x13b // BTN_START
	btnMode     = 0x13c // BTN_MODE: Home
	btnDPadUp   = 0x220 // BTN_DPAD_UP, then DOWN, LEFT and RIGHT
	keyBack     = 158   // KEY_BACK: Cancel on the handheld
	keyHomePage = 172   // KEY_HOMEPAGE: Home on the handheld
)

// padButtons maps a button code to a game button. Both bases are listed because both occur
// on pads of this kind.
var padButtons = map[uint16]sim.Button{
	btnSouth:     sim.ButtonA,
	btnSouth + 1: sim.ButtonB,
	btnStart:     sim.ButtonSelect,
	keyBack:      sim.ButtonCancel,

	btnTrigger + joyA:     sim.ButtonA,
	btnTrigger + joyB:     sim.ButtonB,
	btnTrigger + joyStart: sim.ButtonSelect,
}

// padHome closes the player on its own: Home (BTN_MODE, or KEY_HOMEPAGE) and a pad's Select
// on either kind of pad. Each is a chord of one button, so that it is forgotten after lost
// events and waited for on closing as the keyboard's are.
var padHome = [...][2]uint16{{btnMode, btnMode}, {keyHomePage, keyHomePage}, {btnSelect, btnSelect}, {btnTrigger + joySelect, btnTrigger + joySelect}}

// padDPad maps the D-pad-as-buttons encoding some pads use, and the handheld's.
var padDPad = map[uint16]sim.Button{
	btnDPadUp:     sim.ButtonUp,
	btnDPadUp + 1: sim.ButtonDown,
	btnDPadUp + 2: sim.ButtonLeft,
	btnDPadUp + 3: sim.ButtonRight,
}
