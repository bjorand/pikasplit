// pikasplit-host runs on the Mac: it accepts a paired viewer and injects the
// keyboard and mouse input it receives.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/bjorand/pikasplit/internal/host"
	"github.com/bjorand/pikasplit/internal/keymap"
	"github.com/bjorand/pikasplit/internal/proto"
	"github.com/bjorand/pikasplit/internal/transport"
)

func main() {
	hostname, _ := os.Hostname()
	listen := flag.String("listen", ":"+strconv.Itoa(proto.DefaultPort), "address to listen on")
	pair := flag.Bool("pair", false, "open a 5 minute window to pair a new viewer")
	name := flag.String("name", hostname, "name shown to viewers")
	iso := flag.Bool("iso", true, "the Mac uses an ISO keyboard layout (most European layouts)")
	swap := flag.Bool("swap-ctrl-cmd", false, "PC Ctrl acts as Command, Windows key as Control")
	flag.Parse()

	if err := run(*listen, *pair, *name, keymap.Options{ISO: *iso, SwapCtrlCmd: *swap}); err != nil {
		log.Fatal(err)
	}
}

func run(listen string, pair bool, name string, keys keymap.Options) error {
	inj, err := newInjector()
	if err != nil {
		return err
	}

	dir, err := transport.ConfigDir()
	if err != nil {
		return err
	}
	id, err := transport.LoadOrCreateIdentity(dir, "pikasplit-host "+name)
	if err != nil {
		return err
	}
	viewers, err := transport.OpenStore(filepath.Join(dir, "viewers"))
	if err != nil {
		return err
	}
	h := &transport.Host{ID: id, Name: name, Viewers: viewers}

	ln, err := h.Listen(listen)
	if err != nil {
		return err
	}
	log.Printf("pikasplit-host %q listening on %s (code %s)", name, ln.Addr(), id.Fingerprint.ShortCode())

	if pair {
		pin, err := h.StartPairing(5 * time.Minute)
		if err != nil {
			return err
		}
		ip, port, _ := net.SplitHostPort(ln.Addr().String())
		ips := []string{ip}
		if a := net.ParseIP(ip); a == nil || a.IsUnspecified() {
			ips = localIPs()
		}
		fmt.Printf("\nPairing open for 5 minutes. On the PC:\n\n")
		for _, ip := range ips {
			fmt.Printf("  pikasplit-view -host %s -pair %s -pin %s\n",
				net.JoinHostPort(ip, port), id.Fingerprint.ShortCode(), pin)
		}
		fmt.Println()
	}

	srv := &server{inj: inj, keys: keymap.New(keys)}
	for {
		raw, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			c, err := h.Handshake(raw)
			if err != nil {
				log.Print(err)
				return
			}
			srv.serve(c)
		}()
	}
}

// server drives the injector from one viewer at a time; a new viewer takes
// over from the previous one.
type server struct {
	inj  host.Injector
	keys *keymap.Mapper

	mu      sync.Mutex // serializes injection
	current *transport.Conn
}

func (s *server) serve(c *transport.Conn) {
	log.Printf("viewer %q connected from %s", c.PeerName, c.RemoteAddr())
	s.mu.Lock()
	if s.current != nil {
		s.current.Close()
	}
	s.current = c
	s.mu.Unlock()

	sess := host.NewSession(s.inj, s.keys)
	defer func() {
		s.mu.Lock()
		sess.Release()
		if s.current == c {
			s.current = nil
		}
		s.mu.Unlock()
		c.Close()
	}()
	for {
		m, err := c.Recv()
		if err != nil {
			log.Printf("viewer %q disconnected: %v", c.PeerName, err)
			return
		}
		s.mu.Lock()
		reply := sess.Handle(m)
		s.mu.Unlock()
		if reply != nil {
			if err := c.Send(reply); err != nil {
				return
			}
		}
	}
}

func localIPs() []string {
	var ips []string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.To4() != nil {
			ips = append(ips, n.IP.String())
		}
	}
	if len(ips) == 0 {
		ips = []string{"<mac-ip>"}
	}
	return ips
}
