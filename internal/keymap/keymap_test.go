package keymap

import "testing"

func TestMap(t *testing.T) {
	m := New(Options{})
	cases := []struct {
		hid uint16
		vk  uint16
	}{
		{0x04, 0x00}, // a
		{0x1D, 0x06}, // z
		{0x14, 0x0C}, // q
		{0x1E, 0x12}, // 1
		{0x27, 0x1D}, // 0
		{0x28, 0x24}, // Return
		{0x2A, 0x33}, // Backspace
		{0x3A, 0x7A}, // F1
		{0x45, 0x6F}, // F12
		{0x50, 0x7B}, // Left
		{0x62, 0x52}, // KP 0
		{hidGrave, VKANSIGrave},
		{hidNonUSBackslash, VKISOSection},
		{hidLeftCtrl, VKControl},
		{hidLeftGUI, VKCommand},
		{0xE6, VKRightOption},
	}
	for _, c := range cases {
		got, ok := m.Map(c.hid)
		if !ok || got != c.vk {
			t.Errorf("Map(%#x) = %#x, %v; want %#x", c.hid, got, ok, c.vk)
		}
	}
	for _, hid := range []uint16{0x00, 0x03, 0x65, 0xFFFF} {
		if vk, ok := m.Map(hid); ok {
			t.Errorf("Map(%#x) = %#x, want unmapped", hid, vk)
		}
	}
}

func TestMapOptions(t *testing.T) {
	m := New(Options{ISO: true, SwapCtrlCmd: true})
	cases := map[uint16]uint16{
		hidGrave:          VKISOSection,
		hidNonUSBackslash: VKANSIGrave,
		hidLeftCtrl:       VKCommand,
		hidRightCtrl:      VKRightCommand,
		hidLeftGUI:        VKControl,
		hidRightGUI:       VKRightControl,
		0x04:              0x00,
	}
	for hid, want := range cases {
		if got, _ := m.Map(hid); got != want {
			t.Errorf("Map(%#x) = %#x, want %#x", hid, got, want)
		}
	}
}

// Every mapped HID usage must produce a distinct key code, except the
// documented aliases.
func TestNoAccidentalCollisions(t *testing.T) {
	// NonUS # shares Backslash; PrintScreen/ScrollLock/Pause share F13-F15.
	aliases := map[uint16]bool{0x32: true, 0x46: true, 0x47: true, 0x48: true}
	m := New(Options{})
	seen := map[uint16]uint16{}
	for hid := uint16(0); hid < 0xE8; hid++ {
		vk, ok := m.Map(hid)
		if !ok || aliases[hid] {
			continue
		}
		if prev, dup := seen[vk]; dup {
			t.Errorf("HID %#x and %#x both map to %#x", prev, hid, vk)
		}
		seen[vk] = hid
	}
}

func TestModState(t *testing.T) {
	var s ModState
	s.Update(VKShift, true)
	s.Update(VKCommand, true)
	s.Update(0x00, true) // non-modifier ignored
	if got, want := s.Flags(), FlagShift|FlagCommand; got != want {
		t.Fatalf("flags = %#x, want %#x", got, want)
	}
	// Left and right shift are tracked separately.
	s.Update(VKRightShift, true)
	s.Update(VKShift, false)
	if s.Flags()&FlagShift == 0 {
		t.Fatal("shift released while right shift still held")
	}
	s.Update(VKRightShift, false)
	s.Update(VKCommand, false)
	if s.Flags() != 0 {
		t.Fatalf("flags = %#x, want 0", s.Flags())
	}
	// Caps lock toggles on press only.
	s.Update(VKCapsLock, true)
	s.Update(VKCapsLock, false)
	if s.Flags() != FlagCapsLock {
		t.Fatalf("caps lock not latched: %#x", s.Flags())
	}
	s.Update(VKCapsLock, true)
	if s.Flags() != 0 {
		t.Fatalf("caps lock not toggled off: %#x", s.Flags())
	}
	if len(s.Held()) != 0 {
		t.Fatalf("held = %v", s.Held())
	}
}
