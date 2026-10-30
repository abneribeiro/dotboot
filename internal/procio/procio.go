// Package procio tem os utilitários de leitura do output dos processos
// externos (yt-dlp, ffmpeg) usados pelo downloader e pelo motor.
package procio

import (
	"bytes"
	"sync"
)

// LineWriter é um io.Writer que entrega o output linha a linha a uma função.
// Corta tanto em "\n" como em "\r": as barras de progresso do ffmpeg
// reescrevem a mesma linha com "\r", e sem isto só se via o progresso quando
// a barra terminava.
type LineWriter struct {
	mu   sync.Mutex
	buf  []byte
	line func(string)
}

// NewLineWriter devolve um LineWriter que chama line para cada linha não
// vazia, sem o terminador.
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

// Flush entrega o que ficou sem terminador (a última linha de um processo
// que não acabou com "\n").
func (w *LineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.line(string(w.buf))
		w.buf = nil
	}
}

// TailBuffer guarda só os últimos Max bytes escritos: chega para classificar
// a linha final de erro sem acumular o progresso de uma playlist inteira.
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

// Tail devolve os últimos n bytes de s.
func Tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
