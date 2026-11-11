package supervisor

import (
	"bufio"
	"cloudforge/core/internal/model"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Logbook struct {
	mu    sync.Mutex
	path  string
	lines []model.Log
	seq   uint64
}

func NewLogbook(dir, id string) *Logbook {
	_ = os.MkdirAll(dir, 0700)
	b := &Logbook{path: filepath.Join(dir, id+".jsonl"), lines: []model.Log{}}
	if f, e := os.Open(b.path); e == nil {
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			var l model.Log
			if json.Unmarshal(scanner.Bytes(), &l) == nil {
				b.lines = append(b.lines, l)
				if l.Seq > b.seq {
					b.seq = l.Seq
				}
			}
		}
		f.Close()
		if len(b.lines) > 1000 {
			b.lines = b.lines[len(b.lines)-1000:]
		}
	}
	return b
}
func (b *Logbook) Add(stream, message string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(message) > 16384 {
		message = message[:16384] + " [mensagem truncada]"
	}
	b.seq++
	l := model.Log{Seq: b.seq, Time: time.Now().UTC(), Stream: stream, Message: message}
	b.lines = append(b.lines, l)
	if len(b.lines) > 1000 {
		b.lines = b.lines[len(b.lines)-1000:]
	}
	total := 0
	for i := len(b.lines) - 1; i >= 0; i-- {
		total += len(b.lines[i].Message)
		if total > 1024*1024 {
			b.lines = b.lines[i+1:]
			break
		}
	}
	if info, e := os.Stat(b.path); e == nil && info.Size() > 2*1024*1024 {
		_ = os.Remove(b.path + ".1")
		_ = os.Rename(b.path, b.path+".1")
	}
	if f, e := os.OpenFile(b.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600); e == nil {
		_ = json.NewEncoder(f).Encode(l)
		f.Close()
	}
}
func (b *Logbook) Lines() []model.Log {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]model.Log{}, b.lines...)
}

func (b *Logbook) Remove() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, path := range []string{b.path + ".1", b.path} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	b.lines = nil
	return nil
}
