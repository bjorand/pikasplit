// Package keymap translates physical PC keys (USB HID usage IDs, as reported
// by SDL scancodes) into macOS virtual key codes (kVK_* from
// HIToolbox/Events.h), and tracks modifier state as CGEventFlags.
//
// Physical keys are forwarded, not characters: the Mac applies its own
// keyboard layout, so an AZERTY PC keyboard works with an AZERTY Mac layout.
package keymap

// Options tune the translation.
type Options struct {
	// ISO swaps the two keys macOS treats differently on ISO keyboards: the
	// key left of "1" and the extra key next to left Shift ("<>" on AZERTY).
	// Enable it when the Mac uses an ISO layout (most European layouts).
	ISO bool
	// SwapCtrlCmd maps PC Ctrl to Command and the Windows key to Control, so
	// that Ctrl+C on the PC keyboard is Cmd+C on the Mac.
	SwapCtrlCmd bool
}

// Mapper converts HID usages to macOS key codes.
type Mapper struct {
	opts Options
}

// New returns a Mapper with the given options.
func New(opts Options) *Mapper { return &Mapper{opts: opts} }

// HID usages that depend on Options.
const (
	hidGrave          = 0x35
	hidNonUSBackslash = 0x64
	hidLeftCtrl       = 0xE0
	hidLeftGUI        = 0xE3
	hidRightCtrl      = 0xE4
	hidRightGUI       = 0xE7
)

// macOS key codes referenced outside the table.
const (
	VKShift        = 0x38
	VKRightShift   = 0x3C
	VKControl      = 0x3B
	VKRightControl = 0x3E
	VKOption       = 0x3A
	VKRightOption  = 0x3D
	VKCommand      = 0x37
	VKRightCommand = 0x36
	VKCapsLock     = 0x39
	VKFunction     = 0x3F
	VKANSIGrave    = 0x32
	VKISOSection   = 0x0A
)

// Map returns the macOS key code for a HID usage, or ok=false if the key has
// no Mac equivalent.
func (m *Mapper) Map(hid uint16) (vk uint16, ok bool) {
	switch hid {
	case hidGrave:
		if m.opts.ISO {
			return VKISOSection, true
		}
		return VKANSIGrave, true
	case hidNonUSBackslash:
		if m.opts.ISO {
			return VKANSIGrave, true
		}
		return VKISOSection, true
	}
	if m.opts.SwapCtrlCmd {
		switch hid {
		case hidLeftCtrl:
			return VKCommand, true
		case hidRightCtrl:
			return VKRightCommand, true
		case hidLeftGUI:
			return VKControl, true
		case hidRightGUI:
			return VKRightControl, true
		}
	}
	if int(hid) >= len(table) {
		return 0, false
	}
	e := table[hid]
	if e == 0 {
		return 0, false
	}
	return uint16(e - 1), true
}

// table maps HID usage -> macOS key code + 1 (0 means unmapped).
var table = func() [0xE8]uint8 {
	var t [0xE8]uint8
	set := func(hid, vk uint8) { t[hid] = vk + 1 }

	// Letters a..z (HID 0x04..0x1D).
	letters := [26]uint8{
		0x00, 0x0B, 0x08, 0x02, 0x0E, 0x03, 0x05, 0x04, 0x22, 0x26, 0x28, 0x25, 0x2E, // a..m
		0x2D, 0x1F, 0x23, 0x0C, 0x0F, 0x01, 0x11, 0x20, 0x09, 0x0D, 0x07, 0x10, 0x06, // n..z
	}
	for i, vk := range letters {
		set(0x04+uint8(i), vk)
	}
	// Digits 1..9, 0 (HID 0x1E..0x27).
	digits := [10]uint8{0x12, 0x13, 0x14, 0x15, 0x17, 0x16, 0x1A, 0x1C, 0x19, 0x1D}
	for i, vk := range digits {
		set(0x1E+uint8(i), vk)
	}

	set(0x28, 0x24) // Return
	set(0x29, 0x35) // Escape
	set(0x2A, 0x33) // Backspace -> Delete
	set(0x2B, 0x30) // Tab
	set(0x2C, 0x31) // Space
	set(0x2D, 0x1B) // Minus
	set(0x2E, 0x18) // Equal
	set(0x2F, 0x21) // LeftBracket
	set(0x30, 0x1E) // RightBracket
	set(0x31, 0x2A) // Backslash
	set(0x32, 0x2A) // NonUS # (ISO key left of Return) -> Backslash
	set(0x33, 0x29) // Semicolon
	set(0x34, 0x27) // Quote
	set(0x36, 0x2B) // Comma
	set(0x37, 0x2F) // Period
	set(0x38, 0x2C) // Slash
	set(0x39, VKCapsLock)

	// F1..F12 (HID 0x3A..0x45).
	fkeys := [12]uint8{0x7A, 0x78, 0x63, 0x76, 0x60, 0x61, 0x62, 0x64, 0x65, 0x6D, 0x67, 0x6F}
	for i, vk := range fkeys {
		set(0x3A+uint8(i), vk)
	}
	set(0x46, 0x69) // PrintScreen -> F13
	set(0x47, 0x6B) // ScrollLock -> F14
	set(0x48, 0x71) // Pause -> F15
	set(0x49, 0x72) // Insert -> Help
	set(0x4A, 0x73) // Home
	set(0x4B, 0x74) // PageUp
	set(0x4C, 0x75) // Delete -> ForwardDelete
	set(0x4D, 0x77) // End
	set(0x4E, 0x79) // PageDown
	set(0x4F, 0x7C) // Right
	set(0x50, 0x7B) // Left
	set(0x51, 0x7D) // Down
	set(0x52, 0x7E) // Up

	set(0x53, 0x47) // NumLock -> KeypadClear
	set(0x54, 0x4B) // KP /
	set(0x55, 0x43) // KP *
	set(0x56, 0x4E) // KP -
	set(0x57, 0x45) // KP +
	set(0x58, 0x4C) // KP Enter
	// KP 1..9, 0 (HID 0x59..0x62).
	keypad := [10]uint8{0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5B, 0x5C, 0x52}
	for i, vk := range keypad {
		set(0x59+uint8(i), vk)
	}
	set(0x63, 0x41) // KP .
	set(0x67, 0x51) // KP =

	// F13..F20 (HID 0x68..0x6F).
	fhigh := [8]uint8{0x69, 0x6B, 0x71, 0x6A, 0x40, 0x4F, 0x50, 0x5A}
	for i, vk := range fhigh {
		set(0x68+uint8(i), vk)
	}
	set(0x7F, 0x4A) // Mute
	set(0x80, 0x48) // VolumeUp
	set(0x81, 0x49) // VolumeDown

	set(0xE0, VKControl)
	set(0xE1, VKShift)
	set(0xE2, VKOption)
	set(0xE3, VKCommand)
	set(0xE4, VKRightControl)
	set(0xE5, VKRightShift)
	set(0xE6, VKRightOption) // AltGr
	set(0xE7, VKRightCommand)
	return t
}()

// CGEventFlags modifier masks (CGEventTypes.h).
const (
	FlagCapsLock uint64 = 0x00010000
	FlagShift    uint64 = 0x00020000
	FlagControl  uint64 = 0x00040000
	FlagOption   uint64 = 0x00080000
	FlagCommand  uint64 = 0x00100000
	FlagFunction uint64 = 0x00800000
)

// ModState tracks which modifier keys are held so that injected events can
// carry the right CGEventFlags.
type ModState struct {
	down     map[uint16]bool
	capsLock bool
}

// IsModifier reports whether vk is a modifier key.
func IsModifier(vk uint16) bool {
	return modFlag(vk) != 0
}

func modFlag(vk uint16) uint64 {
	switch vk {
	case VKShift, VKRightShift:
		return FlagShift
	case VKControl, VKRightControl:
		return FlagControl
	case VKOption, VKRightOption:
		return FlagOption
	case VKCommand, VKRightCommand:
		return FlagCommand
	case VKCapsLock:
		return FlagCapsLock
	case VKFunction:
		return FlagFunction
	}
	return 0
}

// Update records a key transition (macOS key code).
func (s *ModState) Update(vk uint16, down bool) {
	if vk == VKCapsLock {
		if down {
			s.capsLock = !s.capsLock
		}
		return
	}
	if modFlag(vk) == 0 {
		return
	}
	if s.down == nil {
		s.down = make(map[uint16]bool)
	}
	if down {
		s.down[vk] = true
	} else {
		delete(s.down, vk)
	}
}

// Flags returns the current modifier flags.
func (s *ModState) Flags() uint64 {
	var f uint64
	for vk := range s.down {
		f |= modFlag(vk)
	}
	if s.capsLock {
		f |= FlagCapsLock
	}
	return f
}

// Held returns the modifier keys currently held, e.g. to release them all
// when the viewer loses focus.
func (s *ModState) Held() []uint16 {
	keys := make([]uint16, 0, len(s.down))
	for vk := range s.down {
		keys = append(keys, vk)
	}
	return keys
}
