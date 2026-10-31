package procio

import (
	"slices"
	"testing"
)

func TestLineWriterSplitsOnNewlineAndCarriageReturn(t *testing.T) {
	var got []string
	w := NewLineWriter(func(l string) { got = append(got, l) })

	// Written in chunks that break lines in the middle, as a real pipe does.
	for _, chunk := range []string{"[download] Dest", "ination: a.mp4\n 10%|█\r 55%", "|█████\r\n", "end without terminator"} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()

	want := []string{"[download] Destination: a.mp4", " 10%|█", " 55%|█████", "end without terminator"}
	if !slices.Equal(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestTailBufferKeepsOnlyTheEnd(t *testing.T) {
	tb := &TailBuffer{Max: 5}
	tb.Write([]byte("abcdef"))
	tb.Write([]byte("gh"))
	if got := tb.String(); got != "defgh" {
		t.Errorf("TailBuffer = %q, want defgh", got)
	}
	if got := Tail("abcdef", 3); got != "def" {
		t.Errorf("Tail = %q", got)
	}
}
