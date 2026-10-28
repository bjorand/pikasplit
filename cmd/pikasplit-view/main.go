//go:build windows

// pikasplit-view runs on the PC: it opens a window and, while the window has
// focus, forwards keyboard and mouse input to the paired Mac.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/bjorand/pikasplit/internal/proto"
	"github.com/bjorand/pikasplit/internal/transport"
	"github.com/bjorand/pikasplit/internal/win/winui"
)

func main() {
	hostname, _ := os.Hostname()
	addr := flag.String("host", "", "Mac address, ip[:port]")
	pairCode := flag.String("pair", "", "host code shown by pikasplit-host -pair (first connection only)")
	pin := flag.String("pin", "", "pairing PIN shown by pikasplit-host -pair")
	name := flag.String("name", hostname, "name shown to the Mac")
	flag.Parse()
	if *addr == "" {
		flag.Usage()
		os.Exit(2)
	}
	if _, _, err := net.SplitHostPort(*addr); err != nil {
		*addr = net.JoinHostPort(*addr, strconv.Itoa(proto.DefaultPort))
	}
	if err := run(*addr, *pairCode, *pin, *name); err != nil {
		log.Fatal(err)
	}
}

func run(addr, pairCode, pin, name string) error {
	dir, err := transport.ConfigDir()
	if err != nil {
		return err
	}
	id, err := transport.LoadOrCreateIdentity(dir, "pikasplit-view "+name)
	if err != nil {
		return err
	}
	hosts, err := transport.OpenStore(filepath.Join(dir, "known_hosts"))
	if err != nil {
		return err
	}

	if pairCode != "" {
		c, err := transport.Dial(context.Background(), addr,
			transport.DialConfig{ID: id, Name: name, PairCode: pairCode, PairPIN: pin})
		if err != nil {
			return err
		}
		c.Close()
		if err := hosts.Replace(c.PeerFingerprint, addr); err != nil {
			return err
		}
		log.Printf("paired with %q (%s)", c.PeerName, addr)
	}
	hostFP, ok := hosts.FindByName(addr)
	if !ok {
		return fmt.Errorf("%s is not paired: run pikasplit-host -pair on the Mac, then the command it prints", addr)
	}

	v := &viewer{
		addr:    addr,
		dial:    transport.DialConfig{ID: id, Name: name, HostFingerprint: &hostFP},
		out:     make(chan proto.Message, 512),
		pressed: map[uint16]bool{},
		start:   time.Now(),
	}
	v.status("Connecting…")
	go v.connectLoop()
	go v.pingLoop()

	return winui.Run(winui.Events{
		Key:     v.key,
		Mouse:   v.mouse,
		Scroll:  func(dx, dy int16) { v.send(&proto.Scroll{DX: dx, DY: dy}) },
		Resize:  func(w, h int) { v.send(&proto.Resize{W: uint16(w), H: uint16(h), Scale: 1}) },
		Release: v.release,
	})
}

type viewer struct {
	addr  string
	dial  transport.DialConfig
	out   chan proto.Message
	start time.Time

	// UI thread only.
	pressed map[uint16]bool
	mx, my  int32
	buttons uint8
}

func (v *viewer) status(text string) {
	winui.SetStatus("pikasplit — "+v.addr, text)
}

// send queues a message without ever blocking the UI thread: when the queue
// is full (network stalled), the message is dropped.
func (v *viewer) send(m proto.Message) {
	select {
	case v.out <- m:
	default:
	}
}

func (v *viewer) key(hid uint16, down bool) {
	if down {
		v.pressed[hid] = true
	} else {
		delete(v.pressed, hid)
	}
	v.send(&proto.Key{Code: hid, Down: down})
}

func (v *viewer) mouse(x, y int32, buttons uint8) {
	v.mx, v.my, v.buttons = x, y, buttons
	v.send(&proto.Mouse{X: x, Y: y, Buttons: buttons})
}

func (v *viewer) release() {
	for hid := range v.pressed {
		delete(v.pressed, hid)
		v.send(&proto.Key{Code: hid, Down: false})
	}
	if v.buttons != 0 {
		v.buttons = 0
		v.send(&proto.Mouse{X: v.mx, Y: v.my})
	}
}

func (v *viewer) pingLoop() {
	for range time.Tick(2 * time.Second) {
		v.send(&proto.Ping{Timestamp: uint64(time.Since(v.start).Microseconds())})
	}
}

// connectLoop owns the connection: it (re)dials the host, writes queued
// messages and reads replies. Messages queued while disconnected are
// dropped, except the last Resize which is replayed on reconnect.
func (v *viewer) connectLoop() {
	connected := make(chan *transport.Conn)
	lost := make(chan *transport.Conn)
	go func() {
		backoff := time.Second
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			c, err := transport.Dial(ctx, v.addr, v.dial)
			cancel()
			if err != nil {
				v.status(fmt.Sprintf("Mac unreachable (%v), retrying…", err))
				time.Sleep(backoff)
				backoff = min(2*backoff, 10*time.Second)
				continue
			}
			backoff = time.Second
			connected <- c
			v.readLoop(c)
			lost <- c
		}
	}()

	var conn *transport.Conn
	var lastResize *proto.Resize
	for {
		select {
		case c := <-connected:
			conn = c
			v.status(fmt.Sprintf("Connected to %s — click here to control the Mac · Ctrl+Alt+End to release", c.PeerName))
			if lastResize != nil {
				conn.Send(lastResize)
			}
		case c := <-lost:
			if conn == c {
				conn = nil
				v.status("Connection lost, reconnecting…")
			}
		case m := <-v.out:
			if r, ok := m.(*proto.Resize); ok {
				lastResize = r
			}
			if conn != nil {
				if err := conn.Send(m); err != nil {
					conn.Close() // readLoop fails and reports it as lost
				}
			}
		}
	}
}

func (v *viewer) readLoop(c *transport.Conn) {
	defer c.Close()
	for {
		m, err := c.Recv()
		if err != nil {
			return
		}
		if p, ok := m.(*proto.Pong); ok {
			rtt := time.Since(v.start) - time.Duration(p.Timestamp)*time.Microsecond
			v.status(fmt.Sprintf("Connected to %s (RTT %.1f ms) — click here to control the Mac · Ctrl+Alt+End to release",
				c.PeerName, float64(rtt.Microseconds())/1000))
		}
	}
}
