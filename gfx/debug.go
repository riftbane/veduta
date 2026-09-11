package gfx

// IDColor returns the false color used for entity id in ModeIDs and in inspection legends.
// Id 0 (no entity) is black. Colors are bright, deterministic and well spread for
// consecutive ids.
func IDColor(id uint32) uint32 {
	if id == 0 {
		return 0xff000000
	}
	// Golden-ratio (Fibonacci) hashing of the id gives a well-spread 16-bit hue; value
	// cycles over three levels and saturation over two, so ids with close hues still
	// differ. Then a cheap integer HSV→RGB.
	h := (id * 2654435769) >> 16
	v := [3]uint32{242, 200, 158}[id%3]
	lo := v * [2]uint32{22, 55}[(id/3)&1] / 100
	sector := h * 6 >> 16
	f := (h * 6) & 0xffff
	rise := lo + (v-lo)*f>>16
	fall := v - (v-lo)*f>>16
	var r, g, b uint32
	switch sector {
	case 0:
		r, g, b = v, rise, lo
	case 1:
		r, g, b = fall, v, lo
	case 2:
		r, g, b = lo, v, rise
	case 3:
		r, g, b = lo, fall, v
	case 4:
		r, g, b = rise, lo, v
	default:
		r, g, b = v, lo, fall
	}
	return 0xff000000 | r<<16 | g<<8 | b
}

// heat is the overdraw palette: index = fragments per pixel, clamped to the last entry.
var heat = [...]uint32{
	0xff000000, // 0: nothing drawn
	0xff1f3b8f, // 1: dark blue
	0xff2fa84f, // 2: green
	0xffe8d33f, // 3: yellow
	0xfff08a24, // 4: orange
	0xffd7263d, // 5: red
	0xffff66ff, // 6+: magenta
}

// HeatColor returns the overdraw heat-map color for n fragments at one pixel.
func HeatColor(n int) uint32 {
	if n < 0 {
		n = 0
	}
	if n >= len(heat) {
		n = len(heat) - 1
	}
	return heat[n]
}

// HeatLevels is the number of distinct overdraw colors; the last one means "or more".
const HeatLevels = len(heat)
