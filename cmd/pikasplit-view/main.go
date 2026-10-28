//go:build windows

// pikasplit-view runs on the PC: it shows the Mac's virtual display in a
// window and, while the window has focus, forwards keyboard and mouse input
// to the paired Mac.
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
	"sync"
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
	scale := flag.Int("scale", 0, "Mac display scale: 1, 2 (HiDPI), or 0 to follow the Windows display scaling")
	flag.Parse()
	if *addr == "" {
		flag.Usage()
		os.Exit(2)
	}
	if _, _, err := net.SplitHostPort(*addr); err != nil {
		*addr = net.JoinHostPort(*addr, strconv.Itoa(proto.DefaultPort))
	}
	if err := run(*addr, *pairCode, *pin, *name, *scale); err != nil {
		log.Fatal(err)
	}
}

func run(addr, pairCode, pin, name string, scale int) error {
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
	v.video = newVideoPipeline(
		func() { v.send(&proto.RequestKeyframe{}) },
		func(fps float64) { v.update(func(u *uiState) { u.fps = fps }) },
	)
	v.status("Connecting…")
	go v.connectLoop()
	go v.pingLoop()

	var lastW, lastH, lastScale int
	return winui.Run(winui.Events{
		Key:    v.key,
		Mouse:  v.mouse,
		Scroll: func(dx, dy int16) { v.send(&proto.Scroll{DX: dx, DY: dy}) },
		Resize: func(w, h, dpi int) {
			s := scale
			if s == 0 {
				// Windows at 150% scaling or more: the monitor is dense
				// enough for a HiDPI Mac display.
				s = 1
				if dpi >= 144 {
					s = 2
				}
			}
			// Restoring a minimized window reports the same size again;
			// resizing restarts the stream on the Mac, so skip it.
			if w == lastW && h == lastH && s == lastScale {
				return
			}
			lastW, lastH, lastScale = w, h, s
			v.send(&proto.Resize{W: uint16(w), H: uint16(h), Scale: uint8(s)})
		},
		Release: v.release,
	})
}

type viewer struct {
	addr  string
	dial  transport.DialConfig
	out   chan proto.Message
	start time.Time

	video *videoPipeline

	// UI thread only.
	pressed map[uint16]bool
	mx, my  int32
	buttons uint8

	uiMu sync.Mutex
	ui   uiState
}

// uiState is what the window shows: text when there is no video, and
// connection figures in the title bar.
type uiState struct {
	text string
	peer string
	rtt  time.Duration
	fps  float64
}

func (v *viewer) update(fn func(u *uiState)) {
	v.uiMu.Lock()
	fn(&v.ui)
	u := v.ui
	v.uiMu.Unlock()

	title := "pikasplit — " + v.addr
	if u.peer != "" {
		title = "pikasplit — " + u.peer
		if u.rtt > 0 {
			title += fmt.Sprintf(" · RTT %.1f ms", float64(u.rtt.Microseconds())/1000)
		}
		if u.fps > 0 {
			title += fmt.Sprintf(" · %.0f fps", u.fps)
		}
	}
	winui.SetStatus(title, u.text)
}

func (v *viewer) status(text string) { v.update(func(u *uiState) { u.text = text }) }

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
			v.update(func(u *uiState) {
				u.peer = c.PeerName
				u.text = "Connected, waiting for video — click here to control the Mac · Ctrl+Alt+End to release"
			})
			if lastResize != nil {
				conn.Send(lastResize)
			}
		case c := <-lost:
			if conn == c {
				conn = nil
				v.video.reset()
				v.update(func(u *uiState) {
					u.peer, u.rtt = "", 0
					u.text = "Connection lost, reconnecting…"
				})
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
		switch m := m.(type) {
		case *proto.VideoFrame:
			v.video.push(m)
		case *proto.Pong:
			rtt := time.Since(v.start) - time.Duration(m.Timestamp)*time.Microsecond
			v.update(func(u *uiState) { u.rtt = rtt })
		}
	}
}
