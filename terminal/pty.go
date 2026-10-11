package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
)

const (
	// scrollbackCap is how much recent PTY output is kept for replay on reattach.
	scrollbackCap = 256 << 10
	// detachedTTL is how long a session without a client survives before it is killed.
	detachedTTL = 24 * time.Hour
)

// scrollback keeps the newest scrollbackCap bytes of PTY output.
type scrollback struct {
	data []byte
}

func (sb *scrollback) write(p []byte) {
	sb.data = append(sb.data, p...)
	if over := len(sb.data) - scrollbackCap; over > 0 {
		sb.data = append(sb.data[:0], sb.data[over:]...)
	}
}

func (sb *scrollback) bytes() []byte {
	return append([]byte(nil), sb.data...)
}

// Session represents an active PTY session.
type Session struct {
	id   string
	ptmx *os.File
	cmd  *exec.Cmd
	mu   sync.Mutex // serializes writes to the PTY
	done chan struct{}

	outMu        sync.Mutex // guards the fields below
	buf          scrollback
	sub          func([]byte)
	subGen       uint64
	lastDetached time.Time
	readDone     chan struct{} // closed when the PTY output has been fully read
}

// Manager manages PTY sessions.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewManager creates a new terminal manager and starts reaping sessions that
// have had no client for detachedTTL.
func NewManager() *Manager {
	m := &Manager{sessions: make(map[string]*Session)}
	go func() {
		for range time.Tick(time.Minute) {
			m.reapStale(time.Now(), detachedTTL)
		}
	}()
	return m
}

// reapStale kills sessions that have had no attached client since before now-ttl.
func (m *Manager) reapStale(now time.Time, ttl time.Duration) {
	m.mu.RLock()
	var stale []*Session
	for _, s := range m.sessions {
		s.outMu.Lock()
		if s.sub == nil && now.Sub(s.lastDetached) > ttl {
			stale = append(stale, s)
		}
		s.outMu.Unlock()
	}
	m.mu.RUnlock()
	for _, s := range stale {
		_ = s.Close()
	}
}

// Create starts a new PTY session with the user's shell.
func (m *Manager) Create(id string) (*Session, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("start pty: %w", err)
	}

	sess := &Session{
		id:   id,
		ptmx: ptmx,
		cmd:  cmd,
		done: make(chan struct{}),

		lastDetached: time.Now(),
		readDone:     make(chan struct{}),
	}
	go sess.pump()

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	// Watch for process exit
	go func() {
		_ = cmd.Wait()
		close(sess.done)
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
	}()

	return sess, nil
}

// Get returns a session by ID.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

// Write sends input to the PTY.
func (s *Session) Write(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.ptmx.Write(data)
	return err
}

// pump is the only reader of the PTY: it records output in the scrollback and
// forwards it to the attached subscriber, so output produced while no client
// is connected is not lost.
func (s *Session) pump() {
	defer close(s.readDone)
	buf := make([]byte, 4096)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			s.outMu.Lock()
			s.buf.write(buf[:n])
			if s.sub != nil {
				s.sub(buf[:n])
			}
			s.outMu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// Attach registers sub for live output and returns the scrollback so far. The
// replay and the subscription are atomic: no output is lost or duplicated. A new
// Attach replaces the previous subscriber. Call detach when the client goes away.
func (s *Session) Attach(sub func([]byte)) (replay []byte, detach func()) {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	s.subGen++
	gen := s.subGen
	s.sub = sub
	return s.buf.bytes(), func() {
		s.outMu.Lock()
		defer s.outMu.Unlock()
		if s.subGen == gen {
			s.sub = nil
			s.lastDetached = time.Now()
		}
	}
}

// Exited returns a channel closed once the PTY output has been fully read.
func (s *Session) Exited() <-chan struct{} {
	return s.readDone
}

// Resize resizes the PTY window.
func (s *Session) Resize(rows, cols uint16) error {
	return pty.Setsize(s.ptmx, &pty.Winsize{Rows: rows, Cols: cols})
}

// Done returns a channel closed when the session exits.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// Close kills the session.
func (s *Session) Close() error {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return s.ptmx.Close()
}
