package platform

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A real device, made by the kernel. Through /dev/uinput a process describes an input
// device, asks for it to be created, and from then on every input_event it writes arrives
// on a new /dev/input/eventN exactly as a USB pad's would: listed in /sys/class/input with
// its name and capability bitmaps, readable, and gone with ENODEV when destroyed. That is
// the one part of the pad path the fakes cannot reach.
//
// The numbers are from linux/uinput.h and linux/input-event-codes.h. The requests use the
// generic ioctl encoding (direction in the top two bits, then size, type 'U' and number)
// of the little-endian architectures in uinputArches, the console's among them; PowerPC,
// MIPS and SPARC encode requests differently, and the decoder reads little-endian records,
// so the test skips everywhere else.
const (
	uinputPath = "/dev/uinput"

	uiDevCreate  = 0x5501     // _IO('U', 1)
	uiDevDestroy = 0x5502     // _IO('U', 2)
	uiSetEvBit   = 0x40045564 // _IOW('U', 100, int)
	uiSetKeyBit  = 0x40045565 // _IOW('U', 101, int)
	uiSetAbsBit  = 0x40045567 // _IOW('U', 103, int)

	uinputMaxName = 80   // UINPUT_MAX_NAME_SIZE
	absCount      = 0x40 // ABS_CNT
	busVirtual    = 0x06 // BUS_VIRTUAL

	synReport = 0x00  // SYN_REPORT
	btnEast   = 0x131 // BTN_EAST, the pad's B
	btnSelect = 0x13a // BTN_SELECT
	btnStart  = 0x13b // BTN_START
)

var uinputArches = []string{"386", "amd64", "arm", "arm64", "loong64", "riscv64"}

// virtualPad is a pad the kernel believes in. It has no keyboard keys on purpose, so the
// console's keyboard handler does not attach to it and nothing it sends can reach a
// terminal on the machine running the test.
type virtualPad struct {
	t       *testing.T
	fd      int
	name    string
	created bool
}

// padSpec describes a virtual pad: what to call it, its buttons, and its axes with the
// ranges they report.
type padSpec struct {
	kind string
	keys []uintptr
	axes []axisSpec
}

type axisSpec struct {
	code     int
	min, max int32
}

var (
	// gamepad is the common kind: A, B, Select, Start and a hat.
	gamepad = padSpec{
		kind: "gamepad",
		keys: []uintptr{btnSouth, btnEast, btnSelect, btnStart},
		axes: []axisSpec{{absHat0X, -1, 1}, {absHat0Y, -1, 1}},
	}
	// joystickPad is the other kind the table knows: buttons from BTN_TRIGGER, Select and
	// Start among them, a stick from 0 to 255 as many cheap pads report their D-pad, and a
	// hat as well.
	joystickPad = padSpec{
		kind: "joystick-style pad",
		keys: []uintptr{btnTrigger, btnTrigger + 1, btnTrigger + 2, btnTrigger + 3, btnTrigger + 4, btnTrigger + 5, btnTrigger + 6, btnTrigger + 7},
		axes: []axisSpec{{absX, 0, 255}, {absY, 0, 255}, {absHat0X, -1, 1}},
	}
)

var virtualPads int

// newVirtualPad creates the device, or skips the test where the kernel will not make one
// for this process: no uinput module, no permission (it takes root, or a udev rule), or an
// emulator such as qemu-user that does not pass these ioctls through. It is destroyed when
// the test ends.
func newVirtualPad(t *testing.T, spec padSpec) *virtualPad {
	t.Helper()
	supported := false
	for _, a := range uinputArches {
		supported = supported || a == runtime.GOARCH
	}
	if !supported {
		t.Skipf("uinput requests are encoded differently on %s", runtime.GOARCH)
	}
	fd, err := syscall.Open(uinputPath, syscall.O_WRONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Skipf("%s cannot be opened for writing (%v): a real device needs the uinput module and root", uinputPath, err)
	}
	virtualPads++
	p := &virtualPad{t: t, fd: fd, name: fmt.Sprintf("Veduta test %s (pid %d, pad %d)", spec.kind, os.Getpid(), virtualPads)}
	t.Cleanup(func() {
		p.destroy()
		syscall.Close(fd)
	})

	p.ioctl(uiSetEvBit, evKey, "UI_SET_EVBIT EV_KEY")
	p.ioctl(uiSetEvBit, evAbs, "UI_SET_EVBIT EV_ABS")
	for _, b := range spec.keys {
		p.ioctl(uiSetKeyBit, b, fmt.Sprintf("UI_SET_KEYBIT %#x", b))
	}
	for _, a := range spec.axes {
		p.ioctl(uiSetAbsBit, uintptr(a.code), fmt.Sprintf("UI_SET_ABSBIT %#x", a.code))
	}

	// struct uinput_user_dev: the name, struct input_id, ff_effects_max, then absmax,
	// absmin, absfuzz and absflat for every axis. Writing it is the setup every kernel
	// accepts, and it needs no pointer passed to an ioctl.
	dev := make([]byte, uinputMaxName+8+4+4*absCount*4)
	copy(dev[:uinputMaxName-1], p.name)
	binary.LittleEndian.PutUint16(dev[uinputMaxName:], busVirtual)
	binary.LittleEndian.PutUint16(dev[uinputMaxName+2:], 0x1209) // vendor
	binary.LittleEndian.PutUint16(dev[uinputMaxName+4:], 0x7665) // product
	binary.LittleEndian.PutUint16(dev[uinputMaxName+6:], 1)      // version
	absMax, absMin := uinputMaxName+12, uinputMaxName+12+4*absCount
	for _, a := range spec.axes {
		binary.LittleEndian.PutUint32(dev[absMax+4*a.code:], uint32(a.max))
		binary.LittleEndian.PutUint32(dev[absMin+4*a.code:], uint32(a.min))
	}
	if _, err := syscall.Write(fd, dev); err != nil {
		t.Fatalf("%s: describing the device: %v", uinputPath, err)
	}
	p.ioctl(uiDevCreate, 0, "UI_DEV_CREATE")
	p.created = true
	return p
}

func (p *virtualPad) ioctl(req, arg uintptr, what string) {
	p.t.Helper()
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(p.fd), req, arg); errno != 0 {
		if errno == syscall.ENOTTY || errno == syscall.ENOSYS {
			p.t.Skipf("%s: %s is not understood here (%v)", uinputPath, what, errno)
		}
		p.t.Fatalf("%s: %s: %v", uinputPath, what, errno)
	}
}

// emit sends one event and the SYN_REPORT that makes the kernel hand it to readers.
func (p *virtualPad) emit(typ, code uint16, value int32) {
	p.t.Helper()
	data := append(record(eventSize, typ, code, value), record(eventSize, evSyn, synReport, 0)...)
	if _, err := syscall.Write(p.fd, data); err != nil {
		p.t.Fatalf("%s: sending type %d code %#x value %d: %v", uinputPath, typ, code, value, err)
	}
}

// destroy unplugs the device. It is safe to call more than once.
func (p *virtualPad) destroy() {
	if !p.created {
		return
	}
	p.created = false
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(p.fd), uiDevDestroy, 0); errno != 0 {
		p.t.Errorf("%s: UI_DEV_DESTROY: %v", uinputPath, errno)
	}
}

// find waits, a bounded time, for the kernel to list the device in sysfs and for its node
// to appear under /dev/input, and returns what discovery makes of it.
func (p *virtualPad) find() inputDevice {
	p.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		devs, err := findInputs(p.name)
		if err == nil {
			if len(devs) != 1 {
				p.t.Fatalf("%d input devices are called %q", len(devs), p.name)
			}
			if _, serr := os.Stat(devs[0].Dev); serr == nil {
				return devs[0]
			} else if time.Now().After(deadline) {
				p.t.Skipf("the kernel lists %s, but %s never appeared (%v): nothing creates device nodes here", devs[0].Node, devs[0].Dev, serr)
			}
		} else if time.Now().After(deadline) {
			p.t.Fatalf("the virtual pad never appeared in /sys/class/input: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// collect polls until the events described by want have arrived, or a bounded time has
// passed, and describes what came. Waiting for nothing means waiting a little and seeing
// that nothing came.
func collect(t *testing.T, src *inputSource, want string) string {
	t.Helper()
	n, wait := 0, 100*time.Millisecond
	if want != "" {
		n, wait = strings.Count(want, ",")+1, 5*time.Second
	}
	var got []Event
	deadline := time.Now().Add(wait)
	for {
		evs, err := src.poll()
		if err != nil {
			t.Fatalf("poll: %v", err)
		}
		got = append(got, evs...)
		if (n > 0 && len(got) >= n) || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Anything past what was expected is already there: the kernel queues events for
	// readers before the write that sends them returns.
	evs, err := src.poll()
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	return describePad(append(got, evs...))
}

// TestEvdevUinput drives the whole pad path with a device the kernel made: discovery reads
// its name and capabilities from /sys/class/input, the source opens /dev/input/eventN and
// reads what the kernel queued, the decoder turns it into W3C keys and the exit chord, and
// unplugging releases what was held and lets the device go. It skips where uinput is not
// available to the test (hosted CI runners, a user without root); where it is, it has to
// pass.
func TestEvdevUinput(t *testing.T) {
	old := sysRoot
	sysRoot = "/"
	t.Cleanup(func() { sysRoot = old })

	pad := newVirtualPad(t, gamepad)
	dev := pad.find()
	t.Logf("the kernel made %s for %q", dev.Dev, dev.Name)

	// Unnamed, discovery has to take it for a pad from the bitmap the kernel printed.
	dir := filepath.Join(sysRoot, "sys", "class", "input")
	keys := readAttr(dir, dev.Node, "device/capabilities/key")
	t.Logf("its key capabilities, as a %d-bit program reads them: %q", strconv.IntSize, keys)
	if !hasPadButtons(keys) {
		t.Fatalf("%s's key capabilities %q do not read as a pad", dev.Node, keys)
	}
	all, err := findInputs("")
	if err != nil {
		t.Fatalf("discovery without a name: %v", err)
	}
	found := false
	for _, d := range all {
		found = found || d.Node == dev.Node
	}
	if !found {
		t.Fatalf("discovery without a name found %s, not %s", describeInputs(all), dev.Node)
	}

	// Reading the node takes membership of its group, which the one who made it may lack.
	probe, err := openEvdev(dev.Dev)
	if errors.Is(err, os.ErrPermission) {
		t.Skipf("%s was made but cannot be read: %v", dev.Dev, err)
	} else if err != nil {
		t.Fatal(err)
	}
	probe.close()

	src := newInputSource(pad.name)
	t.Cleanup(func() { src.close() })
	if got := collect(t, src, ""); got != "" {
		t.Fatalf("before anything was pressed: %s", got)
	}
	if src.Devices() != dev.Node {
		t.Fatalf("the source is reading %q, want %s", src.Devices(), dev.Node)
	}

	for _, step := range []struct {
		name string
		send func()
		want string
	}{
		{"A", func() { pad.emit(evKey, btnSouth, 1); pad.emit(evKey, btnSouth, 0) }, "down Space,up Space"},
		{"B", func() { pad.emit(evKey, btnEast, 1); pad.emit(evKey, btnEast, 0) }, "down Escape,up Escape"},
		{"hat left", func() { pad.emit(evAbs, absHat0X, -1); pad.emit(evAbs, absHat0X, 0) }, "down ArrowLeft,up ArrowLeft"},
		{"hat right and down together", func() {
			pad.emit(evAbs, absHat0X, 1)
			pad.emit(evAbs, absHat0Y, 1)
			pad.emit(evAbs, absHat0X, 0)
			pad.emit(evAbs, absHat0Y, 0)
		}, "down ArrowRight,down ArrowDown,up ArrowRight,up ArrowDown"},
		{"hat up, straight to down", func() {
			pad.emit(evAbs, absHat0Y, -1)
			pad.emit(evAbs, absHat0Y, 1)
			pad.emit(evAbs, absHat0Y, 0)
		}, "down ArrowUp,up ArrowUp,down ArrowDown,up ArrowDown"},
		{"Start alone", func() { pad.emit(evKey, btnStart, 1); pad.emit(evKey, btnStart, 0) }, "down Enter,up Enter"},
		{"Select+Start", func() { pad.emit(evKey, btnSelect, 1); pad.emit(evKey, btnStart, 1) }, "down Tab,up Tab,close"},
		{"letting go of the chord", func() { pad.emit(evKey, btnStart, 0); pad.emit(evKey, btnSelect, 0) }, ""},
		{"A after the chord", func() { pad.emit(evKey, btnSouth, 1) }, "down Space"},
	} {
		step.send()
		if got := collect(t, src, step.want); got != step.want {
			t.Fatalf("%s: got %q, want %q", step.name, got, step.want)
		}
	}

	// Unplugged with A still held: the key comes back up, the device is let go, and the
	// console carries on rather than failing.
	pad.destroy()
	if got := collect(t, src, "up Space"); got != "up Space" {
		t.Fatalf("unplugged with A held: got %q, want %q", got, "up Space")
	}
	if src.Devices() != "" {
		t.Fatalf("still reading %q after the pad was unplugged", src.Devices())
	}
}

// TestEvdevUinputJoystick drives a joystick-style pad the kernel made: its buttons start at
// BTN_TRIGGER and its stick reports 0 to 255, resting in the middle. The source has to ask
// the device for that range, or left and right are dead and a push nearly full left comes
// out as right; and Select+Start has to close the player on this kind of pad too.
func TestEvdevUinputJoystick(t *testing.T) {
	old := sysRoot
	sysRoot = "/"
	t.Cleanup(func() { sysRoot = old })

	pad := newVirtualPad(t, joystickPad)
	dev := pad.find()
	t.Logf("the kernel made %s for %q", dev.Dev, dev.Name)
	if keys := readAttr(filepath.Join(sysRoot, "sys", "class", "input"), dev.Node, "device/capabilities/key"); !hasPadButtons(keys) {
		t.Fatalf("%s's key capabilities %q do not read as a pad", dev.Node, keys)
	}

	probe, err := openEvdev(dev.Dev)
	if errors.Is(err, os.ErrPermission) {
		t.Skipf("%s was made but cannot be read: %v", dev.Dev, err)
	} else if err != nil {
		t.Fatal(err)
	}
	ranges := probe.d.ranges
	probe.close()
	for _, a := range joystickPad.axes {
		if got := ranges[a.code]; got != (axisRange{a.min, a.max}) {
			t.Fatalf("axis %#x: asked for its range, the device gave %+v, want %d..%d", a.code, got, a.min, a.max)
		}
	}
	if got := ranges[absHat0Y]; got != (axisRange{}) {
		t.Fatalf("ABS_HAT0Y, which this pad does not have, has the range %+v", got)
	}

	src := newInputSource(pad.name)
	t.Cleanup(func() { src.close() })
	if got := collect(t, src, ""); got != "" {
		t.Fatalf("before anything was pressed: %s", got)
	}
	for _, step := range []struct {
		name string
		send func()
		want string
	}{
		// The kernel starts every axis at 0 and passes on only changes, so the stick is
		// brought to rest first; at rest it is no direction at all.
		{"stick at rest", func() { pad.emit(evAbs, absX, 127); pad.emit(evAbs, absY, 128) }, ""},
		{"stick left", func() { pad.emit(evAbs, absX, 0); pad.emit(evAbs, absX, 127) }, "down ArrowLeft,up ArrowLeft"},
		{"stick right", func() { pad.emit(evAbs, absX, 255); pad.emit(evAbs, absX, 128) }, "down ArrowRight,up ArrowRight"},
		{"stick nearly full left", func() { pad.emit(evAbs, absX, 1); pad.emit(evAbs, absX, 127) }, "down ArrowLeft,up ArrowLeft"},
		{"stick up", func() { pad.emit(evAbs, absY, 0); pad.emit(evAbs, absY, 128) }, "down ArrowUp,up ArrowUp"},
		// The hat and the stick press the same arrow: it stays down while either holds it.
		{"hat left, stick left and back", func() {
			pad.emit(evAbs, absHat0X, -1)
			pad.emit(evAbs, absX, 0)
			pad.emit(evAbs, absX, 127)
		}, "down ArrowLeft"},
		{"hat back", func() { pad.emit(evAbs, absHat0X, 0) }, "up ArrowLeft"},
		{"first button", func() { pad.emit(evKey, btnTrigger, 1); pad.emit(evKey, btnTrigger, 0) }, "down Space,up Space"},
		{"Start alone", func() { pad.emit(evKey, btnTrigger+7, 1); pad.emit(evKey, btnTrigger+7, 0) }, "down Enter,up Enter"},
		{"Select+Start", func() { pad.emit(evKey, btnTrigger+6, 1); pad.emit(evKey, btnTrigger+7, 1) }, "down Tab,up Tab,close"},
		{"letting go of the chord", func() { pad.emit(evKey, btnTrigger+7, 0); pad.emit(evKey, btnTrigger+6, 0) }, ""},
	} {
		step.send()
		if got := collect(t, src, step.want); got != step.want {
			t.Fatalf("%s: got %q, want %q", step.name, got, step.want)
		}
	}
}
