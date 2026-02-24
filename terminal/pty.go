package terminal

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// Session represents an active PTY session.
type Session struct {
	id   string
	ptmx *os.File
	cmd  *exec.Cmd
	mu   sync.Mutex
	done chan struct{}
}

// Manager manages PTY sessions.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewManager creates a new terminal manager.
func NewManager() *Manager {
	return &Manager{sessions: make(map[string]*Session)}
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
	}

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

// Read reads output from the PTY into a buffer.
// Returns (n, io.EOF) when session ends.
func (s *Session) Read(buf []byte) (int, error) {
	return s.ptmx.Read(buf)
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

// ReadAll is a helper to pipe PTY output to a writer until done.
func (s *Session) ReadAll(w io.Writer) {
	buf := make([]byte, 4096)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}
