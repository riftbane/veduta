package platform

import "github.com/riftbane/veduta/sim"

// Keyboards, read the same way as the pad. These are the kernel's own KEY_* codes from
// linux/input-event-codes.h, the numbers that arrive on /dev/input/eventN.
//
// It matters beyond tidiness. Without it a console with no gamepad — an emulated machine,
// a PC at a text console, a board whose pad is not plugged in yet — draws its dashboard
// and then answers nothing at all, with no message to say why.
//
// Codes are by physical position: on a French keyboard the key where KEY_W is reads as W.

const (
	keyEsc       = 1
	keyQ         = 16
	keyLeftCtrl  = 29
	keyRightCtrl = 97
)

// evdevKeys maps a kernel key code to the game button a keyboard presses with it: the
// arrows or W, A, S, D for the D-pad, Space or Z for A, X or Shift for B, Enter or Tab for
// Select, Escape or Backspace for Cancel. Every other key is ignored.
var evdevKeys = map[uint16]sim.Button{
	103: sim.ButtonUp, 108: sim.ButtonDown, 105: sim.ButtonLeft, 106: sim.ButtonRight, // arrows
	17: sim.ButtonUp, 31: sim.ButtonDown, 30: sim.ButtonLeft, 32: sim.ButtonRight, // W, S, A, D
	57: sim.ButtonA, 44: sim.ButtonA, // Space, Z
	45: sim.ButtonB, 42: sim.ButtonB, 54: sim.ButtonB, // X, left and right Shift
	28: sim.ButtonSelect, 15: sim.ButtonSelect, // Enter, Tab
	keyEsc: sim.ButtonCancel, 14: sim.ButtonCancel, // Escape, Backspace
}

// keyboardTools maps the kernel's F1, F5 and F9 to the player's tool keys.
var keyboardTools = map[uint16]ToolKey{59: ToolOverlay, 63: ToolRecord, 67: ToolReload}

// keyboardExit closes the player from a keyboard, as Home does from a pad: Ctrl+Q, with
// either Ctrl. It is not Escape, which is Cancel.
var keyboardExit = [2][2]uint16{{keyLeftCtrl, keyQ}, {keyRightCtrl, keyQ}}
