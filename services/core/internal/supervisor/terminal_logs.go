package supervisor

import "bytes"

type terminalEscape uint8

const (
	terminalText terminalEscape = iota
	terminalESC
	terminalESCIntermediate
	terminalCSI
	terminalString
	terminalStringEnd
)

// ConPTY emits screen updates and append-only output on the same stream.
// Keep lines that look like appended text. Only the terminal session receives
// the original stream, including cursor and screen updates.
// exec.Cmd serializes Write calls and completes them before Flush.
type terminalLogWriter struct {
	book *Logbook
	line []byte

	escape terminalEscape
	params [32]byte
	length int

	started        bool
	alternate      bool
	redraw         bool
	discardLine    bool
	eraseTail      bool
	hasText        bool
	carriageReturn bool
}

func (w *terminalLogWriter) Write(p []byte) (int, error) {
	for _, b := range p {
		switch w.escape {
		case terminalText:
			switch b {
			case '\x1b':
				w.escape = terminalESC
			case '\n':
				w.finishLine()
			case '\r':
				w.carriageReturn = true
			case '\b':
				w.markRedraw()
			default:
				if b == '\t' || b >= ' ' && b != '\x7f' {
					if w.carriageReturn {
						w.markRedraw()
						w.carriageReturn = false
					}
					w.started = true
					w.hasText = true
					if !w.alternate && !w.discardLine && len(w.line) <= 16384 {
						w.line = append(w.line, b)
					}
				}
			}
		case terminalESC:
			switch b {
			case '[':
				w.escape, w.length = terminalCSI, 0
			case ']', 'P', 'X', '^', '_':
				w.escape = terminalString
			default:
				w.escape = terminalText
				if b >= ' ' && b <= '/' {
					w.escape = terminalESCIntermediate
				}
			}
		case terminalESCIntermediate:
			if b >= '0' {
				w.escape = terminalText
			}
		case terminalCSI:
			switch {
			case b >= '@' && b <= '~':
				w.control(b, string(w.params[:w.length]))
				w.escape = terminalText
			case b == '\x1b':
				w.escape = terminalESC
			case w.length < len(w.params):
				w.params[w.length] = b
				w.length++
			}
		case terminalString:
			switch b {
			case '\a':
				w.escape = terminalText
			case '\x1b':
				w.escape = terminalStringEnd
			}
		case terminalStringEnd:
			w.escape = terminalString
			if b == '\\' || b == '\a' {
				w.escape = terminalText
			}
		}
	}
	return len(p), nil
}

func (w *terminalLogWriter) control(command byte, params string) {
	switch command {
	case 'h', 'l':
		switch params {
		case "?47", "?1047", "?1049":
			w.alternate = command == 'h'
			w.resetLine()
			// Leaving the alternate screen can repaint the primary buffer.
			w.redraw = !w.alternate
		case "?25":
			if command == 'h' && !w.alternate {
				w.discardLine = w.hasText && (w.discardLine || w.redraw && w.eraseTail)
				w.redraw = false
			}
		}
	case 'H', 'f':
		// ConPTY homes the cursor before the first output, even for plain logs.
		if w.started || params != "" && params != "1;1" {
			w.markRedraw()
		}
	case 'J':
		if w.started {
			w.redraw = true
		}
	case 'K':
		if params == "" || params == "0" {
			// ConPTY also clears to the right of ordinary lines.
			w.eraseTail = true
		} else {
			w.markRedraw()
		}
	case 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'X', 'd', 'e', 'a', '`', '@', 'P', 'L', 'M', 'u':
		w.markRedraw()
	}
}

func (w *terminalLogWriter) markRedraw() {
	w.redraw, w.discardLine = true, true
}

func (w *terminalLogWriter) finishLine() {
	if !w.alternate && !w.discardLine && !(w.redraw && w.eraseTail) && len(bytes.TrimSpace(w.line)) > 0 {
		w.book.Add("stdout", string(w.line))
		w.redraw = false
	}
	w.resetLine()
}

func (w *terminalLogWriter) resetLine() {
	w.line = w.line[:0]
	w.discardLine, w.eraseTail, w.carriageReturn, w.hasText = false, false, false, false
}

func (w *terminalLogWriter) Flush() {
	w.finishLine()
}
