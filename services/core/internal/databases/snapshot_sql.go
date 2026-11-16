package databases

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
)

var pgRestrictLine = regexp.MustCompile("(?m)^\\\\restrict ([A-Za-z0-9]+)\\r?$")

func restrictedSQL(source *bufio.Reader) (io.ReadCloser, error) {
	key, err := randomHex(24)
	if err != nil {
		return nil, err
	}
	head, _ := source.Peek(source.Size())
	header := pgRestrictLine.FindSubmatch(head)
	out, w := io.Pipe()
	go func() {
		if _, err := fmt.Fprintf(w, "\\restrict %s\n", key); err != nil {
			_ = w.CloseWithError(err)
			return
		}
		if len(header) == 0 {
			_, err := io.Copy(w, source)
			_ = w.CloseWithError(err)
			return
		}
		oldKey := append([]byte(nil), header[1]...)
		restrict := append([]byte("\\restrict "), oldKey...)
		unrestrict := append([]byte("\\unrestrict "), oldKey...)
		lineStart := true
		for {
			line, err := source.ReadSlice('\n')
			command := bytes.TrimRight(line, "\r\n")
			if !lineStart || errors.Is(err, bufio.ErrBufferFull) ||
				(!bytes.Equal(command, restrict) && !bytes.Equal(command, unrestrict)) {
				if _, writeErr := w.Write(line); writeErr != nil {
					_ = w.CloseWithError(writeErr)
					return
				}
			}
			lineStart = !errors.Is(err, bufio.ErrBufferFull)
			if err == nil || !lineStart {
				continue
			}
			if errors.Is(err, io.EOF) {
				err = nil
			}
			_ = w.CloseWithError(err)
			return
		}
	}()
	return out, nil
}
