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
	// Adopt asks the server to hand over a detached session (e.g. after the
	// browser was closed) before creating a new one with ID. The reply carries
	// the id actually in use.
	Adopt bool `json:"adopt,omitempty"`
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
	var detach func()
	// Leave the PTY running when the client goes away so it can be reattached.
	// cancel first so a blocked send cannot stall detach.
	defer func() {
		cancel()
		if detach != nil {
			detach()
		}
	}()

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
			if activeSess != nil {
				detach()
				if activeSess.id != d.ID {
					// Switching to a different session on one connection: close the
					// old one to prevent leaks.
					_ = activeSess.Close()
				}
				activeSess, detach = nil, nil
			}

			// Stream PTY output to the client via the mutex-protected send. Attach
			// returns the scrollback atomically with the subscription, so the replay is
			// followed by live output with no gap or duplicate. The gate holds live
			// output back until the replay has been sent.
			var gate sync.Mutex
			onOutput := func(b []byte) {
				gate.Lock()
				gate.Unlock()
				send(serverMsg{Type: MsgOutput, Data: string(b)})
			}
			attach := func(sess *Session, status string) {
				activeSess = sess
				gate.Lock()
				var replay []byte
				replay, detach = sess.Attach(onOutput)
				send(serverMsg{Type: MsgCreate, Data: map[string]string{"id": sess.id, "status": status}})
				if len(replay) > 0 {
					send(serverMsg{Type: MsgOutput, Data: string(replay)})
				}
				gate.Unlock()
			}

			switch existing, ok := m.Get(d.ID); {
			case d.Adopt && m.Adopt(func(s *Session) { attach(s, "attached") }):
				// A detached session was handed over; attach already ran.
			case ok:
				attach(existing, "attached")
			default:
				sess, err := m.Create(d.ID)
				if err != nil {
					send(serverMsg{Type: MsgError, Data: err.Error()})
					continue
				}
				attach(sess, "ok")
			}
			sess := activeSess

			go func() {
				select {
				case <-sess.Exited():
					send(serverMsg{Type: MsgExit})
					cancel()
				case <-ctx.Done():
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
