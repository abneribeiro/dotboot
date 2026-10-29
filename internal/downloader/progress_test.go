package downloader

import (
	"testing"
	"time"
)

func TestParseLineProgresso(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantFrac float64
		wantETA  time.Duration
		wantSpd  float64
	}{
		{"total conhecido", "dotboot-progress 5242880 10485760 NA 1048576.5 5", 0.5, 5 * time.Second, 1048576.5},
		// HLS/DASH: só há estimativa do total.
		{"só estimativa", "dotboot-progress 250 NA 1000.0 NA NA", 0.25, -1, 0},
		{"total desconhecido", "dotboot-progress 250 NA NA NA NA", -1, -1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := ParseLine(tt.line)
			if !ok || p.Phase != PhaseDownload {
				t.Fatalf("ParseLine(%q) = %+v, %v", tt.line, p, ok)
			}
			if got := p.Fraction(); got != tt.wantFrac {
				t.Errorf("Fraction = %v, esperado %v", got, tt.wantFrac)
			}
			if p.ETA != tt.wantETA || p.Speed != tt.wantSpd {
				t.Errorf("ETA/Speed = %v/%v, esperado %v/%v", p.ETA, p.Speed, tt.wantETA, tt.wantSpd)
			}
		})
	}
}

func TestParseLineFases(t *testing.T) {
	tests := []struct {
		line      string
		wantPhase Phase
		wantFile  string
		wantIndex int
	}{
		{"[download] Destination: /v/Vídeo [abc].f136.mp4", PhaseDownload, "/v/Vídeo [abc].f136.mp4", 0},
		{`[Merger] Merging formats into "/v/Vídeo [abc].mp4"`, PhaseMerge, "/v/Vídeo [abc].mp4", 0},
		{"[ExtractAudio] Destination: /w/música.wav", PhaseExtract, "/w/música.wav", 0},
		{"[download] /v/a.mp4 has already been downloaded", PhaseDownload, "/v/a.mp4", 0},
		{"[download] Downloading item 3 of 12", PhaseInfo, "", 3},
	}
	for _, tt := range tests {
		p, ok := ParseLine(tt.line)
		if !ok || p.Phase != tt.wantPhase || p.File != tt.wantFile || p.Index != tt.wantIndex {
			t.Errorf("ParseLine(%q) = %+v, %v", tt.line, p, ok)
		}
	}

	for _, line := range []string{"[youtube] abc: Downloading webpage", "", "dotboot-progress 1 2"} {
		if _, ok := ParseLine(line); ok {
			t.Errorf("ParseLine(%q) devia ser ignorada", line)
		}
	}
}

func TestFFmpegFraction(t *testing.T) {
	if f, ok := FFmpegFraction("out_time_us=5000000", 10); !ok || f != 0.5 {
		t.Errorf("FFmpegFraction = %v, %v", f, ok)
	}
	if f, ok := FFmpegFraction("out_time_us=99000000", 10); !ok || f != 1 {
		t.Errorf("acima da duração tem de ficar em 1: %v", f)
	}
	for _, line := range []string{"frame=10", "out_time_us=N/A", "progress=end"} {
		if _, ok := FFmpegFraction(line, 10); ok {
			t.Errorf("FFmpegFraction(%q) devia ser ignorada", line)
		}
	}
	if _, ok := FFmpegFraction("out_time_us=1", 0); ok {
		t.Error("sem duração não há fração")
	}
}
