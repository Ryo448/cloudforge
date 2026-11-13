package supervisor

import (
	"bytes"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestTerminalDisconnectPreservesSessionAndHistory(t *testing.T) {
	terminal := newTerminalSession()
	first, history, disconnect := terminal.subscribe()
	if len(history) != 0 {
		t.Fatal("new terminal has history")
	}
	if _, err := terminal.Write([]byte("ready\r\n")); err != nil {
		t.Fatal(err)
	}
	if got := <-first; !bytes.Equal(got, []byte("ready\r\n")) {
		t.Fatalf("first client received %q", got)
	}
	disconnect()
	if _, err := terminal.Write([]byte("still running\r\n")); err != nil {
		t.Fatal(err)
	}
	second, history, disconnectSecond := terminal.subscribe()
	defer disconnectSecond()
	if !bytes.Equal(history, []byte("ready\r\nstill running\r\n")) {
		t.Fatalf("reconnected history is %q", history)
	}
	if _, err := terminal.Write([]byte("new frame")); err != nil {
		t.Fatal(err)
	}
	if got := <-second; !bytes.Equal(got, []byte("new frame")) {
		t.Fatalf("second client received %q", got)
	}
	terminal.close()
}

func TestTerminalRedrawsStayOutOfProjectLogs(t *testing.T) {
	book := NewLogbook(t.TempDir(), "project")
	terminal := newTerminalSession()
	logs := &terminalLogWriter{book: book}
	output := io.MultiWriter(terminal, logs)
	stream := strings.Join([]string{
		"\x1b[?9001h\x1b[?25l\x1b[2J\x1b[m\x1b[HApp iniciada\x1b]0;App\a\x1b[?25h\r\n",
		"\x1b[33mAviso normal\x1b[m\x1b[K\r\n",
		"\x1b[23X\x1b[38;2;139;209;0m\x1b[23CBanner da TUI\x1b[K\x1b[m\r\n",
		"  \x1b[38;2;142;152;168mSenha de criptografia\x1b[K\x1b[m\r\n",
		"\x1b[126C\r\n",
		"Falha na conexão\r\n",
		"\x1b[?1049h\x1b[2J\x1b[HQuadro da tela alternativa\r\n",
		"\x1b[?1049l\x1b[?25l\x1b[HApp iniciada\x1b[K\r\n\x1b[4;1H\x1b[?25h",
		"Conexão restabelecida\r\n",
		"Progresso 10%\rProgresso 20%\r\n",
		"App encerrada\x1b[?9001l",
	}, "")
	// Reads from ConPTY may split escape sequences, CRLF and UTF-8 anywhere.
	for _, b := range []byte(stream) {
		if _, err := output.Write([]byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	logs.Flush()

	_, history, unsubscribe := terminal.subscribe()
	defer unsubscribe()
	if string(history) != stream {
		t.Fatalf("terminal history differs from the original stream: %q", history)
	}
	var messages []string
	for _, line := range book.Lines() {
		messages = append(messages, line.Message)
	}
	want := []string{"App iniciada", "Aviso normal", "Falha na conexão", "Conexão restabelecida", "App encerrada"}
	if !slices.Equal(messages, want) {
		t.Fatalf("logged %q, want %q", messages, want)
	}
}
