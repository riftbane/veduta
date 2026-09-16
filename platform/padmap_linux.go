package platform

import "github.com/riftbane/veduta/sim"

// The pad becomes the console's buttons: the D-pad, A, B, Select and Cancel go to the game
// as sim.Button, and Home closes the player. A gamepad has more buttons than that (X, Y,
// shoulders, sticks); they mean nothing on the console and are ignored.
//
// This table is provisional. Clone pads permute their button order freely, and no kernel
// call reports which silkscreen letter a code belongs to, so it has to be corrected against
// the real pad with cmd/padprobe before it can be trusted. The handheld's own buttons, wired
// to gpio-keys, report the codes this table names first: BTN_DPAD_*, BTN_SOUTH, BTN_EAST,
// BTN_SELECT, KEY_BACK and BTN_MODE.

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
// on pads of this kind. A pad without a Cancel button uses Start for it.
var padButtons = map[uint16]sim.Button{
	btnSouth:     sim.ButtonA,
	btnSouth + 1: sim.ButtonB,
	btnSelect:    sim.ButtonSelect,
	btnStart:     sim.ButtonCancel,
	keyBack:      sim.ButtonCancel,

	btnTrigger:     sim.ButtonA,
	btnTrigger + 1: sim.ButtonB,
	btnTrigger + 6: sim.ButtonSelect,
	btnTrigger + 7: sim.ButtonCancel,
}

// padExitChords close the player when both buttons of one are held: Select and Start, on a
// gamepad and on a joystick-style pad. A pad with no Home button has to have a way back to
// the console's home that no game can swallow, so no game can use Select and Cancel (Start)
// together.
var padExitChords = [...][2]uint16{{btnSelect, btnStart}, {btnTrigger + 6, btnTrigger + 7}}

// padHome is the Home button, which closes the player on its own: chords of one button, so
// that they are forgotten after lost events and waited for on closing as the others are.
var padHome = [...][2]uint16{{btnMode, btnMode}, {keyHomePage, keyHomePage}}

// padDPad maps the D-pad-as-buttons encoding some pads use, and the handheld's.
var padDPad = map[uint16]sim.Button{
	btnDPadUp:     sim.ButtonUp,
	btnDPadUp + 1: sim.ButtonDown,
	btnDPadUp + 2: sim.ButtonLeft,
	btnDPadUp + 3: sim.ButtonRight,
}
