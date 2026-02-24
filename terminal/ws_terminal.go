package terminal

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// Message types for the terminal WebSocket protocol.
const (
	MsgCreate = "create"
	MsgInput  = "input"
	MsgResize = "resize"
	MsgOutput = "output"
	MsgExit   = "exit"
	MsgError  = "error"
	MsgPing   = "ping"
	MsgPong   = "pong"
)

type clientMsg struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type serverMsg struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

type createData struct {
	ID string `json:"id"`
}

type inputData struct {
	ID   string `json:"id"`
	Data string `json:"data"`
}

type resizeData struct {
	ID   string `json:"id"`
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

// ServeWS handles a WebSocket connection for terminal I/O.
func (m *Manager) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("terminal ws accept: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Protect all WebSocket writes with a mutex — reader loop and PTY goroutine
	// may both write concurrently otherwise.
	var writeMu sync.Mutex
	send := func(msg serverMsg) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = wsjson.Write(ctx, conn, msg)
	}

	var activeSess *Session

	for {
		var msg clientMsg
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return
		}

		switch msg.Type {
		case MsgCreate:
			var d createData
			if err := json.Unmarshal(msg.Data, &d); err != nil || d.ID == "" {
				send(serverMsg{Type: MsgError, Data: "create requires id"})
				continue
			}
			// Close any existing session before creating a new one (prevent leaks).
			if activeSess != nil {
				_ = activeSess.Close()
				activeSess = nil
			}
			sess, err := m.Create(d.ID)
			if err != nil {
				send(serverMsg{Type: MsgError, Data: err.Error()})
				continue
			}
			activeSess = sess
			send(serverMsg{Type: MsgCreate, Data: map[string]string{"id": d.ID, "status": "ok"}})

			// Stream PTY output to client via the mutex-protected send.
			go func() {
				buf := make([]byte, 4096)
				for {
					n, err := sess.Read(buf)
					if n > 0 {
						send(serverMsg{Type: MsgOutput, Data: string(buf[:n])})
					}
					if err != nil {
						send(serverMsg{Type: MsgExit})
						cancel()
						return
					}
				}
			}()

		case MsgInput:
			var d inputData
			if err := json.Unmarshal(msg.Data, &d); err != nil {
				continue
			}
			if activeSess != nil {
				_ = activeSess.Write([]byte(d.Data))
			} else if s, ok := m.Get(d.ID); ok {
				_ = s.Write([]byte(d.Data))
			}

		case MsgResize:
			var d resizeData
			if err := json.Unmarshal(msg.Data, &d); err != nil {
				continue
			}
			if activeSess != nil {
				_ = activeSess.Resize(d.Rows, d.Cols)
			} else if s, ok := m.Get(d.ID); ok {
				_ = s.Resize(d.Rows, d.Cols)
			}

		case MsgPing:
			send(serverMsg{Type: MsgPong})
		}
	}
}
