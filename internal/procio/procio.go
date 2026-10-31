// Package procio reads the output of external processes (yt-dlp, ffmpeg).
package procio

import (
	"bytes"
	"sync"
)

// LineWriter is an io.Writer that hands its input to a function line by line.
// It splits on both "\n" and "\r": ffmpeg rewrites its progress line with
// "\r", and without this the progress would only show once it finished.
type LineWriter struct {
	mu   sync.Mutex
	buf  []byte
	line func(string)
}

// NewLineWriter returns a LineWriter that calls line for every non-empty line,
// without its terminator.
func NewLineWriter(line func(string)) *LineWriter {
	return &LineWriter{line: line}
}

func (w *LineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexAny(w.buf, "\r\n")
		if i < 0 {
			break
		}
		if i > 0 {
			w.line(string(w.buf[:i]))
		}
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

// Flush hands over what is left without a terminator (the last line of a
// process that did not end with "\n").
func (w *LineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.line(string(w.buf))
		w.buf = nil
	}
}

// TailBuffer keeps only the last Max bytes written: enough to classify the
// final error line without holding a whole playlist's progress.
type TailBuffer struct {
	mu  sync.Mutex
	buf []byte
	Max int
}

func (t *TailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.Max {
		t.buf = t.buf[len(t.buf)-t.Max:]
	}
	return len(p), nil
}

func (t *TailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// Tail returns the last n bytes of s.
func Tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
