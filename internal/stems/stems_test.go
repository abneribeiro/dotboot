package stems

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLookupPreset(t *testing.T) {
	p, err := LookupPreset(DefaultPreset)
	if err != nil {
		t.Fatalf("o preset por omissão tem de existir: %v", err)
	}
	// O pedido de origem: separar guitarra, baixo e vozes.
	for _, want := range []string{"guitarra", "baixo", "vozes"} {
		if !slices.Contains(p.Stems, want) {
			t.Errorf("o preset por omissão devia ter %q: %v", want, p.Stems)
		}
	}

	if _, err := LookupPreset("12s"); err == nil || !strings.Contains(err.Error(), "6s") {
		t.Errorf("LookupPreset(12s) = %v, esperado um erro que liste os modelos", err)
	}
}

// Todas as pistas de todos os presets têm de ter tradução: é pelo nome em
// português que o Collect as reconhece.
func TestPresetsUsamNomesConhecidos(t *testing.T) {
	known := make(map[string]bool)
	for _, pt := range stemNames {
		known[pt] = true
	}
	for _, p := range Presets() {
		for _, s := range p.Stems {
			if !known[s] {
				t.Errorf("preset %s: pista %q sem tradução em stemNames", p.Name, s)
			}
		}
	}
}

func TestSeparatorArgs(t *testing.T) {
	p, _ := LookupPreset("6s")
	got := SeparatorArgs("/w/música.wav", "/w/stems", "/models", p)

	if got[0] != "/w/música.wav" {
		t.Errorf("a entrada tem de ser o primeiro argumento: %v", got)
	}
	for flag, want := range map[string]string{
		"--model_filename": "htdemucs_6s.yaml",
		"--model_file_dir": "/models",
		"--output_dir":     "/w/stems",
		"--output_format":  "WAV",
	} {
		if v := argValue(got, flag); v != want {
			t.Errorf("%s = %q, esperado %q", flag, v, want)
		}
	}

	var names map[string]string
	if err := json.Unmarshal([]byte(argValue(got, "--custom_output_names")), &names); err != nil {
		t.Fatalf("--custom_output_names não é JSON válido: %v", err)
	}
	if names["Guitar"] != "guitarra" || names["Vocals"] != "vozes" || names["Instrumental"] != "instrumental" {
		t.Errorf("--custom_output_names = %v", names)
	}
}

func TestStemFromFilename(t *testing.T) {
	tests := []struct {
		name   string
		want   string
		wantOK bool
	}{
		{"guitarra.wav", "guitarra", true},
		{"vozes.wav", "vozes", true},
		// O formato por omissão do audio-separator.
		{"musica_(Vocals)_htdemucs_6s.wav", "vozes", true},
		{"musica_(Instrumental)_model_bs_roformer_ep_317_sdr_12.9755.wav", "instrumental", true},
		// Um título com parênteses próprios não engana: conta o último.
		{"Song_(Live)_(Bass)_htdemucs.wav", "baixo", true},
		{"input.wav", "", false},
		{"musica_(Kazoo)_x.wav", "", false},
	}
	for _, tt := range tests {
		got, ok := stemFromFilename(tt.name)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("stemFromFilename(%q) = (%q, %v), esperado (%q, %v)", tt.name, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestCollect(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"vozes.wav", "musica_(Drums)_htdemucs.wav", "notas.txt", "input.wav"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Collect(dir)
	if err != nil {
		t.Fatalf("Collect falhou: %v", err)
	}
	if len(got) != 2 || got["vozes"] == "" || got["bateria"] == "" {
		t.Errorf("Collect = %v, esperado vozes e bateria", got)
	}

	if _, err := Collect(t.TempDir()); err == nil {
		t.Error("Collect devia falhar numa pasta sem pistas")
	}
}

func TestEncodeArgsPorFormato(t *testing.T) {
	tests := []struct {
		format    string
		wantCodec string
	}{
		{"mp3", "libmp3lame"},
		{"flac", "flac"},
		{"wav", "pcm_s16le"},
	}
	for _, tt := range tests {
		got := EncodeArgs("/w/guitarra.wav", "/out/guitarra."+tt.format, tt.format, "Música (guitarra)")
		if v := argValue(got, "-c:a"); v != tt.wantCodec {
			t.Errorf("%s: -c:a = %q, esperado %q", tt.format, v, tt.wantCodec)
		}
		if v := argValue(got, "-metadata"); v != "title=Música (guitarra)" {
			t.Errorf("%s: -metadata = %q", tt.format, v)
		}
		if got[len(got)-1] != "/out/guitarra."+tt.format {
			t.Errorf("%s: a saída tem de ser o último argumento: %v", tt.format, got)
		}
	}
	if v := argValue(EncodeArgs("a", "b", "mp3", "t"), "-b:a"); v != "320k" {
		t.Errorf("mp3 devia sair a 320k, saiu %q", v)
	}
}

// Sem normalize=0, o amix divide cada pista pelo número de entradas e a
// mistura sairia muito mais baixa do que a música original.
func TestMixArgs(t *testing.T) {
	tests := []struct {
		name       string
		tracks     []Track
		wantInputs int
		wantFilter string // -filter_complex
		wantAF     string // -af
	}{
		{
			name:       "volumes originais mantêm o volume da música",
			tracks:     []Track{{"/w/vozes.wav", 1}, {"/w/baixo.wav", 1}, {"/w/bateria.wav", 1}},
			wantInputs: 3,
			wantFilter: "[0:a][1:a][2:a]amix=inputs=3:normalize=0",
		},
		{
			name:       "o volume do estúdio entra na mistura",
			tracks:     []Track{{"/w/baixo.wav", 0.5}, {"/w/bateria.wav", 1}},
			wantInputs: 2,
			wantFilter: "[0:a]volume=0.500[g0];[g0][1:a]amix=inputs=2:normalize=0",
		},
		{
			name:       "uma pista sozinha não passa pelo amix",
			tracks:     []Track{{"/w/baixo.wav", 1}},
			wantInputs: 1,
		},
		{
			name:       "uma pista com volume",
			tracks:     []Track{{"/w/baixo.wav", 1.25}},
			wantInputs: 1,
			wantAF:     "volume=1.250",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MixArgs(tt.tracks, "/out/mix.wav", "wav", "t")
			if n := strings.Count(strings.Join(got, " "), "-i "); n != tt.wantInputs {
				t.Errorf("esperava %d entradas, há %d: %v", tt.wantInputs, n, got)
			}
			if v := argValue(got, "-filter_complex"); v != tt.wantFilter {
				t.Errorf("-filter_complex = %q, quero %q", v, tt.wantFilter)
			}
			if v := argValue(got, "-af"); v != tt.wantAF {
				t.Errorf("-af = %q, quero %q", v, tt.wantAF)
			}
			if v := argValue(got, "-c:a"); v != "pcm_s16le" {
				t.Errorf("-c:a = %q, quero pcm_s16le para wav", v)
			}
		})
	}
}

func TestRenderValida(t *testing.T) {
	ctx := context.Background()
	if err := Render(ctx, nil, "/out/x.wav", "wav", "t"); err == nil {
		t.Error("Render sem pistas devia falhar")
	}
	if err := Render(ctx, []Track{{"/w/a.wav", 1}}, "/out/x.ogg", "ogg", "t"); err == nil {
		t.Error("Render com um formato desconhecido devia falhar")
	}
}

// pcm escreve amostras s16le.
func pcm(samples ...int16) []byte {
	out := make([]byte, 0, 2*len(samples))
	for _, v := range samples {
		out = binary.LittleEndian.AppendUint16(out, uint16(v))
	}
	return out
}

func TestComputePeaks(t *testing.T) {
	tests := []struct {
		name     string
		pcm      []byte
		n        int
		want     []float64
		duration float64
	}{
		{"pico por intervalo, com negativos", pcm(0, 16384, -32768, 100), 2, []float64{0.5, 1}, 0.5},
		{"menos amostras do que pontos", pcm(8192, -8192), 10, []float64{0.25, 0.25}, 0.25},
		{"silêncio", pcm(0, 0, 0, 0), 2, []float64{0, 0}, 0.5},
		{"vazio", nil, 4, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputePeaks(tt.pcm, 8, tt.n)
			if !slices.Equal(got.Values, tt.want) {
				t.Errorf("Values = %v, quero %v", got.Values, tt.want)
			}
			if got.Duration != tt.duration {
				t.Errorf("Duration = %v, quero %v", got.Duration, tt.duration)
			}
		})
	}
}

func TestToWAVArgs(t *testing.T) {
	full := ToWAVArgs("/m/song.mp3", "/w/in.wav", "", "")
	if slices.Contains(full, "-ss") || slices.Contains(full, "-to") {
		t.Errorf("sem recorte não devia haver -ss/-to: %v", full)
	}
	if argValue(full, "-ar") != "44100" || argValue(full, "-ac") != "2" {
		t.Errorf("os modelos esperam estéreo a 44.1kHz: %v", full)
	}

	cut := ToWAVArgs("/m/song.mp3", "/w/in.wav", "01:00", "02:30")
	if argValue(cut, "-ss") != "01:00" || argValue(cut, "-to") != "02:30" {
		t.Errorf("recorte em falta: %v", cut)
	}
}

// argValue devolve o valor que se segue a uma flag, ou "" se não existir.
func argValue(args []string, flag string) string {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

func TestParseTQDM(t *testing.T) {
	tests := []struct {
		line string
		want float64
		ok   bool
	}{
		{" 45%|████▌     | 9/20 [00:10<00:12,  1.10s/it]", 0.45, true},
		{"100%|██████████| 20/20 [00:22<00:00]", 1, true},
		{"  0%|          | 0/20", 0, true},
		{"2026-10-28 - INFO - Separation complete!", 0, false},
		{"Volume a 150%", 0, false},
	}
	for _, tt := range tests {
		got, ok := ParseTQDM(tt.line)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseTQDM(%q) = (%v, %v), esperado (%v, %v)", tt.line, got, ok, tt.want, tt.ok)
		}
	}
}
