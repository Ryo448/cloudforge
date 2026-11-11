package api

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

func (s *Server) terminal(w http.ResponseWriter, r *http.Request, id string) {
	updates, history, unsubscribe, err := s.Supervisor.Terminal(id)
	if err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	defer unsubscribe()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ws.SetReadLimit(70 * 1024)
	if len(history) > 0 {
		if ws.WriteMessage(websocket.BinaryMessage, history) != nil {
			return
		}
	}
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			var message struct {
				Type string `json:"type"`
				Data string `json:"data"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if ws.ReadJSON(&message) != nil {
				return
			}
			switch message.Type {
			case "input":
				if err := s.Supervisor.TerminalInput(id, message.Data); err != nil {
					return
				}
			case "resize":
				if err := s.Supervisor.TerminalResize(id, message.Cols, message.Rows); err != nil {
					return
				}
			default:
				return
			}
		}
	}()
	for {
		select {
		case data, ok := <-updates:
			if !ok {
				_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "processo encerrado"), time.Now().Add(time.Second))
				return
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, data); err != nil {
				return
			}
		case <-readerDone:
			return
		case <-r.Context().Done():
			return
		}
	}
}
