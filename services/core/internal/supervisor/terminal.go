package supervisor

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"sync"
)

type terminalMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

type terminalSession struct {
	mu      sync.Mutex
	input   io.WriteCloser
	buffer  []byte
	clients map[chan []byte]struct{}
	closed  bool
}

func newTerminalSession() *terminalSession {
	return &terminalSession{clients: make(map[chan []byte]struct{})}
}

func (t *terminalSession) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buffer = append(t.buffer, p...)
	if len(t.buffer) > 1024*1024 {
		t.buffer = append([]byte(nil), t.buffer[len(t.buffer)-1024*1024:]...)
	}
	for ch := range t.clients {
		data := append([]byte(nil), p...)
		select {
		case ch <- data:
		default:
			close(ch)
			delete(t.clients, ch)
		}
	}
	return len(p), nil
}

func (t *terminalSession) subscribe() (chan []byte, []byte, func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ch := make(chan []byte, 64)
	if t.closed {
		close(ch)
	} else {
		t.clients[ch] = struct{}{}
	}
	return ch, append([]byte(nil), t.buffer...), func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if _, ok := t.clients[ch]; ok {
			delete(t.clients, ch)
			close(ch)
		}
	}
}

func (t *terminalSession) send(message terminalMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.input == nil {
		return errors.New("terminal não está ativo")
	}
	if runtime.GOOS != "windows" {
		if message.Type == "resize" {
			return nil
		}
		data, err := base64.StdEncoding.DecodeString(message.Data)
		if err != nil {
			return err
		}
		_, err = t.input.Write(data)
		return err
	}
	return json.NewEncoder(t.input).Encode(message)
}

func (t *terminalSession) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	if t.input != nil {
		_ = t.input.Close()
	}
	for ch := range t.clients {
		close(ch)
		delete(t.clients, ch)
	}
}

func (s *Supervisor) Terminal(id string) (chan []byte, []byte, func(), error) {
	j, err := s.get(id)
	if err != nil {
		return nil, nil, nil, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.p.TerminalEnabled {
		return nil, nil, nil, errors.New("terminal desativado")
	}
	if j.terminal == nil {
		return nil, nil, nil, errors.New("inicie o projeto para abrir o terminal")
	}
	ch, history, unsubscribe := j.terminal.subscribe()
	return ch, history, unsubscribe, nil
}

func (s *Supervisor) TerminalInput(id string, data string) error {
	if len(data) > 65536 {
		return errors.New("entrada muito grande")
	}
	j, err := s.get(id)
	if err != nil {
		return err
	}
	j.mu.Lock()
	terminal := j.terminal
	j.mu.Unlock()
	if terminal == nil {
		return errors.New("terminal não está ativo")
	}
	return terminal.send(terminalMessage{Type: "input", Data: base64.StdEncoding.EncodeToString([]byte(data))})
}

func (s *Supervisor) TerminalResize(id string, cols, rows int) error {
	if cols < 20 || cols > 500 || rows < 5 || rows > 200 {
		return errors.New("tamanho inválido")
	}
	j, err := s.get(id)
	if err != nil {
		return err
	}
	j.mu.Lock()
	terminal := j.terminal
	j.mu.Unlock()
	if terminal == nil {
		return errors.New("terminal não está ativo")
	}
	return terminal.send(terminalMessage{Type: "resize", Cols: cols, Rows: rows})
}
