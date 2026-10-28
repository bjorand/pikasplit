package keymap

import "testing"

func TestFromPCScancode(t *testing.T) {
	cases := []struct {
		scan     uint16
		extended bool
		hid      uint16
	}{
		{0x10, false, 0x14}, // AZERTY "A" key = QWERTY q position
		{0x1E, false, 0x04}, // a
		{0x2C, false, 0x1D}, // z
		{0x02, false, 0x1E}, // 1
		{0x29, false, 0x35}, // key left of 1
		{0x56, false, 0x64}, // "<>"
		{0x1C, false, 0x28}, // Enter
		{0x1C, true, 0x58},  // KP Enter
		{0x1D, false, 0xE0}, // LeftCtrl
		{0x1D, true, 0xE4},  // RightCtrl
		{0x38, true, 0xE6},  // AltGr
		{0x5B, true, 0xE3},  // LeftWin
		{0x48, false, 0x60}, // KP 8
		{0x48, true, 0x52},  // Up
		{0x53, true, 0x4C},  // Delete
		{0x57, false, 0x44}, // F11
	}
	for _, c := range cases {
		got, ok := FromPCScancode(c.scan, c.extended)
		if !ok || got != c.hid {
			t.Errorf("FromPCScancode(%#x, %v) = %#x, %v; want %#x", c.scan, c.extended, got, ok, c.hid)
		}
	}
	for _, c := range []struct {
		scan     uint16
		extended bool
	}{{0x00, false}, {0x2A, true}, {0x80, false}, {0xFFFF, true}} {
		if hid, ok := FromPCScancode(c.scan, c.extended); ok {
			t.Errorf("FromPCScancode(%#x, %v) = %#x, want unmapped", c.scan, c.extended, hid)
		}
	}
}

// Every HID usage produced from a scancode must have a Mac key code, except
// keys macOS has no equivalent for.
func TestPCScancodesReachMac(t *testing.T) {
	noMac := map[uint16]bool{0x65: true} // Application (menu) key
	m := New(Options{})
	for _, ext := range []bool{false, true} {
		for scan := uint16(0); scan < 0x80; scan++ {
			hid, ok := FromPCScancode(scan, ext)
			if !ok || noMac[hid] {
				continue
			}
			if _, ok := m.Map(hid); !ok {
				t.Errorf("scancode %#x ext=%v -> HID %#x has no Mac key", scan, ext, hid)
			}
		}
	}
}
