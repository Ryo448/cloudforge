package supervisor

import (
	"bytes"
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
