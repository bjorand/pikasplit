package transport

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bjorand/pikasplit/internal/proto"
)

type testHost struct {
	*Host
	addr  string
	conns chan *Conn
}

func startHost(t *testing.T) *testHost {
	t.Helper()
	id, err := NewIdentity("host")
	if err != nil {
		t.Fatal(err)
	}
	viewers, _ := OpenStore("")
	h := &testHost{Host: &Host{ID: id, Name: "mac", Viewers: viewers}, conns: make(chan *Conn, 4)}
	ln, err := h.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	h.addr = ln.Addr().String()
	go func() {
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				if c, err := h.Handshake(raw); err == nil {
					h.conns <- c
				}
			}()
		}
	}()
	return h
}

func newViewer(t *testing.T) *Identity {
	t.Helper()
	id, err := NewIdentity("viewer")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func dial(cfg DialConfig, addr string) (*Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return Dial(ctx, addr, cfg)
}

func TestPairThenReconnect(t *testing.T) {
	h := startHost(t)
	vid := newViewer(t)
	pin, err := h.StartPairing(time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	// Lowercase and spaces in the code are tolerated.
	code := strings.ToLower(strings.ReplaceAll(h.ID.Fingerprint.ShortCode(), "-", " "))
	c, err := dial(DialConfig{ID: vid, Name: "pc", PairCode: code, PairPIN: pin}, h.addr)
	if err != nil {
		t.Fatal(err)
	}
	if c.PeerName != "mac" || c.PeerFingerprint != h.ID.Fingerprint {
		t.Fatalf("viewer sees peer %q %s", c.PeerName, c.PeerFingerprint)
	}
	hc := <-h.conns
	if hc.PeerName != "pc" || hc.PeerFingerprint != vid.Fingerprint {
		t.Fatalf("host sees peer %q %s", hc.PeerName, hc.PeerFingerprint)
	}

	// Messages flow both ways.
	if err := c.Send(&proto.Key{Code: 4, Down: true}); err != nil {
		t.Fatal(err)
	}
	if m, err := hc.Recv(); err != nil || m.(*proto.Key).Code != 4 {
		t.Fatalf("host recv %v %v", m, err)
	}
	if err := hc.Send(&proto.Ping{Timestamp: 7}); err != nil {
		t.Fatal(err)
	}
	if m, err := c.Recv(); err != nil || m.(*proto.Ping).Timestamp != 7 {
		t.Fatalf("viewer recv %v %v", m, err)
	}
	c.Close()
	hc.Close()

	if h.Pairing() {
		t.Fatal("pairing window still open after success")
	}

	// Reconnect with the pinned fingerprint and no PIN.
	fp := h.ID.Fingerprint
	c, err = dial(DialConfig{ID: vid, Name: "pc", HostFingerprint: &fp}, h.addr)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func TestRejectsUnpairedViewer(t *testing.T) {
	h := startHost(t)
	fp := h.ID.Fingerprint
	_, err := dial(DialConfig{ID: newViewer(t), HostFingerprint: &fp}, h.addr)
	if err == nil || !strings.Contains(err.Error(), "not paired") {
		t.Fatalf("err = %v", err)
	}
}

func TestRejectsWrongPIN(t *testing.T) {
	h := startHost(t)
	pin, _ := h.StartPairing(time.Minute)
	wrong := "000000"
	if pin == wrong {
		wrong = "111111"
	}
	cfg := DialConfig{ID: newViewer(t), PairCode: h.ID.Fingerprint.ShortCode(), PairPIN: wrong}
	for i := 0; i < maxPINAttempts; i++ {
		if _, err := dial(cfg, h.addr); err == nil {
			t.Fatal("wrong PIN accepted")
		}
	}
	// The window is closed after too many attempts, even for the right PIN.
	cfg.PairPIN = pin
	if _, err := dial(cfg, h.addr); err == nil {
		t.Fatal("pairing still open after max attempts")
	}
}

func TestRejectsWrongHost(t *testing.T) {
	h := startHost(t)
	pin, _ := h.StartPairing(time.Minute)
	other, _ := NewIdentity("impostor")
	_, err := dial(DialConfig{ID: newViewer(t), PairCode: other.Fingerprint.ShortCode(), PairPIN: pin}, h.addr)
	if err == nil {
		t.Fatal("connected to host with mismatching code")
	}
	fp := other.Fingerprint
	if _, err := dial(DialConfig{ID: newViewer(t), HostFingerprint: &fp}, h.addr); err == nil {
		t.Fatal("connected to host with mismatching fingerprint")
	}
	// The PIN was never sent, so the window is still open.
	if !h.Pairing() {
		t.Fatal("pairing window consumed by a viewer that rejected the host")
	}
}

func TestIdentityAndStorePersist(t *testing.T) {
	dir := t.TempDir()
	a, err := LoadOrCreateIdentity(dir, "x")
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadOrCreateIdentity(dir, "x")
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint != b.Fingerprint {
		t.Fatal("identity regenerated")
	}

	path := filepath.Join(dir, "viewers")
	s, _ := OpenStore(path)
	if err := s.Add(a.Fingerprint, "pc\nsalon"); err != nil {
		t.Fatal(err)
	}
	s2, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if name, ok := s2.Lookup(a.Fingerprint); !ok || name != "pc salon" {
		t.Fatalf("lookup = %q %v", name, ok)
	}
}

func TestStoreReplaceAndFind(t *testing.T) {
	s, _ := OpenStore("")
	a, _ := NewIdentity("a")
	b, _ := NewIdentity("b")
	s.Replace(a.Fingerprint, "10.0.0.2:7878")
	s.Replace(b.Fingerprint, "10.0.0.2:7878")
	if fp, ok := s.FindByName("10.0.0.2:7878"); !ok || fp != b.Fingerprint {
		t.Fatalf("FindByName = %s %v", fp, ok)
	}
	if _, ok := s.Lookup(a.Fingerprint); ok {
		t.Fatal("old fingerprint still trusted after Replace")
	}
}
