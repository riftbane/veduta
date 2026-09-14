package platform

import "math"

// The stick. A pad's ABS_X and ABS_Y, a mouse's movement and a tablet's position all move
// the one analog stick a game reads as sim.Input.Stick: each axis -1…1, +X right, +Y up.

// stickDeadZone is the part of the travel on each side of rest that reads as rest: a stick
// never quite comes back to the middle, and a mouse nudged by accident should not walk.
const stickDeadZone = 0.15

// mouseReach is how many counts a mouse moves from rest to push the stick to the end.
const mouseReach = 400

// stickValue maps v, in lo…hi, to -1…1: 0 within the dead zone around the middle, growing
// to ±1 at the ends and clamped beyond them. The arithmetic is exact up to one division, so
// the two sides mirror each other and rest is 0, never -0.
func stickValue(v, lo, hi int64) float32 {
	if hi <= lo {
		return 0
	}
	x := float64(2*v-lo-hi) / float64(hi-lo)
	a := math.Abs(x)
	if a <= stickDeadZone {
		return 0
	}
	a = min((a-stickDeadZone)/(1-stickDeadZone), 1)
	return float32(math.Copysign(a, x))
}

// moveStick moves one axis of the stick to where ABS_X or ABS_Y says, against the range the
// device reports, or as a stick centred on zero when it does not say. The device's Y grows
// downwards and the game's grows up.
func (d *padDecoder) moveStick(out []Event, code uint16, value int32) []Event {
	lo, hi := int64(-32768), int64(32767)
	if r := d.ranges[code]; r.max > r.min {
		lo, hi = int64(r.min), int64(r.max)
	}
	v := int64(value)
	if code == absY {
		v = lo + hi - v
	}
	s := d.stick
	s[code] = stickValue(v, lo, hi)
	return d.setStick(out, s)
}

// moveMouse adds a mouse's movement (REL_X or REL_Y) to the stick, which stays where the
// mouse leaves it: mouseReach counts from rest is the end of the travel, and movement past
// the end is not kept, so coming back starts at once.
func (d *padDecoder) moveMouse(out []Event, code uint16, value int32) []Event {
	d.relative = true
	d.mouse[code] = min(max(d.mouse[code]+int64(value), -mouseReach), mouseReach)
	v := d.mouse[code]
	if code == relY {
		v = -v
	}
	s := d.stick
	s[code] = stickValue(v, -mouseReach, mouseReach)
	return d.setStick(out, s)
}

// recentre brings a mouse's stick back to rest, which a click of any of its buttons does. A
// tablet's clicks move nothing: its position is the stick's.
func (d *padDecoder) recentre(out []Event) []Event {
	if !d.relative {
		return out
	}
	d.mouse = [2]int64{}
	return d.setStick(out, [2]float32{})
}

// setStick reports the stick at s when it moved.
func (d *padDecoder) setStick(out []Event, s [2]float32) []Event {
	if s == d.stick {
		return out
	}
	d.stick = s
	return append(out, Event{Kind: Stick, X: s[0], Y: s[1]})
}
