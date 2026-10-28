package transport

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Store is a persistent set of trusted peer fingerprints, one per line:
//
//	<hex fingerprint> <name>
//
// The host uses it for authorized viewers, the viewer for known hosts.
type Store struct {
	path string

	mu    sync.Mutex
	peers map[Fingerprint]string
}

// OpenStore loads path; a missing file is an empty store. An empty path
// gives an in-memory store.
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, peers: make(map[Fingerprint]string)}
	if path == "" {
		return s, nil
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		hexFP, name, _ := strings.Cut(line, " ")
		fp, err := ParseFingerprint(hexFP)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		s.peers[fp] = strings.TrimSpace(name)
	}
	return s, sc.Err()
}

// Lookup returns the name recorded for fp.
func (s *Store) Lookup(fp Fingerprint) (name string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name, ok = s.peers[fp]
	return name, ok
}

// Add records fp and persists the store.
func (s *Store) Add(fp Fingerprint, name string) error {
	name = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, name)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers[fp] = name
	if s.path == "" {
		return nil
	}
	var b strings.Builder
	for p, n := range s.peers {
		fmt.Fprintf(&b, "%s %s\n", p, n)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
