// Package transport provides the authenticated, encrypted connection between
// host and viewer: TLS 1.3 with mutually pinned self-signed certificates.
//
// Pairing a new viewer is a one-time step. The host displays its ShortCode
// and a random PIN; the viewer checks the host certificate against the code
// (so the channel cannot be intercepted) and then sends the PIN inside TLS
// (so only someone who can see the host screen can pair). Both sides then
// remember each other's fingerprint.
package transport

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/bjorand/pikasplit/internal/proto"
)

const (
	handshakeTimeout = 10 * time.Second
	maxPINAttempts   = 5
)

// Conn is an established, authenticated connection. Send is safe for
// concurrent use; Recv must be called from a single goroutine.
type Conn struct {
	PeerFingerprint Fingerprint
	PeerName        string

	tc  *tls.Conn
	r   *bufio.Reader
	wmu sync.Mutex
}

func newConn(tc *tls.Conn, peer Fingerprint) *Conn {
	return &Conn{PeerFingerprint: peer, tc: tc, r: bufio.NewReaderSize(tc, 64<<10)}
}

// Send writes one message.
func (c *Conn) Send(m proto.Message) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return proto.Write(c.tc, m)
}

// Recv reads the next message.
func (c *Conn) Recv() (proto.Message, error) { return proto.Read(c.r) }

// Close closes the connection.
func (c *Conn) Close() error { return c.tc.Close() }

// RemoteAddr returns the peer address.
func (c *Conn) RemoteAddr() net.Addr { return c.tc.RemoteAddr() }

func peerFingerprint(tc *tls.Conn) (Fingerprint, error) {
	certs := tc.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return Fingerprint{}, errors.New("transport: peer sent no certificate")
	}
	return FingerprintOf(certs[0].Raw), nil
}

// Host accepts viewers.
type Host struct {
	ID      *Identity
	Name    string
	Viewers *Store // authorized viewers

	mu      sync.Mutex
	pairing *pairing
}

type pairing struct {
	pin      string
	expires  time.Time
	attempts int
}

// StartPairing opens a pairing window and returns the PIN to display along
// with h.ID.Fingerprint.ShortCode().
func (h *Host) StartPairing(ttl time.Duration) (pin string, err error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	pin = fmt.Sprintf("%06d", n.Int64())
	h.mu.Lock()
	h.pairing = &pairing{pin: pin, expires: time.Now().Add(ttl)}
	h.mu.Unlock()
	return pin, nil
}

// Pairing reports whether a pairing window is open.
func (h *Host) Pairing() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pairing != nil && time.Now().Before(h.pairing.expires)
}

// checkPIN consumes an attempt; a correct PIN closes the window.
func (h *Host) checkPIN(pin string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.pairing
	if p == nil || time.Now().After(p.expires) {
		h.pairing = nil
		return false
	}
	if subtle.ConstantTimeCompare([]byte(pin), []byte(p.pin)) == 1 {
		h.pairing = nil
		return true
	}
	p.attempts++
	if p.attempts >= maxPINAttempts {
		h.pairing = nil
	}
	return false
}

// Listen returns a TLS listener for viewers. Pass accepted connections to
// Handshake.
func (h *Host) Listen(addr string) (net.Listener, error) {
	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{h.ID.Cert},
		// Self-signed client certs: authorization is done by fingerprint in
		// Handshake, not by a CA.
		ClientAuth: tls.RequireAnyClientCert,
	}
	return tls.Listen("tcp", addr, cfg)
}

// Handshake authenticates a viewer accepted from Listen.
func (h *Host) Handshake(raw net.Conn) (*Conn, error) {
	tc, ok := raw.(*tls.Conn)
	if !ok {
		raw.Close()
		return nil, errors.New("transport: not a TLS connection")
	}
	tc.SetDeadline(time.Now().Add(handshakeTimeout))
	c, err := h.handshake(tc)
	if err != nil {
		tc.Close()
		return nil, err
	}
	tc.SetDeadline(time.Time{})
	return c, nil
}

func (h *Host) handshake(tc *tls.Conn) (*Conn, error) {
	if err := tc.Handshake(); err != nil {
		return nil, err
	}
	fp, err := peerFingerprint(tc)
	if err != nil {
		return nil, err
	}
	c := newConn(tc, fp)
	m, err := c.Recv()
	if err != nil {
		return nil, err
	}
	hello, ok := m.(*proto.Hello)
	if !ok {
		return nil, fmt.Errorf("transport: expected Hello, got %T", m)
	}
	reject := func(reason string) (*Conn, error) {
		c.Send(&proto.Reject{Reason: reason})
		return nil, fmt.Errorf("transport: rejected viewer %s (%q): %s", fp.ShortCode(), hello.Name, reason)
	}
	if hello.Version != proto.Version {
		return reject(fmt.Sprintf("protocol version %d, host speaks %d", hello.Version, proto.Version))
	}
	if _, known := h.Viewers.Lookup(fp); !known {
		if hello.PairPIN == "" {
			return reject("viewer not paired")
		}
		if !h.checkPIN(hello.PairPIN) {
			return reject("wrong or expired pairing PIN")
		}
		if err := h.Viewers.Add(fp, hello.Name); err != nil {
			return nil, err
		}
	}
	c.PeerName = hello.Name
	if err := c.Send(&proto.Welcome{Version: proto.Version, Name: h.Name}); err != nil {
		return nil, err
	}
	return c, nil
}

// DialConfig configures a viewer connection. Exactly one of HostFingerprint
// (already paired) or PairCode (pairing) must identify the host.
type DialConfig struct {
	ID   *Identity
	Name string

	HostFingerprint *Fingerprint

	PairCode string // host ShortCode
	PairPIN  string
}

// Dial connects to a host and performs the Hello/Welcome exchange.
func Dial(ctx context.Context, addr string, cfg DialConfig) (*Conn, error) {
	if (cfg.HostFingerprint == nil) == (cfg.PairCode == "") {
		return nil, errors.New("transport: need exactly one of HostFingerprint or PairCode")
	}
	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cfg.ID.Cert},
		// The host certificate is self-signed; it is verified by pinning in
		// VerifyPeerCertificate instead of against a CA.
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("transport: host sent no certificate")
			}
			fp := FingerprintOf(rawCerts[0])
			if cfg.HostFingerprint != nil {
				if fp != *cfg.HostFingerprint {
					return fmt.Errorf("transport: host fingerprint changed (got %s)", fp.ShortCode())
				}
				return nil
			}
			if normalizeCode(fp.ShortCode()) != normalizeCode(cfg.PairCode) {
				return fmt.Errorf("transport: host code is %s, expected %s", fp.ShortCode(), cfg.PairCode)
			}
			return nil
		},
	}
	d := tls.Dialer{Config: tlsCfg, NetDialer: &net.Dialer{Timeout: handshakeTimeout}}
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	tc := raw.(*tls.Conn)
	tc.SetDeadline(time.Now().Add(handshakeTimeout))
	fp, err := peerFingerprint(tc)
	if err != nil {
		tc.Close()
		return nil, err
	}
	c := newConn(tc, fp)
	if err := c.Send(&proto.Hello{Version: proto.Version, Name: cfg.Name, PairPIN: cfg.PairPIN}); err != nil {
		tc.Close()
		return nil, err
	}
	m, err := c.Recv()
	if err != nil {
		tc.Close()
		return nil, err
	}
	switch m := m.(type) {
	case *proto.Welcome:
		c.PeerName = m.Name
	case *proto.Reject:
		tc.Close()
		return nil, fmt.Errorf("transport: host refused: %s", m.Reason)
	default:
		tc.Close()
		return nil, fmt.Errorf("transport: expected Welcome, got %T", m)
	}
	tc.SetDeadline(time.Time{})
	return c, nil
}
