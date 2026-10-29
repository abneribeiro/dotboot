package procio

import (
	"slices"
	"testing"
)

func TestLineWriterCortaEmNovaLinhaERetorno(t *testing.T) {
	var got []string
	w := NewLineWriter(func(l string) { got = append(got, l) })

	// Escrito em pedaços que partem as linhas a meio, como num pipe real.
	for _, chunk := range []string{"[download] Dest", "ination: a.mp4\n 10%|█\r 55%", "|█████\r\n", "fim sem terminador"} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()

	want := []string{"[download] Destination: a.mp4", " 10%|█", " 55%|█████", "fim sem terminador"}
	if !slices.Equal(got, want) {
		t.Errorf("linhas = %q, esperado %q", got, want)
	}
}

func TestTailBufferGuardaSoOFim(t *testing.T) {
	tb := &TailBuffer{Max: 5}
	tb.Write([]byte("abcdef"))
	tb.Write([]byte("gh"))
	if got := tb.String(); got != "defgh" {
		t.Errorf("TailBuffer = %q, esperado defgh", got)
	}
	if got := Tail("abcdef", 3); got != "def" {
		t.Errorf("Tail = %q", got)
	}
}
