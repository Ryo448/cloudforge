package logging

import (
	"os"
	"sync"
)

type Rotating struct {
	mu   sync.Mutex
	path string
}

func New(path string) *Rotating { return &Rotating{path: path} }
func (r *Rotating) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if info, e := os.Stat(r.path); e == nil && info.Size() > 2*1024*1024 {
		_ = os.Remove(r.path + ".1")
		_ = os.Rename(r.path, r.path+".1")
	}
	f, e := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if e != nil {
		return 0, e
	}
	defer f.Close()
	return f.Write(p)
}
