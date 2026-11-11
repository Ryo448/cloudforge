package supervisor

import (
	"bytes"
	"sync"
)

// exec.Cmd owns the copy goroutines and waits for them; final partial lines are flushed after Wait.
type streamWriter struct {
	mu      sync.Mutex
	book    *Logbook
	stream  string
	partial []byte
}

func (w *streamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	w.partial = append(w.partial, p...)
	for {
		index := bytes.IndexByte(w.partial, '\n')
		if index < 0 {
			if len(w.partial) > 16384 {
				w.book.Add(w.stream, string(w.partial[:16384])+" [linha dividida]")
				w.partial = w.partial[16384:]
				continue
			}
			break
		}
		w.book.Add(w.stream, string(bytes.TrimSuffix(w.partial[:index], []byte{'\r'})))
		w.partial = w.partial[index+1:]
	}
	return n, nil
}
func (w *streamWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.partial) > 0 {
		w.book.Add(w.stream, string(w.partial))
		w.partial = nil
	}
}
